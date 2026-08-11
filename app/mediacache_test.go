package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestMain points the media cache at a throwaway dir for the whole app test
// package, so tests never read or write the real per-user cache (mirrors the
// isolation the group-cache tests do per-test).
func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "reader-mediacache-test")
	if err != nil {
		panic(err)
	}
	mediaCacheDir = func() (string, error) { return tmp, nil }
	code := m.Run()
	_ = os.RemoveAll(tmp)
	os.Exit(code)
}

func TestMediaCacheRoundTrip(t *testing.T) {
	// A stored entry reads back byte-for-byte; an absent key misses.
	if _, ok := mediaCacheGet("https://x/never"); ok {
		t.Fatal("absent key should miss")
	}
	mediaCachePut("https://x/a.jpg", []byte("JPEGDATA"))
	got, ok := mediaCacheGet("https://x/a.jpg")
	if !ok || string(got) != "JPEGDATA" {
		t.Fatalf("get = %q,%v, want JPEGDATA,true", got, ok)
	}
	// Empty data is never stored.
	mediaCachePut("https://x/empty", nil)
	if _, ok := mediaCacheGet("https://x/empty"); ok {
		t.Fatal("empty data must not be stored")
	}
}

func TestMediaCacheKeyStable(t *testing.T) {
	if mediaCacheKey("https://x/a") != mediaCacheKey("https://x/a") {
		t.Fatal("same URL must map to the same key")
	}
	if mediaCacheKey("https://x/a") == mediaCacheKey("https://x/b") {
		t.Fatal("different URLs must map to different keys")
	}
}

func TestMediaCacheDirError(t *testing.T) {
	orig := mediaCacheDir
	mediaCacheDir = func() (string, error) { return "", os.ErrPermission }
	t.Cleanup(func() { mediaCacheDir = orig })
	// Both sides degrade gracefully to no-ops when the dir can't be resolved.
	mediaCachePut("https://x/a", []byte("data")) // must not panic
	if _, ok := mediaCacheGet("https://x/a"); ok {
		t.Fatal("get should miss when the cache dir is unresolvable")
	}
}

func TestDefaultMediaCacheDir(t *testing.T) {
	// Happy path: resolves under the OS cache dir and ends in the media subdir.
	dir, err := defaultMediaCacheDir()
	if err != nil {
		t.Fatalf("defaultMediaCacheDir: %v", err)
	}
	if filepath.Base(dir) != "media" {
		t.Fatalf("dir = %q, want it to end in /media", dir)
	}
	// Error path: with no HOME (and no XDG cache), os.UserCacheDir fails.
	t.Setenv("HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	if _, err := defaultMediaCacheDir(); err == nil {
		t.Fatal("want an error when no cache home is resolvable")
	}
}

func TestMediaCachePutMkdirFailure(t *testing.T) {
	// Point the cache dir under a regular file so MkdirAll fails; Put is a no-op.
	blocker := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	orig := mediaCacheDir
	mediaCacheDir = func() (string, error) { return filepath.Join(blocker, "media"), nil }
	t.Cleanup(func() { mediaCacheDir = orig })
	mediaCachePut("https://x/a", []byte("data")) // must not panic
	if _, ok := mediaCacheGet("https://x/a"); ok {
		t.Fatal("get should miss after a failed put")
	}
}

func TestPruneMediaCacheEvictsOldest(t *testing.T) {
	dir := t.TempDir()
	// Three 100-byte entries with strictly increasing mtimes.
	names := []string{"old", "mid", "new"}
	base := time.Unix(1_700_000_000, 0)
	for i, n := range names {
		p := filepath.Join(dir, n)
		if err := os.WriteFile(p, make([]byte, 100), 0o644); err != nil {
			t.Fatal(err)
		}
		mt := base.Add(time.Duration(i) * time.Hour)
		if err := os.Chtimes(p, mt, mt); err != nil {
			t.Fatal(err)
		}
	}
	// Budget of 250 bytes: total 300 > 250, so the single oldest (100) is evicted.
	pruneMediaCache(dir, 250)
	if _, err := os.Stat(filepath.Join(dir, "old")); !os.IsNotExist(err) {
		t.Fatal("oldest entry should have been evicted")
	}
	for _, n := range []string{"mid", "new"} {
		if _, err := os.Stat(filepath.Join(dir, n)); err != nil {
			t.Fatalf("%s should survive prune: %v", n, err)
		}
	}
}

func TestPruneMediaCacheUnderBudgetKeepsAll(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a"), make([]byte, 10), 0o644); err != nil {
		t.Fatal(err)
	}
	// A subdirectory is ignored by the size accounting.
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	pruneMediaCache(dir, 1<<20)
	if _, err := os.Stat(filepath.Join(dir, "a")); err != nil {
		t.Fatalf("under-budget prune removed an entry: %v", err)
	}
}

func TestPruneMediaCacheMissingDir(t *testing.T) {
	// A non-existent dir is a silent no-op (ReadDir errors).
	pruneMediaCache(filepath.Join(t.TempDir(), "does-not-exist"), 1)
}

func TestMediaCachePutWriteFailure(t *testing.T) {
	// A directory sitting at the exact key path makes WriteFile fail; Put no-ops.
	dir := t.TempDir()
	orig := mediaCacheDir
	mediaCacheDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { mediaCacheDir = orig })
	url := "https://x/collide"
	if err := os.MkdirAll(filepath.Join(dir, mediaCacheKey(url)), 0o755); err != nil {
		t.Fatal(err)
	}
	mediaCachePut(url, []byte("data")) // WriteFile fails (target is a dir); no panic
	if _, ok := mediaCacheGet(url); ok {
		t.Fatal("get should miss when the write failed")
	}
}

func TestPruneMediaCacheSkipsVanishedEntry(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a", "b"} {
		if err := os.WriteFile(filepath.Join(dir, n), make([]byte, 200), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	orig := statFile
	statFile = func(p string) (os.FileInfo, error) {
		if filepath.Base(p) == "a" {
			return nil, os.ErrNotExist // pretend "a" vanished between ReadDir and Stat
		}
		return os.Stat(p)
	}
	t.Cleanup(func() { statFile = orig })
	// "a" is skipped from accounting; only "b" (200) counts, under the 300 budget,
	// so nothing is evicted and the call completes without touching the vanished one.
	pruneMediaCache(dir, 300)
	if _, err := os.Stat(filepath.Join(dir, "b")); err != nil {
		t.Fatalf("b should survive: %v", err)
	}
}
