package app

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"path/filepath"
	"testing"

	"github.com/go-news-reader/reader/feeds"
	"github.com/go-news-reader/reader/internal/httplog"
	"github.com/go-news-reader/reader/internal/settings"
	"github.com/go-news-reader/reader/source"
	"github.com/go-news-reader/reader/ui"
)

// hasRGB reports whether an RGBA buffer contains any pixel of the given colour.
func hasRGB(buf []byte, r, g, b uint8) bool {
	for i := 0; i+3 < len(buf); i += 4 {
		if buf[i] == r && buf[i+1] == g && buf[i+2] == b {
			return true
		}
	}
	return false
}

type fakeProv struct {
	kind  source.Kind
	items []source.Item
	err   error
}

func (f fakeProv) Kind() source.Kind { return f.kind }
func (f fakeProv) Feed(context.Context, source.Query) (source.Result, error) {
	if f.err != nil {
		return source.Result{}, f.err
	}
	return source.Result{Items: f.items}, nil
}

func newReg(provs ...source.Provider) *source.Registry {
	r := source.NewRegistry()
	for _, p := range provs {
		r.Register(p)
	}
	return r
}

func TestNewWithRecorderFeedsLogView(t *testing.T) {
	rec := httplog.NewRecorder(8)
	rec.Log(httplog.Entry{Method: "GET", URL: "https://ex/1", Status: 200, Bytes: 10})
	rec.Log(httplog.Entry{Method: "GET", URL: "https://ex/2", Err: "boom"})
	a := New(Config{Registry: newReg(), Recorder: rec, Width: 400, Height: 300})
	// The scene's log source converts httplog entries into ui.LogEntry, newest
	// first, so the Network-log view shows the recorder's exchanges.
	got := a.Scene().LogEntries()
	if len(got) != 2 {
		t.Fatalf("log entries = %d, want 2", len(got))
	}
	if got[0].URL != "https://ex/2" || got[0].Err != "boom" {
		t.Fatalf("newest-first / field mapping wrong: %+v", got[0])
	}
	if got[1].Status != 200 || got[1].Bytes != 10 {
		t.Fatalf("entry mapping wrong: %+v", got[1])
	}
}

func TestNewDefaultsAndAccessors(t *testing.T) {
	reg := newReg(fakeProv{kind: source.Reddit})
	a := New(Config{
		Registry:      reg,
		Subscriptions: []source.Subscription{{Source: source.Reddit, Channel: "golang"}},
		OS:            ui.OSMac,
	})
	if a.Scene().W != 1000 || a.Scene().H != 700 {
		t.Fatalf("default size = %dx%d", a.Scene().W, a.Scene().H)
	}
	if len(a.Scene().Subs) != 1 || a.Scene().Subs[0].Channel != "golang" {
		t.Fatalf("subs not mapped: %+v", a.Scene().Subs)
	}
}

func TestRefreshSuccess(t *testing.T) {
	reg := newReg(fakeProv{kind: source.Reddit, items: []source.Item{
		{ID: "a", Source: source.Reddit, Created: 2},
		{ID: "b", Source: source.Reddit, Created: 1},
	}})
	a := New(Config{
		Registry:      reg,
		Subscriptions: []source.Subscription{{Source: source.Reddit, Channel: "golang"}},
		Width:         400, Height: 300,
	})
	errs := a.Refresh(context.Background())
	if len(errs) != 0 {
		t.Fatalf("errs = %v", errs)
	}
	if len(a.Items()) != 2 || a.Items()[0].ID != "a" {
		t.Fatalf("items = %+v", a.Items())
	}
	if a.Scene().Status != "" {
		t.Fatalf("status = %q", a.Scene().Status)
	}
}

func TestRefreshError(t *testing.T) {
	reg := newReg(fakeProv{kind: source.Reddit, err: errors.New("boom")})
	a := New(Config{
		Registry:      reg,
		Subscriptions: []source.Subscription{{Source: source.Reddit}},
	})
	errs := a.Refresh(context.Background())
	if len(errs) == 0 {
		t.Fatal("want errs")
	}
	if a.Scene().Status == "" {
		t.Fatal("status should carry the error")
	}
}

func TestRenderPNG(t *testing.T) {
	a := New(Config{Registry: newReg(), Width: 360, Height: 240})
	data, err := a.RenderPNG()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width != 360 || cfg.Height != 240 {
		t.Fatalf("png = %dx%d", cfg.Width, cfg.Height)
	}
}

func TestRenderPNGEncodeError(t *testing.T) {
	orig := encodePNG
	encodePNG = func(io.Writer, image.Image) error { return errors.New("encode") }
	defer func() { encodePNG = orig }()
	a := New(Config{Registry: newReg(), Width: 360, Height: 240})
	if _, err := a.RenderPNG(); err == nil {
		t.Fatal("want encode error")
	}
}

func TestFrameDoubleBuffer(t *testing.T) {
	a := New(Config{Registry: newReg(), Width: 360, Height: 240})
	b1, changed := a.Frame()
	if !changed || len(b1) != 360*240*4 {
		t.Fatalf("first frame changed=%v len=%d", changed, len(b1))
	}
	// No state change -> no redraw, same buffer returned.
	b2, changed := a.Frame()
	if changed {
		t.Fatal("unchanged scene produced a new frame")
	}
	if &b2[0] != &b1[0] {
		t.Fatal("front buffer changed without damage")
	}
	// Mutate -> redraw into the back buffer (the other one).
	a.Scene().Scroll(1)
	b3, changed := a.Frame()
	if !changed {
		t.Fatal("damage did not produce a frame")
	}
	if &b3[0] == &b1[0] {
		t.Fatal("expected double-buffer swap")
	}
	// Resize -> reallocate and force redraw.
	a.Scene().Resize(400, 300)
	b4, changed := a.Frame()
	if !changed || len(b4) != 400*300*4 {
		t.Fatalf("resize frame changed=%v len=%d", changed, len(b4))
	}
}

func TestSetTheme(t *testing.T) {
	a := New(Config{Registry: newReg(), OS: ui.OSLinux})
	light := a.Scene()
	a.SetTheme(ui.OSWindows, true)
	if a.Scene() != light {
		t.Fatal("SetTheme should not replace the scene")
	}
}

func TestNewWithSettings(t *testing.T) {
	set := &settings.Settings{
		Profiles: []settings.Profile{
			{Name: "Home", Subs: []source.Subscription{{Source: source.Reddit, Channel: "golang"}}},
			{Name: "Tech", Subs: []source.Subscription{{Source: source.Lemmy, Channel: "tech"}}},
		},
		Active: 1, Theme: settings.ThemeDark, CachePath: "/c",
	}
	a := New(Config{Registry: newReg(fakeProv{kind: source.Lemmy}), Settings: set, OS: ui.OSMac})
	s := a.Scene()
	if s.ActiveProfileIndex() != 1 || len(s.Subs) != 1 || s.Subs[0].Channel != "tech" {
		t.Fatalf("active profile not applied: idx=%d subs=%+v", s.ActiveProfileIndex(), s.Subs)
	}
	if s.ThemeName() != settings.ThemeDark || s.CachePath() != "/c" {
		t.Fatalf("scalars = %q %q", s.ThemeName(), s.CachePath())
	}
	if len(a.subs) != 1 || a.subs[0].Source != source.Lemmy {
		t.Fatalf("app subs = %+v", a.subs)
	}
}

func TestApplySceneSettingsPersistsAndRebuilds(t *testing.T) {
	set := &settings.Settings{
		Profiles: []settings.Profile{
			{Name: "A", Subs: []source.Subscription{{Source: source.Reddit, Channel: "a"}}},
			{Name: "B", Subs: []source.Subscription{{Source: source.Reddit, Channel: "b"}}},
		},
		Active: 0, Theme: settings.ThemeSystem,
	}
	path := filepath.Join(t.TempDir(), "s.json")
	a := New(Config{
		Registry: newReg(fakeProv{kind: source.Reddit, items: []source.Item{{ID: "x", Source: source.Reddit}}}),
		Settings: set, Store: settings.NewStore(path), OS: ui.OSMac,
	})
	var refreshed int
	a.SetRefreshHook(func() { refreshed++; a.Refresh(context.Background()) })

	a.Scene().SetActiveProfile(1) // switch to B
	a.ApplySceneSettings()

	if refreshed != 1 {
		t.Fatalf("refresh hook not called: %d", refreshed)
	}
	if len(a.subs) != 1 || a.subs[0].Channel != "b" {
		t.Fatalf("subs not rebuilt: %+v", a.subs)
	}
	// Settings were persisted with the new active index.
	loaded, err := settings.NewStore(path).Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Active != 1 {
		t.Fatalf("persisted active = %d", loaded.Active)
	}
	if len(a.Items()) != 1 {
		t.Fatalf("re-aggregate did not load items: %d", len(a.Items()))
	}
}

func TestSetSystemAppearance(t *testing.T) {
	a := New(Config{Registry: newReg(), OS: ui.OSMac, Width: 400, Height: 300})
	accent := color.RGBA{R: 17, G: 99, B: 213, A: 0xFF}
	// A non-empty (here deliberately unparseable) font exercises the font branch;
	// the accent + dark mode must reach the rendered topbar.
	a.SetSystemAppearance(true, accent, true, []byte("not-a-real-font"))
	s := a.Scene()
	buf := make([]byte, s.W*s.H*4)
	s.Draw(buf)
	if !hasRGB(buf, accent.R, accent.G, accent.B) {
		t.Fatal("harvested accent not present in the rendered feed")
	}
	// A colour-only push with hasAccent=false and no font clears the override.
	a.SetSystemAppearance(false, color.RGBA{}, false, nil)
	s.Draw(buf)
	if hasRGB(buf, accent.R, accent.G, accent.B) {
		t.Fatal("accent should be dropped when hasAccent is false")
	}
}

func TestApplyAccountsRebuildsRegistryAndPersists(t *testing.T) {
	set := &settings.Settings{
		Profiles: []settings.Profile{{Name: "Home", Subs: []source.Subscription{{Source: source.Reddit, Channel: "golang"}}}},
		Active:   0, Theme: settings.ThemeSystem,
	}
	path := filepath.Join(t.TempDir(), "s.json")
	rec := httplog.NewRecorder(4)
	a := New(Config{
		Registry: newReg(fakeProv{kind: source.Reddit, items: []source.Item{{ID: "x", Source: source.Reddit}}}),
		Settings: set, Store: settings.NewStore(path), Recorder: rec, Options: feeds.Options{}, OS: ui.OSMac,
	})
	// The rebuilt registry is captured through the builder seam (so no real
	// providers are constructed) and yields a distinct item, proving the swap.
	var gotOpts feeds.Options
	rebuilt := newReg(fakeProv{kind: source.Reddit, items: []source.Item{{ID: "y", Source: source.Reddit}}})
	a.SetRegistryBuilder(func(o feeds.Options) *source.Registry { gotOpts = o; return rebuilt })
	var refreshed int
	a.SetRefreshHook(func() { refreshed++; a.Refresh(context.Background()) })

	// Enter Reddit credentials via the scene editor buffers, then commit.
	a.Scene().SetAccounts([]settings.Account{{Kind: source.Reddit, Fields: map[string]string{"client_id": "cid", "client_secret": "csec"}}})
	a.ApplyAccounts()

	if refreshed != 1 {
		t.Fatalf("refresh hook not called: %d", refreshed)
	}
	if gotOpts.RedditClientID != "cid" || gotOpts.RedditClientSecret != "csec" {
		t.Fatalf("reddit creds not mapped into rebuild options: %+v", gotOpts)
	}
	if gotOpts.Recorder != rec {
		t.Fatal("shared recorder not re-wired into the rebuilt registry")
	}
	loaded, err := settings.NewStore(path).Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := loaded.Account(source.Reddit); !ok {
		t.Fatal("reddit account not persisted")
	}
	if len(a.Items()) != 1 || a.Items()[0].ID != "y" {
		t.Fatalf("registry not swapped (items from old reg): %+v", a.Items())
	}
}

func TestApplyAccountsNoStore(t *testing.T) {
	a := New(Config{Registry: newReg(fakeProv{kind: source.Reddit})})
	a.SetRegistryBuilder(func(feeds.Options) *source.Registry { return newReg() })
	a.SetRefreshHook(func() {})
	a.ApplyAccounts() // store == nil branch, must not panic
}

func TestAccountsToOptions(t *testing.T) {
	accts := []settings.Account{
		{Kind: source.Reddit, Fields: map[string]string{"client_id": "id", "client_secret": "sec", "username": "u", "password": "p"}},
		{Kind: source.Mastodon, Fields: map[string]string{"instance": "https://m", "token": "mt"}},
		{Kind: source.Lemmy, Fields: map[string]string{"instance": "https://l"}},
		{Kind: source.Usenet, Fields: map[string]string{"addr": "news:119", "tls": "true", "indexer_url": "https://ix", "indexer_key": "k"}},
		{Kind: source.Instagram, Fields: map[string]string{"session": "ig"}},
		{Kind: source.TikTok, Fields: map[string]string{"ms_token": "ms", "session": "ts"}},
		{Kind: source.Twitter, Fields: map[string]string{"token": "tw"}},
	}
	o := AccountsToOptions(feeds.Options{}, accts)
	if o.RedditClientID != "id" || o.RedditClientSecret != "sec" || o.RedditUsername != "u" || o.RedditPassword != "p" {
		t.Fatalf("reddit mapping wrong: %+v", o)
	}
	if o.MastodonInstance != "https://m" || o.MastodonToken != "mt" || o.LemmyInstance != "https://l" {
		t.Fatalf("mastodon/lemmy mapping wrong: %+v", o)
	}
	if o.UsenetAddr != "news:119" || !o.UsenetTLS || o.UsenetIndexerURL != "https://ix" || o.UsenetIndexerAPIKey != "k" {
		t.Fatalf("usenet mapping wrong: %+v", o)
	}
	if o.InstagramSession != "ig" || o.TikTokMSToken != "ms" || o.TikTokSession != "ts" || o.TwitterToken != "tw" {
		t.Fatalf("scraper mapping wrong: %+v", o)
	}

	// tls "false" clears; an empty field leaves the base untouched (setIf skip);
	// an absent tls key preserves the base bool.
	base := feeds.Options{UsenetTLS: true, MastodonToken: "keep"}
	o2 := AccountsToOptions(base, []settings.Account{
		{Kind: source.Usenet, Fields: map[string]string{"tls": "false"}},
		{Kind: source.Mastodon, Fields: map[string]string{"instance": ""}},
	})
	if o2.UsenetTLS {
		t.Fatal("tls=false should clear UsenetTLS")
	}
	if o2.MastodonToken != "keep" {
		t.Fatal("empty account field must not overwrite the base value")
	}
	o3 := AccountsToOptions(feeds.Options{UsenetTLS: true}, []settings.Account{{Kind: source.Usenet, Fields: map[string]string{"addr": "x"}}})
	if !o3.UsenetTLS {
		t.Fatal("absent tls key should preserve the base bool")
	}
	// An unknown kind is ignored (default-less switch, no-op).
	if got := AccountsToOptions(feeds.Options{}, []settings.Account{{Kind: source.Bluesky, Fields: map[string]string{"x": "y"}}}); got.RedditClientID != "" {
		t.Fatalf("unknown kind should be a no-op: %+v", got)
	}
}

func TestApplySceneSettingsNoStore(t *testing.T) {
	a := New(Config{Registry: newReg(fakeProv{kind: source.Reddit})})
	a.SetRefreshHook(func() {}) // no-op, synchronous
	a.ApplySceneSettings()      // store == nil branch, must not panic
}

func TestDefaultRefreshHook(t *testing.T) {
	// Exercise the default (goroutine) refresh hook against a fake provider.
	a := New(Config{Registry: newReg(fakeProv{kind: source.Reddit})})
	a.ApplySceneSettings() // spawns go a.Refresh(...)
	// Give the goroutine a chance without asserting on its async result.
	_ = a.Items()
}

func TestRefreshAuthPromptsMixed(t *testing.T) {
	reg := newReg(
		fakeProv{kind: source.Reddit, err: source.NeedsAuth(source.Reddit, "sign in with a Reddit app (oauth)")},
		fakeProv{kind: source.Mastodon, err: source.NeedsAuth(source.Mastodon, "access token required/invalid")},
		fakeProv{kind: source.HackerNews, err: errors.New("boom")}, // a genuine non-auth failure
	)
	a := New(Config{
		Registry: reg,
		Subscriptions: []source.Subscription{
			{Source: source.Reddit},
			{Source: source.Reddit, Channel: "golang"}, // same kind -> de-duplicated
			{Source: source.Mastodon},
			{Source: source.HackerNews},
		},
		Width: 400, Height: 300,
	})
	a.Refresh(context.Background())

	prompts := a.Scene().AuthPrompts()
	if len(prompts) != 2 {
		t.Fatalf("prompts = %+v, want 2 (deduped)", prompts)
	}
	// Stable subscription order: Reddit before Mastodon.
	if prompts[0].Kind != source.Reddit || prompts[1].Kind != source.Mastodon {
		t.Fatalf("prompt order/dedup wrong: %+v", prompts)
	}
	// The lone non-auth failure lands in the status line.
	if a.Scene().Status == "" {
		t.Fatal("non-auth error should be shown in the status line")
	}
}

func TestRefreshAuthOnlyClearsStatus(t *testing.T) {
	reg := newReg(fakeProv{kind: source.Instagram, err: source.NeedsAuth(source.Instagram, "session/token required")})
	a := New(Config{
		Registry:      reg,
		Subscriptions: []source.Subscription{{Source: source.Instagram, Channel: "nasa"}},
		Width:         400, Height: 300,
	})
	a.Refresh(context.Background())
	if got := a.Scene().AuthPrompts(); len(got) != 1 || got[0].Kind != source.Instagram {
		t.Fatalf("prompts = %+v", got)
	}
	// All failures were auth prompts, so the status line is cleared.
	if a.Scene().Status != "" {
		t.Fatalf("status = %q, want empty (all failures were auth prompts)", a.Scene().Status)
	}
}
