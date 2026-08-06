package app

import (
	"context"
	"errors"
	"image"
	"testing"
	"time"

	"github.com/go-news-reader/reader/internal/settings"
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

// syncFetch wires a synchronous render so Open → OnNavigate → webFetch →
// loadPreviewPage all run inline (deterministic, no goroutine, no network).
func syncFetch(a *App) {
	a.SetWebFetchHook(func(target string, width int) { a.loadPreviewPage(context.Background(), target, width) })
}

func TestSelectPreviewRendersWebPage(t *testing.T) {
	a := New(Config{Registry: newReg()})
	page := image.NewRGBA(image.Rect(0, 0, 400, 1200))
	fr := &fakeRenderer{img: page}
	a.SetWebRenderer(fr)
	syncFetch(a)

	a.SelectPreview(webItem("h1", "https://example.com/a"))
	if fr.calls != 1 {
		t.Fatalf("renderer calls = %d, want 1", fr.calls)
	}
	if a.Scene().Browser().CurrentURL() != "https://example.com/a" {
		t.Fatalf("browser URL = %q", a.Scene().Browser().CurrentURL())
	}
	if a.Scene().Browser().Loading() {
		t.Fatal("a delivered page should clear loading")
	}
	// Selecting the same page again does NOT re-open / re-render (URL unchanged).
	a.SelectPreview(webItem("h1", "https://example.com/a"))
	if fr.calls != 1 {
		t.Fatalf("re-select re-rendered (calls=%d), want no new render", fr.calls)
	}
}

func TestSelectPreviewWebNoURL(t *testing.T) {
	a := New(Config{Registry: newReg()})
	fr := &fakeRenderer{img: image.NewRGBA(image.Rect(0, 0, 4, 4))}
	a.SetWebRenderer(fr)
	syncFetch(a)
	// A text-only item with no external link → no web render.
	a.SelectPreview(source.Item{ID: "t", Source: source.HackerNews, Title: "text"})
	if fr.calls != 0 {
		t.Fatalf("no-URL item triggered a render (calls=%d)", fr.calls)
	}
}

func TestLoadPreviewPageError(t *testing.T) {
	a := New(Config{Registry: newReg()})
	a.SetWebRenderer(&fakeRenderer{err: errors.New("boom")})
	syncFetch(a) // render inline via OnNavigate — deterministic, single goroutine
	// Opening a tab triggers the (failing) render, which delivers an empty page
	// for the target, clearing the loading state rather than spinning forever.
	a.Scene().Browser().Open("https://x", "T")
	if a.Scene().Browser().Loading() {
		t.Fatal("a failed render should clear the loading state")
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
	// Drain the queued Deliver (posted right after Render returned).
	ok := false
	for i := 0; i < 200 && !ok; i++ {
		a.drainScene()
		ok = !a.Scene().Browser().Loading() && a.Scene().Browser().CurrentURL() == "https://x"
		if !ok {
			time.Sleep(time.Millisecond)
		}
	}
	if !ok {
		t.Fatal("default async render did not deliver the page")
	}
}

// TestWebNavigateAndBack drives in-pane link navigation, Back/Forward and Reload
// through the embedded browser (its commands invoke OnNavigate → the sync fetch).
func TestWebNavigateAndBack(t *testing.T) {
	a := New(Config{Registry: newReg()})
	fr := &fakeRenderer{img: image.NewRGBA(image.Rect(0, 0, 400, 800)),
		links: []webrender.Link{{Rect: image.Rect(0, 0, 50, 20), Href: "https://site/next"}}}
	a.SetWebRenderer(fr)
	syncFetch(a)
	b := a.Scene().Browser()

	// Initial preview opens the tab at the item's URL and renders it.
	a.SelectPreview(webItem("h1", "https://site/"))
	if b.CurrentURL() != "https://site/" || b.CanBack() {
		t.Fatalf("after select: url=%q canBack=%v", b.CurrentURL(), b.CanBack())
	}
	// Navigate to a link → new page rendered, Back now possible.
	b.Navigate("https://site/next")
	if fr.lastURL != "https://site/next" || !b.CanBack() {
		t.Fatalf("navigate: lastURL=%q canBack=%v", fr.lastURL, b.CanBack())
	}
	// Back → previous URL re-rendered; Back exhausted at the root, Forward now open.
	b.Back()
	if fr.lastURL != "https://site/" || b.CanBack() || !b.CanForward() {
		t.Fatalf("back: lastURL=%q canBack=%v canFwd=%v", fr.lastURL, b.CanBack(), b.CanForward())
	}
	// Forward → the page we came back from is re-rendered.
	b.Forward()
	if fr.lastURL != "https://site/next" || b.CanForward() {
		t.Fatalf("forward: lastURL=%q canFwd=%v", fr.lastURL, b.CanForward())
	}
	// Reload re-renders the current page (a fetch, no history change).
	cur := b.CurrentURL()
	before, depth := fr.calls, b.CanBack()
	b.Reload()
	if fr.calls != before+1 || fr.lastURL != cur || b.CanBack() != depth {
		t.Fatalf("reload: calls=%d lastURL=%q history-changed=%v", fr.calls, fr.lastURL, b.CanBack() != depth)
	}
}

// TestWebFetchDebounce covers the keyboard-navigation debounce: an armed page
// open fires only after the selection has settled for webDebounceFrames ticks,
// and rapid re-selection fires only the last one.
func TestWebFetchDebounce(t *testing.T) {
	a := New(Config{Registry: newReg(), Width: 800, Height: 600})
	a.SetWebRenderer(&fakeRenderer{img: image.NewRGBA(image.Rect(0, 0, 400, 800))})
	calls := 0
	lastURL := ""
	a.SetWebFetchHook(func(target string, width int) { calls++; lastURL = target })

	// Debounced select arms but does not open/fetch immediately.
	a.selectPreview(webItem("a", "https://a/"), true)
	if calls != 0 || !a.webArmed {
		t.Fatalf("after debounced select: armed=%v calls=%d, want true,0", a.webArmed, calls)
	}
	// Ticks below the threshold keep it armed, unfired.
	for i := uint64(0); i < a.webDebounceFrames-1; i++ {
		a.Frame()
	}
	if calls != 0 {
		t.Fatalf("fired before settling: calls=%d", calls)
	}
	// The tick that crosses the threshold opens (fires) exactly once.
	a.Frame()
	if calls != 1 || a.webArmed || lastURL != "https://a/" {
		t.Fatalf("after threshold: calls=%d armed=%v last=%q", calls, a.webArmed, lastURL)
	}

	// Rapid re-selection resets the deadline; only the final item settles.
	a.selectPreview(webItem("b", "https://b/"), true)
	a.Frame() // a partial wait for b …
	a.selectPreview(webItem("c", "https://c/"), true) // … superseded by c
	before := calls
	for i := uint64(0); i < a.webDebounceFrames; i++ {
		a.Frame()
	}
	if calls != before+1 || lastURL != "https://c/" {
		t.Fatalf("rapid nav: fired %d times last=%q, want 1 and c", calls-before, lastURL)
	}

	// A direct click (debounceWeb=false) opens immediately.
	before = calls
	a.selectPreview(webItem("d", "https://d/"), false)
	if calls != before+1 || a.webArmed {
		t.Fatalf("direct select: calls=%d armed=%v, want immediate open", calls-before, a.webArmed)
	}
}

// TestToBrowserLinks covers the empty and populated link-conversion paths.
func TestToBrowserLinks(t *testing.T) {
	if toBrowserLinks(nil) != nil {
		t.Fatal("no links → nil")
	}
	out := toBrowserLinks([]webrender.Link{{Rect: image.Rect(1, 2, 3, 4), Href: "https://x"}})
	if len(out) != 1 || out[0].Href != "https://x" || out[0].Rect != image.Rect(1, 2, 3, 4) {
		t.Fatalf("converted = %+v", out)
	}
}

// TestBrowserSingleTabConfig covers applying the persisted single-tab preference
// at construction.
func TestBrowserSingleTabConfig(t *testing.T) {
	set := &settings.Settings{
		Profiles: []settings.Profile{{Name: "Home"}}, Active: 0,
		Theme: settings.ThemeSystem, BrowserSingleTab: true,
	}
	a := New(Config{Registry: newReg(), Settings: set})
	if !a.Scene().BrowserSingleTab() {
		t.Fatal("BrowserSingleTab from settings should apply to the scene")
	}
}
