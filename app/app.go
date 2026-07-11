// Package app wires the aggregator together: it aggregates the active profile's
// subscriptions across the provider registry and renders the unified feed into
// the go-widgets UI scene. It is windowing-agnostic — the same App is driven by
// the CLI (render to PNG / serve) and by the native window front-end.
package app

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"

	"github.com/go-news-reader/reader/internal/settings"
	"github.com/go-news-reader/reader/source"
	"github.com/go-news-reader/reader/ui"
)

// App holds the runtime state: the provider registry, the user's persisted
// settings (profiles/theme/cache), the active profile's subscriptions, and the
// UI scene they render into.
type App struct {
	reg   *source.Registry
	store *settings.Store // optional; nil disables persistence (CLI PNG/JSON/serve)
	set   *settings.Settings
	subs  []source.Subscription // == the active profile's subscriptions
	scene *ui.Scene

	osName string
	dark   bool

	// accent, when hasAccent, overrides the palette's accent with a value
	// harvested from the host UI (macOS controlAccentColor). Re-applied on every
	// theme rebuild so profile/theme switches keep the system accent.
	accent    color.RGBA
	hasAccent bool

	// refresh triggers an asynchronous re-aggregate after a settings change.
	// A field so front-ends/tests can substitute a synchronous variant.
	refresh func()

	// Double-buffered present state (see Frame): two framebuffers plus the
	// last-presented damage sequence, so a window/canvas front-end only redraws
	// and uploads when the scene actually changed.
	bufs    [2][]byte
	cur     int
	lastRev int
}

// Config configures a new App.
type Config struct {
	Registry *source.Registry
	// Settings supplies the profiles/theme/cache. When nil, a single "Home"
	// profile is synthesized from Subscriptions (the CLI PNG/JSON/serve paths).
	Settings *settings.Settings
	// Store persists settings edits/profile switches. Optional (nil = no disk).
	Store *settings.Store
	// Subscriptions seeds the synthesized profile when Settings is nil.
	Subscriptions []source.Subscription
	Width, Height int
	OS            string // ui.OSMac | ui.OSLinux | ui.OSWindows
	Dark          bool
}

// New builds an App with a scene themed and populated from the settings.
func New(cfg Config) *App {
	w, h := cfg.Width, cfg.Height
	if w == 0 {
		w = 1000
	}
	if h == 0 {
		h = 700
	}
	set := cfg.Settings
	if set == nil {
		set = &settings.Settings{
			Profiles: []settings.Profile{{Name: "Home", Subs: cfg.Subscriptions}},
			Active:   0,
			Theme:    settings.ThemeSystem,
		}
	}
	set.Normalize()

	scene := ui.New(w, h, ui.ResolveTheme(set.Theme, cfg.OS, cfg.Dark))
	scene.SetThemeName(set.Theme)
	scene.SetCachePath(set.CachePath)
	scene.SetProfiles(set.Profiles, set.Active)

	a := &App{
		reg: cfg.Registry, store: cfg.Store, set: set,
		subs: set.ActiveProfile().Subs, scene: scene,
		osName: cfg.OS, dark: cfg.Dark, lastRev: -1,
	}
	a.refresh = func() { go a.Refresh(context.Background()) }
	return a
}

// SetRefreshHook overrides the asynchronous re-aggregate trigger (tests use a
// synchronous variant for determinism).
func (a *App) SetRefreshHook(f func()) { a.refresh = f }

// ApplySceneSettings snapshots the scene's edited settings, persists them (when
// a store is configured), reselects the active profile's subscriptions and
// theme, and triggers a re-aggregate. Front-ends call it after any profile
// switch or settings-view edit.
func (a *App) ApplySceneSettings() {
	set := a.scene.Settings()
	a.set = set
	if a.store != nil {
		_ = a.store.Save(set)
	}
	a.subs = set.ActiveProfile().Subs
	a.applyTheme()
	a.refresh()
}

// applyTheme resolves the palette from the persisted theme name, the host OS and
// the current dark preference, then layers the harvested system accent on top so
// it survives every profile/theme switch. All theme changes funnel through here.
func (a *App) applyTheme() {
	t := ui.ResolveTheme(a.scene.ThemeName(), a.osName, a.dark)
	if a.hasAccent {
		t = ui.WithAccent(t, a.accent.R, a.accent.G, a.accent.B)
	}
	a.scene.SetTheme(t)
}

// SetSystemAppearance applies look-and-feel harvested from the host UI: the
// effective dark/light mode (honoured only when the user's theme is "system"),
// the accent colour, and — when fontTTF parses — the system font. Called by the
// native window back-end at launch and whenever the system appearance changes.
func (a *App) SetSystemAppearance(dark bool, accent color.RGBA, hasAccent bool, fontTTF []byte) {
	a.dark = dark
	a.accent, a.hasAccent = accent, hasAccent
	if len(fontTTF) > 0 {
		ui.SetSystemFont(fontTTF)
	}
	a.applyTheme()
}

// Frame returns the current framebuffer, redrawing into the back buffer only
// when the scene's damage sequence has advanced since the last call.
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

// Refresh aggregates the active profile's subscriptions (concurrently,
// newest-first) and loads the merged items into the scene.
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
	a.osName, a.dark = osName, dark
	a.applyTheme()
}
