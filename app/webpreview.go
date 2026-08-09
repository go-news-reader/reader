package app

import (
	"context"
	"image"

	"github.com/go-widgets/toolkit"

	"github.com/go-news-reader/reader/internal/webrender"
)

// loadPreviewPage renders target (via webRender) and delivers the render into
// the embedded browser on the UI thread (through post). It is the body of the
// browser's OnNavigate seam: the widget marks the tab loading and calls
// OnNavigate, this renders off-thread, and Deliver clears the loading state when
// the page lands.
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
	// deliver hands one render to the browser on the UI thread. final marks the
	// last frame: an intermediate progressive frame (final=false) keeps the
	// loading indicator animating and refines in place, so the staged render
	// (first paint → refine → final) is visibly progressive rather than snapping
	// to "done" on the first frame.
	deliver := func(img *image.RGBA, links []webrender.Link, final bool) {
		bl := toBrowserLinks(links)
		var pix []byte
		var iw, ih int
		if img != nil {
			bnd := img.Bounds()
			iw, ih, pix = bnd.Dx(), bnd.Dy(), img.Pix
		}
		a.post(func() {
			b := a.scene.Browser()
			// Preserve the tab title the widget already carries (seeded from the feed
			// item) — the renderer does not extract a page <title>.
			b.DeliverStage(target, pix, iw, ih, width, bl, b.ActiveTitle(), final)
		})
	}
	if pr, ok := a.webRender.(webrender.ProgressiveRenderer); ok {
		// Progressive: deliver every staged frame (first paint → refine → final),
		// keeping the pane in its loading state until the final frame lands.
		if err := pr.RenderProgressive(ctx, target, width, func(f webrender.Frame) {
			deliver(f.Img, f.Links, f.Final)
		}); err != nil {
			deliver(nil, nil, true) // clear the spinner on a fetch failure (no frames)
		}
		return
	}
	// Non-progressive fallback: one render, one (final) delivery.
	img, links, _, err := a.webRender.Render(ctx, target, width)
	if err != nil {
		deliver(nil, nil, true)
		return
	}
	deliver(img, links, true)
}

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
