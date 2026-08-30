package ui

import (
	"strconv"
	"testing"

	"github.com/go-news-reader/reader/source"
)

// TestSidebarScrollbar checks the open section's account ListBox draws its own
// scrollbar only when the accounts overflow its window, as a thumb down the
// section's right edge, while the tree itself stays within the band.
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
	// Few subs: the open section fits its ListBox window, so it does not scroll and
	// the widget draws no bar.
	s := overflowing(2)
	s.Draw(make([]byte, s.W*s.H*4))
	if s.sectionListOverflows() {
		t.Fatal("2 subs should fit the open section's window without scrolling")
	}

	// Many subs: the open section overflows its window, so the ListBox paints its OWN
	// scrollbar (a muted thumb over a SurfaceAlt track) down the section's right
	// edge. The other headers stay pinned, so the tree itself never overflows.
	s = overflowing(40)
	buf := make([]byte, s.W*s.H*4)
	s.Draw(buf)
	if !s.sectionListOverflows() {
		t.Fatal("40 subs should overflow the open section's ListBox window")
	}
	if s.sidebarListOverflows() {
		t.Fatal("a windowed section must keep the tree itself within the band")
	}
	// The sidebar ground (and the ListBox's scrollbar track) is SurfaceAlt, so the
	// only thing painting a different colour in the section's right-edge track column
	// is the widget's own thumb.
	th := s.theme
	found := false
	r := s.sideAccountRect
	for x := r.X + r.W - rpxOf(s, 12); x < r.X+r.W && !found; x++ {
		for y := r.Y; y < r.Y+r.H; y++ {
			c := px(buf, s.W, x, y)
			if c.R != th.SurfaceAlt.R || c.G != th.SurfaceAlt.G || c.B != th.SurfaceAlt.B {
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatal("no ListBox scrollbar thumb painted in the open section")
	}
}

// TestSidebarSectionBarNoOverlap: with an open accordion section overflowing its
// window, its account rows carry count chips at the right — none may paint in the
// ListBox's reserved scrollbar gutter (the gap the widget insets its content by so
// no chip sits under the thumb).
func TestSidebarSectionBarNoOverlap(t *testing.T) {
	s := New(760, 380, ThemeFor(OSLinux, false))
	var subs []Subscription
	for i := 0; i < 40; i++ {
		subs = append(subs, Subscription{Source: source.Instagram, Channel: "ig" + strconv.Itoa(i)})
	}
	s.SetSubs(subs)
	items := make([]source.Item, 0, 40)
	for i := 0; i < 40; i++ {
		items = append(items, source.Item{ID: strconv.Itoa(i), Source: source.Instagram, Channel: "ig" + strconv.Itoa(i), Title: "t"})
	}
	s.SetItems(items)
	s.ToggleSidebarSource(source.Instagram)
	buf := make([]byte, s.W*s.H*4)
	s.Draw(buf)
	if !s.sectionListOverflows() {
		t.Fatal("expected an overflowing open section with a scrollbar")
	}
	// The ListBox insets its content by scrollGutter (track + gap); the account row
	// content therefore stops a gap short of the track. That gap column — between the
	// content's right edge and the track's left edge — must be clear of any row
	// content across the whole account window, else a count chip is bleeding under
	// the reserved gutter. track = 12 logical px; gap = 4 logical px.
	th := s.theme
	r := s.sideAccountRect
	track := rpxOf(s, 12)
	gap := rpxOf(s, 4)
	contentRight := r.X + r.W - track - gap
	trackLeft := r.X + r.W - track
	for y := r.Y; y < r.Y+r.H; y++ {
		for x := contentRight; x < trackLeft; x++ {
			if c := px(buf, s.W, x, y); c.R != th.SurfaceAlt.R || c.G != th.SurfaceAlt.G || c.B != th.SurfaceAlt.B {
				t.Fatalf("row content paints in the ListBox gutter at (%d,%d): %v", x, y, c)
			}
		}
	}
}
