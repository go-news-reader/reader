package app

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-news-reader/reader/source"
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

// loadGroupCache reads the persisted group list for server (one "name\tcount"
// per line), reporting false when there is no usable cache — so the browser
// opens instantly from disk instead of re-fetching tens of thousands of groups
// on every launch. A legacy name-only line (no tab) parses with a zero count.
func loadGroupCache(server string) ([]source.GroupInfo, bool) {
	path := groupCachePath(server)
	if path == "" {
		return nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var groups []source.GroupInfo
	for _, line := range strings.Split(string(data), "\n") {
		if line = strings.TrimSpace(line); line == "" {
			continue
		}
		name, count := line, 0
		if tab := strings.IndexByte(line, '\t'); tab >= 0 {
			name = line[:tab]
			count, _ = strconv.Atoi(line[tab+1:])
		}
		groups = append(groups, source.GroupInfo{Name: name, Count: count})
	}
	if len(groups) == 0 {
		return nil, false
	}
	return groups, true
}

// saveGroupCache persists the group list for server (best-effort; a failure just
// means the next launch re-fetches). The Refresh button rewrites it.
func saveGroupCache(server string, groups []source.GroupInfo) {
	path := groupCachePath(server)
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	var b strings.Builder
	for _, g := range groups {
		b.WriteString(g.Name)
		b.WriteByte('\t')
		b.WriteString(strconv.Itoa(g.Count))
		b.WriteByte('\n')
	}
	_ = os.WriteFile(path, []byte(b.String()), 0o644)
}
