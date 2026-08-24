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
	"strings"
	"testing"
	"time"

	"github.com/go-news-reader/reader/feeds"
	"github.com/go-news-reader/reader/internal/browsercookies"
	"github.com/go-news-reader/reader/internal/httplog"
	"github.com/go-news-reader/reader/internal/settings"
	"github.com/go-news-reader/reader/source"
	"github.com/go-news-reader/reader/ui"
)

// fakeCookieFinder is a cookieImporter stub for the Firefox-import tests. rs/err
// drive RedditSession; session/err drive the social-provider session importers.
type fakeCookieFinder struct {
	rs      browsercookies.RedditSession
	session string
	err     error
}

func (f fakeCookieFinder) RedditSession() (browsercookies.RedditSession, error) {
	return f.rs, f.err
}
func (f fakeCookieFinder) InstagramSession() (string, error) { return f.session, f.err }
func (f fakeCookieFinder) TikTokSession() (string, error)    { return f.session, f.err }
func (f fakeCookieFinder) TwitterSession() (string, error)   { return f.session, f.err }

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

func TestRefreshStreaming(t *testing.T) {
	a := New(Config{
		Subscriptions: []source.Subscription{
			{Source: source.Reddit, Channel: "golang"},
			{Source: source.Mastodon},
		},
		Width: 400, Height: 300,
	})
	// Inline (single-threaded) mode: onUpdate applies the scene writes on this
	// goroutine, and no provider reads the scene, so there is no concurrent access.
	reg := newReg(
		fakeProv{kind: source.Reddit, items: []source.Item{{ID: "r", Source: source.Reddit, Created: 5}}},
		fakeProv{kind: source.Mastodon, err: source.NeedsAuth(source.Mastodon, "token required")},
	)
	a.reg = reg

	errs := a.RefreshStreaming(context.Background())

	// After the last source the indicator is cleared.
	if a.Scene().Loading() || a.Scene().PendingCount() != 0 {
		t.Fatalf("post-stream loading=%v pending=%d, want false/0", a.Scene().Loading(), a.Scene().PendingCount())
	}
	if d, tot := a.Scene().LoadingProgress(); d != 2 || tot != 2 {
		t.Fatalf("final progress = %d/%d, want 2/2", d, tot)
	}
	// The good source's item is loaded; the auth failure becomes a prompt.
	if len(a.Items()) != 1 || a.Items()[0].ID != "r" {
		t.Fatalf("items = %+v", a.Items())
	}
	if p := a.Scene().AuthPrompts(); len(p) != 1 || p[0].Kind != source.Mastodon {
		t.Fatalf("prompts = %+v", p)
	}
	if a.Scene().Status != "" {
		t.Fatalf("status = %q, want empty (only auth failures)", a.Scene().Status)
	}
	// The auth failure rides back in the returned errors (compactErrs path).
	if len(errs) != 1 {
		t.Fatalf("errs = %v, want 1", errs)
	}
}

// blockProv blocks in Feed until its gate is closed, so a test can hold a fetch
// mid-flight and observe the live loading indicator deterministically.
type blockProv struct {
	kind  source.Kind
	items []source.Item
	gate  <-chan struct{}
}

func (p blockProv) Kind() source.Kind { return p.kind }
func (p blockProv) Feed(context.Context, source.Query) (source.Result, error) {
	<-p.gate
	return source.Result{Items: p.items}, nil
}

// TestRefreshStreamingDeferredIndicator exercises the native-window path:
// DeferSceneWrites marshals every scene mutation onto the render thread, so the
// background aggregation goroutine only touches the view-model and the queue
// while the render thread (this goroutine, via Frame) owns the scene. With a
// provider held mid-fetch it proves the loading indicator is live before the
// source returns, then that draining the queue after completion clears it and
// surfaces the item — all without a data race (go test -race).
func TestRefreshStreamingDeferredIndicator(t *testing.T) {
	a := New(Config{
		Subscriptions: []source.Subscription{{Source: source.Reddit, Channel: "golang"}},
		Width:         400, Height: 300,
	})
	a.DeferSceneWrites()
	gate := make(chan struct{})
	a.reg = newReg(blockProv{kind: source.Reddit, items: []source.Item{{ID: "r", Source: source.Reddit, Created: 5}}, gate: gate})

	done := make(chan struct{})
	go func() { a.RefreshStreaming(context.Background()); close(done) }()

	// RefreshStreaming enqueues SetPending/SetLoading(true) before the fetch; drain
	// frames on the render thread until the live indicator surfaces.
	waitFor(t, func() bool {
		a.Frame()
		return a.Scene().Loading() && a.Scene().PendingCount() == 1
	})
	if len(a.Items()) != 0 {
		t.Fatalf("items should be empty while the source is blocked: %+v", a.Items())
	}

	close(gate) // let the provider return
	<-done      // the completion updates are now enqueued

	a.Frame() // drain them on the render thread
	if a.Scene().Loading() || a.Scene().PendingCount() != 0 {
		t.Fatalf("post-stream loading=%v pending=%d, want false/0", a.Scene().Loading(), a.Scene().PendingCount())
	}
	if len(a.Items()) != 1 || a.Items()[0].ID != "r" {
		t.Fatalf("items = %+v, want [r]", a.Items())
	}
}

// waitFor spins cond (which advances the render loop) until it holds or the
// deadline elapses, so a deferred-mode test need not sleep on a fixed timer.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met before deadline")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestRefreshStreamingNonAuthErrorInStatus(t *testing.T) {
	a := New(Config{Subscriptions: []source.Subscription{{Source: source.Reddit}}, Width: 400, Height: 300})
	a.reg = newReg(fakeProv{kind: source.Reddit, err: errors.New("upstream 500")})
	errs := a.RefreshStreaming(context.Background())
	if len(errs) != 1 {
		t.Fatalf("errs = %v, want 1", errs)
	}
	if a.Scene().Status == "" {
		t.Fatal("non-auth failure should land in the status line")
	}
	if a.Scene().Loading() {
		t.Fatal("loading not cleared after completion")
	}
}

func TestRefreshStreamingNoSubs(t *testing.T) {
	// No subscriptions: the terminal (index -1) update still clears loading and
	// leaves no pending markers, with no errors.
	a := New(Config{Registry: newReg(), Width: 400, Height: 300})
	errs := a.RefreshStreaming(context.Background())
	if len(errs) != 0 {
		t.Fatalf("errs = %v, want none", errs)
	}
	if a.Scene().Loading() || a.Scene().PendingCount() != 0 {
		t.Fatalf("loading=%v pending=%d, want false/0", a.Scene().Loading(), a.Scene().PendingCount())
	}
	if len(a.Items()) != 0 {
		t.Fatalf("items = %+v, want none", a.Items())
	}
}

func TestFrameAnimatesWhileLoading(t *testing.T) {
	a := New(Config{Registry: newReg(), Width: 360, Height: 240})
	clock := time.Unix(0, 0)
	a.now = func() time.Time { return clock }
	// Idle: a second Frame with no state change does not redraw.
	a.Frame()
	if _, changed := a.Frame(); changed {
		t.Fatal("idle scene redrew without damage")
	}
	// Loading: the animation clock advances by the real time elapsed since it last
	// stepped, so the spinner keeps a steady speed however often Frame is called.
	a.Scene().SetLoading(true, 0, 2)
	a.Frame() // drain the SetLoading damage AND establish the animation clock
	f0 := a.Scene().AnimFrame()
	// A frame less than one animation interval later steps nothing and redraws
	// nothing (the spinner has not moved).
	clock = clock.Add(animFrameInterval / 2)
	if _, changed := a.Frame(); changed {
		t.Fatal("a sub-interval frame redrew without the spinner moving")
	}
	if a.Scene().AnimFrame() != f0 {
		t.Fatal("the animation clock advanced before a full interval elapsed")
	}
	// Four intervals of real time later, the clock steps by exactly four frames and
	// the scene redraws — regardless of how few times Frame was called between.
	clock = time.Unix(0, 0).Add(4*animFrameInterval + animFrameInterval/2)
	if _, changed := a.Frame(); !changed {
		t.Fatal("the spinner moved but the frame was not redrawn")
	}
	if got := a.Scene().AnimFrame(); got != f0+4 {
		t.Fatalf("anim frame = %d, want %d (stepped by the elapsed intervals)", got, f0+4)
	}
	// Loading off: animation stops, the clock resets, idle gate restored.
	a.Scene().SetLoading(false, 2, 2)
	a.Frame() // consume the damage from SetLoading
	if _, changed := a.Frame(); changed {
		t.Fatal("scene kept animating after loading cleared")
	}
	if !a.lastAnim.IsZero() {
		t.Fatal("the animation clock should reset when animation stops")
	}
}

// TestFrameContentChangeRedrawsImmediatelyWhileThrottled proves the throttle
// only gates the spinner cadence: a content change queued between spinner steps
// redraws on its own tick, so streamed sources still appear promptly, and the
// present loop is told to blit it immediately (PresentImmediate).
func TestFrameContentChangeRedrawsImmediatelyWhileThrottled(t *testing.T) {
	a := New(Config{Registry: newReg(), Width: 360, Height: 240})
	clock := time.Unix(0, 0)
	a.now = func() time.Time { return clock }
	a.DeferSceneWrites()
	a.Scene().SetLoading(true, 0, 3)
	a.Frame() // consume the SetLoading damage; the spinner clock is now set
	f0 := a.Scene().AnimFrame()
	// A background scene write lands (a streamed source): it raises the present
	// wake, so the loop is told to blit at once and NOT to throttle.
	a.post(func() { a.Scene().SetItems([]source.Item{{ID: "1", Source: source.HackerNews, Title: "hi"}}) })
	if !a.PresentImmediate() {
		t.Fatal("a queued content write must ask the present loop to blit immediately")
	}
	if _, changed := a.Frame(); !changed { // no clock advance: same instant
		t.Fatal("a content change between spinner steps must redraw immediately")
	}
	// It redrew on the content change, not because the spinner stepped.
	if a.Scene().AnimFrame() != f0 {
		t.Fatal("content-change redraw must not advance the animation clock early")
	}
	// Frame drained the queued write, so the wake drops and the loop can throttle
	// the spinner again.
	if a.PresentImmediate() {
		t.Fatal("Frame should drain the queued write and drop the present wake")
	}
}

// TestPresentThrottleOnlyForSpinner proves the present-throttle signal is raised
// for the indeterminate loading spinner but cleared the moment real content
// motion (a playing GIF) needs the full blit cadence.
func TestPresentThrottleOnlyForSpinner(t *testing.T) {
	a := New(Config{Registry: newReg(), Width: 360, Height: 240})
	if a.PresentThrottle() {
		t.Fatal("an idle scene is not throttleable — nothing is animating")
	}
	a.Scene().SetLoading(true, 0, 2)
	a.Frame()
	if !a.PresentThrottle() {
		t.Fatal("a loading spinner should be throttleable")
	}
	// A playing GIF is real content: the loop must present its frames at full rate.
	a.Scene().SetGIFPlaying(true)
	a.Frame()
	if a.PresentThrottle() {
		t.Fatal("a playing GIF must not be throttled")
	}
	a.Scene().SetGIFPlaying(false)
	a.Scene().SetLoading(false, 2, 2)
	a.Frame()
	if a.PresentThrottle() {
		t.Fatal("a settled scene is not throttleable")
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

func TestSelectAndDeleteProfile(t *testing.T) {
	set := &settings.Settings{
		Profiles: []settings.Profile{
			{Name: "A", Subs: []source.Subscription{{Source: source.Reddit, Channel: "a"}}},
			{Name: "B", Subs: []source.Subscription{{Source: source.Reddit, Channel: "b"}}},
			{Name: "C", Subs: []source.Subscription{{Source: source.Reddit, Channel: "c"}}},
		},
		Active: 0, Theme: settings.ThemeSystem,
	}
	a := New(Config{Registry: newReg(fakeProv{kind: source.Reddit}), Settings: set, Width: 400, Height: 300})
	var refreshed int
	a.SetRefreshHook(func() { refreshed++ })

	// SelectProfile switches the scene, keeps vm.Profile in step, and re-aggregates.
	a.SelectProfile(2)
	if a.VM().Profile.Get() != 2 || a.Scene().ActiveProfileIndex() != 2 {
		t.Fatalf("select: vm=%d scene=%d", a.VM().Profile.Get(), a.Scene().ActiveProfileIndex())
	}
	if len(a.subs) != 1 || a.subs[0].Channel != "c" {
		t.Fatalf("subs not rebuilt on select: %+v", a.subs)
	}

	// DeleteProfile removes the active profile; the active index re-clamps and
	// vm.Profile stays synced with the scene's re-clamped selection.
	a.DeleteProfile(2)
	if a.VM().Profile.Get() != a.Scene().ActiveProfileIndex() {
		t.Fatalf("delete desync: vm=%d scene=%d", a.VM().Profile.Get(), a.Scene().ActiveProfileIndex())
	}
	if refreshed != 2 {
		t.Fatalf("refresh hook calls = %d, want 2", refreshed)
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
		Settings: set, Store: testStore(t, path), OS: ui.OSMac,
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
	loaded, err := testStore(t, path).Load()
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
		Settings: set, Store: testStore(t, path), Recorder: rec, Options: feeds.Options{}, OS: ui.OSMac,
	})
	// The rebuilt registry is captured through the builder seam (so no real
	// providers are constructed) and yields a distinct item, proving the swap.
	var gotOpts feeds.Options
	rebuilt := newReg(fakeProv{kind: source.Reddit, items: []source.Item{{ID: "y", Source: source.Reddit}}})
	a.SetRegistryBuilder(func(o feeds.Options) *source.Registry { gotOpts = o; return rebuilt })
	var refreshed int
	a.SetRefreshHook(func() { refreshed++; a.Refresh(context.Background()) })

	// Enter the Reddit session cookie via the scene editor buffers, then commit.
	a.Scene().SetAccounts([]settings.Account{{Kind: source.Reddit, Fields: map[string]string{"session_cookie": "reddit_session=xyz"}}})
	a.ApplyAccounts()

	if refreshed != 1 {
		t.Fatalf("refresh hook not called: %d", refreshed)
	}
	if gotOpts.RedditSessionCookie != "reddit_session=xyz" {
		t.Fatalf("reddit creds not mapped into rebuild options: %+v", gotOpts)
	}
	if gotOpts.Recorder != rec {
		t.Fatal("shared recorder not re-wired into the rebuilt registry")
	}
	loaded, err := testStore(t, path).Load()
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

func TestImportRedditSessionFromFirefoxSuccess(t *testing.T) {
	set := &settings.Settings{
		Profiles: []settings.Profile{{Name: "Home", Subs: []source.Subscription{{Source: source.Reddit, Channel: "golang"}}}},
		Active:   0, Theme: settings.ThemeSystem,
	}
	path := filepath.Join(t.TempDir(), "s.json")
	a := New(Config{
		Registry: newReg(fakeProv{kind: source.Reddit, items: []source.Item{{ID: "x", Source: source.Reddit}}}),
		Settings: set, Store: testStore(t, path), Options: feeds.Options{}, OS: ui.OSMac,
	})
	var gotOpts feeds.Options
	a.SetRegistryBuilder(func(o feeds.Options) *source.Registry {
		gotOpts = o
		return newReg(fakeProv{kind: source.Reddit})
	})
	a.SetRefreshHook(func() {})
	a.SetCookieFinder(fakeCookieFinder{rs: browsercookies.RedditSession{Value: "COOKIEVAL", Host: ".reddit.com"}})

	ok, err := a.ImportRedditSessionFromFirefox()
	if err != nil || !ok {
		t.Fatalf("import = %v, %v; want true, nil", ok, err)
	}
	// The imported cookie is mapped into the rebuilt registry options...
	if gotOpts.RedditSessionCookie != "COOKIEVAL" {
		t.Fatalf("session cookie not applied to rebuilt options: %+v", gotOpts)
	}
	// ...persisted to disk...
	loaded, err := testStore(t, path).Load()
	if err != nil {
		t.Fatal(err)
	}
	acct, ok := loaded.Account(source.Reddit)
	if !ok || acct.Fields["session_cookie"] != "COOKIEVAL" {
		t.Fatalf("cookie not persisted: %+v ok=%v", acct, ok)
	}
	// ...surfaced in the editor buffer, and reported on the status line.
	if a.VM().Status.Get() == "" {
		t.Fatal("status line should report the import outcome")
	}
}

func TestImportRedditSessionFromFirefoxFailure(t *testing.T) {
	a := New(Config{Registry: newReg(fakeProv{kind: source.Reddit}), OS: ui.OSMac})
	a.SetRefreshHook(func() {})
	var rebuilt bool
	a.SetRegistryBuilder(func(feeds.Options) *source.Registry { rebuilt = true; return newReg() })

	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"no cookie", browsercookies.ErrNoCookie, "log into Reddit in Firefox first"},
		{"no firefox", browsercookies.ErrNoFirefox, "Firefox not found"},
		{"no profile", browsercookies.ErrNoProfile, "no readable Firefox profile"},
		{"other", errors.New("disk exploded"), "disk exploded"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a.SetCookieFinder(fakeCookieFinder{err: tc.err})
			ok, err := a.ImportRedditSessionFromFirefox()
			if ok || err == nil {
				t.Fatalf("import = %v, %v; want false, err", ok, err)
			}
			if got := a.VM().Status.Get(); !strings.Contains(got, tc.want) {
				t.Fatalf("status = %q, want to contain %q", got, tc.want)
			}
		})
	}
	if rebuilt {
		t.Fatal("a failed import must not rebuild the registry")
	}
	// No Reddit account should have been stored by the failures.
	if _, ok := a.Scene().Settings().Account(source.Reddit); ok {
		t.Fatal("failed import must not persist a reddit account")
	}
}

func TestImportSessionFromFirefoxSuccess(t *testing.T) {
	cases := []struct {
		kind     source.Kind
		wantOpts func(feeds.Options) string
	}{
		{source.Instagram, func(o feeds.Options) string { return o.InstagramSession }},
		{source.Twitter, func(o feeds.Options) string { return o.TwitterSession }},
	}
	for _, tc := range cases {
		t.Run(string(tc.kind), func(t *testing.T) {
			set := &settings.Settings{
				Profiles: []settings.Profile{{Name: "Home"}}, Active: 0, Theme: settings.ThemeSystem,
			}
			path := filepath.Join(t.TempDir(), "s.json")
			a := New(Config{
				Registry: newReg(fakeProv{kind: tc.kind}), Settings: set,
				Store: testStore(t, path), OS: ui.OSMac,
			})
			var gotOpts feeds.Options
			a.SetRegistryBuilder(func(o feeds.Options) *source.Registry { gotOpts = o; return newReg() })
			a.SetRefreshHook(func() {})
			a.SetCookieFinder(fakeCookieFinder{session: "SESSION-STR"})

			a.Scene().OpenAccounts()
			a.Scene().SelectAccount(tc.kind)
			ok, err := a.ImportSessionFromFirefox()
			if err != nil || !ok {
				t.Fatalf("import = %v, %v; want true, nil", ok, err)
			}
			if got := tc.wantOpts(gotOpts); got != "SESSION-STR" {
				t.Fatalf("%s session not applied to rebuilt options: %q", tc.kind, got)
			}
			loaded, err := testStore(t, path).Load()
			if err != nil {
				t.Fatal(err)
			}
			acct, ok := loaded.Account(tc.kind)
			if !ok || acct.Fields["session"] != "SESSION-STR" {
				t.Fatalf("session not persisted: %+v ok=%v", acct, ok)
			}
			if !strings.Contains(a.VM().Status.Get(), "Imported") {
				t.Fatalf("status = %q", a.VM().Status.Get())
			}
		})
	}
}

func TestImportSessionFromFirefoxUnsupported(t *testing.T) {
	// Reddit selected: the generic importer does not handle it (Reddit has its own
	// richer flow), so it reports the typed unsupported error and rebuilds nothing.
	a := New(Config{Registry: newReg(fakeProv{kind: source.Reddit}), OS: ui.OSMac})
	a.SetRefreshHook(func() {})
	rebuilt := false
	a.SetRegistryBuilder(func(feeds.Options) *source.Registry { rebuilt = true; return newReg() })
	a.SetCookieFinder(fakeCookieFinder{session: "x"})
	a.Scene().OpenAccounts() // Reddit selected by default
	ok, err := a.ImportSessionFromFirefox()
	if ok || !errors.Is(err, errUnsupportedSessionImport) {
		t.Fatalf("import = %v, %v; want false, errUnsupportedSessionImport", ok, err)
	}
	if rebuilt {
		t.Fatal("an unsupported import must not rebuild the registry")
	}
	if !strings.Contains(a.VM().Status.Get(), "no session import") {
		t.Fatalf("status = %q", a.VM().Status.Get())
	}
}

func TestImportSessionFromFirefoxFailure(t *testing.T) {
	a := New(Config{Registry: newReg(fakeProv{kind: source.Instagram}), OS: ui.OSMac})
	a.SetRefreshHook(func() {})
	a.SetRegistryBuilder(func(feeds.Options) *source.Registry { return newReg() })
	a.Scene().OpenAccounts()
	a.Scene().SelectAccount(source.Instagram)

	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"no cookie", browsercookies.ErrNoCookie, "not signed in in Firefox"},
		{"no firefox", browsercookies.ErrNoFirefox, "Firefox not found"},
		{"no profile", browsercookies.ErrNoProfile, "no readable Firefox profile"},
		{"other", errors.New("disk exploded"), "disk exploded"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a.SetCookieFinder(fakeCookieFinder{err: tc.err})
			ok, err := a.ImportSessionFromFirefox()
			if ok || err == nil {
				t.Fatalf("import = %v, %v; want false, err", ok, err)
			}
			if got := a.VM().Status.Get(); !strings.Contains(got, tc.want) {
				t.Fatalf("status = %q, want %q", got, tc.want)
			}
		})
	}

	// With a non-Firefox sign-in browser, the failure spells out the Firefox-only
	// limitation.
	a.Scene().SetSignInBrowser(settings.SignInBrowserChrome)
	a.SetCookieFinder(fakeCookieFinder{err: browsercookies.ErrNoCookie})
	if _, _ = a.ImportSessionFromFirefox(); !strings.Contains(a.VM().Status.Get(), "requires Firefox") {
		t.Fatalf("non-Firefox hint missing: %q", a.VM().Status.Get())
	}
}

func TestLaunchRedditSignIn(t *testing.T) {
	a := New(Config{Registry: newReg(fakeProv{kind: source.Reddit}), OS: ui.OSMac})
	a.SetRefreshHook(func() {})

	// No opener installed (the CLI front-ends never set one): error + status, and
	// nothing is launched.
	if err := a.LaunchRedditSignIn(); err == nil {
		t.Fatal("LaunchRedditSignIn with no opener should error")
	}
	if got := a.VM().Status.Get(); !strings.Contains(got, "Cannot open") {
		t.Fatalf("status = %q, want a no-opener message", got)
	}

	// Success: the opener runs with the configured browser and Reddit's login URL,
	// and the status names the browser.
	var gotBrowser, gotURL string
	a.SetURLOpener(func(browser, url string) error { gotBrowser, gotURL = browser, url; return nil })
	a.Scene().SetSignInBrowser(settings.SignInBrowserChrome)
	if err := a.LaunchRedditSignIn(); err != nil {
		t.Fatal(err)
	}
	if gotBrowser != settings.SignInBrowserChrome || gotURL != redditLoginURL {
		t.Fatalf("opener args = %q,%q; want chrome,%q", gotBrowser, gotURL, redditLoginURL)
	}
	if got := a.VM().Status.Get(); !strings.Contains(got, "Chrome") {
		t.Fatalf("status = %q, want it to name the browser", got)
	}

	// Failure: the opener's error surfaces on the status line.
	a.SetURLOpener(func(string, string) error { return errors.New("boom") })
	if err := a.LaunchRedditSignIn(); err == nil {
		t.Fatal("opener error should propagate")
	}
	if got := a.VM().Status.Get(); !strings.Contains(got, "boom") {
		t.Fatalf("status = %q, want the opener error", got)
	}
}

func TestSignInBrowserLabel(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{settings.SignInBrowserFirefox, "Firefox"},
		{settings.SignInBrowserChrome, "Chrome"},
		{settings.SignInBrowserSafari, "Safari"},
		{settings.SignInBrowserEdge, "Edge"},
		{settings.SignInBrowserDefault, "your default browser"},
	} {
		if got := signInBrowserLabel(tc.in); got != tc.want {
			t.Fatalf("signInBrowserLabel(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestImportRedditNonFirefoxHint(t *testing.T) {
	// With a non-Firefox sign-in browser, a failed import spells out that cookie
	// import requires Firefox (signing in elsewhere leaves nothing importable).
	a := New(Config{Registry: newReg(fakeProv{kind: source.Reddit}), OS: ui.OSMac})
	a.SetRefreshHook(func() {})
	a.SetRegistryBuilder(func(feeds.Options) *source.Registry { return newReg() })
	a.Scene().SetSignInBrowser(settings.SignInBrowserChrome)
	a.SetCookieFinder(fakeCookieFinder{err: browsercookies.ErrNoCookie})
	if _, err := a.ImportRedditSessionFromFirefox(); err == nil {
		t.Fatal("want an import failure")
	}
	if got := a.VM().Status.Get(); !strings.Contains(got, "requires Firefox") {
		t.Fatalf("status = %q, want the Firefox-only import hint", got)
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
		{Kind: source.Reddit, Fields: map[string]string{"session_cookie": "reddit_session=xyz"}},
		{Kind: source.Mastodon, Fields: map[string]string{"instance": "https://m", "token": "mt"}},
		{Kind: source.Lemmy, Fields: map[string]string{"instance": "https://l"}},
		{Kind: source.Usenet, Fields: map[string]string{"addr": "news:119", "tls": "true", "username": "usr", "password": "pw", "indexer_url": "https://ix", "indexer_key": "k"}},
		{Kind: source.Instagram, Fields: map[string]string{"session": "ig"}},
		{Kind: source.TikTok, Fields: map[string]string{"ms_token": "ms", "session": "ts"}},
		{Kind: source.Twitter, Fields: map[string]string{"session": "auth_token=at; ct0=c"}},
	}
	o := AccountsToOptions(feeds.Options{}, accts)
	if o.RedditSessionCookie != "reddit_session=xyz" {
		t.Fatalf("reddit mapping wrong: %+v", o)
	}
	if o.MastodonInstance != "https://m" || o.MastodonToken != "mt" || o.LemmyInstance != "https://l" {
		t.Fatalf("mastodon/lemmy mapping wrong: %+v", o)
	}
	if o.UsenetAddr != "news:119" || !o.UsenetTLS || o.UsenetUsername != "usr" || o.UsenetPassword != "pw" ||
		o.UsenetIndexerURL != "https://ix" || o.UsenetIndexerAPIKey != "k" {
		t.Fatalf("usenet mapping wrong: %+v", o)
	}
	if o.InstagramSession != "ig" || o.TikTokMSToken != "ms" || o.TikTokSession != "ts" {
		t.Fatalf("scraper mapping wrong: %+v", o)
	}
	if o.TwitterSession != "auth_token=at; ct0=c" {
		t.Fatalf("twitter session mapping wrong: %+v", o)
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
	if got := AccountsToOptions(feeds.Options{}, []settings.Account{{Kind: source.Bluesky, Fields: map[string]string{"x": "y"}}}); got.RedditSessionCookie != "" {
		t.Fatalf("unknown kind should be a no-op: %+v", got)
	}
}

func TestApplySceneSettingsNoStore(t *testing.T) {
	a := New(Config{Registry: newReg(fakeProv{kind: source.Reddit})})
	a.SetRefreshHook(func() {}) // no-op, synchronous
	a.ApplySceneSettings()      // store == nil branch, must not panic
}

// signalProv reports on fed each time Feed is called, so a test can join an
// otherwise-anonymous background refresh goroutine deterministically.
type signalProv struct {
	kind  source.Kind
	items []source.Item
	fed   chan struct{}
}

func (p signalProv) Kind() source.Kind { return p.kind }
func (p signalProv) Feed(context.Context, source.Query) (source.Result, error) {
	p.fed <- struct{}{}
	return source.Result{Items: p.items}, nil
}

func TestDefaultRefreshHook(t *testing.T) {
	// Exercise the default (goroutine) refresh hook — the async path the window
	// front-end uses. In deferred mode the background Refresh only enqueues its
	// scene write (guarded by the queue mutex), so it never touches the scene the
	// test reads, and draining via Frame applies the result on this goroutine.
	fed := make(chan struct{}, 1)
	a := New(Config{
		Registry:      newReg(signalProv{kind: source.Reddit, items: []source.Item{{ID: "z", Source: source.Reddit}}, fed: fed}),
		Subscriptions: []source.Subscription{{Source: source.Reddit}},
		Width:         400, Height: 300,
	})
	a.DeferSceneWrites()
	a.ApplySceneSettings() // spawns go a.Refresh(...)
	select {
	case <-fed:
	case <-time.After(2 * time.Second):
		t.Fatal("default refresh hook did not run")
	}
	// Drain until the enqueued items land (the goroutine posts after Feed returns).
	waitFor(t, func() bool { a.Frame(); return len(a.Items()) == 1 })
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

// TestImportTikTokSessionSplitsFields: TikTok's Firefox import splits the
// "sessionid=…; msToken=…" cookie string into the RAW session + ms_token fields
// the signer needs (msToken is also a signed query param), not one blob.
func TestImportTikTokSessionSplitsFields(t *testing.T) {
	set := &settings.Settings{
		Profiles: []settings.Profile{{Name: "Home"}}, Active: 0, Theme: settings.ThemeSystem,
	}
	path := filepath.Join(t.TempDir(), "s.json")
	a := New(Config{
		Registry: newReg(fakeProv{kind: source.TikTok}), Settings: set,
		Store: testStore(t, path), OS: ui.OSMac,
	})
	var gotOpts feeds.Options
	a.SetRegistryBuilder(func(o feeds.Options) *source.Registry { gotOpts = o; return newReg() })
	a.SetRefreshHook(func() {})
	a.SetCookieFinder(fakeCookieFinder{session: "sessionid=SID123; msToken=MST456"})

	a.Scene().OpenAccounts()
	a.Scene().SelectAccount(source.TikTok)
	if ok, err := a.ImportSessionFromFirefox(); err != nil || !ok {
		t.Fatalf("import = %v, %v; want true, nil", ok, err)
	}
	if gotOpts.TikTokSession != "SID123" || gotOpts.TikTokMSToken != "MST456" {
		t.Fatalf("options = session %q / msToken %q, want SID123 / MST456", gotOpts.TikTokSession, gotOpts.TikTokMSToken)
	}
	loaded, err := testStore(t, path).Load()
	if err != nil {
		t.Fatal(err)
	}
	acct, ok := loaded.Account(source.TikTok)
	if !ok || acct.Fields["session"] != "SID123" || acct.Fields["ms_token"] != "MST456" {
		t.Fatalf("persisted fields = %+v ok=%v, want session=SID123 ms_token=MST456", acct.Fields, ok)
	}
}

func TestCookieValue(t *testing.T) {
	s := "sessionid=SID; msToken=MST; ttwid=W"
	if v := cookieValue(s, "sessionid"); v != "SID" {
		t.Errorf("sessionid = %q, want SID", v)
	}
	if v := cookieValue(s, "msToken"); v != "MST" {
		t.Errorf("msToken = %q, want MST", v)
	}
	if v := cookieValue(s, "absent"); v != "" {
		t.Errorf("absent = %q, want empty", v)
	}
	if v := cookieValue("noequalsign; x=1", "noequalsign"); v != "" {
		t.Errorf("malformed pair should yield empty, got %q", v)
	}
}

// TestSetBiometricUnlock covers the app-level biometric toggle: it updates the
// scene preference (and persists + re-applies without error).
func TestSetBiometricUnlock(t *testing.T) {
	a := New(Config{Registry: newReg(), Width: 400, Height: 300})
	a.SetRefreshHook(func() {})
	defer settings.SetSecretUserPresence(false)() // reset the process-wide gate

	a.SetBiometricUnlock(true)
	if !a.Scene().BiometricUnlock() {
		t.Fatal("SetBiometricUnlock(true) did not update the scene")
	}
	a.SetBiometricUnlock(false)
	if a.Scene().BiometricUnlock() {
		t.Fatal("SetBiometricUnlock(false) did not clear the scene")
	}
}
