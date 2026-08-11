package app

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
)

// mediaCacheBudget bounds the on-disk media cache. Thumbnails are re-encoded to
// at most thumbMaxDim, so entries are small (tens of KB); 256 MiB holds many
// thousands of them. When a write pushes the directory past the budget the
// oldest entries are pruned, so the cache is self-limiting rather than growing
// without bound.
const mediaCacheBudget = 256 << 20

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

// mediaCacheKey maps a media URL to its cache filename: the hex SHA-256 of the
// URL, so arbitrary URLs become safe, fixed-length filenames.
func mediaCacheKey(url string) string {
	sum := sha256.Sum256([]byte(url))
	return hex.EncodeToString(sum[:])
}

// mediaCacheGet returns the cached bytes for url and whether a usable entry was
// found. A missing file, an unresolvable cache dir, or an empty entry is a miss.
func mediaCacheGet(url string) ([]byte, bool) {
	dir, err := mediaCacheDir()
	if err != nil {
		return nil, false
	}
	data, err := os.ReadFile(filepath.Join(dir, mediaCacheKey(url)))
	if err != nil || len(data) == 0 {
		return nil, false
	}
	return data, true
}

// mediaCachePut stores data under url's key, creating the cache dir as needed,
// then prunes the directory back under the budget. It is best-effort: any error
// (no cache dir, write failure) is silently ignored, since the cache is only an
// optimisation and the caller already has the bytes. Empty data is not stored.
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
	if err := os.WriteFile(filepath.Join(dir, mediaCacheKey(url)), data, 0o644); err != nil {
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
