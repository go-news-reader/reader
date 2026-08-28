// Package feedstore persists a [source.FeedCache]'s entries to disk — one small
// JSON file per subscription — so relaunching the reader serves each feed's last
// posts and skips re-fetching what is still fresh, instead of hitting every source
// again on startup. It implements [source.FeedStore].
package feedstore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/go-news-reader/reader/source"
)

// appCacheSubdir is the per-user cache subdirectory the reader owns.
const appCacheSubdir = "go-news-reader"

// DefaultDir is the per-user feed-cache directory, alongside the media cache under
// the OS cache dir. It errors only when the OS has no resolvable cache dir.
func DefaultDir() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, appCacheSubdir, "feeds"), nil
}

// Store is a directory of per-subscription JSON files. It is safe for concurrent
// use: each entry lives in its own file, written atomically.
type Store struct {
	dir    string
	maxAge time.Duration
	now    func() time.Time // nil → time.Now (tests inject a clock)
}

// New returns a Store writing under dir, creating it if absent. maxAge bounds how
// old a persisted entry may be to survive a Load (older ones are dropped and their
// files deleted); a non-positive maxAge keeps every entry.
func New(dir string, maxAge time.Duration) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Store{dir: dir, maxAge: maxAge}, nil
}

func (s *Store) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

// fileFor returns the path an entry for (kind, channel) is stored at. The name is
// a hash so any channel string (URLs, "@handles", "#tags") is a safe filename.
func (s *Store) fileFor(kind source.Kind, channel string) string {
	sum := sha256.Sum256([]byte(string(kind) + "\x00" + channel))
	return filepath.Join(s.dir, hex.EncodeToString(sum[:])+".json")
}

// Save writes one entry, atomically (temp file + rename) so a concurrent Load
// never sees a half-written file. Errors are swallowed: a cache write that fails
// to persist is a lost optimisation, not a failure worth surfacing on the fetch
// path.
func (s *Store) Save(e source.StoredEntry) {
	b, _ := json.Marshal(e) // a StoredEntry is plain data — Marshal cannot fail
	path := s.fileFor(e.Kind, e.Channel)
	tmp := path + ".tmp"
	if os.WriteFile(tmp, b, 0o600) != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

// Load reads every persisted entry, deleting any that is corrupt or older than
// maxAge on the way. The pattern is always valid, so the glob cannot error.
func (s *Store) Load() []source.StoredEntry {
	names, _ := filepath.Glob(filepath.Join(s.dir, "*.json"))
	var out []source.StoredEntry
	for _, name := range names {
		b, err := os.ReadFile(name)
		if err != nil {
			continue // vanished or unreadable between glob and read
		}
		var e source.StoredEntry
		if json.Unmarshal(b, &e) != nil {
			_ = os.Remove(name) // corrupt file: drop it
			continue
		}
		if s.maxAge > 0 && s.clock().Sub(e.At) > s.maxAge {
			_ = os.Remove(name) // too old to be useful: evict
			continue
		}
		out = append(out, e)
	}
	return out
}
