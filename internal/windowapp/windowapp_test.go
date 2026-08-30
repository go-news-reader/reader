package windowapp

import (
	"image"
	"image/color"
	"testing"

	window "github.com/go-widgets/application"
	"github.com/go-widgets/toolkit"

	"github.com/go-news-reader/reader/app"
	"github.com/go-news-reader/reader/feeds"
	"github.com/go-news-reader/reader/internal/browsercookies"
	"github.com/go-news-reader/reader/internal/settings"
	"github.com/go-news-reader/reader/source"
	"github.com/go-news-reader/reader/ui"
)

// profApp builds an App with two profiles and a synchronous no-op refresh hook
// (so ApplySceneSettings never spawns a goroutine during the test).
func profApp(t *testing.T) *app.App {
	t.Helper()
	set := &settings.Settings{
		Profiles: []settings.Profile{
			{Name: "Home", Subs: []source.Subscription{{Source: source.Reddit, Channel: "golang"}}},
			{Name: "Tech", Subs: []source.Subscription{{Source: source.Lemmy, Channel: "tech"}}},
		},
		Active: 0, Theme: settings.ThemeSystem,
	}
	a := app.New(app.Config{Registry: source.NewRegistry(), Settings: set, Width: 1000, Height: 700})
	a.Scene().SetScale(1)
	a.SetRefreshHook(func() {})
	return a
}

// findHit scans the surface for the first coordinate whose HitTest matches kind.
func findHit(s *ui.Scene, kind ui.HitKind) (int, int, bool) {
	for y := 0; y < s.H; y += 3 {
		for x := 0; x < s.W; x += 3 {
			if s.HitTest(x, y).Kind == kind {
				return x, y, true
			}
		}
	}
	return 0, 0, false
}

// click routes a full press+release (no drag) at the first coordinate matching
// kind (fatal if none). It drives MouseDown then MouseUp at the same point so a
// text-selectable hit (whose action is deferred to release) fires its click
// action, while an immediate control acts on the press and shrugs off the up.
func click(t *testing.T, h *Handler, kind ui.HitKind) {
	t.Helper()
	x, y, ok := findHit(h.a.Scene(), kind)
	if !ok {
		t.Fatalf("no hit region for kind %d", kind)
	}
	h.MouseDown(x, y)
	h.MouseUp(x, y)
}

// clickValue is click for a kind whose several buttons differ only by Value (e.g.
// the infinite-scroll on/off pair): it presses the first region matching both.
func clickValue(t *testing.T, h *Handler, kind ui.HitKind, value string) {
	t.Helper()
	s := h.a.Scene()
	for y := 0; y < s.H; y += 3 {
		for x := 0; x < s.W; x += 3 {
			if hit := s.HitTest(x, y); hit.Kind == kind && hit.Value == value {
				h.MouseDown(x, y)
				h.MouseUp(x, y)
				return
			}
		}
	}
	t.Fatalf("no hit region for kind %d value %q", kind, value)
}

// newApp builds an App with an empty registry at a known size and scale.
func newApp(t *testing.T) *app.App {
	t.Helper()
	a := app.New(app.Config{Registry: source.NewRegistry(), Width: 1000, Height: 700})
	a.Scene().SetScale(1)
	return a
}

func TestSystemAppearance(t *testing.T) {
	a := newApp(t)
	h := New(a)
	accent := color.RGBA{R: 17, G: 99, B: 213, A: 0xFF}
	h.SystemAppearance(window.SystemAppearance{Dark: true, Accent: accent, HasAccent: true})
	s := a.Scene()
	buf := make([]byte, s.W*s.H*4)
	s.Draw(buf)
	for i := 0; i+3 < len(buf); i += 4 {
		if buf[i] == accent.R && buf[i+1] == accent.G && buf[i+2] == accent.B {
			return // accent forwarded to the app and rendered
		}
	}
	t.Fatal("SystemAppearance did not forward the harvested accent to the app")
}

// TestTopbarRefreshButton clicks the visible topbar Refresh button and asserts it
// fires the very same scene seam (OnPullRefresh) the pull-to-refresh gesture uses,
// so the button and the overscroll gesture do one thing.
func TestTopbarRefreshButton(t *testing.T) {
	a := newApp(t)
	h := New(a)
	fired := 0
	a.Scene().OnPullRefresh = func() { fired++ }
	click(t, h, ui.HitRefresh)
	if fired != 1 {
		t.Fatalf("topbar Refresh fired OnPullRefresh %d times, want 1", fired)
	}
}

// TestTopbarRefreshButtonNilSafe guards the runHit branch when no refresh seam is
// wired (OnPullRefresh nil): the click must be a no-op, not a panic.
func TestTopbarRefreshButtonNilSafe(t *testing.T) {
	a := newApp(t)
	h := New(a)
	a.Scene().OnPullRefresh = nil
	click(t, h, ui.HitRefresh) // must not panic
}

func TestFrame(t *testing.T) {
	h := New(newApp(t))
	buf, w, hgt, changed := h.Frame()
	if !changed || w != 1000 || hgt != 700 || len(buf) != w*hgt*4 {
		t.Fatalf("Frame = %d bytes %dx%d changed=%v", len(buf), w, hgt, changed)
	}
	// second call: unchanged (damage-gated)
	if _, _, _, changed := h.Frame(); changed {
		t.Fatal("second Frame reported changed with no state change")
	}
}

func TestResize(t *testing.T) {
	a := newApp(t)
	New(a).Resize(500, 400, 2.0)
	s := a.Scene()
	if s.W != 500 || s.H != 400 || s.Scale != 2.0 {
		t.Fatalf("scene = %dx%d scale=%v", s.W, s.H, s.Scale)
	}
}

func TestScroll(t *testing.T) {
	a := newApp(t)
	// Load enough items to make the feed scrollable.
	items := make([]source.Item, 40)
	for i := range items {
		items[i] = source.Item{ID: string(rune('a' + i)), Source: source.Reddit, Title: "t", Score: -1, Comments: -1}
	}
	a.Scene().SetItems(items)
	New(a).Scroll(120)
	if a.Scene().ScrollY() <= 0 {
		t.Fatalf("ScrollY = %d, want > 0", a.Scene().ScrollY())
	}
}

func TestMouseDownItemSelectsPreview(t *testing.T) {
	a := newApp(t)
	a.Scene().SetItems([]source.Item{{ID: "1", Source: source.Reddit, Title: "hi", Permalink: "https://ex/1", Score: -1, Comments: -1}})
	h := New(a)
	// A plain click (press+release, no drag) selects the item into the preview
	// pane: the action is deferred to release now that a card is drag-selectable.
	h.MouseDown(250, 60)
	h.MouseUp(250, 60)
	s := a.Scene()
	if it, ok := s.PreviewItem(); !ok || it.ID != "1" {
		t.Fatalf("item click should select the preview; got %+v ok=%v", it, ok)
	}
	if s.Mode() == ui.ModeDetail {
		t.Fatal("item click should not jump straight to full-screen detail")
	}
	// The preview pane's "Open" button opens the full-screen reading view.
	s.HitTest(0, 0) // force a layout so the button rect is current
	oc, shown := s.PreviewOpenButton()
	if !shown {
		t.Fatal("Open button should be shown for a selected item")
	}
	h.MouseDown(oc.X+oc.W/2, oc.Y+oc.H/2) // the Open pill acts immediately on press
	if s.Mode() != ui.ModeDetail || s.Detail().ID != "1" {
		t.Fatalf("Open button should open detail; mode=%v id=%q", s.Mode(), s.Detail().ID)
	}
}

func TestMouseDownDetailBack(t *testing.T) {
	a := newApp(t)
	s := a.Scene()
	a.VM().OpenDetail(source.Item{ID: "1", Title: "t"}) // drive mode through the VM
	New(a).MouseDown(20, 24)                            // Back button in the detail topbar
	if s.Mode() != ui.ModeFeed {
		t.Fatal("Back should return to the feed")
	}
}

func TestMouseDownDetailOpenExternal(t *testing.T) {
	a := newApp(t)
	s := a.Scene()
	var opened string
	orig := openURL
	openURL = func(u string) error { opened = u; return nil }
	t.Cleanup(func() { openURL = orig })

	// Link is used when present.
	s.OpenDetail(source.Item{ID: "1", Title: "t", Link: "https://ex/link"})
	New(a).MouseDown(s.W-20, 24) // "Open original" button (right of the topbar)
	if opened != "https://ex/link" {
		t.Fatalf("opened = %q, want the Link", opened)
	}
	// Permalink is the fallback when Link is empty.
	s.OpenDetail(source.Item{ID: "2", Title: "t", Permalink: "https://ex/perm"})
	New(a).MouseDown(s.W-20, 24)
	if opened != "https://ex/perm" {
		t.Fatalf("opened = %q, want the Permalink fallback", opened)
	}
}

func TestMouseDownSub(t *testing.T) {
	a := newApp(t)
	a.Scene().SetSubs([]ui.Subscription{{Source: source.Reddit, Channel: "golang"}})
	h := New(a)
	h.MouseDown(10, 60) // "All" sidebar row: a click (press+release) switches the filter
	h.MouseUp(10, 60)
	if a.Scene().Active != ui.AllFilter {
		t.Fatalf("Active = %d, want AllFilter", a.Scene().Active)
	}
}

func TestMouseDownSubRow(t *testing.T) {
	// With a profile-tab band present, exercise a real subscription-row click
	// (which lands below the tabs) so the HitSub branch runs.
	a := profApp(t)
	h := New(a)
	s := a.Scene()
	s.SetActive(0)
	click(t, h, ui.HitSub) // the "All Sources" row selects AllFilter
	if s.Active != ui.AllFilter {
		t.Fatalf("sub row click Active = %d", s.Active)
	}
}

func TestMouseDownSearch(t *testing.T) {
	a := newApp(t)
	New(a).MouseDown(250, 24) // topbar search field
	if !a.Scene().SearchFocused() {
		t.Fatal("click on search should focus it")
	}
}

func TestMouseDownNone(t *testing.T) {
	a := newApp(t)
	a.VM().FocusSearch(true)
	h := New(a)
	// Empty sidebar area → HitNone over the selectable sidebar band: the blur is
	// deferred to release (a drag there would select), so a plain click blurs.
	h.MouseDown(10, 400)
	h.MouseUp(10, 400)
	if a.Scene().SearchFocused() {
		t.Fatal("click on empty area should blur search")
	}
}

func TestKey(t *testing.T) {
	a := newApp(t)
	h := New(a)
	s := a.Scene()
	a.VM().FocusSearch(true)

	h.Key("", 'a') // printable rune
	h.Key("", 'b')
	if s.Search() != "ab" {
		t.Fatalf("search = %q", s.Search())
	}
	h.Key("Backspace", 0)
	if s.Search() != "a" {
		t.Fatalf("after backspace = %q", s.Search())
	}
	h.Key("", 0) // no-op default (no rune)
	if s.Search() != "a" {
		t.Fatalf("no-op changed search to %q", s.Search())
	}
	h.Key("Enter", 0)
	if s.SearchFocused() {
		t.Fatal("Enter should blur search")
	}
	a.VM().FocusSearch(true)
	h.Key("Escape", 0)
	if s.SearchFocused() {
		t.Fatal("Escape should blur search")
	}
	// In the detail view, Escape returns to the feed.
	a.VM().OpenDetail(source.Item{ID: "x", Title: "t"})
	h.Key("Escape", 0)
	if s.Mode() != ui.ModeFeed {
		t.Fatal("Escape in detail should close it")
	}
}

func TestMouseDownProfileAndSettings(t *testing.T) {
	a := profApp(t)
	h := New(a)
	s := a.Scene()

	// Click the second profile tab (index 1) -> active profile switches.
	var switched bool
	for y := 0; y < s.H && !switched; y += 3 {
		for x := 0; x < s.W; x += 3 {
			if hit := s.HitTest(x, y); hit.Kind == ui.HitProfile && hit.Profile == 1 {
				h.MouseDown(x, y)
				switched = true
				break
			}
		}
	}
	if !switched || s.ActiveProfileIndex() != 1 {
		t.Fatalf("profile not switched: %d", s.ActiveProfileIndex())
	}
	// Open the settings editor via the sidebar entry.
	click(t, h, ui.HitSettings)
	if s.Mode() != ui.ModeSettings {
		t.Fatal("settings did not open")
	}
	// Drive every settings action that has a hit region.
	click(t, h, ui.HitSelectProfile) // pick a profile to edit
	click(t, h, ui.HitNewProfile)    // add a profile
	click(t, h, ui.HitSelectKind)    // choose a source in the palette
	click(t, h, ui.HitFocusChannel)  // focus the channel field
	if s.Focus() != ui.FocusChannel {
		t.Fatal("channel not focused")
	}
	click(t, h, ui.HitAddSub) // commit the (empty) channel as a sub
	click(t, h, ui.HitRemoveSub)
	click(t, h, ui.HitRenameProfile) // focus rename
	if s.Focus() != ui.FocusRename {
		t.Fatal("rename not focused")
	}
	click(t, h, ui.HitFocusCache)
	if s.Focus() != ui.FocusCache {
		t.Fatal("cache not focused")
	}
	click(t, h, ui.HitTheme)         // pick a theme
	click(t, h, ui.HitBrowserTabs)   // switch the web-preview browser tab mode
	click(t, h, ui.HitBrowserChrome) // toggle the web-preview toolbar visibility
	click(t, h, ui.HitDeleteProfile) // (near the top; click before scrolling away)
	// The FEED infinite-scroll toggle is laid out last, so scroll to the bottom of
	// the settings view once to bring it on-screen, then flip it off then on and
	// assert the route reached SetInfiniteScroll (Done stays in the fixed topbar).
	s.SetInfiniteScroll(true)
	s.Scroll(1 << 20) // pin the settings view to its bottom
	clickValue(t, h, ui.HitInfiniteScroll, "off")
	if s.InfiniteScroll() {
		t.Fatal("infinite scroll still on after clicking off")
	}
	clickValue(t, h, ui.HitInfiniteScroll, "on")
	if !s.InfiniteScroll() {
		t.Fatal("infinite scroll still off after clicking on")
	}
	// The dismiss-preview-on-switch toggle sits just below infinite scroll; re-pin
	// the bottom and flip it off then on, asserting the route reached the scene.
	s.Scroll(1 << 20)
	clickValue(t, h, ui.HitDismissPreviewOnSwitch, "off")
	if s.DismissPreviewOnSwitch() {
		t.Fatal("dismiss-preview-on-switch still on after clicking off")
	}
	clickValue(t, h, ui.HitDismissPreviewOnSwitch, "on")
	if !s.DismissPreviewOnSwitch() {
		t.Fatal("dismiss-preview-on-switch still off after clicking on")
	}
	// The SECURITY biometric-unlock toggle sits below infinite scroll; re-pin the
	// bottom and flip it on then off, asserting the route reached SetBiometricUnlock.
	s.Scroll(1 << 20)
	clickValue(t, h, ui.HitBiometricUnlock, "on")
	if !s.BiometricUnlock() {
		t.Fatal("biometric unlock still off after clicking on")
	}
	clickValue(t, h, ui.HitBiometricUnlock, "off")
	if s.BiometricUnlock() {
		t.Fatal("biometric unlock still on after clicking off")
	}
	// Close the editor -> back to the feed.
	click(t, h, ui.HitCloseSettings)
	if s.Mode() != ui.ModeFeed {
		t.Fatal("settings did not close")
	}
}

func TestKeySettingsCommits(t *testing.T) {
	a := profApp(t)
	h := New(a)
	s := a.Scene()
	a.VM().OpenSettings.Execute() // enter the editor through the VM

	// Enter with the channel field focused adds a subscription.
	s.FocusChannel()
	s.TypeRune('r')
	s.TypeRune('s')
	n := len(s.ActiveProfile().Subs)
	h.Key("Enter", 0)
	if len(s.ActiveProfile().Subs) != n+1 {
		t.Fatalf("Enter did not add sub: %d", len(s.ActiveProfile().Subs))
	}
	// Enter with the rename field focused renames the profile.
	s.FocusRename()
	for _, r := range "Renamed" {
		s.TypeRune(r)
	}
	h.Key("Enter", 0)
	// Enter with the cache field focused commits the cache path.
	s.FocusCache()
	for _, r := range "/mnt/c" {
		s.TypeRune(r)
	}
	h.Key("Enter", 0)
	if s.CachePath() == "" {
		t.Fatal("cache not committed on Enter")
	}
	// Enter with no field focused (fresh OpenSettings) is a harmless no-op.
	s.OpenSettings()
	if s.Focus() != ui.FocusNone {
		t.Fatal("fresh settings should have no focus")
	}
	h.Key("Enter", 0)

	// Escape closes the settings editor.
	h.Key("Escape", 0)
	if s.Mode() != ui.ModeFeed {
		t.Fatal("Escape should close settings")
	}
	// Backspace routes into the focused settings field.
	a.VM().OpenSettings.Execute()
	s.FocusChannel()
	s.TypeRune('x')
	h.Key("Backspace", 0)
	if s.Focus() != ui.FocusChannel {
		t.Fatal("backspace should keep channel focus")
	}
}

func TestAccountsRouting(t *testing.T) {
	a := profApp(t)
	h := New(a)
	s := a.Scene()

	// Open the accounts editor via the sidebar entry.
	click(t, h, ui.HitAccounts)
	if s.Mode() != ui.ModeAccounts {
		t.Fatal("HitAccounts should open the accounts editor")
	}
	// Pick a provider (Mastodon), which has a bool-less field set.
	before := s.Mode()
	found := false
	for y := 0; y < s.H && !found; y += 2 {
		for x := 0; x < s.W; x += 2 {
			if hit := s.HitTest(x, y); hit.Kind == ui.HitSelectAccount && hit.Value == string(source.Usenet) {
				h.MouseDown(x, y)
				found = true
				break
			}
		}
	}
	if !found || s.Mode() != before {
		t.Fatal("selecting the Usenet provider failed")
	}
	// Focus a text field and type into it.
	click(t, h, ui.HitFocusAccountField)
	h.Key("", 'a')
	// Toggle the Usenet TLS boolean field on.
	click(t, h, ui.HitToggleAccountBool)
	accs := s.EditedAccounts()
	var tlsOn bool
	for _, acc := range accs {
		if acc.Kind == source.Usenet && acc.Fields["tls"] == "true" {
			tlsOn = true
		}
	}
	if !tlsOn {
		t.Fatalf("TLS toggle did not set tls=true: %+v", accs)
	}
	// Enter applies in place (stays in the editor).
	h.Key("Enter", 0)
	if s.Mode() != ui.ModeAccounts {
		t.Fatal("Enter should apply accounts without leaving the editor")
	}
	// Done commits and returns to the feed.
	click(t, h, ui.HitCloseAccounts)
	if s.Mode() != ui.ModeFeed {
		t.Fatal("HitCloseAccounts should return to the feed")
	}
	// Escape also commits + closes the editor.
	a.VM().OpenAccounts.Execute()
	h.Key("Escape", 0)
	if s.Mode() != ui.ModeFeed {
		t.Fatal("Escape in the accounts editor should return to the feed")
	}
}

// fakeImporter is a cookieImporter stub for the Firefox-import routing tests.
type fakeImporter struct{ v string }

func (f fakeImporter) RedditSession() (browsercookies.RedditSession, error) {
	return browsercookies.RedditSession{Value: f.v, Host: ".reddit.com"}, nil
}
func (f fakeImporter) InstagramSession() (string, error) { return "sessionid=" + f.v, nil }
func (f fakeImporter) TikTokSession() (string, error)    { return "sessionid=" + f.v, nil }
func (f fakeImporter) TwitterSession() (string, error)   { return "auth_token=" + f.v, nil }

func TestImportRedditFirefoxRouting(t *testing.T) {
	a := profApp(t)
	a.SetCookieFinder(fakeImporter{v: "FF-COOKIE"})
	a.SetRegistryBuilder(func(feeds.Options) *source.Registry { return source.NewRegistry() })
	h := New(a)
	s := a.Scene()

	// Reddit is the default provider when the editor opens, so its "Import session
	// from Firefox" button is present. Clicking it routes to the app import method.
	click(t, h, ui.HitAccounts)
	if s.SelectedAccount() != source.Reddit {
		t.Fatalf("accounts editor should default to Reddit, got %v", s.SelectedAccount())
	}
	click(t, h, ui.HitImportRedditFirefox)

	// The imported cookie landed in the Reddit account's session_cookie field.
	acct, ok := s.Settings().Account(source.Reddit)
	if !ok || acct.Fields["session_cookie"] != "FF-COOKIE" {
		t.Fatalf("Firefox import did not store the cookie: %+v ok=%v", acct, ok)
	}
}

func TestImportSessionRouting(t *testing.T) {
	a := profApp(t)
	a.SetCookieFinder(fakeImporter{v: "FF"})
	a.SetRegistryBuilder(func(feeds.Options) *source.Registry { return source.NewRegistry() })
	h := New(a)
	s := a.Scene()

	// Selecting Instagram surfaces the generic "Import session from Firefox" button;
	// clicking it routes to app.ImportSessionFromFirefox, which stores the cookie in
	// the Instagram account's "session" field.
	click(t, h, ui.HitAccounts)
	s.SelectAccount(source.Instagram)
	click(t, h, ui.HitImportSession)

	acct, ok := s.Settings().Account(source.Instagram)
	if !ok || acct.Fields["session"] != "sessionid=FF" {
		t.Fatalf("session import did not store the cookie: %+v ok=%v", acct, ok)
	}
}

func TestMouseDownLogAndClose(t *testing.T) {
	a := profApp(t)
	h := New(a)
	s := a.Scene()
	// The sidebar Network-log entry opens the log view.
	click(t, h, ui.HitLog)
	if s.Mode() != ui.ModeLog {
		t.Fatal("HitLog should open the Network log")
	}
	// Its "< Back" button closes it.
	click(t, h, ui.HitCloseLog)
	if s.Mode() != ui.ModeFeed {
		t.Fatal("HitCloseLog should return to the feed")
	}
	// Escape also closes the log.
	a.VM().OpenLog.Execute()
	h.Key("Escape", 0)
	if s.Mode() != ui.ModeFeed {
		t.Fatal("Escape in the log should return to the feed")
	}
}

func TestMouseDownBurgerToggles(t *testing.T) {
	a := newApp(t)
	h := New(a)
	s := a.Scene()
	if s.SidebarCollapsed() {
		t.Fatal("sidebar should start expanded")
	}
	h.MouseDown(5, 10) // burger at the topbar's left
	if !s.SidebarCollapsed() {
		t.Fatal("burger click should collapse the sidebar")
	}
	h.MouseDown(5, 10) // second click restores it
	if s.SidebarCollapsed() {
		t.Fatal("second burger click should expand the sidebar")
	}
}

// dividerX returns the leftmost x at which HitTest reports the sidebar divider,
// which tracks the sidebar's right edge (a proxy for the effective width).
func dividerX(t *testing.T, s *ui.Scene) int {
	t.Helper()
	for x := 0; x < s.W; x++ {
		if s.HitTest(x, s.H/2).Kind == ui.HitSidebarDivider {
			return x
		}
	}
	t.Fatal("no divider hit region")
	return 0
}

func TestSidebarDividerDrag(t *testing.T) {
	a := newApp(t)
	h := New(a)
	s := a.Scene()
	before := dividerX(t, s)
	// Grab the divider and drag it wider; the edge (and its divider) move right.
	h.MouseDown(before, s.H/2)
	if !s.DraggingSidebar() {
		t.Fatal("MouseDown on the divider should begin a resize")
	}
	h.MouseMove(before+150, s.H/2)
	afterDrag := dividerX(t, s)
	if afterDrag <= before {
		t.Fatalf("drag did not widen the sidebar: edge %d <= %d", afterDrag, before)
	}
	h.MouseUp(before+150, s.H/2)
	if s.DraggingSidebar() {
		t.Fatal("MouseUp should end the resize")
	}
	// After release, a further move must not resize (the edge stays put).
	held := dividerX(t, s)
	h.MouseMove(before+300, s.H/2)
	if dividerX(t, s) != held {
		t.Fatal("move after release must not resize")
	}
}

func TestPreviewDividerDrag(t *testing.T) {
	a := newApp(t)
	a.Scene().SetItems([]source.Item{{ID: "1", Source: source.Reddit, Title: "hi", Score: -1, Comments: -1}})
	h := New(a)
	s := a.Scene()
	s.HitTest(0, 0) // lay out the pane
	// The grip sits at the pane's left edge; find it by scanning right-to-left.
	gripX := -1
	for x := s.W - 1; x >= 0; x-- {
		if s.HitTest(x, s.H/2).Kind == ui.HitPreviewDivider {
			gripX = x
			break
		}
	}
	if gripX < 0 {
		t.Fatal("no preview divider hit region")
	}
	h.MouseDown(gripX, s.H/2)
	if !s.DraggingPreview() {
		t.Fatal("MouseDown on the pane grip should begin a resize")
	}
	h.MouseMove(gripX-120, s.H/2) // drag left => wider pane
	h.MouseUp(gripX-120, s.H/2)
	if s.DraggingPreview() {
		t.Fatal("MouseUp should end the preview resize")
	}
}

func TestMouseDownUnsubscribeGroup(t *testing.T) {
	set := &settings.Settings{Profiles: []settings.Profile{{Name: "Home", Subs: []source.Subscription{
		{Source: source.Usenet, Channel: "control"},
	}}}, Active: 0, Theme: settings.ThemeSystem}
	a := app.New(app.Config{Registry: source.NewRegistry(), Settings: set, Width: 900, Height: 600})
	a.SetRefreshHook(func() {})
	a.Scene().SetUsenetServer("news.free.fr:119")
	a.Scene().SetBrowseGroups([]source.GroupInfo{{Name: "control"}})
	a.Scene().OpenBrowse()
	s := a.Scene()
	// Click the subscribed "control" leaf's ✓ marker → unsubscribe.
	s.HitTest(0, 0)
	var hit bool
	for x := 0; x < s.W && !hit; x++ {
		for y := 0; y < s.H; y++ {
			if s.HitTest(x, y).Kind == ui.HitUnsubscribeGroup {
				New(a).MouseDown(x, y)
				hit = true
				break
			}
		}
	}
	if !hit {
		t.Fatal("no HitUnsubscribeGroup region found")
	}
	if s.IsSubscribed(source.Usenet, "control") {
		t.Fatal("control should be unsubscribed after clicking its ✓")
	}
}

func TestBrowserCommand(t *testing.T) {
	cases := []struct {
		goos, wantCmd string
		wantArg0      string
	}{
		{"darwin", "open", "https://x"},
		{"windows", "rundll32", "url.dll,FileProtocolHandler"},
		{"linux", "xdg-open", "https://x"},
	}
	for _, c := range cases {
		cmd, args := browserCommand(c.goos, "https://x")
		if cmd != c.wantCmd || len(args) == 0 || args[0] != c.wantArg0 {
			t.Fatalf("%s: cmd=%q args=%v", c.goos, cmd, args)
		}
	}
}

func TestExecStart(t *testing.T) {
	// Exercise the real execStart closure. An empty command name makes
	// exec.Command("").Start() fail immediately, so no process is spawned.
	if err := execStart(""); err == nil {
		t.Fatal("execStart(\"\") should error")
	}
}

func TestDefaultOpenURL(t *testing.T) {
	var gotName string
	var gotArgs []string
	orig := execStart
	execStart = func(name string, args ...string) error { gotName, gotArgs = name, args; return nil }
	t.Cleanup(func() { execStart = orig })
	if err := defaultOpenURL("https://ex"); err != nil {
		t.Fatal(err)
	}
	if gotName == "" || len(gotArgs) == 0 {
		t.Fatalf("execStart not called: %q %v", gotName, gotArgs)
	}
}

func TestMouseDownFixAuthOpensAccounts(t *testing.T) {
	a := newApp(t)
	s := a.Scene()
	s.SetAuthPrompts([]ui.AuthPrompt{
		{Kind: source.Mastodon, Reason: "access token required/invalid"},
	})
	h := New(a)
	// Click the in-feed "needs sign-in" banner.
	click(t, h, ui.HitFixAuth)
	if s.Mode() != ui.ModeAccounts {
		t.Fatalf("mode = %v, want ModeAccounts", s.Mode())
	}
	if s.SelectedAccount() != source.Mastodon {
		t.Fatalf("selected account = %q, want mastodon", s.SelectedAccount())
	}
}

// TestWebPreviewEventForwarding covers routing raw mouse / scroll / key events
// into the embedded web-preview browser (a link click, a wheel, address-field
// typing) versus falling through to the feed. The async render is stubbed to a
// no-op so no network runs; a canned page is delivered directly.
func TestWebPreviewEventForwarding(t *testing.T) {
	a := app.New(app.Config{Registry: source.NewRegistry(), Width: 1000, Height: 700})
	a.Scene().SetScale(1)
	a.SetWebFetchHook(func(string, int, int64) {}) // no-op: navigation must not hit the network
	s := a.Scene()
	it := source.Item{ID: "w", Source: source.HackerNews, Title: "T", Link: "https://site/"}
	s.SelectPreview(it) // opens the browser tab (OnNavigate → the no-op fetch)
	page := image.NewRGBA(image.Rect(0, 0, 400, 1200))
	s.Browser().Deliver("https://site/", page.Pix, 400, 1200, 400,
		[]toolkit.BrowserLink{{Rect: image.Rect(0, 0, 400, 1200), Href: "https://site/next"}}, "T")
	a.Frame() // lay out the browser bounds

	h := New(a)
	b := s.Browser().Bounds()

	// A click in the browser's content area is forwarded (a link → in-pane nav).
	h.MouseDown(b.X+b.W/2, b.Y+b.H-4)
	if !s.BrowserFocused() {
		t.Fatal("a click inside the browser should focus + be forwarded to it")
	}
	// Release the button: the wheel gestures below are made with no button held, so
	// motion updates the hover position rather than being captured as a browser drag.
	h.MouseUp(b.X+b.W/2, b.Y+b.H-4)

	// A wheel with the pointer over the browser scrolls the page (forwarded);
	// with the pointer over the feed it scrolls the feed instead.
	h.MouseMove(b.X+b.W/2, b.Y+b.H/2)
	h.Scroll(60)
	h.MouseMove(2, s.H/2)
	h.Scroll(60)

	// Focus the address field, then keys route to the browser; Enter commits it.
	h.MouseDown(b.X+b.W*3/4, b.Y+toolkit.BrowserToolbarH/2)
	h.Key("", 'q')        // → EventChar into the address buffer
	h.Key("Backspace", 0) // → EventKeyDown Backspace
	h.Key("Enter", 0)     // → commit/navigate (stubbed fetch)

	// A click on empty pane / feed space is NOT forwarded and blurs the browser.
	h.MouseDown(b.X+b.W/2, b.Y+b.H-4) // re-focus first
	h.MouseDown(2, s.H-2)             // bottom-left (feed) → default, not forwarded
	if s.BrowserFocused() {
		t.Fatal("a click off the browser should blur its focus")
	}
}

// mustFrame returns just the current framebuffer.
func mustFrame(a *app.App) []byte { buf, _ := a.Frame(); return buf }

// TestMouseMoveRoutesBrowserDrag checks the MouseMove seam: while a press is held
// inside the embedded web-preview browser, pointer motion is routed to the browser
// as a drag (moving a grabbed scrollbar thumb); once released, motion falls through
// to the scene and no longer moves the browser.
func TestMouseMoveRoutesBrowserDrag(t *testing.T) {
	th := ui.ThemeFor(ui.OSMac, false)
	a := app.New(app.Config{Registry: source.NewRegistry(), Width: 1000, Height: 700})
	s := a.Scene()
	s.SetScale(1)
	s.SetTheme(th) // known accent so the drawn thumb colour is predictable
	a.SetWebFetchHook(func(string, int, int64) {})
	s.SelectPreview(source.Item{ID: "w", Source: source.HackerNews, Title: "T", Link: "https://site/"})
	a.Frame() // lay out the browser bounds
	b := s.Browser().Bounds()
	// Open the tab and deliver a page 3× the content height (matching the tab URL so
	// the render is accepted) so a vertical scrollbar thumb exists.
	s.Browser().Open("https://site/", "T")
	page := image.NewRGBA(image.Rect(0, 0, b.W, b.H*3))
	s.Browser().Deliver("https://site/", page.Pix, b.W, b.H*3, b.W, nil, "T")

	h := New(a)
	// The browser's own bar is hidden (the reader overlays the shared Scrollbar);
	// its thumb stays grabbable via geometry and hugs the content top at scroll=0.
	// The scroll offset (ScrollExtent), not a painted thumb, proves the motion.
	off := func() int { o, _, _, _ := s.Browser().ScrollExtent(); return o }
	_, vp, _, shown := s.Browser().ScrollExtent()
	if !shown {
		t.Fatal("no vertical overflow for a 3×-tall page")
	}
	contentTop := b.Y + b.H - vp
	x := b.X + b.W - 3
	before := off()

	// A move with NO press held must NOT drag the browser (offset stays put).
	h.MouseMove(x, contentTop+b.H/3)
	mustFrame(a)
	if got := off(); got != before {
		t.Fatalf("move without a press scrolled: %d -> %d", before, got)
	}

	// Press ON the thumb, then move: the motion is routed as a browser drag and the
	// page scrolls (offset grows).
	h.MouseDown(x, contentTop+2)
	h.MouseMove(x, contentTop+b.H/3)
	mustFrame(a)
	after := off()
	if !(after > before) {
		t.Fatalf("MouseMove during a browser press did not scroll: %d -> %d", before, after)
	}

	// Release clears the grab: a further move no longer drags the browser, so the
	// offset stays where the release left it.
	h.MouseUp(x, contentTop+b.H/3)
	mustFrame(a)
	settled := off()
	h.MouseMove(x, contentTop+2) // would scroll UP if still routed
	mustFrame(a)
	if got := off(); got != settled {
		t.Fatalf("after release a move still dragged the browser: %d -> %d", settled, got)
	}
}

// TestA11yElementsSatisfiesTheWindowContract is the check that the bridge is
// wired at all: the back-end asks for the description through an OPTIONAL
// interface, so a Handler that fails to implement it is not a compile error —
// it silently presents pixels and nothing else, and a screen reader stays quiet.
func TestA11yElementsSatisfiesTheWindowContract(t *testing.T) {
	var h any = New(profApp(t))
	if _, ok := h.(window.Accessible); !ok {
		t.Fatal("the handler does not implement window.Accessible; the back-end would never ask it for anything")
	}
}

// TestA11yElementsTranslatesTheSceneTree checks the translation carries what a
// reader needs — roles, names, values and rects — without the window package
// learning what a feed is.
func TestA11yElementsTranslatesTheSceneTree(t *testing.T) {
	a := profApp(t)
	a.Scene().SetItems([]source.Item{
		{ID: "1", Source: source.HackerNews, Title: "Show HN: a pure-Go news reader", Score: 99, Comments: 4},
	})
	h := New(a)

	elems := h.A11yElements()
	if len(elems) < 6 {
		t.Fatalf("got %d elements, too few to describe a populated feed", len(elems))
	}

	var post, settings *window.A11yElement
	for i := range elems {
		switch elems[i].Name {
		case "Show HN: a pure-Go news reader":
			post = &elems[i]
		case "Settings":
			settings = &elems[i]
		}
	}
	if post == nil {
		t.Fatal("the feed item is not described")
	}
	if post.Role != "group" {
		t.Errorf("post role = %q, want the scene's own role name", post.Role)
	}
	if post.Value == "" {
		t.Error("the post carries no meta line")
	}
	if post.W <= 0 || post.H <= 0 {
		t.Errorf("post rect = %dx%d, want a real area", post.W, post.H)
	}
	if settings == nil {
		t.Fatal("the Settings control is not described")
	}
	if settings.Role != "button" {
		t.Errorf("Settings role = %q, want button", settings.Role)
	}

	// The tree must mirror the scene's, one for one: dropping or inventing
	// elements here would make the accessibility view disagree with the UI.
	if len(elems) != len(a.Scene().A11yTree()) {
		t.Fatalf("translated %d elements from a tree of %d", len(elems), len(a.Scene().A11yTree()))
	}
}

// TestA11yElementsRectsAreInTheEventCoordinateSpace pins the contract the macOS
// back-end relies on: element rects are in the SAME device-pixel, top-left space
// as MouseDown, so the back-end's one conversion is correct for every element.
// If these drifted apart, VoiceOver would point at the wrong place on screen.
func TestA11yElementsRectsAreInTheEventCoordinateSpace(t *testing.T) {
	a := profApp(t)
	h := New(a)
	for _, e := range h.A11yElements() {
		if e.Name != "Settings" {
			continue
		}
		cx, cy := e.X+e.W/2, e.Y+e.H/2
		if got := a.Scene().HitTest(cx, cy); got.Kind != ui.HitSettings {
			t.Fatalf("the Settings element's centre hit-tests to %v, not the Settings control", got.Kind)
		}
		return
	}
	t.Fatal("no Settings element to check")
}

// TestSidebarSourceAccordionClick clicks a source-group header and checks the
// click toggles the accordion open, then closed — covering the HitToggleSource
// dispatch and the scene's single-open accordion.
func TestSidebarSourceAccordionClick(t *testing.T) {
	a := app.New(app.Config{Registry: source.NewRegistry(), Width: 1000, Height: 700})
	h := New(a)
	s := a.Scene()
	s.SetSubs([]ui.Subscription{{Source: source.Reddit, Channel: "golang"}})

	x, y, ok := findHit(s, ui.HitToggleSource)
	if !ok {
		t.Fatal("no source-group header row in the sidebar")
	}
	h.MouseDown(x, y)
	if s.SidebarSourceOpen() != source.Reddit {
		t.Fatalf("first click: SidebarSourceOpen = %q, want reddit", s.SidebarSourceOpen())
	}
	x, y, ok = findHit(s, ui.HitToggleSource) // the header stays put; re-find and click again
	if !ok {
		t.Fatal("source header vanished after expanding")
	}
	h.MouseDown(x, y)
	if s.SidebarSourceOpen() != "" {
		t.Fatalf("second click: SidebarSourceOpen = %q, want collapsed", s.SidebarSourceOpen())
	}
}

// findSidebarSub scans for a HitSub row, returning coords for one that maps to a
// real subscription (real=true) or to the All filter (real=false).
func findSidebarSub(s *ui.Scene, real bool) (int, int, bool) {
	for y := 0; y < s.H; y += 2 {
		for x := 0; x < s.W; x += 2 {
			if hit := s.HitTest(x, y); hit.Kind == ui.HitSub {
				if _, ok := s.SubAt(hit.Sub); ok == real {
					return x, y, true
				}
			}
		}
	}
	return 0, 0, false
}

func TestSecondaryClickOpensSubMenu(t *testing.T) {
	a := app.New(app.Config{Registry: source.NewRegistry(), Width: 1000, Height: 700})
	h := New(a)
	s := a.Scene()
	s.SetSubs([]ui.Subscription{
		{Source: source.HackerNews, Channel: "top"},
		{Source: source.Reddit, Channel: "golang"},
	})
	s.ToggleSidebarSource(source.HackerNews) // expand a group so a real subscription row is visible

	// A press off the sidebar (the feed area) opens nothing.
	h.SecondaryClick(s.W-5, s.H-5)
	if h.ContextMenuActive() {
		t.Fatal("a press off a subscription must not open a menu")
	}

	// The All-filter row is a HitSub but maps to no subscription: still nothing.
	if ax, ay, ok := findSidebarSub(s, false); ok {
		h.SecondaryClick(ax, ay)
		if h.ContextMenuActive() {
			t.Fatal("right-click on the All row must not open a subscription menu")
		}
	}

	// A real subscription row opens the menu.
	rx, ry, ok := findSidebarSub(s, true)
	if !ok {
		t.Fatal("no real subscription row found in the sidebar")
	}
	h.SecondaryClick(rx, ry)
	if !h.ContextMenuActive() {
		t.Fatal("right-click on a subscription should open the context menu")
	}
	// Escape closes it (drives ContextMenuActive + ContextMenuEvent).
	h.ContextMenuEvent(toolkit.Event{Kind: toolkit.EventKeyDown, Code: "Escape"})
	if h.ContextMenuActive() {
		t.Error("Escape should close the menu")
	}
}

func TestSubContextMenuActions(t *testing.T) {
	a := app.New(app.Config{Registry: source.NewRegistry(), Width: 800, Height: 600})
	// Mirror the native front-end: background scene writes are enqueued under a
	// lock and applied on the render thread, so the Refresh/re-aggregate actions
	// these items fire serialize instead of racing (as they do with inline writes).
	a.DeferSceneWrites()
	h := New(a)
	s := a.Scene()
	// A real profile so the Unsubscribe action (which edits the active profile)
	// actually removes the sub and reaches ApplySceneSettings.
	s.SetProfiles([]settings.Profile{{Name: "Home", Subs: []source.Subscription{
		{Source: source.HackerNews, Channel: "top"},
	}}}, 0)

	menu := h.subContextMenu(0, source.HackerNews, "top")
	labels := []string{}
	for _, it := range menu.Menu.Items {
		if it.Separator {
			labels = append(labels, "---")
			continue
		}
		labels = append(labels, it.Label)
	}
	want := []string{"Refresh", "Mark as read", "---", "New folder", "Remove from folder", "---", "Unsubscribe"}
	if len(labels) != len(want) {
		t.Fatalf("menu labels = %v, want %v", labels, want)
	}
	for i := range want {
		if labels[i] != want[i] {
			t.Fatalf("menu label[%d] = %q, want %q", i, labels[i], want[i])
		}
	}

	// Fire "Mark as read" and "Unsubscribe" (Unsubscribe drops the sub); leave the
	// Refresh action (which spawns a background aggregate) for last and assert
	// nothing about the scene after it.
	menu.Menu.Items[1].Action() // Mark as read
	menu.Menu.Items[6].Action() // Unsubscribe
	if _, ok := s.SubAt(0); ok {
		t.Error("the Unsubscribe action should have removed the subscription")
	}
	menu.Menu.Items[0].Action() // Refresh (spawns a goroutine on an empty registry)
}

// TestBrowseFilterCaretEditing: a focused newsgroup filter accepts caret keys
// (Left/Right, Home/End) so text can be corrected mid-value, while Up/Down and
// unmapped keys fall through and an unfocused filter ignores caret keys.
func TestBrowseFilterCaretEditing(t *testing.T) {
	a := newApp(t)
	h := New(a)
	s := a.Scene()
	s.SetUsenetServer("news:119")
	s.OpenBrowse()
	s.FocusBrowseFilter(true)

	// Type "ac", move the caret back between them, insert "b" → "abc".
	h.Key("", 'a')
	h.Key("", 'c')
	h.Key("Left", 0)
	h.Key("", 'b')
	if got := s.BrowseEntry().Text().Get(); got != "abc" {
		t.Fatalf("Left+insert = %q, want abc (caret must move back)", got)
	}
	// Home jumps to the start; typing prepends.
	h.Key("Home", 0)
	h.Key("", 'z')
	if got := s.BrowseEntry().Text().Get(); got != "zabc" {
		t.Fatalf("Home+insert = %q, want zabc", got)
	}
	// End jumps to the end; typing appends.
	h.Key("End", 0)
	h.Key("", 'q')
	if got := s.BrowseEntry().Text().Get(); got != "zabcq" {
		t.Fatalf("End+insert = %q, want zabcq", got)
	}
	// Right at end is a harmless no-op; text unchanged.
	h.Key("Right", 0)
	// An unmapped key (PageUp) falls through the caret router unchanged.
	h.Key("PageUp", 0)
	if got := s.BrowseEntry().Text().Get(); got != "zabcq" {
		t.Fatalf("unmapped key changed filter text to %q", got)
	}
	// With the filter unfocused, caret keys are not consumed here (no text change).
	s.FocusBrowseFilter(false)
	h.Key("Left", 0)
	if got := s.BrowseEntry().Text().Get(); got != "zabcq" {
		t.Fatalf("Left while unfocused changed text to %q", got)
	}
}
