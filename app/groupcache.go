package app

import (
	"os"
	"path/filepath"
	"strings"
)

// groupCacheAppDir is the per-user cache subdirectory for the reader.
const groupCacheAppDir = "go-news-reader"

// groupCacheDir returns the directory where cached newsgroup lists live. A
// package var so tests can point it at a temp dir.
var groupCacheDir = func() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, groupCacheAppDir, "groups"), nil
}

// groupCachePath is the cache file for a server, or "" when the server is empty
// or the cache dir cannot be resolved.
func groupCachePath(server string) string {
	if server == "" {
		return ""
	}
	dir, err := groupCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, sanitizeServer(server)+".txt")
}

// sanitizeServer turns a "host:port" into a filesystem-safe file name.
func sanitizeServer(s string) string {
	return strings.NewReplacer(":", "_", "/", "_", "\\", "_").Replace(s)
}

// loadGroupCache reads the persisted group list for server (one name per line),
// reporting false when there is no usable cache — so the browser opens instantly
// from disk instead of re-fetching tens of thousands of groups on every launch.
func loadGroupCache(server string) ([]string, bool) {
	path := groupCachePath(server)
	if path == "" {
		return nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var names []string
	for _, line := range strings.Split(string(data), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			names = append(names, line)
		}
	}
	if len(names) == 0 {
		return nil, false
	}
	return names, true
}

// saveGroupCache persists the group list for server (best-effort; a failure just
// means the next launch re-fetches). The Refresh button rewrites it.
func saveGroupCache(server string, names []string) {
	path := groupCachePath(server)
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(path, []byte(strings.Join(names, "\n")), 0o644)
}
