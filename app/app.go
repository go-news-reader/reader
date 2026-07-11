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

	"github.com/go-news-reader/reader/feeds"
	"github.com/go-news-reader/reader/internal/httplog"
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

	// Live-rebuild inputs (window path). recorder is the shared HTTP recorder the
	// registry logs into; baseOpts is the flag-derived feeds config; newRegistry
	// is the registry builder (a seam so tests avoid constructing real providers).
	// Together they let ApplyAccounts rebuild the registry with new credentials
	// while keeping the same recorder wired, so the Network log keeps updating.
	recorder    *httplog.Recorder
	baseOpts    feeds.Options
	newRegistry func(feeds.Options) *source.Registry

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
	// Recorder, when set, feeds the scene's Network-log view. It should be the
	// same recorder the provider registry logs into.
	Recorder *httplog.Recorder
	// Subscriptions seeds the synthesized profile when Settings is nil.
	Subscriptions []source.Subscription
	// Options is the base (flag-derived) feeds configuration. The window path
	// rebuilds the registry from Options + Settings.Accounts + Recorder whenever
	// accounts change; the CLI paths leave it zero and pass a prebuilt Registry.
	Options       feeds.Options
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
	scene.SetAccounts(set.Accounts)
	if rec := cfg.Recorder; rec != nil {
		// Feed the Network-log view live, converting httplog entries into the
		// ui-local shape so the ui package need not depend on internal/httplog.
		scene.SetLogSource(func() []ui.LogEntry {
			snap := rec.Snapshot()
			out := make([]ui.LogEntry, len(snap))
			for i, e := range snap {
				out[i] = ui.LogEntry{
					When: e.When, Method: e.Method, URL: e.URL,
					Status: e.Status, Bytes: e.Bytes, Dur: e.Dur, Err: e.Err,
				}
			}
			return out
		})
	}

	a := &App{
		reg: cfg.Registry, store: cfg.Store, set: set,
		subs: set.ActiveProfile().Subs, scene: scene,
		recorder: cfg.Recorder, baseOpts: cfg.Options, newRegistry: feeds.Registry,
		osName: cfg.OS, dark: cfg.Dark, lastRev: -1,
	}
	a.refresh = func() { go a.Refresh(context.Background()) }
	return a
}

// SetRefreshHook overrides the asynchronous re-aggregate trigger (tests use a
// synchronous variant for determinism).
func (a *App) SetRefreshHook(f func()) { a.refresh = f }

// SetRegistryBuilder overrides how ApplyAccounts rebuilds the provider registry
// (a seam so tests inject a fake registry instead of real providers).
func (a *App) SetRegistryBuilder(f func(feeds.Options) *source.Registry) { a.newRegistry = f }

// ApplyAccounts snapshots the scene's edited accounts into the settings,
// persists them, rebuilds the provider registry with the new credentials
// (Reddit switches to authenticated OAuth) while keeping the same HTTP recorder
// wired so the Network log keeps updating, then re-aggregates the feed. The
// front-end calls it after the accounts editor is committed.
func (a *App) ApplyAccounts() {
	a.set = a.scene.Settings() // Settings() now carries the edited Accounts
	if a.store != nil {
		_ = a.store.Save(a.set)
	}
	a.rebuildRegistry()
	a.refresh()
}

// rebuildRegistry constructs a fresh registry from the base options overlaid
// with the current accounts and the shared recorder, so authenticated providers
// come up live without a restart.
func (a *App) rebuildRegistry() {
	opts := AccountsToOptions(a.baseOpts, a.set.Accounts)
	opts.Recorder = a.recorder
	a.reg = a.newRegistry(opts)
}

// AccountsToOptions overlays stored per-provider credentials onto base feeds
// options, mapping each account's well-known field keys onto the matching
// Options fields. Absent fields leave the base value untouched, so flags still
// apply where no account overrides them.
func AccountsToOptions(base feeds.Options, accts []settings.Account) feeds.Options {
	out := base
	for _, a := range accts {
		switch a.Kind {
		case source.Reddit:
			setIf(&out.RedditClientID, a.Fields["client_id"])
			setIf(&out.RedditClientSecret, a.Fields["client_secret"])
			setIf(&out.RedditUsername, a.Fields["username"])
			setIf(&out.RedditPassword, a.Fields["password"])
		case source.Mastodon:
			setIf(&out.MastodonInstance, a.Fields["instance"])
			setIf(&out.MastodonToken, a.Fields["token"])
		case source.Lemmy:
			setIf(&out.LemmyInstance, a.Fields["instance"])
		case source.Usenet:
			setIf(&out.UsenetAddr, a.Fields["addr"])
			if v, ok := a.Fields["tls"]; ok {
				out.UsenetTLS = v == "true"
			}
			setIf(&out.UsenetIndexerURL, a.Fields["indexer_url"])
			setIf(&out.UsenetIndexerAPIKey, a.Fields["indexer_key"])
		case source.Instagram:
			setIf(&out.InstagramSession, a.Fields["session"])
		case source.TikTok:
			setIf(&out.TikTokMSToken, a.Fields["ms_token"])
			setIf(&out.TikTokSession, a.Fields["session"])
		case source.Twitter:
			setIf(&out.TwitterToken, a.Fields["token"])
		}
	}
	return out
}

// setIf copies v into *dst only when v is non-empty.
func setIf(dst *string, v string) {
	if v != "" {
		*dst = v
	}
}

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
// newest-first) and loads the merged items into the scene. It splits the
// failures: every provider that needs sign-in/configuration becomes a clickable
// in-feed prompt (de-duplicated, in subscription order), while any remaining
// non-auth failure is shown in the status line.
func (a *App) Refresh(ctx context.Context) []error {
	items, errs := a.reg.Aggregate(ctx, a.subs)
	a.scene.SetItems(items)
	a.scene.SetAuthPrompts(authPrompts(errs))
	a.scene.Status = firstNonAuthError(errs)
	return errs
}

// authPrompts extracts a de-duplicated, subscription-ordered list of providers
// whose fetch failed because they need authentication or configuration.
func authPrompts(errs []error) []ui.AuthPrompt {
	var out []ui.AuthPrompt
	seen := map[source.Kind]bool{}
	for _, e := range errs {
		ae, ok := source.AsAuthError(e)
		if !ok || seen[ae.Kind] {
			continue
		}
		seen[ae.Kind] = true
		out = append(out, ui.AuthPrompt{Kind: ae.Kind, Reason: ae.Reason})
	}
	return out
}

// firstNonAuthError returns the message of the first failure that is not an
// auth/config prompt (those get the banner instead), or "" when there is none.
func firstNonAuthError(errs []error) string {
	for _, e := range errs {
		if _, ok := source.AsAuthError(e); !ok {
			return e.Error()
		}
	}
	return ""
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
