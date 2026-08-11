package app

import (
	"bytes"
	"errors"
	"image"
	"image/gif"
	"os"
	"testing"

	"github.com/go-news-reader/reader/internal/settings"
	"github.com/go-news-reader/reader/mediacache"
	"github.com/go-news-reader/reader/source"
	"github.com/go-news-reader/reader/ui"
)

// gifBytes returns a real (single-frame) GIF, so http.DetectContentType reports
// image/gif and the bytes survive a cache round-trip as a browsable .gif.
func gifBytes(w, h int) []byte {
	var b bytes.Buffer
	if err := gif.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h)), nil); err != nil {
		panic(err)
	}
	return b.Bytes()
}

// TestMain points the media cache at a throwaway dir for the whole app test
// package, so tests never read or write the real per-user cache (mirrors the
// isolation the group-cache tests do per-test). New wires this package var into
// every DiskCache it builds.
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

// diskCache returns the app's built-in on-disk cache, failing the test if the
// backend is not a DiskCache (the default when no plugin is configured).
func diskCache(t *testing.T, a *App) *mediacache.DiskCache {
	t.Helper()
	dc, ok := a.mediaCache.(*mediacache.DiskCache)
	if !ok {
		t.Fatalf("mediaCache = %T, want *mediacache.DiskCache", a.mediaCache)
	}
	return dc
}

// TestMediaCacheBudgetWiring verifies the configurable media-cache size limit
// flows from Settings into the built-in DiskCache's Budget both at construction
// (New) and on a live edit (ApplySceneSettings).
func TestMediaCacheBudgetWiring(t *testing.T) {
	set := settings.Default()
	set.MediaCacheMB = 512
	a := New(Config{
		Registry: newReg(fakeProv{kind: source.Reddit}),
		Settings: set,
		OS:       ui.OSMac,
		Width:    400, Height: 300,
	})
	if want := int64(512) << 20; diskCache(t, a).Budget != want {
		t.Fatalf("budget after New = %d, want %d", diskCache(t, a).Budget, want)
	}

	// A live edit through the scene re-applies the budget on ApplySceneSettings.
	a.Scene().SetMediaCacheMB(1024)
	a.ApplySceneSettings()
	if want := int64(1024) << 20; diskCache(t, a).Budget != want {
		t.Fatalf("budget after ApplySceneSettings = %d, want %d", diskCache(t, a).Budget, want)
	}
}

// TestDefaultBackendIsDiskCache: with no CacheBackend configured the reader uses
// the built-in on-disk cache.
func TestDefaultBackendIsDiskCache(t *testing.T) {
	a := New(Config{Registry: newReg()})
	if _, ok := a.mediaCache.(*mediacache.DiskCache); !ok {
		t.Fatalf("default backend = %T, want *mediacache.DiskCache", a.mediaCache)
	}
	if a.mediaCacheClose != nil {
		t.Fatal("disk-cache backend must have no plugin closer")
	}
}

// TestCachePluginBackendLoaded: a CacheBackend path selects the plugin backend
// through the (seam-injected) loader, and the returned cache + closer are wired.
func TestCachePluginBackendLoaded(t *testing.T) {
	orig := loadMediaCachePlugin
	t.Cleanup(func() { loadMediaCachePlugin = orig })

	var gotPath string
	var closed bool
	want := staticCache{data: []byte("plugin-hit"), found: true}
	loadMediaCachePlugin = func(path string) (mediacache.Cache, func() error, error) {
		gotPath = path
		return want, func() error { closed = true; return nil }, nil
	}

	set := settings.Default()
	set.CacheBackend = "/opt/reader/shared-cache"
	a := New(Config{Registry: newReg(), Settings: set})

	if gotPath != "/opt/reader/shared-cache" {
		t.Fatalf("loader path = %q, want /opt/reader/shared-cache", gotPath)
	}
	if data, found := a.mediaCache.Get("k"); !found || string(data) != "plugin-hit" {
		t.Fatalf("plugin cache Get = %q,%v, want plugin-hit,true", data, found)
	}
	if a.mediaCacheClose == nil {
		t.Fatal("plugin backend must expose a closer")
	}
	if err := a.mediaCacheClose(); err != nil || !closed {
		t.Fatalf("closer: err=%v closed=%v", err, closed)
	}
	// A live budget edit is a no-op for a plugin backend (it manages its own
	// capacity) and must not panic or change the backend.
	a.ApplySceneSettings()
	if _, ok := a.mediaCache.(staticCache); !ok {
		t.Fatalf("plugin backend changed to %T after ApplySceneSettings", a.mediaCache)
	}
}

// TestCachePluginLoadFailureFallsBack: when the configured plugin fails to load
// the reader falls back to the built-in DiskCache (never crashes) and surfaces a
// status.
func TestCachePluginLoadFailureFallsBack(t *testing.T) {
	orig := loadMediaCachePlugin
	t.Cleanup(func() { loadMediaCachePlugin = orig })
	loadMediaCachePlugin = func(string) (mediacache.Cache, func() error, error) {
		return nil, nil, errors.New("no such plugin")
	}

	set := settings.Default()
	set.CacheBackend = "/does/not/exist"
	a := New(Config{Registry: newReg(), Settings: set})

	if _, ok := a.mediaCache.(*mediacache.DiskCache); !ok {
		t.Fatalf("fallback backend = %T, want *mediacache.DiskCache", a.mediaCache)
	}
	if a.mediaCacheClose != nil {
		t.Fatal("failed plugin load must leave no closer")
	}
	if got := a.Scene().Status; got == "" {
		t.Fatal("a plugin load failure should surface a status line")
	}
}

// staticCache is an in-test mediacache.Cache used by the plugin-backend seam
// tests (no real subprocess).
type staticCache struct {
	data  []byte
	found bool
}

func (c staticCache) Get(string) ([]byte, bool) { return c.data, c.found }
func (staticCache) Put(string, []byte)          {}
