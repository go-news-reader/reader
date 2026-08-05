package app

import (
	"context"

	"github.com/go-news-reader/reader/internal/webrender"
	"github.com/go-news-reader/reader/ui"
)

// loadPreviewPage renders the item's target page (via webRender) and delivers
// the image + its clickable link map to the preview pane on the UI thread. On a
// render error it delivers nil, which clears the pane's web-loading state so it
// falls back to the text summary. The default webFetch runs this on its own
// goroutine; tests call it directly for determinism.
func (a *App) loadPreviewPage(ctx context.Context, id, url string, width int) {
	img, links, _, err := a.webRender.Render(ctx, url, width)
	if err != nil {
		a.post(func() { a.scene.SetPreviewWeb(id, nil, nil, 0) })
		return
	}
	sl := toSceneLinks(links)
	a.post(func() { a.scene.SetPreviewWeb(id, img, sl, width) })
}

// toSceneLinks converts webrender links to the UI's link type (the app is the
// only place that depends on both packages).
func toSceneLinks(links []webrender.Link) []ui.WebLink {
	if len(links) == 0 {
		return nil
	}
	out := make([]ui.WebLink, len(links))
	for i, l := range links {
		out[i] = ui.WebLink{Rect: l.Rect, Href: l.Href}
	}
	return out
}

// NavigateWeb follows a clicked link in the preview's web view: it records the
// jump on the item's back-stack and re-renders the target page in place.
func (a *App) NavigateWeb(id, href string) {
	if href == "" {
		return
	}
	a.scene.PushWebURL(id, href)
	a.scene.SetPreviewWebLoading(true)
	a.webFetch(id, href, a.scene.PreviewWebWidth())
}

// WebBack navigates the preview's web view to the previous page in the item's
// history. A no-op when there is nothing to go back to.
func (a *App) WebBack(id string) {
	url, ok := a.scene.WebBackURL(id)
	if !ok {
		return
	}
	a.scene.SetPreviewWebLoading(true)
	a.webFetch(id, url, a.scene.PreviewWebWidth())
}

// WebForward navigates the preview's web view to the next page in the item's
// history (after one or more Backs). A no-op when there is nothing forward.
func (a *App) WebForward(id string) {
	url, ok := a.scene.WebForwardURL(id)
	if !ok {
		return
	}
	a.scene.SetPreviewWebLoading(true)
	a.webFetch(id, url, a.scene.PreviewWebWidth())
}

// WebReload re-renders the page currently shown in the preview's web view
// without touching the history. A no-op when no page is shown.
func (a *App) WebReload(id string) {
	url := a.scene.CurrentWebURL(id)
	if url == "" {
		return
	}
	a.scene.SetPreviewWebLoading(true)
	a.webFetch(id, url, a.scene.PreviewWebWidth())
}

// CommitWebURL navigates the preview's web view to the URL typed into the
// address field (Enter). A no-op when the field is empty.
func (a *App) CommitWebURL(id string) {
	url, ok := a.scene.CommitWebURL()
	if !ok {
		return
	}
	a.NavigateWeb(id, url)
}

// SetWebFetchHook overrides the async page render (tests use a synchronous
// variant for determinism).
func (a *App) SetWebFetchHook(f func(id, url string, width int)) { a.webFetch = f }

// SetWebRenderer overrides the page renderer (tests inject a fake that returns a
// canned image without touching the network).
func (a *App) SetWebRenderer(r webrender.Renderer) { a.webRender = r }
