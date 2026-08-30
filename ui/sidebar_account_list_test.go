package ui

import (
	"strconv"
	"testing"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"

	"github.com/go-news-reader/reader/source"
)

// redditSectionScene builds a feed scene with n Reddit subscriptions and opens
// that source's accordion section, so its accounts are handed to the ListBox.
func redditSectionScene(t *testing.T, n int) *Scene {
	t.Helper()
	s := New(720, 420, ThemeFor(OSLinux, false))
	var subs []Subscription
	for i := 0; i < n; i++ {
		subs = append(subs, Subscription{Source: source.Reddit, Channel: "r" + strconv.Itoa(i)})
	}
	s.SetSubs(subs)
	s.ToggleSidebarSource(source.Reddit)
	s.layout()
	return s
}

// TestSectionListHighlightsActive checks the open section's ListBox points its
// selection at the active subscription's row (layoutOpenSection's selection scan).
func TestSectionListHighlightsActive(t *testing.T) {
	s := redditSectionScene(t, 5)
	s.SetActive(2) // a subscription inside the open section
	s.layout()
	if got := s.sideAccountList.Selected().Get(); got != 2 {
		t.Fatalf("ListBox selection = %d, want the active sub's row 2", got)
	}
	if !s.sectionListShown() {
		t.Fatal("the open section should hand its accounts to the ListBox")
	}
}

// TestSectionListClickSelects covers the account-row hit paths: a left-click in
// the section rect resolves to HitSub for the account it names, a right-click
// resolves to the same subscription's context, and the ListBox's own OnActivate
// (its widget-native selection) drives SetActive.
func TestSectionListClickSelects(t *testing.T) {
	s := redditSectionScene(t, 5)
	r := s.sideAccountRect
	rh := s.m.sideItemH
	// The first visible account row is Items[0] → subscription 0.
	x, y := 10, r.Y+rh/2
	if h := s.HitTest(x, y); h.Kind != HitSub || h.Sub != 0 {
		t.Fatalf("click on the first account = %+v, want HitSub sub 0", h)
	}
	// The third row → subscription 2.
	if h := s.HitTest(x, r.Y+2*rh+rh/2); h.Kind != HitSub || h.Sub != 2 {
		t.Fatalf("click on the third account = %+v, want HitSub sub 2", h)
	}
	// A right-click on the second row resolves to that subscription's context.
	if c := s.SidebarContextAt(x, r.Y+rh+rh/2); c.Kind != SidebarCtxSub || c.Sub != 1 {
		t.Fatalf("right-click on the second account = %+v, want SidebarCtxSub sub 1", c)
	}
	// The ListBox's own OnActivate (its widget-native click path) selects the sub.
	s.sideAccountList.OnActivate(3)
	if s.Active != 3 {
		t.Fatalf("OnActivate(3) set Active=%d, want 3", s.Active)
	}
	// A bad row is a no-op (accountRowSub rejects it), leaving Active unchanged.
	s.sideAccountList.OnActivate(-1)
	if s.Active != 3 {
		t.Fatalf("OnActivate(-1) changed Active to %d, want it left at 3", s.Active)
	}
}

// TestAccountRowSubGuards exercises accountRowSub's and drawAccountRow's defensive
// branches: an out-of-range row, an unparsable item, and an in-range-but-unknown
// subscription index all decode to "no account" and paint nothing.
func TestAccountRowSubGuards(t *testing.T) {
	s := redditSectionScene(t, 3)
	if _, ok := s.accountRowSub(-1); ok {
		t.Fatal("a negative row must not resolve to a subscription")
	}
	if _, ok := s.accountRowSub(99); ok {
		t.Fatal("a row past the last account must not resolve to a subscription")
	}
	// An unparsable item is rejected (the model always holds decimal indices, but
	// the decode is defensive).
	s.sideAccountList.Items = []string{"not-a-number"}
	if _, ok := s.accountRowSub(0); ok {
		t.Fatal("an unparsable item must not resolve to a subscription")
	}

	// drawAccountRow tolerates a bad item and an out-of-range index: it paints
	// nothing rather than indexing s.Subs out of bounds.
	buf := make([]byte, s.W*s.H*4)
	p := painter.NewPixelPainter(buf, s.W, s.H)
	rc := toolkit.Rect{X: 0, Y: 0, W: s.m.sidebarW, H: s.m.sideItemH}
	ink := s.theme.OnSurface
	s.drawAccountRow(p, s.theme, rc, 0, "not-a-number", false, ink) // Atoi fails
	s.drawAccountRow(p, s.theme, rc, 0, "9999", false, ink)         // index out of range
	for _, b := range buf {
		if b != 0 {
			t.Fatal("drawAccountRow painted for an undecodable/out-of-range item")
		}
	}
}
