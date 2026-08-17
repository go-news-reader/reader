// Package mediacache is the reader's pluggable media-cache backend: a small
// key/value store keyed by a media URL, holding the ORIGINAL downloaded bytes so
// media already seen is never re-fetched (across restarts too) and the cached
// file stays real, correctly-typed media a user can browse in Finder.
//
// The store is an interface, [Cache], so the backend is swappable:
//
//   - [DiskCache] is the built-in, default backend: it keeps originals under the
//     per-user OS cache directory with content-typed, Finder-friendly filenames
//     and prunes the directory back under a configurable byte budget.
//   - A shared/network backend can be provided out-of-process as a gRPC plugin
//     (see package mediacacheplugin), which adapts a remote cache to this same
//     [Cache] interface — so the reader code path is identical either way.
//
// The package is deliberately free of any app imports so both the reader and a
// third-party plugin author can depend on it: the budget is passed in as a
// number of bytes rather than read from the settings package.
package mediacache

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
	"time"
)

// Cache is a media-bytes store keyed by URL. Get reports a hit with the stored
// bytes; Put stores the original bytes for a URL. Both are best-effort: a Put
// that cannot store, or a Get with no usable entry, is a silent miss, because the
// cache is only an optimisation and the caller already holds the bytes on a Put.
type Cache interface {
	// Get returns the cached original bytes for url and whether a usable entry
	// was found.
	Get(url string) ([]byte, bool)
	// Put stores the original data for url. Empty data is not stored.
	Put(url string, data []byte)
}

// TimedCache is an optional extension of [Cache] for backends that can stamp a
// stored entry with a specific modification time — the creation time of the POST
// the media was downloaded for — so the on-disk cache reflects post chronology
// (for coherent sorting and budget-driven eviction) rather than download order.
// A backend that cannot honour it (for instance an out-of-process plugin whose
// storage has no per-entry mtime) simply implements [Cache]; callers type-assert
// for TimedCache and fall back to Put.
type TimedCache interface {
	Cache
	// PutTimed stores the original data for url and, when unixSec > 0, sets the
	// stored entry's modification time to that Unix second (UTC). A unixSec of 0
	// (an item with no known creation time) leaves the default write-time mtime.
	// Empty data is not stored.
	PutTimed(url string, data []byte, unixSec int64)
}

// appCacheSubdir is the per-user cache subdirectory for the reader; mediaSubdir
// is the media store within it. Together they resolve the built-in DiskCache's
// default directory under the OS per-user cache dir.
const (
	appCacheSubdir = "go-news-reader"
	mediaSubdir    = "media"
)

// DiskCache is the built-in [Cache]: it stores each URL's ORIGINAL bytes on disk
// under a content-typed, Finder-friendly filename and prunes the directory back
// under Budget on every write, so the cache is self-limiting rather than growing
// without bound.
type DiskCache struct {
	// Dir resolves the on-disk directory the cache lives in. It is a field (not a
	// fixed path) so the host can point it at the per-user cache dir in production
	// and a test can point it at a throwaway dir. Defaults to [DefaultDir].
	Dir func() (string, error)

	// Budget bounds the directory's total size, in bytes. When a write pushes the
	// directory past it the oldest entries are pruned. It is a field so the host
	// can apply a changed size setting live.
	Budget int64

	// stat stats a cache entry during a prune; a seam so a test can force the
	// "entry vanished mid-prune" skip path. Defaults to os.Stat.
	stat func(string) (os.FileInfo, error)
}

// NewDiskCache builds a DiskCache bounded to budget bytes, resolving its
// directory through [DefaultDir]. The caller may override Dir afterwards (the
// reader points it at a test-overridable resolver).
func NewDiskCache(budget int64) *DiskCache {
	return &DiskCache{Dir: DefaultDir, Budget: budget, stat: os.Stat}
}

// DefaultDir resolves the media cache under the OS per-user cache directory
// (…/go-news-reader/media). It errors only when no per-user cache home is
// resolvable (no HOME and no XDG cache).
func DefaultDir() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, appCacheSubdir, mediaSubdir), nil
}

// statFn returns the DiskCache's stat seam, defaulting to os.Stat when a
// zero-value DiskCache is used directly (rather than via NewDiskCache).
func (c *DiskCache) statFn() func(string) (os.FileInfo, error) {
	if c.stat != nil {
		return c.stat
	}
	return os.Stat
}

// Get returns the cached original bytes for url and whether a usable entry was
// found. The stored extension is content-derived and so unknown from the URL
// alone: it globs "<stem>.*" and reads the single match. A missing file, an
// unresolvable cache dir, or an empty entry is a miss.
func (c *DiskCache) Get(url string) ([]byte, bool) {
	dir, err := c.Dir()
	if err != nil {
		return nil, false
	}
	matches, _ := filepath.Glob(filepath.Join(dir, GlobPrefix(url)+".*"))
	if len(matches) == 0 {
		return nil, false
	}
	data, err := os.ReadFile(matches[0])
	if err != nil || len(data) == 0 {
		return nil, false
	}
	return data, true
}

// Put stores the ORIGINAL data under url's stem plus a content-derived extension
// (so the file is previewable in Finder), creating the cache dir as needed, then
// prunes the directory back under Budget. It is best-effort: any error (no cache
// dir, write failure) is silently ignored. Empty data is not stored. It is
// [PutTimed] with no post time, so the entry keeps its write-time mtime.
func (c *DiskCache) Put(url string, data []byte) {
	c.PutTimed(url, data, 0)
}

// PutTimed stores data exactly as [Put] does and, when unixSec > 0, sets the
// stored file's modification time to that Unix second (UTC) — the creation time
// of the post the media belongs to — so the cache on disk mirrors post
// chronology rather than the moment of download. A unixSec of 0 leaves the
// default write-time mtime. The cache key (and therefore a later cache hit) is
// unchanged: only the file's mtime differs. Best-effort throughout; a failed
// chtimes just leaves the write-time mtime. Empty data is not stored.
func (c *DiskCache) PutTimed(url string, data []byte, unixSec int64) {
	if len(data) == 0 {
		return
	}
	dir, err := c.Dir()
	if err != nil {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	p := filepath.Join(dir, GlobPrefix(url)+Ext(url, data))
	if err := os.WriteFile(p, data, 0o644); err != nil {
		return
	}
	if unixSec > 0 {
		t := time.Unix(unixSec, 0)
		_ = os.Chtimes(p, t, t)
	}
	c.prune(dir)
}

// prune deletes the oldest entries (by modification time) until the directory's
// total size is at or below Budget. It is best-effort: a stat/remove error just
// leaves that entry in place.
func (c *DiskCache) prune(dir string) {
	stat := c.statFn()
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
		p := filepath.Join(dir, e.Name())
		info, err := stat(p)
		if err != nil {
			continue // vanished between ReadDir and Stat; skip it
		}
		files = append(files, ent{path: p, size: info.Size(), mtime: info.ModTime().UnixNano()})
		total += info.Size()
	}
	if total <= c.Budget {
		return
	}
	// Oldest first, so the least-recently-written entries are evicted.
	sort.Slice(files, func(i, j int) bool { return files[i].mtime < files[j].mtime })
	for _, f := range files {
		if total <= c.Budget {
			break
		}
		if os.Remove(f.path) == nil {
			total -= f.size
		}
	}
}

// hash maps a media URL to the hex SHA-256 of the URL, the unique, fixed-length
// part of a cache filename (so two URLs that share a basename never collide).
func hash(url string) string {
	sum := sha256.Sum256([]byte(url))
	return hex.EncodeToString(sum[:])
}

// urlBasename returns the last path segment of url (empty when url does not parse
// or carries no path), used to build a readable cache-filename label.
func urlBasename(url string) string {
	u, err := neturl.Parse(url)
	if err != nil {
		return ""
	}
	return path.Base(u.Path)
}

// label is the human-readable, Finder-friendly prefix of a cache filename: the
// URL's basename with its own extension stripped, lowercased and sanitised to
// [A-Za-z0-9._-] (dropping any other rune, which also keeps the glob
// metacharacters *, ? and [ out so Get's Glob pattern stays literal), truncated
// to a sane length. Falls back to "media" when nothing usable remains.
func label(url string) string {
	base := urlBasename(url)
	base = strings.TrimSuffix(base, path.Ext(base))
	var b strings.Builder
	for _, r := range strings.ToLower(base) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		}
	}
	lab := b.String()
	if len(lab) > 48 {
		lab = lab[:48]
	}
	if lab == "" {
		return "media"
	}
	return lab
}

// GlobPrefix is the deterministic, extension-less stem of a cache filename for
// url: "<label>-<hash>". Both Put (which appends the content-derived extension)
// and Get (which globs "<stem>.*") derive it from the URL alone, so lookups find
// whatever extension the bytes were stored under.
func GlobPrefix(url string) string {
	return label(url) + "-" + hash(url)
}

// extForContentType maps a sniffed MIME type to the file extension the cache
// should store it under, reporting whether the type is a known media type. The
// video cases (mp4/webm) are kept ready even though the feed-thumbnail path only
// downloads images today.
func extForContentType(ct string) (string, bool) {
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

// knownURLExt returns the URL basename's own extension when it is a known media
// extension (normalising .jpeg to .jpg), else "". It is the fallback when content
// sniffing is inconclusive.
func knownURLExt(url string) string {
	switch ext := strings.ToLower(path.Ext(urlBasename(url))); ext {
	case ".jpg", ".jpeg":
		return ".jpg"
	case ".png", ".gif", ".webp", ".bmp", ".mp4", ".webm":
		return ext
	default:
		return ""
	}
}

// Ext picks the on-disk extension for data cached from url: the content type
// sniffed from the bytes wins; when that is inconclusive
// (application/octet-stream) the URL's own known media extension is used; else
// ".bin". Content-typing the file (rather than forcing one extension) is what
// lets Finder show real, correctly-typed media.
func Ext(url string, data []byte) string {
	ct := http.DetectContentType(data)
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	if ext, ok := extForContentType(ct); ok {
		return ext
	}
	if ct == "application/octet-stream" {
		if ext := knownURLExt(url); ext != "" {
			return ext
		}
	}
	return ".bin"
}
