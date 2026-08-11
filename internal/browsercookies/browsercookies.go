// Package browsercookies imports authentication cookies straight from a locally
// installed browser, so the reader can pick up a session the user already
// established by logging in normally — no API app registration, no manual
// copy-paste of an opaque token.
//
// Today it targets Firefox, whose cookie store is the plaintext SQLite database
// cookies.sqlite (unlike Chromium, Firefox does not encrypt cookie values, so no
// OS keychain is involved). The primary use is Reddit: Reddit's self-serve OAuth
// app registration is effectively closed to new personal projects, so lifting
// the logged-in reddit_session cookie is the practical path to authenticated
// reads for an individual.
//
// Robustness notes:
//   - Profile discovery parses profiles.ini and honours the [Install*] Default
//     pointer, then a [Profile*] with Default=1, then any *.default* directory —
//     mirroring how Firefox itself resolves the default profile. Multiple or
//     absent profiles are handled gracefully with typed errors.
//   - cookies.sqlite is frequently WAL-locked while Firefox runs. Rather than
//     fight the lock, the whole cookie database (the main file plus any -wal /
//     -shm sidecars) is copied to a private temp directory and the copy is read,
//     then the temp files are removed. This is immune to concurrent writes and
//     needs no exclusive access to the live file.
//   - The SQLite reader is pure Go (modernc.org/sqlite, CGO=0), so the whole
//     reader stays CGO-free and cross-compiles to every target.
package browsercookies

import (
	"bufio"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	_ "modernc.org/sqlite" // pure-Go database/sql driver, registered as "sqlite"
)

// Seams for the rare, otherwise-untriggerable failure paths, so tests can force
// them deterministically without depending on the host filesystem or OS.
var (
	mkdirTemp = os.MkdirTemp
	sqlOpen   = sql.Open
)

// Sentinel errors let callers (the UI) message the user precisely — e.g. "log
// into Reddit in Firefox first" — instead of surfacing a raw failure. Test with
// errors.Is.
var (
	// ErrNoFirefox reports that no Firefox profile directory could be located
	// (Firefox is not installed, or has never been run).
	ErrNoFirefox = errors.New("no Firefox profile found")
	// ErrNoProfile reports that a Firefox install exists but no usable profile
	// (or its cookies database) could be resolved.
	ErrNoProfile = errors.New("no usable Firefox profile")
	// ErrNoCookie reports that the profile was read but the requested cookie was
	// absent — for Reddit, the user is not logged in.
	ErrNoCookie = errors.New("cookie not found in Firefox profile")
)

// RedditSession is the imported reddit_session cookie.
type RedditSession struct {
	Value string // the bare cookie value (what goreddit's WithSessionCookie wants)
	Host  string // the host the cookie was scoped to (e.g. ".reddit.com")
}

// Finder locates and reads Firefox cookies. Its fields are seams so tests can
// point discovery at a fixture HOME and a chosen GOOS without touching the real
// environment. The zero value is not usable; construct with [New].
type Finder struct {
	goos     string
	getenv   func(string) string
	userHome func() (string, error)
}

// New returns a Finder bound to the real runtime OS and environment.
func New() *Finder {
	return &Finder{goos: runtime.GOOS, getenv: os.Getenv, userHome: os.UserHomeDir}
}

// profileRoot returns the per-OS Firefox root directory (the parent of
// profiles.ini and the Profiles/ folder).
func (f *Finder) profileRoot() (string, error) {
	switch f.goos {
	case "darwin":
		home, err := f.userHome()
		if err != nil {
			return "", fmt.Errorf("%w: %v", ErrNoFirefox, err)
		}
		return filepath.Join(home, "Library", "Application Support", "Firefox"), nil
	case "windows":
		appData := f.getenv("APPDATA")
		if appData == "" {
			return "", fmt.Errorf("%w: APPDATA is unset", ErrNoFirefox)
		}
		return filepath.Join(appData, "Mozilla", "Firefox"), nil
	default: // linux and other unixes
		home, err := f.userHome()
		if err != nil {
			return "", fmt.Errorf("%w: %v", ErrNoFirefox, err)
		}
		return filepath.Join(home, ".mozilla", "firefox"), nil
	}
}

// iniSection is one [Name] block of a profiles.ini file with its key/value pairs.
type iniSection struct {
	name string
	keys map[string]string
}

// parseProfilesINI parses the minimal INI dialect Firefox writes: [Section]
// headers and Key=Value lines, ignoring blanks and ';'/'#' comments. Later keys
// in a section overwrite earlier ones.
func parseProfilesINI(r io.Reader) []iniSection {
	var out []iniSection
	var cur *iniSection
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			out = append(out, iniSection{name: line[1 : len(line)-1], keys: map[string]string{}})
			cur = &out[len(out)-1]
			continue
		}
		if cur == nil {
			continue // a key before any section header: skip it
		}
		if eq := strings.IndexByte(line, '='); eq >= 0 {
			key := strings.TrimSpace(line[:eq])
			val := strings.TrimSpace(line[eq+1:])
			cur.keys[key] = val
		}
	}
	return out
}

// ProfileDir resolves the default Firefox profile directory. It reads
// profiles.ini under the per-OS root and applies Firefox's own precedence: the
// [Install*] section's Default pointer, then a [Profile*] marked Default=1, then
// any profile whose path looks like a "*.default*" directory.
func (f *Finder) ProfileDir() (string, error) {
	root, err := f.profileRoot()
	if err != nil {
		return "", err
	}
	iniPath := filepath.Join(root, "profiles.ini")
	file, err := os.Open(iniPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("%w: %s missing", ErrNoFirefox, iniPath)
		}
		return "", fmt.Errorf("%w: %v", ErrNoProfile, err)
	}
	defer file.Close()
	sections := parseProfilesINI(file)

	rel := resolveProfilePath(sections)
	if rel == "" {
		return "", fmt.Errorf("%w: no default profile in %s", ErrNoProfile, iniPath)
	}
	if filepath.IsAbs(rel) {
		return rel, nil
	}
	return filepath.Join(root, filepath.FromSlash(rel)), nil
}

// resolveProfilePath picks the default profile's Path (as written in
// profiles.ini, forward-slashed and possibly relative) from parsed sections,
// applying Firefox's precedence. It returns "" when nothing usable is present.
func resolveProfilePath(sections []iniSection) string {
	// 1. An [Install*] section names the active default profile directly.
	for _, s := range sections {
		if strings.HasPrefix(s.name, "Install") {
			if d := s.keys["Default"]; d != "" {
				return d
			}
		}
	}
	// 2. A [Profile*] explicitly flagged Default=1.
	for _, s := range sections {
		if strings.HasPrefix(s.name, "Profile") && s.keys["Default"] == "1" {
			if p := s.keys["Path"]; p != "" {
				return p
			}
		}
	}
	// 3. Fallback: any profile whose path looks like the stock "*.default*" dir.
	for _, s := range sections {
		if !strings.HasPrefix(s.name, "Profile") {
			continue
		}
		if p := s.keys["Path"]; strings.Contains(p, ".default") {
			return p
		}
	}
	return ""
}

// RedditSession imports the logged-in reddit_session cookie from the default
// Firefox profile. It returns [ErrNoFirefox] when Firefox/profile is absent,
// [ErrNoProfile] when the cookies database cannot be located, and [ErrNoCookie]
// when the user is not logged into Reddit.
func (f *Finder) RedditSession() (RedditSession, error) {
	dbPath, err := f.cookieDBPath()
	if err != nil {
		return RedditSession{}, err
	}
	return readRedditSession(dbPath)
}

// cookieDBPath resolves the default Firefox profile's cookies.sqlite, returning
// [ErrNoFirefox]/[ErrNoProfile] when the profile or database cannot be located.
func (f *Finder) cookieDBPath() (string, error) {
	dir, err := f.ProfileDir()
	if err != nil {
		return "", err
	}
	dbPath := filepath.Join(dir, "cookies.sqlite")
	if _, err := os.Stat(dbPath); err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("%w: %s missing", ErrNoProfile, dbPath)
		}
		return "", fmt.Errorf("%w: %v", ErrNoProfile, err)
	}
	return dbPath, nil
}

// readRedditSession opens a private copy of the cookie database and queries it for
// the reddit_session cookie.
func readRedditSession(dbPath string) (RedditSession, error) {
	db, cleanup, err := openCookieDB(dbPath)
	if err != nil {
		return RedditSession{}, err
	}
	defer cleanup()

	// Prefer the most specific host (longest name) so a scoped cookie wins over a
	// stale broader one, and require a non-empty value.
	const q = `SELECT value, host FROM moz_cookies
		WHERE name = 'reddit_session' AND host LIKE '%reddit.com%' AND value <> ''
		ORDER BY LENGTH(host) DESC LIMIT 1`
	var rs RedditSession
	switch err := db.QueryRow(q).Scan(&rs.Value, &rs.Host); {
	case err == sql.ErrNoRows:
		return RedditSession{}, fmt.Errorf("%w: reddit_session (log into Reddit in Firefox first)", ErrNoCookie)
	case err != nil:
		return RedditSession{}, err
	}
	return rs, nil
}

// openCookieDB copies the cookie database (and any WAL/shm sidecars) to a private
// temp directory — dodging Firefox's live WAL lock — and opens the copy. The
// returned cleanup closes the handle and removes the temp files; always call it.
func openCookieDB(dbPath string) (*sql.DB, func(), error) {
	tmpDir, err := mkdirTemp("", "rd-ffcookies-")
	if err != nil {
		return nil, nil, err
	}
	removeTmp := func() { _ = os.RemoveAll(tmpDir) }

	// Copy the main database plus any WAL/shm sidecars so the copy is consistent
	// even while Firefox holds the live file open in WAL mode.
	tmpDB := filepath.Join(tmpDir, "cookies.sqlite")
	if err := copyFile(dbPath, tmpDB); err != nil {
		removeTmp()
		return nil, nil, err
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		src := dbPath + suffix
		if _, statErr := os.Stat(src); statErr == nil {
			if err := copyFile(src, tmpDB+suffix); err != nil {
				removeTmp()
				return nil, nil, err
			}
		}
	}

	db, err := sqlOpen("sqlite", "file:"+tmpDB+"?_pragma=busy_timeout(5000)")
	if err != nil {
		removeTmp()
		return nil, nil, err
	}
	return db, func() { _ = db.Close(); removeTmp() }, nil
}

// sessionSpec describes how to import one platform's session: which cookies to
// collect (in output order), which is mandatory, the host-match LIKE patterns,
// and the hint shown when the mandatory cookie is missing.
type sessionSpec struct {
	hostPatterns []string // SQL LIKE patterns, OR-combined
	primary      string   // the mandatory cookie name
	names        []string // cookies to collect, in output order
	signInHint   string   // guidance when the primary cookie is absent
}

// InstagramSession imports the logged-in Instagram session (sessionid, and
// csrftoken when present) as a "sessionid=…; csrftoken=…" cookie string ready for
// the Instagram provider. sessionid is mandatory; without it the user is not
// logged in ([ErrNoCookie]).
func (f *Finder) InstagramSession() (string, error) {
	return f.importSession(sessionSpec{
		hostPatterns: []string{"%instagram.com"},
		primary:      "sessionid",
		names:        []string{"sessionid", "csrftoken"},
		signInHint:   "log into Instagram in Firefox first",
	})
}

// TikTokSession imports the logged-in TikTok session (sessionid, and msToken when
// present) as a cookie string ready for the TikTok provider. sessionid is
// mandatory.
func (f *Finder) TikTokSession() (string, error) {
	return f.importSession(sessionSpec{
		hostPatterns: []string{"%tiktok.com"},
		primary:      "sessionid",
		names:        []string{"sessionid", "msToken"},
		signInHint:   "log into TikTok in Firefox first",
	})
}

// TwitterSession imports the logged-in X/Twitter session (auth_token + ct0) as an
// "auth_token=…; ct0=…" cookie string ready for the Twitter provider's home feed.
// auth_token is mandatory. Host patterns are anchored to the x.com / twitter.com
// domains so an unrelated "…x.com" host cannot match.
func (f *Finder) TwitterSession() (string, error) {
	return f.importSession(sessionSpec{
		hostPatterns: []string{"x.com", "%.x.com", "twitter.com", "%.twitter.com"},
		primary:      "auth_token",
		names:        []string{"auth_token", "ct0"},
		signInHint:   "log into X in Firefox first",
	})
}

// importSession locates the cookies database and reads the session per spec.
func (f *Finder) importSession(spec sessionSpec) (string, error) {
	dbPath, err := f.cookieDBPath()
	if err != nil {
		return "", err
	}
	return readSession(dbPath, spec)
}

// readSession opens a private copy of the cookie database and assembles the
// spec's cookies into a "name=value; …" string. It returns [ErrNoCookie] when the
// mandatory cookie is absent; optional cookies that are missing are simply
// omitted.
func readSession(dbPath string, spec sessionSpec) (string, error) {
	db, cleanup, err := openCookieDB(dbPath)
	if err != nil {
		return "", err
	}
	defer cleanup()

	var parts []string
	havePrimary := false
	for _, name := range spec.names {
		v, err := lookupCookie(db, name, spec.hostPatterns)
		if err != nil {
			return "", err
		}
		if v == "" {
			continue
		}
		parts = append(parts, name+"="+v)
		if name == spec.primary {
			havePrimary = true
		}
	}
	if !havePrimary {
		return "", fmt.Errorf("%w: %s (%s)", ErrNoCookie, spec.primary, spec.signInHint)
	}
	return strings.Join(parts, "; "), nil
}

// lookupCookie returns the most-specific-host value of the named cookie whose host
// matches any of the LIKE patterns, or "" when it is absent.
func lookupCookie(db *sql.DB, name string, hostPatterns []string) (string, error) {
	likes := make([]string, len(hostPatterns))
	args := []any{name}
	for i, p := range hostPatterns {
		likes[i] = "host LIKE ?"
		args = append(args, p)
	}
	q := `SELECT value FROM moz_cookies WHERE name = ? AND value <> '' AND (` +
		strings.Join(likes, " OR ") + `) ORDER BY LENGTH(host) DESC LIMIT 1`
	var v string
	switch err := db.QueryRow(q, args...).Scan(&v); {
	case err == sql.ErrNoRows:
		return "", nil
	case err != nil:
		return "", err
	}
	return v, nil
}

// copyFile copies src to dst byte-for-byte. A cookie database is small (tens of
// KB), so reading it whole is simpler and just as fast as streaming.
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o600)
}
