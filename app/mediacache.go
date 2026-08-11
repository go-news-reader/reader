package app

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	neturl "net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/go-news-reader/reader/internal/settings"
)

// mediaCacheBudget bounds the on-disk media cache. The cache now holds the
// ORIGINAL downloaded media (not a re-encoded thumbnail) so files stay real,
// correctly-typed media that Finder can preview — a .gif still animates, a .png
// keeps its alpha. Originals are larger than the old 512px JPEGs, which is
// exactly why the budget is user-configurable; see [settings.Settings.MediaCacheBytes].
// When a write pushes the directory past the budget the oldest entries are
// pruned, so the cache is self-limiting rather than growing without bound. It is
// a package var (not a const) so the app can apply the configurable size setting
// live.
var mediaCacheBudget int64 = int64(settings.DefaultMediaCacheMB) << 20

// mediaCacheDir returns the on-disk directory for cached remote-media bytes. A
// package var so tests can point it at a temp dir (mirrors groupCacheDir).
var mediaCacheDir = defaultMediaCacheDir

// statFile stats a cache entry during a prune; a package var so a test can force
// the "entry vanished mid-prune" skip path.
var statFile = os.Stat

// defaultMediaCacheDir resolves the media cache under the OS per-user cache dir.
func defaultMediaCacheDir() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, groupCacheAppDir, "media"), nil
}

// mediaCacheHash maps a media URL to the hex SHA-256 of the URL, the unique,
// fixed-length part of a cache filename (so two URLs that share a basename never
// collide).
func mediaCacheHash(url string) string {
	sum := sha256.Sum256([]byte(url))
	return hex.EncodeToString(sum[:])
}

// urlBasename returns the last path segment of url (empty when url does not
// parse or carries no path), used to build a readable cache-filename label.
func urlBasename(url string) string {
	u, err := neturl.Parse(url)
	if err != nil {
		return ""
	}
	return path.Base(u.Path)
}

// mediaCacheLabel is the human-readable, Finder-friendly prefix of a cache
// filename: the URL's basename with its own extension stripped, lowercased and
// sanitised to [A-Za-z0-9._-] (dropping any other rune, which also keeps the
// glob metacharacters *, ? and [ out so mediaCacheGet's Glob pattern stays
// literal), truncated to a sane length. Falls back to "media" when nothing
// usable remains (e.g. a URL with no path segment).
func mediaCacheLabel(url string) string {
	base := urlBasename(url)
	base = strings.TrimSuffix(base, path.Ext(base))
	var b strings.Builder
	for _, r := range strings.ToLower(base) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		}
	}
	label := b.String()
	if len(label) > 48 {
		label = label[:48]
	}
	if label == "" {
		return "media"
	}
	return label
}

// mediaCacheGlobPrefix is the deterministic, extension-less stem of a cache
// filename for url: "<label>-<hash>". Both mediaCachePut (which appends the
// content-derived extension) and mediaCacheGet (which globs "<stem>.*") derive
// it from the URL alone, so lookups find whatever extension the bytes were
// stored under.
func mediaCacheGlobPrefix(url string) string {
	return mediaCacheLabel(url) + "-" + mediaCacheHash(url)
}

// mediaExtForContentType maps a sniffed MIME type to the file extension the
// cache should store it under, reporting whether the type is a known media type.
// The video cases (mp4/webm) are kept ready even though the feed-thumbnail path
// only downloads images today.
func mediaExtForContentType(ct string) (string, bool) {
	switch ct {
	case "image/jpeg":
		return ".jpg", true
	case "image/png":
		return ".png", true
	case "image/gif":
		return ".gif", true
	case "image/webp":
		return ".webp", true
	case "image/bmp":
		return ".bmp", true
	case "video/mp4":
		return ".mp4", true
	case "video/webm":
		return ".webm", true
	default:
		return "", false
	}
}

// knownURLMediaExt returns the URL basename's own extension when it is a known
// media extension (normalising .jpeg to .jpg), else "". It is the fallback when
// content sniffing is inconclusive.
func knownURLMediaExt(url string) string {
	switch ext := strings.ToLower(path.Ext(urlBasename(url))); ext {
	case ".jpg", ".jpeg":
		return ".jpg"
	case ".png", ".gif", ".webp", ".bmp", ".mp4", ".webm":
		return ext
	default:
		return ""
	}
}

// mediaExt picks the on-disk extension for data cached from url: the content
// type sniffed from the bytes wins; when that is inconclusive
// (application/octet-stream) the URL's own known media extension is used; else
// ".bin". Content-typing the file (rather than forcing one extension) is what
// lets Finder show real, correctly-typed media.
func mediaExt(url string, data []byte) string {
	ct := http.DetectContentType(data)
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	if ext, ok := mediaExtForContentType(ct); ok {
		return ext
	}
	if ct == "application/octet-stream" {
		if ext := knownURLMediaExt(url); ext != "" {
			return ext
		}
	}
	return ".bin"
}

// mediaCacheGet returns the cached original bytes for url and whether a usable
// entry was found. The stored extension is content-derived and so unknown from
// the URL alone: it globs "<stem>.*" and reads the single match. A missing file,
// an unresolvable cache dir, or an empty entry is a miss. The directory scan
// runs on the per-thumbnail worker goroutines, off the render path.
func mediaCacheGet(url string) ([]byte, bool) {
	dir, err := mediaCacheDir()
	if err != nil {
		return nil, false
	}
	matches, _ := filepath.Glob(filepath.Join(dir, mediaCacheGlobPrefix(url)+".*"))
	if len(matches) == 0 {
		return nil, false
	}
	data, err := os.ReadFile(matches[0])
	if err != nil || len(data) == 0 {
		return nil, false
	}
	return data, true
}

// mediaCachePut stores the ORIGINAL data under url's stem plus a content-derived
// extension (so the file is previewable in Finder), creating the cache dir as
// needed, then prunes the directory back under the budget. It is best-effort:
// any error (no cache dir, write failure) is silently ignored, since the cache
// is only an optimisation and the caller already has the bytes. Empty data is
// not stored.
func mediaCachePut(url string, data []byte) {
	if len(data) == 0 {
		return
	}
	dir, err := mediaCacheDir()
	if err != nil {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	name := mediaCacheGlobPrefix(url) + mediaExt(url, data)
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		return
	}
	pruneMediaCache(dir, mediaCacheBudget)
}

// pruneMediaCache deletes the oldest entries (by modification time) until the
// directory's total size is at or below budget. It is best-effort: a stat/remove
// error just leaves that entry in place.
func pruneMediaCache(dir string, budget int64) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type ent struct {
		path  string
		size  int64
		mtime int64
	}
	var files []ent
	var total int64
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		path := filepath.Join(dir, e.Name())
		info, err := statFile(path)
		if err != nil {
			continue // vanished between ReadDir and Stat; skip it
		}
		files = append(files, ent{path: path, size: info.Size(), mtime: info.ModTime().UnixNano()})
		total += info.Size()
	}
	if total <= budget {
		return
	}
	// Oldest first, so the least-recently-written entries are evicted.
	sort.Slice(files, func(i, j int) bool { return files[i].mtime < files[j].mtime })
	for _, f := range files {
		if total <= budget {
			break
		}
		if os.Remove(f.path) == nil {
			total -= f.size
		}
	}
}
