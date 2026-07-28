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
		return s
	}
	// Few subs: fits, no overflow → no scrollbar.
	s := overflowing(2)
	s.Draw(make([]byte, s.W*s.H*4))
	if s.sideMaxScroll != 0 {
		t.Fatalf("2 subs should not overflow, maxScroll=%d", s.sideMaxScroll)
	}

	// Many subs: overflows → the scrollbar thumb (theme Border) paints near the
	// sidebar's right edge, inside the scrollable band.
	s = overflowing(40)
	buf := make([]byte, s.W*s.H*4)
	s.Draw(buf)
	if s.sideMaxScroll <= 0 {
		t.Fatalf("40 subs should overflow, maxScroll=%d", s.sideMaxScroll)
	}
	th := s.theme
	found := false
	for x := s.m.sidebarW - s.scrollbarW() - rpxOf(s, 4); x < s.m.sidebarW && !found; x++ {
		for y := s.sideBandTop; y < s.sideBandBot; y++ {
			c := px(buf, s.W, x, y)
			if c.R == th.Border.R && c.G == th.Border.G && c.B == th.Border.B {
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatal("no scrollbar thumb painted in the sidebar band")
	}
}
