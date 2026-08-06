package windowapp

import (
	"image"
	"image/color"
	"testing"

	"github.com/go-widgets/toolkit"

	"github.com/go-news-reader/reader/app"
	"github.com/go-news-reader/reader/internal/settings"
	"github.com/go-news-reader/reader/internal/window"
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

// click routes a MouseDown at the first coordinate matching kind (fatal if none).
func click(t *testing.T, h *Handler, kind ui.HitKind) {
	t.Helper()
	x, y, ok := findHit(h.a.Scene(), kind)
	if !ok {
		t.Fatalf("no hit region for kind %d", kind)
	}
	h.MouseDown(x, y)
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
	h.MouseDown(250, 60) // feed row 0 -> select into the right preview pane
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
	h.MouseDown(oc.X+oc.W/2, oc.Y+oc.H/2)
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
	New(a).MouseDown(10, 60) // "All" sidebar row
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
	New(a).MouseDown(10, 400) // empty sidebar area -> HitNone
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
	click(t, h, ui.HitTheme)       // pick a theme
	click(t, h, ui.HitBrowserTabs) // switch the web-preview browser tab mode
	click(t, h, ui.HitDeleteProfile)
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
	a.SetWebFetchHook(func(string, int) {}) // no-op: navigation must not hit the network
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

	// A wheel with the pointer over the browser scrolls the page (forwarded);
	// with the pointer over the feed it scrolls the feed instead.
	h.MouseMove(b.X+b.W/2, b.Y+b.H/2)
	h.Scroll(60)
	h.MouseMove(2, s.H/2)
	h.Scroll(60)

	// Focus the address field, then keys route to the browser; Enter commits it.
	h.MouseDown(b.X+b.W*3/4, b.Y+toolkit.BrowserToolbarH/2)
	h.Key("", 'q')       // → EventChar into the address buffer
	h.Key("Backspace", 0) // → EventKeyDown Backspace
	h.Key("Enter", 0)     // → commit/navigate (stubbed fetch)

	// A click on empty pane / feed space is NOT forwarded and blurs the browser.
	h.MouseDown(b.X+b.W/2, b.Y+b.H-4) // re-focus first
	h.MouseDown(2, s.H-2)             // bottom-left (feed) → default, not forwarded
	if s.BrowserFocused() {
		t.Fatal("a click off the browser should blur its focus")
	}
}
