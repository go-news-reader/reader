package ui

import (
	"fmt"
	"testing"

	"github.com/go-widgets/toolkit"

	"github.com/go-news-reader/reader/internal/settings"
	"github.com/go-news-reader/reader/source"
)

// C3: a click in the browser's filter/count chrome (above the tree viewport)
// must not select a scrolled-out row through it.
func TestBrowseHitTestClampsChrome(t *testing.T) {
	s := New(760, 460, ThemeFor(OSMac, false))
	s.SetUsenetServer("news.free.fr:119")
	var g []source.GroupInfo
	for i := 0; i < 300; i++ {
		g = append(g, source.GroupInfo{Name: fmt.Sprintf("grp%03d", i)}) // flat, distinct top-level rows
	}
	s.SetBrowseGroups(g)
	s.OpenBrowse()
	s.layoutBrowse()
	for i := 0; i < 12; i++ { // scroll so rows leave the viewport top AND bottom
		s.Scroll(60)
	}
	if s.browseScrollY <= 0 {
		t.Fatal("expected the tree to have scrolled")
	}
	if s.browseTreeTop <= s.m.topbarH {
		t.Fatal("tree top not below the topbar")
	}
	if h := s.browseHitTest(30, s.browseTreeTop-2); h.Kind != HitNone {
		t.Fatalf("click in count/filter band = %v, want HitNone", h.Kind)
	}
	// A click on the first visible row still resolves — the loop skips the rows
	// scrolled above the viewport (top+sideItemH < treeTop) and below it (top >=
	// H) to reach it.
	if h := s.browseHitTest(30, s.browseTreeTop+2); h.Kind != HitSubscribeGroup {
		t.Fatalf("click on first visible row = %v, want HitSubscribeGroup", h.Kind)
	}
}

// C3 (cont.): clicking a top-level leaf group's subscribe (＋) rect subscribes to
// it (the inRect branch), distinct from clicking elsewhere on the row.
func TestBrowseHitTestSubscribeRect(t *testing.T) {
	s := New(760, 460, ThemeFor(OSMac, false))
	s.SetUsenetServer("news.free.fr:119")
	s.SetBrowseGroups(gis("control", "junk")) // flat top-level leaves, no nesting
	s.OpenBrowse()
	s.layoutBrowse()
	r := s.browseRows[0]
	if !r.node.IsGroup {
		t.Fatalf("first row not a leaf group: %+v", r.node)
	}
	top := s.browseTreeTop + r.top - s.browseScrollY
	m := s.m
	sr := s.browseSubscribeRect(m.pad, top, s.W-2*m.pad)
	if h := s.browseHitTest(sr.X+sr.W/2, sr.Y+sr.H/2); h.Kind != HitSubscribeGroup || h.Value != "control" {
		t.Fatalf("subscribe-rect click = %+v, want HitSubscribeGroup control", h)
	}
	// A click in the empty tree area below the two rows (still inside the viewport)
	// falls through the loop to HitNone.
	if h := s.browseHitTest(30, s.H-1); h.Kind != HitNone {
		t.Fatalf("click below all rows = %v, want HitNone", h.Kind)
	}
}

// C2: textFace.clipRight keeps the trailing run that fits, returns "" when even
// the last rune does not fit or the width is non-positive, and passes short
// strings through unchanged.
func TestClipRight(t *testing.T) {
	s := New(500, 300, ThemeFor(OSMac, false))
	s.layout()
	f := s.m.tab
	if got := f.clipRight("hello", 0); got != "" {
		t.Fatalf("w=0 -> %q, want empty", got)
	}
	if got := f.clipRight("hi", 100000); got != "hi" {
		t.Fatalf("fits -> %q, want 'hi'", got)
	}
	long := "abcdefghijklmnopqrstuvwxyz0123456789"
	got := f.clipRight(long, f.width("xyz789"))
	if got == "" || got == long || len(got) >= len(long) {
		t.Fatalf("clip -> %q, want a shorter trailing run", got)
	}
	if want := long[len(long)-len(got):]; got != want {
		t.Fatalf("clip -> %q, want trailing %q", got, want)
	}
	if got := f.clipRight(long, 1); got != "" {
		t.Fatalf("width below one glyph -> %q, want empty", got)
	}
}

// C2/C5: drawInput clips over-long text and draws a caret for a focused field —
// including an empty one — with the caret clamped inside the box.
func TestDrawInputClipAndCaret(t *testing.T) {
	s := New(520, 360, ThemeFor(OSMac, false))
	s.SetProfiles([]settings.Profile{{Name: "P"}}, 0)
	s.OpenSettings()
	buf := make([]byte, s.W*s.H*4)
	// Long value in a focused field: exercises the clip + caret-clamp path.
	s.channelInput = "averyverylongchannelnamethatoverflowsthefieldwidthbyquitealot"
	s.FocusChannel()
	s.Draw(buf)
	// Focused empty field: caret drawn at the field start (C5).
	s.renameInput = ""
	s.FocusRename()
	s.Draw(buf)
}

// C3: a click in the Accounts topbar band (above the scrolled content) hits no
// row/pill through the chrome.
func TestAccountsHitTestClampsTopbar(t *testing.T) {
	s := New(500, 300, ThemeFor(OSMac, false))
	s.OpenAccounts()
	s.layoutAccounts()
	if h := s.accountsHitTest(s.W/2, s.m.topbarH-1); h.Kind != HitNone && h.Kind != HitCloseAccounts {
		t.Fatalf("click in topbar band = %v, want HitNone/Close", h.Kind)
	}
}

// C4: reopening Accounts (and switching provider) starts scrolled to the top.
func TestAccountsScrollResetOnOpen(t *testing.T) {
	s := New(500, 300, ThemeFor(OSMac, false))
	s.OpenAccounts()
	s.accScrollY = 999 // simulate a prior scroll
	s.OpenAccounts()
	if s.accScrollY != 0 {
		t.Fatalf("OpenAccounts left accScrollY=%d, want 0", s.accScrollY)
	}
	s.accScrollY = 777
	s.SelectAccount(source.Usenet)
	if s.accScrollY != 0 {
		t.Fatalf("SelectAccount left accScrollY=%d, want 0", s.accScrollY)
	}
}

// C1: in a short window the settings editor overflows; the bottom controls (the
// media-cache field) must be reachable by scrolling, not stranded off-screen.
func TestSettingsScrollReachesBottom(t *testing.T) {
	s := New(420, 240, ThemeFor(OSMac, false))
	var many []source.Subscription
	for i := 0; i < 8; i++ {
		many = append(many, source.Subscription{Source: source.Reddit, Channel: "chan", Limit: 25})
	}
	s.SetProfiles([]settings.Profile{{Name: "P", Subs: many}}, 0)
	s.OpenSettings()
	s.layoutSettings()
	if s.settingsContentH <= s.H-s.m.topbarH {
		t.Skip("content fits; window too tall to exercise overflow")
	}
	// Before scrolling, the cache field sits below the viewport (unreachable).
	if s.sCacheR.Y < s.H {
		t.Fatalf("cache field already on-screen (Y=%d, H=%d); test needs it below", s.sCacheR.Y, s.H)
	}
	// Scroll to the bottom (over-scroll is clamped) and it must be reachable.
	for i := 0; i < 40; i++ {
		s.Scroll(120)
	}
	if s.sCacheR.Y < s.m.topbarH || s.sCacheR.Y+s.sCacheR.H > s.H {
		t.Fatalf("cache field not in viewport after scroll: Y=%d H=%d winH=%d", s.sCacheR.Y, s.sCacheR.H, s.H)
	}
	if h := s.HitTest(s.sCacheR.X+4, s.sCacheR.Y+s.sCacheR.H/2); h.Kind != HitFocusCache {
		t.Fatalf("click on scrolled-in cache field = %v, want HitFocusCache", h.Kind)
	}
}

// B2: themeOnAccent must not fall back to the background colour for themes that
// don't tag Extra["OnAccent"] (WhiteSur, Default) — it would paint near-black
// topbar text on the blue accent. It must instead pick a luminance-contrasting
// ink.
func TestThemeOnAccentFallback(t *testing.T) {
	th := ThemeFor(OSMac, true) // WhiteSurDark: no Extra["OnAccent"]
	if _, ok := th.Extra["OnAccent"]; ok {
		t.Skip("theme now tags OnAccent; fallback path not exercised")
	}
	got := themeOnAccent(th)
	if got == th.Background {
		t.Fatal("themeOnAccent fell back to Background (illegible on accent)")
	}
	if got != onAccentFor(th.Accent) {
		t.Fatalf("themeOnAccent = %v, want onAccentFor(accent) %v", got, onAccentFor(th.Accent))
	}
}

// B4: a bright source colour (TikTok cyan) must get black badge ink, not white.
func TestBadgeInkContrast(t *testing.T) {
	black := toolkit.RGBA{A: 0xFF}
	if ink := onAccentFor(sourceColor(source.TikTok)); ink != black {
		t.Fatalf("TikTok badge ink = %v, want black %v on its bright fill", ink, black)
	}
	// A dark fill still gets white ink.
	white := toolkit.RGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
	if ink := onAccentFor(toolkit.RGBA{R: 0x20, G: 0x20, B: 0x20, A: 0xFF}); ink != white {
		t.Fatalf("dark fill ink = %v, want white", ink)
	}
}

// B1: two items sharing a non-unique ID but differing in Source/Title must NOT
// collide in the card sprite cache.
func TestCardCacheNoCollision(t *testing.T) {
	s := New(900, 600, ThemeFor(OSMac, false))
	s.SetSubs(nil)
	s.layout() // build metrics/fonts before rasterising a card
	th := s.theme
	onAccent := themeOnAccent(th)
	muteS := mute(th.OnSurface, th.Surface)
	a := source.Item{ID: "1", Source: source.HackerNews, Title: "AAAA"}
	b := source.Item{ID: "1", Source: source.Lemmy, Title: "BBBB"}
	spA := s.cardSprite(a, 600, onAccent, muteS)
	spB := s.cardSprite(b, 600, onAccent, muteS)
	if spA == spB {
		t.Fatal("distinct items with the same ID shared one cached sprite")
	}
	// And an identical re-request hits the cache (same pointer).
	if s.cardSprite(a, 600, onAccent, muteS) != spA {
		t.Fatal("identical item re-request missed the cache")
	}
}

// B3: when the subscription list overflows the sidebar band it scrolls, and a
// sub scrolled out of the band is neither drawn over the footer nor hit-testable
// through the chrome; scrolling brings it back.
func TestSidebarOverflowScrollAndClip(t *testing.T) {
	s := New(760, 360, ThemeFor(OSMac, false))
	s.SetUsenetServer("news.free.fr:119") // adds the scrollable Browse entry
	var subs []Subscription
	for i := 0; i < 20; i++ {
		subs = append(subs, Subscription{Source: source.Reddit, Channel: string(rune('a' + i%26))})
	}
	s.SetSubs(subs)
	s.layout()
	if s.sideMaxScroll <= 0 {
		t.Fatal("expected overflow (sideMaxScroll > 0)")
	}
	// From a fresh (top) state, a wheel with the pointer over the sidebar scrolls
	// the sub list, not the feed.
	s.MouseMove(10, 150)
	before := s.sideScrollY
	s.Scroll(120)
	if s.sideScrollY <= before {
		t.Fatalf("sidebar did not scroll: %d -> %d", before, s.sideScrollY)
	}
	// Clicking inside the pinned footer resolves to a pinned entry, never a
	// scrolled sub row bleeding into it.
	if h := s.HitTest(2, s.accountsR.Y+s.m.sideItemH/2); h.Kind != HitAccounts {
		t.Fatalf("click in footer band resolved to %v, want HitAccounts", h.Kind)
	}
	// Render so the sidebar sprite draws (and clips) the overflowing rows.
	buf := make([]byte, s.W*s.H*4)
	s.Draw(buf)
	// An over-scroll is clamped back to the max on the next layout.
	s.sideScrollY = s.sideMaxScroll + 10_000
	s.layout()
	if s.sideScrollY != s.sideMaxScroll {
		t.Fatalf("over-scroll not clamped: %d != %d", s.sideScrollY, s.sideMaxScroll)
	}
	// At a high zoom on a minimum-height window the pinned footer's top falls above
	// the sub-list top, so the band is negative before the guard clamps it to 0 —
	// the scroll range must still be non-negative (band<0 guard).
	tiny := New(MinW, MinH, ThemeFor(OSMac, false))
	tiny.SetScale(3.0) // metrics scale past the min height
	tiny.SetProfiles([]settings.Profile{{Name: "P"}}, 0)
	tiny.SetSubs(subs)
	tiny.layout()
	if tiny.sideBandBot-tiny.sideBandTop >= 0 {
		t.Fatalf("expected a negative raw band at high zoom, got %d..%d", tiny.sideBandTop, tiny.sideBandBot)
	}
	if tiny.sideMaxScroll < 0 {
		t.Fatalf("tiny window sideMaxScroll = %d, want >= 0", tiny.sideMaxScroll)
	}
}
