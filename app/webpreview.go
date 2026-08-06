package app

import (
	"context"

	"github.com/go-widgets/toolkit"

	"github.com/go-news-reader/reader/internal/webrender"
)

// loadPreviewPage renders target (via webRender) and delivers the finished
// render — pixels, dimensions, clickable link map and title — into the embedded
// browser on the UI thread (through post). It is the body of the browser's
// OnNavigate seam: the widget marks the tab loading and calls OnNavigate, this
// renders off-thread, and Deliver clears the loading state when the page lands.
// A render error delivers an empty page for the same target, which clears the
// spinner (the tab shows blank rather than spinning forever). The default
// webFetch runs this on its own goroutine; tests call it directly for
// determinism.
func (a *App) loadPreviewPage(ctx context.Context, target string, width int) {
	img, links, _, err := a.webRender.Render(ctx, target, width)
	if err != nil {
		a.post(func() {
			b := a.scene.Browser()
			b.Deliver(target, nil, 0, 0, width, nil, b.ActiveTitle())
		})
		return
	}
	bl := toBrowserLinks(links)
	bnd := img.Bounds()
	iw, ih := bnd.Dx(), bnd.Dy()
	a.post(func() {
		b := a.scene.Browser()
		// Preserve the tab title the widget already carries (seeded from the feed
		// item) — the renderer does not extract a page <title>.
		b.Deliver(target, img.Pix, iw, ih, width, bl, b.ActiveTitle())
	})
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
