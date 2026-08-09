package ui

import (
	"strings"
	"testing"

	"github.com/go-widgets/toolkit"

	"github.com/go-news-reader/reader/source"
)

// findRun returns the accumulated selectable run whose text equals want (after a
// Draw populated s.selAccum), so a test can drag across the exact on-screen
// glyphs. Fatal if no such run was collected.
func findRun(t *testing.T, s *Scene, want string) toolkit.TextRun {
	t.Helper()
	for _, r := range s.selAccum {
		if r.Text == want {
			return r
		}
	}
	t.Fatalf("no selectable run with text %q (have %d runs)", want, len(s.selAccum))
	return toolkit.TextRun{}
}

// changedBBox returns the bounding box of the pixels that differ between a and b
// (same W×H RGBA buffers) and how many differed.
func changedBBox(a, b []byte, w, h int) (bx0, by0, bx1, by1, n int) {
	bx0, by0, bx1, by1 = w, h, -1, -1
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := (y*w + x) * 4
			if a[i] != b[i] || a[i+1] != b[i+1] || a[i+2] != b[i+2] {
				n++
				if x < bx0 {
					bx0 = x
				}
				if x > bx1 {
					bx1 = x
				}
				if y < by0 {
					by0 = y
				}
				if y > by1 {
					by1 = y
				}
			}
		}
	}
	return
}

// TestCardTitleDragSelectAndHighlight drives a drag across a feed card's title
// run and asserts (1) the selected text is the title, (2) the selection survives
// for a copy, and (3) the highlight pixels land strictly inside the title's
// on-screen bounds — not merely "something painted".
func TestCardTitleDragSelectAndHighlight(t *testing.T) {
	toolkit.SetClipboard(nil)
	s := New(1000, 700, ThemeFor(OSMac, false))
	s.SetScale(1)
	s.SetSubs(nil)
	s.SetItems([]source.Item{{ID: "1", Source: source.Reddit, Channel: "chan",
		Title: "SELECTME", Score: -1, Comments: -1}})

	buf0 := make([]byte, s.W*s.H*4)
	s.Draw(buf0) // lays out + commits the selectable runs

	run := findRun(t, s, "SELECTME")
	// The title run must sit inside the feed list region (right of the sidebar,
	// below the topbar) — a bounds-containment check on where it landed.
	feed := s.feedListRegion()
	if !inRect(feed, run.Bounds.X, run.Bounds.Y) ||
		!inRect(feed, run.Bounds.X+run.Bounds.W-1, run.Bounds.Y+run.Bounds.H-1) {
		t.Fatalf("title run %+v not inside feed region %+v", run.Bounds, feed)
	}

	cy := run.Bounds.Y + run.Bounds.H/2
	s.SelectionBegin(run.Bounds.X+1, cy)
	s.MouseMove(run.Bounds.X+run.Bounds.W-1, cy) // drag to the run's end
	if !s.HasSelection() {
		t.Fatal("a drag across the card title should produce a selection")
	}
	if got := s.textSel.SelectedText(); !strings.Contains("SELECTME", strings.TrimSpace(got)) || strings.TrimSpace(got) == "" {
		t.Fatalf("selected text = %q, want a substring of SELECTME", got)
	}

	// Repaint with the selection active and diff: the highlight is confined to the
	// title run's rectangle.
	buf1 := make([]byte, s.W*s.H*4)
	s.Draw(buf1)
	bx0, by0, bx1, by1, n := changedBBox(buf0, buf1, s.W, s.H)
	if n == 0 {
		t.Fatal("selection highlight painted nothing")
	}
	if bx0 < run.Bounds.X || bx1 >= run.Bounds.X+run.Bounds.W ||
		by0 < run.Bounds.Y || by1 >= run.Bounds.Y+run.Bounds.H {
		t.Fatalf("highlight bbox (%d,%d)-(%d,%d) escaped the title run %+v", bx0, by0, bx1, by1, run.Bounds)
	}
	// A sampled interior position was tinted.
	if px(buf0, s.W, run.Bounds.X+2, cy) == px(buf1, s.W, run.Bounds.X+2, cy) {
		t.Fatal("the sampled title pixel was not tinted by the highlight")
	}

	// Copy yields the title text.
	if !s.Copy() {
		t.Fatal("Copy over a card selection should report true")
	}
	if got := toolkit.ClipboardText(); !strings.Contains("SELECTME", got) || got == "" {
		t.Fatalf("card selection copy = %q", got)
	}
}

// TestSidebarLabelDragSelect drives a drag across a sidebar subscription label
// and asserts the label text is selected and highlighted within its bounds.
func TestSidebarLabelDragSelect(t *testing.T) {
	toolkit.SetClipboard(nil)
	s := New(1000, 700, ThemeFor(OSMac, false))
	s.SetScale(1)
	s.SetSubs([]Subscription{{Source: source.Reddit, Channel: "c", Label: "SIDELBL"}})

	buf0 := make([]byte, s.W*s.H*4)
	s.Draw(buf0)

	run := findRun(t, s, "SIDELBL")
	sb := s.sidebarTextRegion()
	if !inRect(sb, run.Bounds.X, run.Bounds.Y) {
		t.Fatalf("sidebar run %+v not inside sidebar region %+v", run.Bounds, sb)
	}

	cy := run.Bounds.Y + run.Bounds.H/2
	s.SelectionBegin(run.Bounds.X+1, cy)
	s.MouseMove(run.Bounds.X+run.Bounds.W-1, cy)
	if got := strings.TrimSpace(s.textSel.SelectedText()); !strings.Contains("SIDELBL", got) || got == "" {
		t.Fatalf("sidebar selected text = %q", got)
	}

	buf1 := make([]byte, s.W*s.H*4)
	s.Draw(buf1)
	bx0, by0, bx1, by1, n := changedBBox(buf0, buf1, s.W, s.H)
	if n == 0 {
		t.Fatal("sidebar highlight painted nothing")
	}
	if bx0 < run.Bounds.X || bx1 >= run.Bounds.X+run.Bounds.W ||
		by0 < run.Bounds.Y || by1 >= run.Bounds.Y+run.Bounds.H {
		t.Fatalf("sidebar highlight bbox (%d,%d)-(%d,%d) escaped the label run %+v", bx0, by0, bx1, by1, run.Bounds)
	}
}

// lumaSpread samples the run's centre row across its selected span and returns
// the spread between the lightest and darkest pixel luminance. A readable
// highlight (translucent, glyphs showing through) keeps a large spread; a solid
// block that hid the glyphs would collapse it.
func lumaSpread(buf []byte, w int, r toolkit.Rect) int {
	minL, maxL := 255, 0
	y := r.Y + r.H/2
	for x := r.X; x < r.X+r.W; x++ {
		p := px(buf, w, x, y)
		l := (int(p.R) + int(p.G) + int(p.B)) / 3
		if l < minL {
			minL = l
		}
		if l > maxL {
			maxL = l
		}
	}
	return maxL - minL
}

// TestCardHighlightReadableBothThemes proves the translucent over-highlight keeps
// the title glyphs distinct from the highlight fill in BOTH light and dark
// themes (the glyphs still read after the tint is blended on top).
func TestCardHighlightReadableBothThemes(t *testing.T) {
	for _, dark := range []bool{false, true} {
		s := New(1000, 700, ThemeFor(OSMac, dark))
		s.SetScale(1)
		s.SetSubs(nil)
		s.SetItems([]source.Item{{ID: "1", Source: source.Reddit, Channel: "chan",
			Title: "READABLE", Score: -1, Comments: -1}})
		buf := make([]byte, s.W*s.H*4)
		s.Draw(buf)
		run := findRun(t, s, "READABLE")
		cy := run.Bounds.Y + run.Bounds.H/2
		s.SelectionBegin(run.Bounds.X+1, cy)
		s.MouseMove(run.Bounds.X+run.Bounds.W-1, cy)
		s.Draw(buf) // repaint with the highlight
		if spread := lumaSpread(buf, s.W, run.Bounds); spread < 40 {
			t.Fatalf("dark=%v: glyphs not distinct under highlight (luma spread %d)", dark, spread)
		}
	}
}

// TestCollapsedSidebarRegionEmpty covers the collapsed-sidebar branch of
// sidebarTextRegion: with the sidebar hidden there is no selectable label band.
func TestCollapsedSidebarRegionEmpty(t *testing.T) {
	s := New(1000, 700, ThemeFor(OSMac, false))
	s.SetScale(1)
	s.SetSubs([]Subscription{{Source: source.Reddit, Channel: "c", Label: "X"}})
	s.ToggleSidebar() // collapse
	buf := make([]byte, s.W*s.H*4)
	s.Draw(buf)
	if r := s.sidebarTextRegion(); r.W != 0 {
		t.Fatalf("collapsed sidebar region should be empty, got %+v", r)
	}
	// A press where the sidebar used to be now falls in the feed list region.
	if !s.SelectableAt(5, 300) {
		t.Fatal("with the sidebar collapsed the feed list starts at the left edge")
	}
}
