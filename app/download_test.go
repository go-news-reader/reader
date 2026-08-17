package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-news-reader/reader/provider/usenet"
	"github.com/go-news-reader/reader/source"
)

// fakePrefetcher is a Usenet provider that returns a fixed image (or error) for
// every prefetch request, synchronously.
type fakePrefetcher struct {
	jpeg []byte
	err  error
}

func (f *fakePrefetcher) Kind() source.Kind { return source.Usenet }
func (f *fakePrefetcher) Feed(context.Context, source.Query) (source.Result, error) {
	return source.Result{}, nil
}
func (f *fakePrefetcher) PrefetchImages(_ context.Context, reqs []usenet.ImageRequest, _, _ int, onResult func(usenet.ImageResult)) {
	for _, r := range reqs {
		onResult(usenet.ImageResult{ID: r.ID, JPEG: f.jpeg, Err: f.err})
	}
}

func TestDecodeThumb(t *testing.T) {
	if decodeThumb([]byte("not an image")) != nil {
		t.Fatal("bad bytes should decode to nil")
	}
	if decodeThumb(pngBytes(6, 4)) == nil {
		t.Fatal("valid PNG should decode")
	}
}

// Async path: the default goroutine drain stores the thumbnail; a busy-drain
// loop applies the marshalled scene write.
func TestDownloaderAsyncStoresThumb(t *testing.T) {
	a := New(Config{Registry: newReg(&fakePrefetcher{jpeg: pngBytes(8, 8)})})
	a.DeferSceneWrites()
	a.dl.enqueue(usenet.ImageRequest{ID: "x", Parts: []usenet.ReconstructPart{{MessageID: "m"}}})
	// Wait on a deadline, not on an iteration count. The download runs in its own
	// goroutine, and a million spins of drainScene is not a duration: on a loaded
	// machine (the full package suite, or CI) the loop can burn through all of
	// them before that goroutine is ever scheduled, failing a test that is not
	// broken. Yielding between polls lets the worker run, and a wall-clock
	// deadline is what "it never arrived" actually means.
	stored := false
	for deadline := time.Now().Add(10 * time.Second); !stored && time.Now().Before(deadline); {
		a.drainScene()
		if stored = a.scene.HasThumb("x"); !stored {
			time.Sleep(time.Millisecond)
		}
	}
	if !stored {
		t.Fatal("async prefetch did not store the thumbnail")
	}
	// A cached id and an empty id are both skipped (no new work).
	a.dl.enqueue(usenet.ImageRequest{ID: "x"}, usenet.ImageRequest{ID: ""})
}

// Synchronous path (async off): error / no-image and missing-provider branches.
func TestDownloaderErrorAndNoProvider(t *testing.T) {
	// Provider returns an error → no thumb, inflight cleared (a retry can enqueue).
	a := New(Config{Registry: newReg(&fakePrefetcher{err: errors.New("boom")})})
	a.dl.async = false
	a.dl.enqueue(usenet.ImageRequest{ID: "e", Parts: []usenet.ReconstructPart{{MessageID: "m"}}})
	a.drainScene()
	if a.scene.HasThumb("e") {
		t.Fatal("errored prefetch must not store a thumb")
	}
	a.dl.mu.Lock()
	stuck := a.dl.inflight["e"]
	a.dl.mu.Unlock()
	if stuck {
		t.Fatal("inflight not cleared after an errored batch")
	}

	// Provider present but without the prefetch capability → batch dropped.
	aPlain := New(Config{Registry: newReg(fakeProv{kind: source.Usenet})})
	aPlain.dl.async = false
	aPlain.dl.enqueue(usenet.ImageRequest{ID: "p", Parts: []usenet.ReconstructPart{{MessageID: "m"}}})
	aPlain.drainScene()
	if aPlain.scene.HasThumb("p") {
		t.Fatal("non-prefetcher provider should store nothing")
	}
}

func TestAppPrefetchImages(t *testing.T) {
	a := New(Config{Registry: newReg(&fakePrefetcher{jpeg: pngBytes(8, 8)})})
	a.dl.async = false
	// No Usenet image posts → no-op.
	a.PrefetchImages()
	// A shown Usenet image post is enqueued and downloaded.
	a.scene.SetSubs(nil)
	a.scene.SetItems([]source.Item{
		{ID: "pic", Source: source.Usenet, Permalink: "news:<m>", Title: `"pic.jpg" yEnc (1/1)`},
	})
	a.PrefetchImages()
	a.drainScene()
	if !a.scene.HasThumb("pic") {
		t.Fatal("shown Usenet image was not prefetched")
	}
}
