package app

import (
	"context"
	"image"

	"github.com/go-widgets/toolkit"

	"github.com/go-news-reader/reader/internal/webrender"
	"github.com/go-news-reader/reader/source"
)

// loadPreviewPage renders target (via webRender) and delivers the render into
// the embedded browser on the UI thread (through post). It is the body of the
// browser's OnNavigate seam: the widget marks the tab loading and calls
// OnNavigate, this renders off-thread, and Deliver clears the loading state when
// the page lands.
//
// A previously rendered (url, width) is served from the render cache without
// touching the renderer at all: the stored final frame is delivered as a single
// final DeliverStage, so re-selecting or navigating back to a page is instant.
// On a cache miss the page is rendered as usual; when the final frame arrives it
// is copied into the cache for next time.
//
// When the renderer is progressive (the default go-webengine one), each staged
// frame is delivered as it arrives — a fast styled first paint, then
// refinements, then the final frame — so the pane shows content well before the
// whole page (scripts, all resources) finishes instead of staying blank. A
// plain renderer (or a render error) delivers a single frame; an error delivers
// an empty page for the same target so the spinner clears rather than spinning
// forever. The default webFetch runs this on its own goroutine; tests call it
// directly for determinism.
func (a *App) loadPreviewPage(ctx context.Context, target string, width int) {
	key := renderKey{url: target, width: width}
	// Cache hit: deliver the stored final frame and return without rendering.
	if e, ok := a.rcache.get(key); ok {
		links := toBrowserLinks(e.links)
		a.post(func() {
			b := a.scene.Browser()
			b.DeliverStage(target, e.pix, e.iw, e.ih, width, links, b.ActiveTitle(), true)
		})
		return
	}
	// deliver hands one render to the browser on the UI thread. final marks the
	// last frame: an intermediate progressive frame (final=false) keeps the
	// loading indicator animating and refines in place, so the staged render
	// (first paint → refine → final) is visibly progressive rather than snapping
	// to "done" on the first frame. The final frame is also stored in the cache.
	a.renderFrames(ctx, target, width, func(img *image.RGBA, links []webrender.Link, finalURL string, final bool) {
		bl := toBrowserLinks(links)
		var pix []byte
		var iw, ih int
		if img != nil {
			bnd := img.Bounds()
			iw, ih, pix = bnd.Dx(), bnd.Dy(), img.Pix
		}
		if final {
			a.cacheFinal(key, img, links, finalURL)
		}
		a.post(func() {
			b := a.scene.Browser()
			// Preserve the tab title the widget already carries (seeded from the feed
			// item) — the renderer does not extract a page <title>.
			b.DeliverStage(target, pix, iw, ih, width, bl, b.ActiveTitle(), final)
		})
	})
}

// renderFrames renders target at width and invokes onFrame for each frame: every
// staged frame of a progressive renderer (the last with final=true), or the one
// frame of a plain renderer. A render error delivers a single empty final frame
// (img nil) so the caller can clear the loading state; onFrame always fires at
// least once. It is the shared render step behind both the live preview and the
// background prefetch.
func (a *App) renderFrames(ctx context.Context, target string, width int, onFrame func(img *image.RGBA, links []webrender.Link, finalURL string, final bool)) {
	if pr, ok := a.webRender.(webrender.ProgressiveRenderer); ok {
		if err := pr.RenderProgressive(ctx, target, width, func(f webrender.Frame) {
			onFrame(f.Img, f.Links, f.FinalURL, f.Final)
		}); err != nil {
			onFrame(nil, nil, "", true) // clear the spinner on a fetch failure (no frames)
		}
		return
	}
	// Non-progressive fallback: one render, one (final) delivery.
	img, links, finalURL, err := a.webRender.Render(ctx, target, width)
	if err != nil {
		onFrame(nil, nil, "", true)
		return
	}
	onFrame(img, links, finalURL, true)
}

// cacheFinal copies a rendered final frame into the render cache under key. A
// nil/empty frame (an error or blank delivery) is never cached. The pixels are
// copied so the cache owns its bytes independent of the renderer's frame buffer.
func (a *App) cacheFinal(key renderKey, img *image.RGBA, links []webrender.Link, finalURL string) {
	if img == nil {
		return
	}
	bnd := img.Bounds()
	if len(img.Pix) == 0 {
		return
	}
	pix := make([]byte, len(img.Pix))
	copy(pix, img.Pix)
	var lcopy []webrender.Link
	if len(links) > 0 {
		lcopy = append(lcopy, links...)
	}
	a.rcache.put(key, &renderEntry{
		pix: pix, iw: bnd.Dx(), ih: bnd.Dy(), links: lcopy, finalURL: finalURL,
	})
}

// prefetchNeighbors kicks off background renders of a small, bounded set of
// likely-next pages — the web-renderable feed items adjacent to it — into the
// render cache, so selecting one is instant. It reads the width the browser last
// rendered at (the pages will be requested at the same content width, so the
// warmed entries hit); before any page has been opened there is no width yet and
// it is a no-op. Never blocks the UI.
func (a *App) prefetchNeighbors(it source.Item) {
	w := a.lastRenderWidth
	if w <= 0 {
		return
	}
	for _, u := range a.scene.PrefetchWebURLs(it, prefetchCount) {
		a.prefetchFetch(u, w)
	}
}

// doPrefetch renders url at width into the cache, invisibly (no delivery to the
// browser). It skips a page that is already cached or already being rendered, so
// a prefetch never duplicates work.
func (a *App) doPrefetch(ctx context.Context, url string, width int) {
	key := renderKey{url: url, width: width}
	if !a.rcache.begin(key) {
		return // already cached or in flight
	}
	defer a.rcache.done(key)
	a.renderFrames(ctx, url, width, func(img *image.RGBA, links []webrender.Link, finalURL string, final bool) {
		if final {
			a.cacheFinal(key, img, links, finalURL)
		}
	})
}

// ClearRenderCache empties the web-preview render cache (freeing its pixels) and
// reports it on the status line. Wired to the Settings view's "Clear render
// cache" control.
func (a *App) ClearRenderCache() {
	a.rcache.clear()
	a.vmStatus("Render cache cleared")
}

// RenderCacheStats reports how many pages the render cache holds and their total
// pixel bytes (shown as the Settings caption).
func (a *App) RenderCacheStats() (pages, bytes int) { return a.rcache.stats() }

// toBrowserLinks converts webrender links to the toolkit.Browser's link type
// (the app is the only place that depends on both packages).
func toBrowserLinks(links []webrender.Link) []toolkit.BrowserLink {
	if len(links) == 0 {
		return nil
	}
	out := make([]toolkit.BrowserLink, len(links))
	for i, l := range links {
		out[i] = toolkit.BrowserLink{Rect: l.Rect, Href: l.Href}
	}
	return out
}

// SetWebFetchHook overrides the async page render (tests use a synchronous
// variant for determinism).
func (a *App) SetWebFetchHook(f func(target string, width int)) { a.webFetch = f }

// SetWebRenderer overrides the page renderer (tests inject a fake that returns a
// canned image without touching the network).
func (a *App) SetWebRenderer(r webrender.Renderer) { a.webRender = r }

// SetPrefetchHook overrides the background prefetch trigger (tests use a
// synchronous variant so a warmed neighbour is populated deterministically).
func (a *App) SetPrefetchHook(f func(url string, width int)) { a.prefetchFetch = f }
