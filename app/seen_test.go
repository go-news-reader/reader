package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-news-reader/reader/source"
)

func tmpSeen(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	orig := seenFilePath
	seenFilePath = func() (string, error) { return filepath.Join(dir, "seen.json"), nil }
	t.Cleanup(func() { seenFilePath = orig })
	return dir
}

func TestSeenPersistence(t *testing.T) {
	dir := tmpSeen(t)
	if len(loadSeen()) != 0 {
		t.Fatal("no file → empty markers")
	}
	saveSeen(map[string]int{"usenet|g": 500})
	if loadSeen()["usenet|g"] != 500 {
		t.Fatal("marker not persisted")
	}
	// A corrupt file reads back as empty (reset).
	os.WriteFile(filepath.Join(dir, "seen.json"), []byte("{not json"), 0o644)
	if len(loadSeen()) != 0 {
		t.Fatal("corrupt file should reset markers")
	}
}

func TestSeenDirError(t *testing.T) {
	orig := seenFilePath
	seenFilePath = func() (string, error) { return "", errors.New("no cache home") }
	t.Cleanup(func() { seenFilePath = orig })
	if len(loadSeen()) != 0 {
		t.Fatal("unresolved dir → empty")
	}
	saveSeen(map[string]int{"x": 1}) // must not panic
}

func TestSaveSeenMkdirError(t *testing.T) {
	tmp := t.TempDir()
	blocker := filepath.Join(tmp, "afile")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	orig := seenFilePath
	// The parent is a regular file, so MkdirAll fails and the write is skipped.
	seenFilePath = func() (string, error) { return filepath.Join(blocker, "sub", "seen.json"), nil }
	t.Cleanup(func() { seenFilePath = orig })
	saveSeen(map[string]int{"x": 1}) // must not panic; nothing written
	if len(loadSeen()) != 0 {
		t.Fatal("nothing should be persisted when the dir cannot be created")
	}
}

func TestSeenFilePathDefault(t *testing.T) {
	// Exercise the real (unstubbed) seenFilePath: the happy path resolves under
	// the user cache dir; with no HOME, os.UserCacheDir fails and it errors out.
	_ = loadSeen() // happy path (a missing file just yields an empty map)
	t.Setenv("HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	if len(loadSeen()) != 0 {
		t.Fatal("no cache home → empty markers")
	}
	saveSeen(map[string]int{"x": 1}) // must not panic
}

func TestViewSubMarksSeen(t *testing.T) {
	tmpSeen(t)
	a := New(Config{
		Registry:      newReg(),
		Subscriptions: []source.Subscription{{Source: source.Usenet, Channel: "g"}, {Source: source.Usenet, Channel: "empty"}},
	})
	a.scene.SetItems([]source.Item{{Source: source.Usenet, Channel: "g", GroupCount: 100, GroupHigh: 500}})

	a.ViewSub(0) // marks group g seen at its high water
	if a.seen["usenet|g"] != 500 {
		t.Fatalf("ViewSub did not record the marker: %v", a.seen)
	}
	if loadSeen()["usenet|g"] != 500 {
		t.Fatal("ViewSub did not persist the marker")
	}
	// The nil-map guard: a fresh (unloaded) seen map is initialised on demand.
	a.seen = nil
	a.ViewSub(0)
	if a.seen["usenet|g"] != 500 {
		t.Fatal("ViewSub should initialise a nil seen map")
	}
	// A group with no loaded items (marker 0) only switches the filter.
	a.ViewSub(1)
	if _, ok := a.seen["usenet|empty"]; ok {
		t.Fatal("a marker-less sub must not be recorded")
	}
	// AllFilter has no marker either.
	a.ViewSub(-1)
}
