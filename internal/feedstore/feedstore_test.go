package feedstore

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-news-reader/reader/source"
)

func TestDefaultDir(t *testing.T) {
	dir, err := DefaultDir()
	if err != nil {
		t.Fatalf("DefaultDir: %v", err)
	}
	if filepath.Base(dir) != "feeds" || filepath.Base(filepath.Dir(dir)) != appCacheSubdir {
		t.Fatalf("DefaultDir = %q, want …/%s/feeds", dir, appCacheSubdir)
	}
	// Error path: with no HOME and no XDG cache, os.UserCacheDir fails.
	t.Setenv("HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	if _, err := DefaultDir(); err == nil {
		t.Fatal("DefaultDir with no cache dir should error")
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	s, err := New(t.TempDir(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	e := source.StoredEntry{
		Kind:    source.Reddit,
		Channel: "golang",
		Res:     source.Result{Items: []source.Item{{ID: "x", Title: "hi"}}, Cursor: "c"},
		At:      time.Now(),
	}
	s.Save(e)
	got := s.Load()
	if len(got) != 1 || got[0].Channel != "golang" || got[0].Kind != source.Reddit ||
		len(got[0].Res.Items) != 1 || got[0].Res.Items[0].ID != "x" || got[0].Res.Cursor != "c" {
		t.Fatalf("round-trip = %+v", got)
	}
}

func TestNewMkdirError(t *testing.T) {
	// A path whose parent is a regular file cannot be created.
	f := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(filepath.Join(f, "sub"), time.Hour); err == nil {
		t.Fatal("New under a regular file should fail")
	}
}

func TestSaveWriteErrorSwallowed(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir, time.Hour)
	os.RemoveAll(dir) // the target dir is gone → WriteFile fails
	s.Save(source.StoredEntry{Kind: source.Reddit, Channel: "a"})
	if got := s.Load(); got != nil {
		t.Fatalf("load after a failed save = %+v, want nil", got)
	}
}

func TestLoadSkipsUnreadableCorruptAndOld(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir, time.Hour)
	s.now = func() time.Time { return time.Unix(2_000_000, 0) }

	// A good, recent entry.
	s.Save(source.StoredEntry{
		Kind: source.Reddit, Channel: "keep",
		Res: source.Result{Items: []source.Item{{ID: "k"}}}, At: time.Unix(2_000_000, 0),
	})
	// A corrupt file.
	if err := os.WriteFile(filepath.Join(dir, "corrupt.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A too-old entry (2 h old against the 1 h maxAge).
	s.Save(source.StoredEntry{Kind: source.Reddit, Channel: "old", At: time.Unix(2_000_000, 0).Add(-2 * time.Hour)})
	// A directory matching the glob → ReadFile fails on it.
	if err := os.Mkdir(filepath.Join(dir, "adir.json"), 0o700); err != nil {
		t.Fatal(err)
	}

	got := s.Load()
	if len(got) != 1 || got[0].Channel != "keep" {
		t.Fatalf("Load should keep only the good recent entry, got %+v", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "corrupt.json")); !os.IsNotExist(err) {
		t.Error("a corrupt file should be deleted on load")
	}
}

func TestLoadNoMaxAgeKeepsAncient(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(dir, 0) // no eviction; also exercises the default (real) clock
	s.Save(source.StoredEntry{Kind: source.Reddit, Channel: "a", At: time.Unix(1, 0)})
	if got := s.Load(); len(got) != 1 {
		t.Fatalf("maxAge 0 should keep even an ancient entry, got %+v", got)
	}
}
