package app

import (
	"context"
	"errors"
	"image"
	"testing"
	"time"

	"github.com/go-news-reader/reader/internal/webrender"
	"github.com/go-news-reader/reader/source"
)

// fakeRenderer is a webrender.Renderer that returns a canned image/links/error
// without touching the network; done (when non-nil) is closed as Render returns
// so a test can await the async default webFetch goroutine. lastURL records the
// most recent URL rendered (to assert navigation targets).
type fakeRenderer struct {
	img     *image.RGBA
	links   []webrender.Link
	err     error
	calls   int
	lastURL string
	done    chan struct{}
}

func (f *fakeRenderer) Render(_ context.Context, url string, _ int) (*image.RGBA, []webrender.Link, string, error) {
	f.calls++
	f.lastURL = url
	if f.done != nil {
		close(f.done)
	}
	return f.img, f.links, url, f.err
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

// TestWebNavigateAndBack exercises in-pane link navigation + Back through the
// synchronous hook (deterministic).
func TestWebNavigateAndBack(t *testing.T) {
	a := New(Config{Registry: newReg()})
	fr := &fakeRenderer{img: image.NewRGBA(image.Rect(0, 0, 400, 800)),
		links: []webrender.Link{{Rect: image.Rect(0, 0, 50, 20), Href: "https://site/next"}}}
	a.SetWebRenderer(fr)
	a.SetWebFetchHook(func(id, url string, width int) { a.loadPreviewPage(context.Background(), id, url, width) })

	// Initial preview seeds the history at the item's URL and renders it.
	a.SelectPreview(webItem("h1", "https://site/"))
	if !a.Scene().HasWeb("h1") || a.Scene().WebCanBack("h1") {
		t.Fatalf("after select: has=%v canBack=%v", a.Scene().HasWeb("h1"), a.Scene().WebCanBack("h1"))
	}
	// Navigate to a link → new page rendered, Back now possible.
	a.NavigateWeb("h1", "https://site/next")
	if fr.lastURL != "https://site/next" || !a.Scene().WebCanBack("h1") {
		t.Fatalf("navigate: lastURL=%q canBack=%v", fr.lastURL, a.Scene().WebCanBack("h1"))
	}
	// Empty href is ignored.
	before := fr.calls
	a.NavigateWeb("h1", "")
	if fr.calls != before {
		t.Fatal("empty href should not render")
	}
	// Back → previous URL re-rendered, Back no longer possible (at the root),
	// but Forward now is.
	a.WebBack("h1")
	if fr.lastURL != "https://site/" || a.Scene().WebCanBack("h1") || !a.Scene().WebCanForward("h1") {
		t.Fatalf("back: lastURL=%q canBack=%v canFwd=%v", fr.lastURL, a.Scene().WebCanBack("h1"), a.Scene().WebCanForward("h1"))
	}
	// Back at the root is a no-op.
	before = fr.calls
	a.WebBack("h1")
	if fr.calls != before {
		t.Fatal("back at root should be a no-op")
	}
	// Forward → the page we came back from is re-rendered.
	a.WebForward("h1")
	if fr.lastURL != "https://site/next" || a.Scene().WebCanForward("h1") {
		t.Fatalf("forward: lastURL=%q canFwd=%v", fr.lastURL, a.Scene().WebCanForward("h1"))
	}
	// Forward at the tip is a no-op.
	before = fr.calls
	a.WebForward("h1")
	if fr.calls != before {
		t.Fatal("forward at tip should be a no-op")
	}
}
