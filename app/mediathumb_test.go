package app

import (
	"bytes"
	"context"
	"errors"
	"image"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-news-reader/reader/mediacache"
	"github.com/go-news-reader/reader/source"
	"github.com/go-news-reader/reader/ui"
)

// webPost is a non-Usenet item carrying one remote image, the shape whose
// thumbnail nothing used to fetch.
func webPost(id, url string) source.Item {
	return source.Item{ID: id, Source: source.Twitter, Channel: "NASA", Score: -1, Comments: -1,
		Body: "post " + id, Media: []source.Media{{URL: url, Kind: source.MediaImage}}}
}

// imageServer serves a real PNG at /ok, a 404 at /missing and non-image bytes at
// /notimage, and reports how many requests it saw.
func imageServer(t *testing.T) (*httptest.Server, func() int) {
	t.Helper()
	var mu sync.Mutex
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		n++
		mu.Unlock()
		switch r.URL.Path {
		case "/missing":
			w.WriteHeader(http.StatusNotFound)
		case "/notimage":
			io.WriteString(w, "this is not an image")
		default:
			w.Header().Set("Content-Type", "image/png")
			w.Write(pngBytes(8, 6))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, func() int { mu.Lock(); defer mu.Unlock(); return n }
}

// TestPrefetchMediaFetchesAndSetsThumbs is the end-to-end check: a feed of
// remote-image posts ends up with decoded thumbnails in the scene.
func TestPrefetchMediaFetchesAndSetsThumbs(t *testing.T) {
	srv, hits := imageServer(t)
	a := New(Config{Registry: newReg()})
	a.mediaClient = srv.Client()
	// Synchronous fetch so the assertion does not race the goroutines.
	a.SetMediaFetchHook(func(reqs []ui.MediaRequest) { a.loadMediaThumbs(context.Background(), reqs) })

	a.scene.SetItems([]source.Item{
		webPost("a", srv.URL+"/a.png"),
		webPost("b", srv.URL+"/b.png"),
	})
	a.PrefetchMedia()

	for _, id := range []string{"a", "b"} {
		if !a.scene.HasThumb(id) {
			t.Fatalf("item %q has no thumbnail after prefetch", id)
		}
		img := a.scene.Thumbs[id]
		if img == nil || img.Bounds().Dx() != 8 || img.Bounds().Dy() != 6 {
			t.Fatalf("item %q thumbnail = %v, want the served 8x6 image", id, img)
		}
	}
	if got := hits(); got != 2 {
		t.Fatalf("server saw %d requests, want 2", got)
	}

	// A second prefetch asks for nothing: both items are already decoded.
	a.PrefetchMedia()
	if got := hits(); got != 2 {
		t.Fatalf("server saw %d requests after re-prefetch, want no refetch", got)
	}
}

// TestPrefetchMediaNoRequestsIsNoOp checks an empty feed never reaches the hook.
func TestPrefetchMediaNoRequestsIsNoOp(t *testing.T) {
	a := New(Config{Registry: newReg()})
	called := false
	a.SetMediaFetchHook(func([]ui.MediaRequest) { called = true })
	a.PrefetchMedia() // no items at all
	if called {
		t.Fatal("prefetch with nothing to fetch should not call the hook")
	}
}

// TestPrefetchMediaUsesHook checks PrefetchMedia routes through the seam, with
// the requests the scene derived.
func TestPrefetchMediaUsesHook(t *testing.T) {
	a := New(Config{Registry: newReg()})
	var got []ui.MediaRequest
	a.SetMediaFetchHook(func(reqs []ui.MediaRequest) { got = reqs })
	a.scene.SetItems([]source.Item{webPost("x", "https://i/x.png")})
	a.PrefetchMedia()
	if len(got) != 1 || got[0].ID != "x" || got[0].URL != "https://i/x.png" {
		t.Fatalf("hook got %+v", got)
	}
}

// TestClaimMediaDedupsInFlight checks the in-flight guard that keeps the
// per-update incremental prefetch from re-requesting a still-downloading
// thumbnail: a claimed ID is skipped until it is released, then let through
// again (the failure-retry path).
func TestClaimMediaDedupsInFlight(t *testing.T) {
	a := New(Config{Registry: newReg()})
	reqs := []ui.MediaRequest{{ID: "a", URL: "u1"}, {ID: "b", URL: "u2"}}

	first := a.claimMedia(reqs)
	if len(first) != 2 {
		t.Fatalf("first claim = %d requests, want both", len(first))
	}
	// Both are in flight now: a second claim of the same IDs yields nothing.
	if again := a.claimMedia([]ui.MediaRequest{{ID: "a", URL: "u1"}, {ID: "b", URL: "u2"}}); len(again) != 0 {
		t.Fatalf("re-claim of in-flight IDs = %+v, want none", again)
	}
	// Releasing exactly one lets only it through on the next claim.
	a.releaseMedia("a")
	third := a.claimMedia([]ui.MediaRequest{{ID: "a", URL: "u1"}, {ID: "b", URL: "u2"}})
	if len(third) != 1 || third[0].ID != "a" {
		t.Fatalf("after releasing a, claim = %+v, want just a", third)
	}
}

// TestLoadMediaThumbsSkipsFailures checks a failing item leaves the scene alone
// rather than failing the batch: only the good one gets a thumbnail.
func TestLoadMediaThumbsSkipsFailures(t *testing.T) {
	srv, _ := imageServer(t)
	a := New(Config{Registry: newReg()})
	a.mediaClient = srv.Client()

	a.loadMediaThumbs(context.Background(), []ui.MediaRequest{
		{ID: "good", URL: srv.URL + "/ok.png"},
		{ID: "gone", URL: srv.URL + "/missing"},
		{ID: "junk", URL: srv.URL + "/notimage"},
	})
	if !a.scene.HasThumb("good") {
		t.Fatal("the decodable image should have produced a thumbnail")
	}
	for _, id := range []string{"gone", "junk"} {
		if a.scene.HasThumb(id) {
			t.Fatalf("item %q should have no thumbnail", id)
		}
	}
}

func TestFetchThumbRejectsUnusableResponses(t *testing.T) {
	srv, _ := imageServer(t)
	a := New(Config{Registry: newReg()})
	a.mediaClient = srv.Client()
	ctx := context.Background()

	// A URL that cannot even be turned into a request.
	if img := a.fetchThumb(ctx, "://\x7f bad", 0); img != nil {
		t.Fatal("a malformed URL should yield no thumbnail")
	}
	// A transport failure (nothing listening).
	if img := a.fetchThumb(ctx, "http://127.0.0.1:1/x.png", 0); img != nil {
		t.Fatal("an unreachable host should yield no thumbnail")
	}
	// A non-200 answer.
	if img := a.fetchThumb(ctx, srv.URL+"/missing", 0); img != nil {
		t.Fatal("a 404 should yield no thumbnail")
	}
	// A 200 that is not an image.
	if img := a.fetchThumb(ctx, srv.URL+"/notimage", 0); img != nil {
		t.Fatal("non-image bytes should yield no thumbnail")
	}
	// The happy path, for contrast.
	if img := a.fetchThumb(ctx, srv.URL+"/ok.png", 0); img == nil {
		t.Fatal("a served PNG should decode")
	}
}

// TestFetchThumbBodyReadError covers the read failure mid-body, which no real
// server reproduces on demand.
func TestFetchThumbBodyReadError(t *testing.T) {
	a := New(Config{Registry: newReg()})
	a.mediaClient = &http.Client{Transport: brokenBodyRT{}}
	if img := a.fetchThumb(context.Background(), "https://example.invalid/x.png", 0); img != nil {
		t.Fatal("a body that fails to read should yield no thumbnail")
	}
}

// TestFetchThumbReDecodeError covers the "bounded fine, re-decode failed"
// branch, unreachable without the decodeImage seam.
func TestFetchThumbReDecodeError(t *testing.T) {
	srv, _ := imageServer(t)
	a := New(Config{Registry: newReg()})
	a.mediaClient = srv.Client()

	orig := decodeImage
	decodeImage = func(io.Reader) (image.Image, string, error) {
		return nil, "", errors.New("decode boom")
	}
	defer func() { decodeImage = orig }()

	if img := a.fetchThumb(context.Background(), srv.URL+"/ok.png", 0); img != nil {
		t.Fatal("a failed re-decode should yield no thumbnail")
	}
}

// TestSetMediaSyncBlocksUntilFetched checks the one-shot CLI seam: after
// SetMediaSync, PrefetchMedia returns with the thumbnail already in the scene,
// so a PNG rendered straight afterwards carries the image rather than a
// placeholder.
func TestSetMediaSyncBlocksUntilFetched(t *testing.T) {
	srv, _ := imageServer(t)
	a := New(Config{Registry: newReg()})
	a.mediaClient = srv.Client()
	a.SetMediaSync()

	a.scene.SetItems([]source.Item{webPost("sync", srv.URL+"/s.png")})
	a.PrefetchMedia()

	if !a.scene.HasThumb("sync") {
		t.Fatal("SetMediaSync must leave the thumbnail decoded when PrefetchMedia returns")
	}
}

// TestDefaultMediaFetchIsAsync exercises the production seam installed by New:
// it runs the download on its own goroutine and lands the thumbnail in the
// scene. The scene write goes through post, so drain it as the render thread.
func TestDefaultMediaFetchIsAsync(t *testing.T) {
	srv, _ := imageServer(t)
	a := New(Config{Registry: newReg()})
	a.mediaClient = srv.Client()
	a.DeferSceneWrites() // queue scene writes instead of applying them inline

	a.scene.SetItems([]source.Item{webPost("async", srv.URL+"/a.png")})
	a.PrefetchMedia() // default hook: go loadMediaThumbs(...)

	deadline := time.Now().Add(5 * time.Second)
	for !a.scene.HasThumb("async") {
		if time.Now().After(deadline) {
			t.Fatal("async fetch never delivered a thumbnail")
		}
		a.drainScene()
		time.Sleep(2 * time.Millisecond)
	}
}

type brokenBodyRT struct{}

func (brokenBodyRT) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(errBody{}), Header: make(http.Header)}, nil
}

type errBody struct{}

func (errBody) Read([]byte) (int, error) { return 0, errors.New("read boom") }

// TestFetchThumbCachesAndSkipsRedownload proves the disk cache saves the network
// round-trip: fetching the same media URL twice hits the server only once, and
// both calls return a decoded image (the second straight from the cache).
func TestFetchThumbCachesAndSkipsRedownload(t *testing.T) {
	srv, hits := imageServer(t)
	a := New(Config{Registry: newReg()})
	a.mediaClient = srv.Client()
	url := srv.URL + "/photo.png"

	if img := a.fetchThumb(context.Background(), url, 0); img == nil {
		t.Fatal("first fetch returned no image")
	}
	if hits() != 1 {
		t.Fatalf("after first fetch, server hits = %d, want 1", hits())
	}
	// Second fetch: served from the on-disk cache, no new request.
	if img := a.fetchThumb(context.Background(), url, 0); img == nil {
		t.Fatal("cached fetch returned no image")
	}
	if hits() != 1 {
		t.Fatalf("second fetch re-downloaded: server hits = %d, want 1 (cache hit)", hits())
	}
}

// TestFetchThumbDownloadErrorsNotCached: a non-200 / non-image response yields no
// image and stores nothing, so a later success can still populate the cache.
func TestFetchThumbDownloadErrorsNotCached(t *testing.T) {
	srv, _ := imageServer(t)
	a := New(Config{Registry: newReg()})
	a.mediaClient = srv.Client()

	if img := a.fetchThumb(context.Background(), srv.URL+"/missing", 0); img != nil {
		t.Fatal("404 should yield no image")
	}
	if _, ok := a.mediaCache.Get(srv.URL + "/missing"); ok {
		t.Fatal("a failed fetch must not be cached")
	}
	if img := a.fetchThumb(context.Background(), srv.URL+"/notimage", 0); img != nil {
		t.Fatal("non-image should yield no image")
	}
	// A transport error (bad URL) also yields nil without caching.
	if img := a.fetchThumb(context.Background(), "http://%zz", 0); img != nil {
		t.Fatal("malformed URL should yield no image")
	}
}

// TestFetchThumbCachesOriginalMedia proves the cache preserves the RAW original
// media under a content-typed, Finder-friendly filename: a served PNG lands as
// <...>.png and a served GIF as <...>.gif, each byte-for-byte equal to what the
// server sent, and a second fetch is served from disk with no extra request.
func TestFetchThumbCachesOriginalMedia(t *testing.T) {
	png := pngBytes(8, 6)
	anim := gifBytes(8, 6)
	var mu sync.Mutex
	hits := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits[r.URL.Path]++
		mu.Unlock()
		switch r.URL.Path {
		case "/anim.gif":
			w.Header().Set("Content-Type", "image/gif")
			w.Write(anim)
		default:
			w.Header().Set("Content-Type", "image/png")
			w.Write(png)
		}
	}))
	t.Cleanup(srv.Close)

	a := New(Config{Registry: newReg()})
	a.mediaClient = srv.Client()
	dir, err := mediaCacheDir()
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		url, ext string
		raw      []byte
	}{
		{srv.URL + "/pic.png", ".png", png},
		{srv.URL + "/anim.gif", ".gif", anim},
	} {
		if img := a.fetchThumb(context.Background(), tc.url, 0); img == nil {
			t.Fatalf("%s: first fetch returned no thumbnail", tc.url)
		}
		matches, _ := filepath.Glob(filepath.Join(dir, mediacache.GlobPrefix(tc.url)+".*"))
		if len(matches) != 1 || !strings.HasSuffix(matches[0], tc.ext) {
			t.Fatalf("%s cached as %v, want a single %s", tc.url, matches, tc.ext)
		}
		stored, err := os.ReadFile(matches[0])
		if err != nil || !bytes.Equal(stored, tc.raw) {
			t.Fatalf("%s: cached bytes not the raw original (err=%v equal=%v)", tc.url, err, bytes.Equal(stored, tc.raw))
		}
	}

	// Second fetch of each URL is served from the cache: no extra request.
	before := map[string]int{}
	mu.Lock()
	for k, v := range hits {
		before[k] = v
	}
	mu.Unlock()
	for _, u := range []string{srv.URL + "/pic.png", srv.URL + "/anim.gif"} {
		if img := a.fetchThumb(context.Background(), u, 0); img == nil {
			t.Fatalf("%s: cached fetch returned no thumbnail", u)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	for p, n := range before {
		if hits[p] != n {
			t.Fatalf("path %s re-fetched: hits %d, want %d (cache hit)", p, hits[p], n)
		}
	}
}
