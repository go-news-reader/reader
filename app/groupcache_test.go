package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-news-reader/reader/feeds"
	"github.com/go-news-reader/reader/internal/settings"
)

// tmpGroupCache points the cache dir at a temp dir for the duration of a test.
func tmpGroupCache(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	orig := groupCacheDir
	groupCacheDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { groupCacheDir = orig })
	return dir
}

func TestGroupCacheRoundTrip(t *testing.T) {
	dir := tmpGroupCache(t)
	const server = "news.free.fr:119"
	if _, ok := loadGroupCache(server); ok {
		t.Fatal("expected a miss before anything is cached")
	}
	saveGroupCache(server, []string{"alt.test", "comp.lang.go", ""})
	names, ok := loadGroupCache(server)
	if !ok || len(names) != 2 || names[0] != "alt.test" {
		t.Fatalf("round trip = %v ok=%v", names, ok)
	}
	// The file name is filesystem-safe (":" → "_").
	if _, err := os.Stat(filepath.Join(dir, "news.free.fr_119.txt")); err != nil {
		t.Fatalf("sanitized cache file missing: %v", err)
	}
	// An empty server has no cache; a blank file reads as a miss.
	saveGroupCache("", []string{"x"})
	if _, ok := loadGroupCache(""); ok {
		t.Fatal("empty server must not cache")
	}
	os.WriteFile(filepath.Join(dir, "blank_1.txt"), []byte(" \n\n\t"), 0o644)
	if _, ok := loadGroupCache("blank:1"); ok {
		t.Fatal("a blank cache file must read as a miss")
	}
}

func TestGroupCacheDirError(t *testing.T) {
	orig := groupCacheDir
	groupCacheDir = func() (string, error) { return "", errors.New("no cache home") }
	t.Cleanup(func() { groupCacheDir = orig })
	if groupCachePath("s:1") != "" {
		t.Fatal("unresolved cache dir should yield an empty path")
	}
	if _, ok := loadGroupCache("s:1"); ok {
		t.Fatal("unresolved cache dir should miss")
	}
	saveGroupCache("s:1", []string{"x"}) // must not panic
}

func TestSaveGroupCacheMkdirError(t *testing.T) {
	tmp := t.TempDir()
	blocker := filepath.Join(tmp, "afile")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	orig := groupCacheDir
	// The cache dir's parent is a regular file, so MkdirAll fails.
	groupCacheDir = func() (string, error) { return filepath.Join(blocker, "groups"), nil }
	t.Cleanup(func() { groupCacheDir = orig })
	saveGroupCache("s:1", []string{"a"}) // must not panic; write is skipped
	if _, ok := loadGroupCache("s:1"); ok {
		t.Fatal("nothing should be cached when the dir cannot be created")
	}
}

func TestDoLoadGroupsServesCacheAndRefreshRewrites(t *testing.T) {
	tmpGroupCache(t)
	const server = "news.free.fr:119"
	saveGroupCache(server, []string{"cached.group"})

	fg := &fakeGrouper{names: []string{"fresh.group.a", "fresh.group.b"}}
	set := &settings.Settings{Profiles: []settings.Profile{{Name: "Home"}}, Active: 0, Theme: settings.ThemeSystem}
	a := New(Config{Registry: newReg(fg), Settings: set, Options: feeds.Options{UsenetAddr: server}, Width: 500, Height: 400})
	syncGroups(a)

	// Open (not force) → served from disk, no provider call.
	a.LoadGroups()
	if fg.groupsCalls != 0 {
		t.Fatalf("cache hit should not call the provider, got %d calls", fg.groupsCalls)
	}
	if got := a.Scene().BrowseGroups(); len(got) != 1 || got[0] != "cached.group" {
		t.Fatalf("browse groups from cache = %v", got)
	}

	// Refresh (force) → fetch from the provider and rewrite the cache.
	a.RefreshGroups()
	if fg.refreshCall != 1 {
		t.Fatalf("refresh should call the provider, got %d", fg.refreshCall)
	}
	if got := a.Scene().BrowseGroups(); len(got) != 2 || got[0] != "fresh.group.a" {
		t.Fatalf("browse groups after refresh = %v", got)
	}
	if c, ok := loadGroupCache(server); !ok || len(c) != 2 || c[0] != "fresh.group.a" {
		t.Fatalf("cache not rewritten on refresh: %v", c)
	}
}

func TestDoLoadGroupsCacheMissThenFetch(t *testing.T) {
	tmpGroupCache(t)
	const server = "news.example:119"
	fg := &fakeGrouper{names: []string{"g1", "g2"}}
	set := &settings.Settings{Profiles: []settings.Profile{{Name: "Home"}}, Active: 0, Theme: settings.ThemeSystem}
	a := New(Config{Registry: newReg(fg), Settings: set, Options: feeds.Options{UsenetAddr: server}, Width: 500, Height: 400})
	syncGroups(a)
	a.LoadGroups() // no cache yet → fetch + save
	if fg.groupsCalls != 1 {
		t.Fatalf("cache miss should fetch once, got %d", fg.groupsCalls)
	}
	if c, ok := loadGroupCache(server); !ok || len(c) != 2 {
		t.Fatalf("fetch did not populate the cache: %v", c)
	}
}
