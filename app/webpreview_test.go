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

// fakeProgressive is a webrender.ProgressiveRenderer (also a Renderer) that
// emits its frames in order — the last one Final — without touching the network.
type fakeProgressive struct {
	frames []*image.RGBA // emitted in order; last is the Final frame
	err    error
	calls  int
}

func (f *fakeProgressive) Render(_ context.Context, url string, _ int) (*image.RGBA, []webrender.Link, string, error) {
	if len(f.frames) == 0 {
		return nil, nil, url, f.err
	}
	return f.frames[len(f.frames)-1], nil, url, f.err
}

func (f *fakeProgressive) RenderProgressive(_ context.Context, url string, _ int, onFrame func(webrender.Frame)) error {
	f.calls++
	if f.err != nil {
		return f.err
	}
	for i, img := range f.frames {
		onFrame(webrender.Frame{Img: img, FinalURL: url, Final: i == len(f.frames)-1})
	}
	return nil
}

// TestLoadPreviewPageProgressive: a progressive renderer's frames (a styled
// first paint, then the final) are each delivered; the last clears loading.
func TestLoadPreviewPageProgressive(t *testing.T) {
	a := New(Config{Registry: newReg()})
	fp := &fakeProgressive{frames: []*image.RGBA{
		image.NewRGBA(image.Rect(0, 0, 400, 200)),  // initial styled first paint
		image.NewRGBA(image.Rect(0, 0, 400, 1200)), // final (taller once settled)
	}}
	a.SetWebRenderer(fp)
	syncFetch(a)

	a.SelectPreview(webItem("h1", "https://example.com/a"))
	if fp.calls != 1 {
		t.Fatalf("RenderProgressive calls = %d, want 1 (progressive path taken)", fp.calls)
	}
	if a.Scene().Browser().CurrentURL() != "https://example.com/a" {
		t.Fatalf("browser URL = %q", a.Scene().Browser().CurrentURL())
	}
	if a.Scene().Browser().Loading() {
		t.Fatal("the final frame should clear the loading state")
	}
}

// TestLoadPreviewPageProgressiveError: a progressive render that errors before
// any frame delivers an empty page, clearing the spinner.
func TestLoadPreviewPageProgressiveError(t *testing.T) {
	a := New(Config{Registry: newReg()})
	a.SetWebRenderer(&fakeProgressive{err: errors.New("boom")})
	syncFetch(a)
	a.Scene().Browser().Open("https://x", "T")
	if a.Scene().Browser().Loading() {
		t.Fatal("a failed progressive render should clear the loading state")
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

	// Initial preview opens the tab at the item's URL and renders it (miss #1).
	a.SelectPreview(webItem("h1", "https://site/"))
	if b.CurrentURL() != "https://site/" || b.CanBack() {
		t.Fatalf("after select: url=%q canBack=%v", b.CurrentURL(), b.CanBack())
	}
	// Navigate to a link → new page rendered (miss #2), Back now possible.
	b.Navigate("https://site/next")
	if fr.lastURL != "https://site/next" || !b.CanBack() {
		t.Fatalf("navigate: lastURL=%q canBack=%v", fr.lastURL, b.CanBack())
	}
	if fr.calls != 2 {
		t.Fatalf("render calls = %d, want 2 (one per distinct page)", fr.calls)
	}
	// Back → the already-rendered root is served from the render cache: the
	// history updates but the renderer is NOT invoked again (the point of the
	// cache). Back is exhausted at the root, Forward now open.
	b.Back()
	if b.CurrentURL() != "https://site/" || b.CanBack() || !b.CanForward() {
		t.Fatalf("back: url=%q canBack=%v canFwd=%v", b.CurrentURL(), b.CanBack(), b.CanForward())
	}
	// Forward → likewise served from the cache.
	b.Forward()
	if b.CurrentURL() != "https://site/next" || b.CanForward() {
		t.Fatalf("forward: url=%q canFwd=%v", b.CurrentURL(), b.CanForward())
	}
	if fr.calls != 2 {
		t.Fatalf("Back/Forward re-rendered (calls=%d), want the cache to serve them", fr.calls)
	}
	// Reload of a cached page is also served from the cache (no history change).
	depth := b.CanBack()
	b.Reload()
	if fr.calls != 2 || b.CanBack() != depth {
		t.Fatalf("reload: calls=%d history-changed=%v, want a cache hit", fr.calls, b.CanBack() != depth)
	}
	// Clearing the cache forces the next navigation to re-render.
	a.ClearRenderCache()
	b.Reload()
	if fr.calls != 3 || fr.lastURL != "https://site/next" {
		t.Fatalf("after clear+reload: calls=%d lastURL=%q, want a fresh render", fr.calls, fr.lastURL)
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
	a.Frame()                                         // a partial wait for b …
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

// TestLoadPreviewPageCachesAndReuses: the first render of a (url, width) is a
// cache miss that renders + stores; a second identical call is served from the
// cache without re-rendering; a different width misses and re-renders.
func TestLoadPreviewPageCachesAndReuses(t *testing.T) {
	a := New(Config{Registry: newReg()})
	a.SetWebFetchHook(func(string, int) {}) // Open must not auto-render
	fr := &fakeRenderer{img: image.NewRGBA(image.Rect(0, 0, 50, 60))}
	a.SetWebRenderer(fr)

	a.loadPreviewPage(context.Background(), "https://p/", 800) // miss → render #1
	if fr.calls != 1 {
		t.Fatalf("first render calls = %d, want 1", fr.calls)
	}
	if pages, _ := a.RenderCacheStats(); pages != 1 {
		t.Fatalf("final frame not cached: pages=%d", pages)
	}
	a.loadPreviewPage(context.Background(), "https://p/", 800) // hit → no render
	if fr.calls != 1 {
		t.Fatalf("cache hit re-rendered: calls=%d", fr.calls)
	}
	a.loadPreviewPage(context.Background(), "https://p/", 900) // different width → miss
	if fr.calls != 2 {
		t.Fatalf("different width should miss: calls=%d", fr.calls)
	}
	// Clearing forces the next identical call to re-render.
	a.ClearRenderCache()
	if pages, _ := a.RenderCacheStats(); pages != 0 {
		t.Fatalf("clear should empty the cache: pages=%d", pages)
	}
	a.loadPreviewPage(context.Background(), "https://p/", 800)
	if fr.calls != 3 {
		t.Fatalf("after clear the page should re-render: calls=%d", fr.calls)
	}
}

// TestCacheFinalSkipsEmpty covers cacheFinal's guards: a nil frame and a
// zero-pixel frame are never cached (an error/blank delivery must not poison the
// cache).
func TestCacheFinalSkipsEmpty(t *testing.T) {
	a := New(Config{Registry: newReg()})
	k := renderKey{"https://e/", 800}
	a.cacheFinal(k, nil, nil, "")                                   // nil image
	a.cacheFinal(k, image.NewRGBA(image.Rect(0, 0, 0, 0)), nil, "") // zero pixels
	if pages, _ := a.RenderCacheStats(); pages != 0 {
		t.Fatalf("empty frames were cached: pages=%d", pages)
	}
}

// TestPrefetchWarmsNeighbors: selecting a web item pre-renders its immediate
// feed neighbours into the cache (synchronous hook for determinism).
func TestPrefetchWarmsNeighbors(t *testing.T) {
	a := New(Config{Registry: newReg(), Width: 900, Height: 700})
	a.Scene().SetItems([]source.Item{
		webItem("a", "https://a/"), webItem("b", "https://b/"),
		webItem("c", "https://c/"), webItem("d", "https://d/"), webItem("e", "https://e/"),
	})
	fr := &fakeRenderer{img: image.NewRGBA(image.Rect(0, 0, 40, 40))}
	a.SetWebRenderer(fr)
	syncFetch(a)
	a.SetPrefetchHook(func(url string, width int) { a.doPrefetch(context.Background(), url, width) })

	// The browser's render width is only known once its pane has been drawn, so a
	// first selection then a Frame primes it (the very first navigation renders at
	// width 0, where prefetch correctly no-ops).
	a.SelectPreview(webItem("b", "https://b/"))
	a.Frame()

	// Select the middle item d → open it + warm its neighbours c and e.
	a.SelectPreview(webItem("d", "https://d/"))
	w := a.lastRenderWidth
	if w <= 0 {
		t.Fatalf("browser render width still unknown: %d", w)
	}
	if _, ok := a.rcache.get(renderKey{"https://c/", w}); !ok {
		t.Fatal("previous neighbour c was not prefetched")
	}
	if _, ok := a.rcache.get(renderKey{"https://e/", w}); !ok {
		t.Fatal("next neighbour e was not prefetched")
	}
}

// TestPrefetchSkipsCachedAndInflight: doPrefetch renders nothing for a page that
// is already cached or already being rendered.
func TestPrefetchSkipsCachedAndInflight(t *testing.T) {
	a := New(Config{Registry: newReg()})
	fr := &fakeRenderer{img: image.NewRGBA(image.Rect(0, 0, 10, 10))}
	a.SetWebRenderer(fr)

	// Already cached → skip.
	a.cacheFinal(renderKey{"https://x/", 800}, image.NewRGBA(image.Rect(0, 0, 10, 10)), nil, "")
	a.doPrefetch(context.Background(), "https://x/", 800)
	if fr.calls != 0 {
		t.Fatalf("prefetch re-rendered a cached page: calls=%d", fr.calls)
	}
	// Already in flight → skip.
	k := renderKey{"https://y/", 800}
	if !a.rcache.begin(k) {
		t.Fatal("begin should win")
	}
	a.doPrefetch(context.Background(), "https://y/", 800)
	if fr.calls != 0 {
		t.Fatalf("prefetch rendered an in-flight page: calls=%d", fr.calls)
	}
	a.rcache.done(k)
	// Now it renders.
	a.doPrefetch(context.Background(), "https://y/", 800)
	if fr.calls != 1 {
		t.Fatalf("prefetch of a free key should render: calls=%d", fr.calls)
	}
}

// TestPrefetchNeighborsNoWidth: with no page yet opened (no render width known),
// prefetch is a no-op.
func TestPrefetchNeighborsNoWidth(t *testing.T) {
	a := New(Config{Registry: newReg()})
	fr := &fakeRenderer{img: image.NewRGBA(image.Rect(0, 0, 4, 4))}
	a.SetWebRenderer(fr)
	a.SetPrefetchHook(func(url string, width int) { a.doPrefetch(context.Background(), url, width) })
	a.lastRenderWidth = 0
	a.prefetchNeighbors(webItem("b", "https://b/"))
	if fr.calls != 0 {
		t.Fatalf("prefetch with no known width should be a no-op: calls=%d", fr.calls)
	}
}

// TestPrefetchAsyncDefaultAndSaturation exercises the default (unhooked)
// background prefetch: a saturated worker pool skips without spawning, and a
// free pool renders into the cache on its own goroutine.
func TestPrefetchAsyncDefaultAndSaturation(t *testing.T) {
	a := New(Config{Registry: newReg()})
	fr := &fakeRenderer{img: image.NewRGBA(image.Rect(0, 0, 12, 12))}
	a.SetWebRenderer(fr)

	// Saturate the pool: prefetchFetch must skip (the non-blocking default branch).
	a.prefetchSem <- struct{}{}
	a.prefetchSem <- struct{}{}
	a.prefetchFetch("https://z/", 800)
	if _, ok := a.rcache.get(renderKey{"https://z/", 800}); ok {
		t.Fatal("a saturated pool should not prefetch")
	}
	<-a.prefetchSem
	<-a.prefetchSem

	// Free pool: the background render populates the cache.
	a.prefetchFetch("https://z/", 800)
	deadline := time.After(5 * time.Second)
	for {
		if _, ok := a.rcache.get(renderKey{"https://z/", 800}); ok {
			return
		}
		select {
		case <-deadline:
			t.Fatal("async prefetch never populated the cache")
		default:
			time.Sleep(time.Millisecond)
		}
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
	single := true
	set := &settings.Settings{
		Profiles: []settings.Profile{{Name: "Home"}}, Active: 0,
		Theme: settings.ThemeSystem, BrowserSingleTab: &single,
	}
	a := New(Config{Registry: newReg(), Settings: set})
	if !a.Scene().BrowserSingleTab() {
		t.Fatal("BrowserSingleTab from settings should apply to the scene")
	}
}

// TestBrowserDefaultSingleTab covers the changed default: settings with an unset
// tab-mode preference (a fresh install) apply as single-tab, so the preview
// shows no tab strip out of the box.
func TestBrowserDefaultSingleTab(t *testing.T) {
	set := &settings.Settings{
		Profiles: []settings.Profile{{Name: "Home"}}, Active: 0,
		Theme: settings.ThemeSystem, // BrowserSingleTab left nil (unset)
	}
	a := New(Config{Registry: newReg(), Settings: set})
	if !a.Scene().BrowserSingleTab() {
		t.Fatal("a fresh install (unset preference) should default to single-tab")
	}
}

// TestBookmarkPersistence covers the address-bar bookmark toggle: it persists
// the current page's URL through the store, and OnNavigate syncs the star.
func TestBookmarkPersistence(t *testing.T) {
	dir := t.TempDir()
	store := settings.NewStore(dir + "/settings.json")
	a := New(Config{Registry: newReg(), Store: store, Width: 900, Height: 600})
	syncFetch(a)
	a.SelectPreview(webItem("h1", "https://site/")) // Open → OnNavigate(SyncBookmarkStar)+fetch
	if a.Scene().Browser().Bookmarked {
		t.Fatal("a freshly opened page should not be bookmarked")
	}
	// Toggle on via the widget's hook → persisted.
	a.Scene().Browser().OnBookmarkToggle(true)
	saved, err := store.Load()
	if err != nil || len(saved.Bookmarks) != 1 || saved.Bookmarks[0] != "https://site/" {
		t.Fatalf("persist on: err=%v bookmarks=%v", err, saved.Bookmarks)
	}
	// Re-navigating (Reload) syncs the star to the bookmarked state.
	a.Scene().Browser().Reload()
	if !a.Scene().Browser().Bookmarked {
		t.Fatal("SyncBookmarkStar after reload should reflect the bookmark")
	}
	// Toggle off → removed from the store.
	a.Scene().Browser().OnBookmarkToggle(false)
	if saved2, _ := store.Load(); len(saved2.Bookmarks) != 0 {
		t.Fatalf("persist off: %v", saved2.Bookmarks)
	}
	// store == nil: toggling is a safe no-op (no persistence, no panic).
	a2 := New(Config{Registry: newReg(), Width: 900, Height: 600})
	syncFetch(a2)
	a2.SelectPreview(webItem("x", "https://x/"))
	a2.Scene().Browser().OnBookmarkToggle(true)
}
