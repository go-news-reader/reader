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

// Frame is one progressive render result: a fully styled snapshot the reader
// can show immediately. The first frame (a styled first paint) arrives well
// before Final, then refinements, then the Final frame (identical to what
// [Renderer.Render] would return). Img is an independent allocation per frame.
type Frame struct {
	Img      *image.RGBA
	Links    []Link
	FinalURL string
	Final    bool
}

// ProgressiveRenderer is an optional capability: it renders in stages, calling
// onFrame for each (a fast styled first paint, then refinements, then the final
// frame), so the host can display content progressively instead of waiting for
// the whole page. onFrame always fires at least once; the last call has
// Final==true. A renderer that lacks this capability delivers only the final
// image via [Renderer.Render].
type ProgressiveRenderer interface {
	RenderProgressive(ctx context.Context, url string, width int, onFrame func(Frame)) error
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
// go-browserhttp stack the reader's providers use. MetaFallback is enabled so a
// page the engine can't hydrate (a JS-only SPA — Mastodon, X, …) renders a
// readable OpenGraph card (title + text + image) instead of a blank pane; the
// fallback only triggers when the real render is empty, so normal pages are
// unaffected.
func New() *Engine {
	e := engine.New()
	e.MetaFallback = true
	return &Engine{e: e}
}

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

// RenderProgressive renders url at width pixels wide in stages, invoking onFrame
// for each styled frame as it becomes available (a fast first paint, then
// refinements, then the final frame). It satisfies [ProgressiveRenderer]. A
// non-positive width uses the default.
func (r *Engine) RenderProgressive(ctx context.Context, url string, width int, onFrame func(Frame)) error {
	if width <= 0 {
		width = defaultWidth
	}
	_, err := r.e.RenderProgressive(ctx, url, image.Rect(0, 0, width, minViewportH), func(f engine.ProgressiveFrame) {
		links := make([]Link, len(f.Links))
		for i, l := range f.Links {
			links[i] = Link{Rect: l.Rect, Href: l.Href}
		}
		finalURL := url
		if f.Info != nil && f.Info.URL != "" {
			finalURL = f.Info.URL
		}
		onFrame(Frame{Img: f.Img, Links: links, FinalURL: finalURL, Final: f.Final})
	})
	return err
}
