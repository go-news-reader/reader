package mediacacheplugin

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	goplugin "github.com/hashicorp/go-plugin"

	"github.com/go-news-reader/reader/internal/mediacacheplugin/transport"
	"github.com/go-news-reader/reader/mediacache"
)

// staticCache is a trivial in-test mediacache.Cache used by the launcher-seam
// tests (which never spawn a real process).
type staticCache struct {
	data  []byte
	found bool
}

func (c staticCache) Get(string) ([]byte, bool) { return c.data, c.found }
func (staticCache) Put(string, []byte)          {}

// TestServe covers the SDK entry point by injecting the go-plugin server seam so
// the call does not block on a live server, and asserts the served config wraps
// the given cache.
func TestServe(t *testing.T) {
	orig := pluginServe
	defer func() { pluginServe = orig }()

	var got *goplugin.ServeConfig
	pluginServe = func(c *goplugin.ServeConfig) { got = c }

	cache := staticCache{data: []byte("x"), found: true}
	Serve(cache)

	if got == nil {
		t.Fatal("pluginServe was not called")
	}
	mp, ok := got.Plugins["mediacache"].(*transport.MediaCachePlugin)
	if !ok {
		t.Fatalf("served plugin is not a *transport.MediaCachePlugin: %T", got.Plugins["mediacache"])
	}
	data, found := mp.Impl.Get("any")
	if !found || string(data) != "x" {
		t.Fatalf("served cache Get = %q,%v, want x,true", data, found)
	}
}

// TestLoadDelegates: load forwards to the injected launcher and returns its
// cache + closer on success.
func TestLoadDelegates(t *testing.T) {
	want := staticCache{data: []byte("hit"), found: true}
	var closed bool
	launch := func(path string) (mediacache.Cache, func() error, error) {
		if path != "/some/plugin" {
			t.Fatalf("launcher got path %q, want /some/plugin", path)
		}
		return want, func() error { closed = true; return nil }, nil
	}
	cache, closeFn, err := load("/some/plugin", launch)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if data, found := cache.Get("k"); !found || string(data) != "hit" {
		t.Fatalf("loaded cache Get = %q,%v, want hit,true", data, found)
	}
	if err := closeFn(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if !closed {
		t.Fatal("closer was not invoked")
	}
}

// TestLoadLaunchError: a launcher failure is propagated (the host falls back).
func TestLoadLaunchError(t *testing.T) {
	launch := func(string) (mediacache.Cache, func() error, error) {
		return nil, nil, errors.New("not a plugin")
	}
	if _, _, err := load("/bad", launch); err == nil {
		t.Fatal("expected the launcher error to propagate")
	}
}

// TestLoadEndToEnd builds the reference HTTP-backed cache plugin, launches it
// through the real transport (exercising Load, transport.Launch, Serve and the
// gRPC round trip) against an httptest store, and asserts a Put then Get round
// trips the bytes through the subprocess, a miss reports found=false, and Close
// stops the subprocess. No live network: the store is an in-process httptest
// server and CACHE_BASE_URL points the plugin at it.
func TestLoadEndToEnd(t *testing.T) {
	// An in-memory HTTP key/value store standing in for the shared/network cache.
	var mu sync.Mutex
	store := map[string][]byte{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.URL.Path, "/media/")
		mu.Lock()
		defer mu.Unlock()
		switch r.Method {
		case http.MethodPut:
			body, _ := io.ReadAll(r.Body)
			store[key] = body
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			data, ok := store[key]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write(data)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer srv.Close()

	// Build the reference plugin.
	dir := t.TempDir()
	binName := "example-mediacache-plugin"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	binPath := filepath.Join(dir, binName)
	build := exec.Command("go", "build", "-o", binPath, "github.com/go-news-reader/reader/cmd/example-mediacache-plugin")
	build.Env = append(os.Environ(), "GOWORK=off")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building reference plugin failed: %v\n%s", err, out)
	}

	// Point the plugin at the httptest store, then launch it.
	t.Setenv("CACHE_BASE_URL", srv.URL+"/media")
	cache, closeFn, err := Load(binPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	t.Cleanup(func() {
		if e := closeFn(); e != nil {
			t.Errorf("Close: %v", e)
		}
	})

	// A miss (nothing stored yet) reports found=false across the boundary.
	if _, found := cache.Get("https://x/never"); found {
		t.Fatal("expected a miss for an unstored URL")
	}

	// A Put then Get round-trips the bytes through the subprocess + HTTP store.
	url := "https://x/pic.png"
	want := []byte("PNGDATA-shared-cache")
	cache.Put(url, want)
	got, found := cache.Get(url)
	if !found || string(got) != string(want) {
		t.Fatalf("round trip = %q,%v, want %q,true", got, found, want)
	}

	// The bytes really landed in the shared store under the content-addressed key.
	mu.Lock()
	defer mu.Unlock()
	if len(store) != 1 {
		t.Fatalf("store holds %d entries, want 1", len(store))
	}
}
