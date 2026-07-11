// Package app wires the aggregator together: it aggregates a set of
// subscriptions across the provider registry and renders the unified feed into
// the go-widgets UI scene. It is windowing-agnostic — the same App is driven by
// the CLI (render to PNG / serve) and, later, by a native or webview front-end.
package app

import (
	"bytes"
	"context"
	"image"
	"image/png"

	"github.com/go-news-reader/reader/source"
	"github.com/go-news-reader/reader/ui"
)

// App holds the runtime state: the provider registry, the user's subscriptions,
// and the UI scene they render into.
type App struct {
	reg   *source.Registry
	subs  []source.Subscription
	scene *ui.Scene

	// Double-buffered present state (see Frame): two framebuffers plus the
	// last-presented damage sequence, so a window/canvas front-end only redraws
	// and uploads when the scene actually changed.
	bufs    [2][]byte
	cur     int
	lastRev int
}

// Config configures a new App.
type Config struct {
	Registry      *source.Registry
	Subscriptions []source.Subscription
	Width, Height int
	OS            string // ui.OSMac | ui.OSLinux | ui.OSWindows
	Dark          bool
}

// New builds an App with an empty scene themed for the given OS.
func New(cfg Config) *App {
	w, h := cfg.Width, cfg.Height
	if w == 0 {
		w = 1000
	}
	if h == 0 {
		h = 700
	}
	scene := ui.New(w, h, ui.ThemeFor(cfg.OS, cfg.Dark))
	scene.SetSubs(toUISubs(cfg.Subscriptions))
	return &App{reg: cfg.Registry, subs: cfg.Subscriptions, scene: scene, lastRev: -1}
}

// Frame returns the current framebuffer, redrawing into the back buffer only
// when the scene's damage sequence has advanced since the last call (the
// Wayland commit-seq / Evas dirty model). changed reports whether a new frame
// was produced; a window/canvas front-end uploads the buffer only when changed.
// The buffer is s.W*s.H*4 RGBA bytes and is reused across frames (double
// buffered), so callers must not retain it past the next Frame.
func (a *App) Frame() (buf []byte, changed bool) {
	s := a.scene
	size := s.W * s.H * 4
	if len(a.bufs[0]) != size {
		a.bufs[0] = make([]byte, size)
		a.bufs[1] = make([]byte, size)
		a.lastRev = -1 // force a redraw after a resize
	}
	if s.Rev() == a.lastRev {
		return a.bufs[a.cur], false
	}
	back := 1 - a.cur
	s.Draw(a.bufs[back])
	a.cur = back
	a.lastRev = s.Rev()
	return a.bufs[a.cur], true
}

// Scene exposes the scene so front-ends can dispatch input to it.
func (a *App) Scene() *ui.Scene { return a.scene }

// Refresh aggregates every subscription (concurrently, newest-first) and loads
// the merged items into the scene. Per-subscription failures are returned but
// do not prevent the surviving items from being shown.
func (a *App) Refresh(ctx context.Context) []error {
	items, errs := a.reg.Aggregate(ctx, a.subs)
	a.scene.SetItems(items)
	if len(errs) > 0 {
		a.scene.Status = errs[0].Error()
	} else {
		a.scene.Status = ""
	}
	return errs
}

// Items returns the currently loaded feed.
func (a *App) Items() []source.Item { return a.scene.Items }

// RenderPNG draws the current scene and encodes it as PNG.
func (a *App) RenderPNG() ([]byte, error) {
	s := a.scene
	buf := make([]byte, s.W*s.H*4)
	s.Draw(buf)
	img := &image.RGBA{Pix: buf, Stride: s.W * 4, Rect: image.Rect(0, 0, s.W, s.H)}
	var out bytes.Buffer
	if err := encodePNG(&out, img); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// encodePNG is a package var so tests can force the (otherwise-untriggerable)
// encode-error branch.
var encodePNG = png.Encode

// SetTheme reselects the palette (e.g. on a system light/dark change).
func (a *App) SetTheme(osName string, dark bool) {
	a.scene.SetTheme(ui.ThemeFor(osName, dark))
}

// toUISubs maps source subscriptions to sidebar entries.
func toUISubs(subs []source.Subscription) []ui.Subscription {
	out := make([]ui.Subscription, 0, len(subs))
	for _, s := range subs {
		out = append(out, ui.Subscription{Source: s.Source, Channel: s.Channel})
	}
	return out
}
