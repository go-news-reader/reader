// Package webrender renders a web page (the external target of a feed item)
// into an image, using the pure-Go go-webengine (CGO=0, no Chromium). The
// reader shows the result in its preview pane so a HackerNews / Reddit / RSS
// item's linked article can be read — and, via the returned link map, browsed —
// in-app.
package webrender

import (
	"context"
	"image"

	"github.com/go-webengine/engine"
)

// Link is one clickable anchor in a rendered page: Rect is its bounding box in
// the rendered image's pixel coordinates (at the width the page was rendered),
// and Href is the resolved absolute target URL.
type Link struct {
	Rect image.Rectangle
	Href string
}

// Renderer renders the page at url into a top-aligned RGBA image whose width is
// the given pixel width and whose height is however tall the content lays out.
// It also returns the page's clickable link map (for in-pane navigation) and
// the final URL after any redirects (the base a host resolves a "back" target
// against).
type Renderer interface {
	Render(ctx context.Context, url string, width int) (img *image.RGBA, links []Link, finalURL string, err error)
}

// defaultWidth is used when a caller passes a non-positive width.
const defaultWidth = 900

// minViewportH is a small starting viewport height; go-webengine expands the
// output to the content height, so this only bounds a very short page (which
// would otherwise be padded to a large fixed height).
const minViewportH = 120

// Engine is a Renderer backed by go-webengine. Its zero value is not usable;
// build one with New.
type Engine struct{ e *engine.Engine }

// New returns an Engine renderer using go-webengine's browser-like HTTP client
// (Chrome TLS fingerprint, cookie jar, redirect following) — the same
// go-browserhttp stack the reader's providers use.
func New() *Engine { return &Engine{e: engine.New()} }

// WithEngine wraps a pre-configured *engine.Engine (e.g. with a custom HTTP
// client, for tests or a shared client). A nil engine falls back to New's.
func WithEngine(e *engine.Engine) *Engine {
	if e == nil {
		return New()
	}
	return &Engine{e: e}
}

// Render fetches, lays out and paints url at width pixels wide, returning the
// page image, its clickable link map and the final (post-redirect) URL. A
// non-positive width uses a sane default.
func (r *Engine) Render(ctx context.Context, url string, width int) (*image.RGBA, []Link, string, error) {
	if width <= 0 {
		width = defaultWidth
	}
	img, info, elinks, err := r.e.RenderWithLinks(ctx, url, image.Rect(0, 0, width, minViewportH))
	if err != nil {
		return nil, nil, "", err
	}
	links := make([]Link, len(elinks))
	for i, l := range elinks {
		links[i] = Link{Rect: l.Rect, Href: l.Href}
	}
	finalURL := url
	if info != nil && info.URL != "" {
		finalURL = info.URL
	}
	return img, links, finalURL, nil
}
