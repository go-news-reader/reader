// Package webrender renders a web page (the external target of a feed item)
// into an image, using the pure-Go go-webengine (CGO=0, no Chromium). The
// reader shows the result in its preview pane so a HackerNews / Reddit / RSS
// item's linked article can be read in-app.
package webrender

import (
	"context"
	"image"

	"github.com/go-webengine/engine"
)

// Renderer renders the page at url into a top-aligned RGBA image whose width is
// the given pixel width and whose height is however tall the content lays out.
type Renderer interface {
	Render(ctx context.Context, url string, width int) (*image.RGBA, error)
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
// page image (height = laid-out content height). A non-positive width uses a
// sane default.
func (r *Engine) Render(ctx context.Context, url string, width int) (*image.RGBA, error) {
	if width <= 0 {
		width = defaultWidth
	}
	img, _, err := r.e.Render(ctx, url, image.Rect(0, 0, width, minViewportH))
	if err != nil {
		return nil, err
	}
	return img, nil
}
