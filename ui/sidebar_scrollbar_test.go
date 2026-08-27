package ui

import (
	"strconv"
	"testing"

	"github.com/go-news-reader/reader/source"
)

// TestSidebarScrollbar checks the sub-list scrollbar appears only when the list
// overflows the band, drawn as a thumb down the sidebar's right edge.
func TestSidebarScrollbar(t *testing.T) {
	overflowing := func(n int) *Scene {
		s := New(720, 360, ThemeFor(OSLinux, false))
		subs := make([]Subscription, n)
		for i := range subs {
			ch := "group" + strconv.Itoa(i)
			subs[i] = Subscription{Source: source.Usenet, Channel: ch, Label: ch}
		}
		s.SetSubs(subs)
		s.ToggleSidebarSource(source.Usenet) // expand the group so its rows fill the band
		return s
	}
	// Few subs: fits, no overflow → no scrollbar.
	s := overflowing(2)
	s.Draw(make([]byte, s.W*s.H*4))
	if s.sidebarListOverflows() {
		t.Fatal("2 subs should not overflow the sidebar band")
	}

	// Many subs: overflows → the TreeView paints its own scrollbar (accent thumb
	// over a SurfaceAlt track) near the sidebar's right edge, inside the band.
	s = overflowing(40)
	buf := make([]byte, s.W*s.H*4)
	s.Draw(buf)
	if !s.sidebarListOverflows() {
		t.Fatal("40 subs should overflow the sidebar band")
	}
	// The sidebar background (and the scrollbar track) is SurfaceAlt, so the only
	// thing that paints a different colour in the right-edge column is the thumb
	// (the TreeView paints it in theme.Accent).
	th := s.theme
	found := false
	for x := s.m.sidebarW - rpxOf(s, 16); x < s.m.sidebarW && !found; x++ {
		for y := s.sideBandTop; y < s.sideBandBot; y++ {
			c := px(buf, s.W, x, y)
			if c.R != th.SurfaceAlt.R || c.G != th.SurfaceAlt.G || c.B != th.SurfaceAlt.B {
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatal("no scrollbar thumb painted in the sidebar band")
	}
}
