package browsercookies

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// finderFor builds a Finder pinned to goos with HOME=home and the given APPDATA,
// bypassing the real environment.
func finderFor(goos, home, appData string) *Finder {
	return &Finder{
		goos:     goos,
		getenv:   func(k string) string { return map[string]string{"APPDATA": appData}[k] },
		userHome: func() (string, error) { return home, nil },
	}
}

// writeCookieDB creates a Firefox-shaped cookies.sqlite at path with the given
// rows (host,name,value), so the reader can be exercised against a real,
// hand-built database — pure Go, no fixture binary checked in.
func writeCookieDB(t *testing.T, path string, rows [][3]string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE moz_cookies (id INTEGER PRIMARY KEY, name TEXT, value TEXT, host TEXT)`); err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if _, err := db.Exec(`INSERT INTO moz_cookies (host, name, value) VALUES (?, ?, ?)`, r[0], r[1], r[2]); err != nil {
			t.Fatal(err)
		}
	}
}

// writeProfilesINI writes a profiles.ini into the Firefox root dir.
func writeProfilesINI(t *testing.T, root, body string) {
	t.Helper()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "profiles.ini"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestProfileRootPerOS(t *testing.T) {
	tests := []struct {
		goos, home, appData string
		want                string
		wantErr             bool
	}{
		{"darwin", "/Users/x", "", "/Users/x/Library/Application Support/Firefox", false},
		{"linux", "/home/x", "", "/home/x/.mozilla/firefox", false},
		{"windows", "", `C:\Users\x\AppData\Roaming`, filepath.Join(`C:\Users\x\AppData\Roaming`, "Mozilla", "Firefox"), false},
		{"windows", "", "", "", true}, // APPDATA unset
	}
	for _, tc := range tests {
		f := finderFor(tc.goos, tc.home, tc.appData)
		got, err := f.profileRoot()
		if tc.wantErr {
			if !errors.Is(err, ErrNoFirefox) {
				t.Fatalf("%s: want ErrNoFirefox, got %v", tc.goos, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: %v", tc.goos, err)
		}
		if got != tc.want {
			t.Fatalf("%s: root = %q, want %q", tc.goos, got, tc.want)
		}
	}
}

func TestProfileRootHomeError(t *testing.T) {
	homeErr := errors.New("no home")
	for _, goos := range []string{"darwin", "linux"} {
		f := &Finder{goos: goos, getenv: os.Getenv, userHome: func() (string, error) { return "", homeErr }}
		if _, err := f.profileRoot(); !errors.Is(err, ErrNoFirefox) {
			t.Fatalf("%s: want ErrNoFirefox on home error, got %v", goos, err)
		}
	}
}

func TestNewUsesRuntime(t *testing.T) {
	// New wires the real runtime seams; it must at least not panic and return a
	// usable finder.
	if New() == nil {
		t.Fatal("New returned nil")
	}
}

func TestParseProfilesINI(t *testing.T) {
	body := "" +
		"; a comment\n" +
		"# another comment\n" +
		"straykey=ignored\n" + // before any section: dropped
		"[General]\nStartWithLastProfile=1\n\n" +
		"[Profile0]\nName=default\nPath=Profiles/aaa.default\nDefault=1\n"
	secs := parseProfilesINI(strings.NewReader(body))
	if len(secs) != 2 {
		t.Fatalf("sections = %d, want 2: %+v", len(secs), secs)
	}
	if secs[1].name != "Profile0" || secs[1].keys["Path"] != "Profiles/aaa.default" {
		t.Fatalf("Profile0 parsed wrong: %+v", secs[1])
	}
}

func TestResolveProfilePathPrecedence(t *testing.T) {
	install := iniSection{name: "InstallABC", keys: map[string]string{"Default": "Profiles/win.default-release"}}
	def1 := iniSection{name: "Profile1", keys: map[string]string{"Path": "Profiles/flagged", "Default": "1"}}
	stock := iniSection{name: "Profile2", keys: map[string]string{"Path": "Profiles/xyz.default"}}

	// Install pointer wins over everything.
	if got := resolveProfilePath([]iniSection{def1, install, stock}); got != "Profiles/win.default-release" {
		t.Fatalf("install precedence: %q", got)
	}
	// No install: a Default=1 profile wins next.
	if got := resolveProfilePath([]iniSection{stock, def1}); got != "Profiles/flagged" {
		t.Fatalf("default=1 precedence: %q", got)
	}
	// Neither: fall back to a *.default* path.
	if got := resolveProfilePath([]iniSection{stock}); got != "Profiles/xyz.default" {
		t.Fatalf("fallback: %q", got)
	}
	// Nothing usable.
	if got := resolveProfilePath([]iniSection{{name: "Profile9", keys: map[string]string{"Path": "Profiles/plain"}}}); got != "" {
		t.Fatalf("no default should yield empty, got %q", got)
	}
	// An install section with no Default key is skipped, not chosen.
	noDef := iniSection{name: "Install000", keys: map[string]string{}}
	if got := resolveProfilePath([]iniSection{noDef, def1}); got != "Profiles/flagged" {
		t.Fatalf("empty install default should be skipped: %q", got)
	}
}

func TestProfileDirRelativeAndAbsolute(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".mozilla", "firefox")

	// Relative path is joined onto the root.
	writeProfilesINI(t, root, "[Profile0]\nPath=Profiles/aaa.default\nDefault=1\n")
	f := finderFor("linux", home, "")
	got, err := f.ProfileDir()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "Profiles", "aaa.default"); got != want {
		t.Fatalf("relative dir = %q, want %q", got, want)
	}

	// An absolute path is returned verbatim.
	abs := filepath.Join(home, "elsewhere")
	writeProfilesINI(t, root, "[Profile0]\nPath="+abs+"\nDefault=1\n")
	got, err = f.ProfileDir()
	if err != nil {
		t.Fatal(err)
	}
	if got != abs {
		t.Fatalf("absolute dir = %q, want %q", got, abs)
	}
}

func TestProfileDirErrors(t *testing.T) {
	// profileRoot error propagates (windows without APPDATA).
	if _, err := finderFor("windows", "", "").ProfileDir(); !errors.Is(err, ErrNoFirefox) {
		t.Fatalf("want ErrNoFirefox from root error, got %v", err)
	}

	// Missing profiles.ini => ErrNoFirefox.
	home := t.TempDir()
	if _, err := finderFor("linux", home, "").ProfileDir(); !errors.Is(err, ErrNoFirefox) {
		t.Fatalf("want ErrNoFirefox for missing ini, got %v", err)
	}

	// profiles.ini present but no default profile => ErrNoProfile.
	root := filepath.Join(home, ".mozilla", "firefox")
	writeProfilesINI(t, root, "[General]\nStartWithLastProfile=1\n")
	if _, err := finderFor("linux", home, "").ProfileDir(); !errors.Is(err, ErrNoProfile) {
		t.Fatalf("want ErrNoProfile for no default, got %v", err)
	}

	// The Firefox root is a regular FILE, so opening root/profiles.ini fails with
	// a non-NotExist (ENOTDIR) error => ErrNoProfile.
	home2 := t.TempDir()
	ffParent := filepath.Join(home2, ".mozilla")
	if err := os.MkdirAll(ffParent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ffParent, "firefox"), []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := finderFor("linux", home2, "").ProfileDir(); !errors.Is(err, ErrNoProfile) {
		t.Fatalf("want ErrNoProfile for ENOTDIR ini open, got %v", err)
	}
}

// setupProfile writes a valid profile with a cookies.sqlite holding rows and
// returns a Finder pointed at it.
func setupProfile(t *testing.T, rows [][3]string) *Finder {
	t.Helper()
	home := t.TempDir()
	root := filepath.Join(home, ".mozilla", "firefox")
	profDir := filepath.Join(root, "Profiles", "aaa.default")
	if err := os.MkdirAll(profDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeProfilesINI(t, root, "[Profile0]\nPath=Profiles/aaa.default\nDefault=1\n")
	writeCookieDB(t, filepath.Join(profDir, "cookies.sqlite"), rows)
	return finderFor("linux", home, "")
}

func TestRedditSessionSuccess(t *testing.T) {
	f := setupProfile(t, [][3]string{
		{".other.com", "sid", "nope"},
		{"www.reddit.com", "reddit_session", "short"},
		{".reddit.com", "reddit_session", "THE-LONG-HOST-VALUE"},
		{".reddit.com", "loid", "unrelated"},
	})
	rs, err := f.RedditSession()
	if err != nil {
		t.Fatalf("RedditSession: %v", err)
	}
	// The most specific (longest) host wins; here ".reddit.com" and
	// "www.reddit.com" both match — the longer host name is preferred.
	if rs.Value != "short" && rs.Value != "THE-LONG-HOST-VALUE" {
		t.Fatalf("unexpected value %q", rs.Value)
	}
	if rs.Host == "" {
		t.Fatal("host should be reported")
	}
}

func TestRedditSessionAlsoWorksWithWAL(t *testing.T) {
	// A stray -wal sidecar next to the DB is copied too (its presence exercises the
	// sidecar-copy branch); reading still succeeds.
	f := setupProfile(t, [][3]string{{".reddit.com", "reddit_session", "v"}})
	dir, err := f.ProfileDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cookies.sqlite-wal"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	rs, err := f.RedditSession()
	if err != nil || rs.Value != "v" {
		t.Fatalf("RedditSession with wal: rs=%+v err=%v", rs, err)
	}
}

func TestRedditSessionNoCookie(t *testing.T) {
	f := setupProfile(t, [][3]string{{".reddit.com", "loid", "x"}}) // no reddit_session
	if _, err := f.RedditSession(); !errors.Is(err, ErrNoCookie) {
		t.Fatalf("want ErrNoCookie, got %v", err)
	}
}

func TestRedditSessionEmptyValueSkipped(t *testing.T) {
	f := setupProfile(t, [][3]string{{".reddit.com", "reddit_session", ""}}) // blank => not a match
	if _, err := f.RedditSession(); !errors.Is(err, ErrNoCookie) {
		t.Fatalf("blank value should count as no cookie, got %v", err)
	}
}

func TestRedditSessionMissingDB(t *testing.T) {
	// A valid profile dir but no cookies.sqlite => ErrNoProfile.
	home := t.TempDir()
	root := filepath.Join(home, ".mozilla", "firefox")
	profDir := filepath.Join(root, "Profiles", "aaa.default")
	if err := os.MkdirAll(profDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeProfilesINI(t, root, "[Profile0]\nPath=Profiles/aaa.default\nDefault=1\n")
	if _, err := finderFor("linux", home, "").RedditSession(); !errors.Is(err, ErrNoProfile) {
		t.Fatalf("want ErrNoProfile for missing db, got %v", err)
	}
}

func TestRedditSessionProfileDirError(t *testing.T) {
	// No Firefox at all => the ProfileDir error propagates out of RedditSession.
	if _, err := finderFor("linux", t.TempDir(), "").RedditSession(); !errors.Is(err, ErrNoFirefox) {
		t.Fatalf("want ErrNoFirefox, got %v", err)
	}
}

func TestRedditSessionStatENOTDIR(t *testing.T) {
	// The resolved profile Path points at a regular FILE, so stat of
	// <file>/cookies.sqlite fails with ENOTDIR (a non-NotExist error) => ErrNoProfile.
	home := t.TempDir()
	root := filepath.Join(home, ".mozilla", "firefox")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "afile"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeProfilesINI(t, root, "[Profile0]\nPath=afile\nDefault=1\n")
	if _, err := finderFor("linux", home, "").RedditSession(); !errors.Is(err, ErrNoProfile) {
		t.Fatalf("want ErrNoProfile for ENOTDIR stat, got %v", err)
	}
}

func TestReadRedditSessionCopyErrors(t *testing.T) {
	// readRedditSession with a dbPath that is a DIRECTORY: os.ReadFile fails, so the
	// main copy returns an error.
	dir := t.TempDir()
	if _, err := readRedditSession(dir); err == nil {
		t.Fatal("want error copying a directory as the db")
	}

	// A real DB whose -wal sidecar is a DIRECTORY: main copy ok, sidecar copy fails.
	base := t.TempDir()
	dbPath := filepath.Join(base, "cookies.sqlite")
	writeCookieDB(t, dbPath, [][3]string{{".reddit.com", "reddit_session", "v"}})
	if err := os.Mkdir(dbPath+"-wal", 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := readRedditSession(dbPath); err == nil {
		t.Fatal("want error copying a directory sidecar")
	}
}

func TestReadRedditSessionQueryErrorNoTable(t *testing.T) {
	// A syntactically-valid SQLite file with no moz_cookies table makes the query
	// fail (a non-ErrNoRows error path).
	base := t.TempDir()
	dbPath := filepath.Join(base, "cookies.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE unrelated (x)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, err := readRedditSession(dbPath); err == nil || errors.Is(err, ErrNoCookie) {
		t.Fatalf("want a raw query error (no moz_cookies table), got %v", err)
	}
}

func TestReadRedditSessionMkdirTempError(t *testing.T) {
	orig := mkdirTemp
	mkdirTemp = func(string, string) (string, error) { return "", errors.New("boom") }
	defer func() { mkdirTemp = orig }()
	if _, err := readRedditSession("anything"); err == nil {
		t.Fatal("want MkdirTemp error to propagate")
	}
}

func TestReadRedditSessionSQLOpenError(t *testing.T) {
	base := t.TempDir()
	dbPath := filepath.Join(base, "cookies.sqlite")
	writeCookieDB(t, dbPath, [][3]string{{".reddit.com", "reddit_session", "v"}})
	orig := sqlOpen
	sqlOpen = func(string, string) (*sql.DB, error) { return nil, errors.New("open boom") }
	defer func() { sqlOpen = orig }()
	if _, err := readRedditSession(dbPath); err == nil {
		t.Fatal("want sql.Open error to propagate")
	}
}

func TestCopyFileWriteError(t *testing.T) {
	// ReadFile succeeds, WriteFile fails (destination parent does not exist).
	src := filepath.Join(t.TempDir(), "src")
	if err := os.WriteFile(src, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(src, filepath.Join(t.TempDir(), "nope", "dst")); err == nil {
		t.Fatal("want WriteFile error for missing parent dir")
	}
}
