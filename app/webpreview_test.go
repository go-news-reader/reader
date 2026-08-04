package app

import (
	"context"
	"errors"
	"image"
	"testing"
	"time"

	"github.com/go-news-reader/reader/source"
)

// fakeRenderer is a webrender.Renderer that returns a canned image/error without
// touching the network; done (when non-nil) is closed as Render returns so a
// test can await the async default webFetch goroutine.
type fakeRenderer struct {
	img   *image.RGBA
	err   error
	calls int
	done  chan struct{}
}

func (f *fakeRenderer) Render(_ context.Context, _ string, _ int) (*image.RGBA, error) {
	f.calls++
	if f.done != nil {
		close(f.done)
	}
	return f.img, f.err
}

func webItem(id, link string) source.Item {
	return source.Item{ID: id, Source: source.HackerNews, Channel: "news", Title: "T", Body: "summary", Link: link}
}

func TestSelectPreviewRendersWebPage(t *testing.T) {
	a := New(Config{Registry: newReg()})
	page := image.NewRGBA(image.Rect(0, 0, 400, 1200))
	fr := &fakeRenderer{img: page}
	a.SetWebRenderer(fr)
	// Synchronous hook so the render + delivery happen inline (deterministic).
	a.SetWebFetchHook(func(id, url string, width int) { a.loadPreviewPage(context.Background(), id, url, width) })

	a.SelectPreview(webItem("h1", "https://example.com/a"))
	if fr.calls != 1 {
		t.Fatalf("renderer calls = %d, want 1", fr.calls)
	}
	if !a.Scene().HasWeb("h1") {
		t.Fatal("expected the rendered page to be stored for h1")
	}
	// Selecting it again does NOT re-render (already cached).
	a.SelectPreview(webItem("h1", "https://example.com/a"))
	if fr.calls != 1 {
		t.Fatalf("re-select re-rendered (calls=%d), want no new render", fr.calls)
	}
}

func TestSelectPreviewWebNoURL(t *testing.T) {
	a := New(Config{Registry: newReg()})
	fr := &fakeRenderer{img: image.NewRGBA(image.Rect(0, 0, 4, 4))}
	a.SetWebRenderer(fr)
	a.SetWebFetchHook(func(id, url string, width int) { a.loadPreviewPage(context.Background(), id, url, width) })
	// A text-only item with no external link → no web render.
	a.SelectPreview(source.Item{ID: "t", Source: source.HackerNews, Title: "text"})
	if fr.calls != 0 {
		t.Fatalf("no-URL item triggered a render (calls=%d)", fr.calls)
	}
}

func TestLoadPreviewPageError(t *testing.T) {
	a := New(Config{Registry: newReg()})
	a.SetWebRenderer(&fakeRenderer{err: errors.New("boom")})
	a.scene.SelectPreview(webItem("e1", "https://x"))
	a.scene.SetPreviewWebLoading(true)
	a.loadPreviewPage(context.Background(), "e1", "https://x", 400)
	if a.Scene().HasWeb("e1") {
		t.Fatal("a failed render must not store an image")
	}
	if a.Scene().WebLoading() {
		t.Fatal("a failed render should clear the web-loading flag (text fallback)")
	}
}

// TestWebFetchDefaultAsync exercises the default (unhooked) webFetch closure —
// the `go loadPreviewPage` line — with a fake renderer so no network is hit.
func TestWebFetchDefaultAsync(t *testing.T) {
	a := New(Config{Registry: newReg()})
	done := make(chan struct{})
	a.SetWebRenderer(&fakeRenderer{img: image.NewRGBA(image.Rect(0, 0, 8, 8)), done: done})
	a.DeferSceneWrites() // scene writes marshal onto the queue, not the goroutine
	a.SelectPreview(webItem("w", "https://x"))
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("default webFetch goroutine never rendered")
	}
	// Drain the queued SetPreviewWeb (posted right after Render returned).
	ok := false
	for i := 0; i < 200 && !ok; i++ {
		a.drainScene()
		ok = a.Scene().HasWeb("w")
		if !ok {
			time.Sleep(time.Millisecond)
		}
	}
	if !ok {
		t.Fatal("default async render did not deliver the page")
	}
}
