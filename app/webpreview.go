package app

import (
	"context"

	"github.com/go-news-reader/reader/internal/webrender"
)

// loadPreviewPage renders the item's target page (via webRender) and delivers
// the image to the preview pane on the UI thread. On a render error it delivers
// nil, which clears the pane's web-loading state so it falls back to the text
// summary. The default webFetch runs this on its own goroutine; tests call it
// directly for determinism.
func (a *App) loadPreviewPage(ctx context.Context, id, url string, width int) {
	img, err := a.webRender.Render(ctx, url, width)
	if err != nil {
		a.post(func() { a.scene.SetPreviewWeb(id, nil) })
		return
	}
	a.post(func() { a.scene.SetPreviewWeb(id, img) })
}

// SetWebFetchHook overrides the async page render (tests use a synchronous
// variant for determinism).
func (a *App) SetWebFetchHook(f func(id, url string, width int)) { a.webFetch = f }

// SetWebRenderer overrides the page renderer (tests inject a fake that returns a
// canned image without touching the network).
func (a *App) SetWebRenderer(r webrender.Renderer) { a.webRender = r }
