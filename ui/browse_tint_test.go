package ui

import (
	"fmt"
	"testing"

	"github.com/go-news-reader/reader/source"
)

// TestBrowseSelectionTintClearsScrollbar renders the newsgroup browser with a tree
// long enough to show the scrollbar and a keyboard-selected row, then checks the
// selection tint stops at the shared gutter instead of painting across the whole
// window under the bar (the previous full-width `W: s.W` tint bug).
func TestBrowseSelectionTintClearsScrollbar(t *testing.T) {
	s := New(760, 460, ThemeFor(OSMac, false))
	s.SetUsenetServer("news.free.fr:119")
	var g []source.GroupInfo
	for i := 0; i < 300; i++ {
		g = append(g, source.GroupInfo{Name: fmt.Sprintf("grp%03d", i)})
	}
	s.SetBrowseGroups(g)
	s.OpenBrowse()
	s.layoutBrowse()

	buf := make([]byte, s.W*s.H*4)
	s.Draw(buf)
	if !s.scrollbarNeeded(s.browseScroll.contentH, s.H-s.m.topbarH) {
		t.Fatal("300 groups should overflow the browse viewport")
	}

	// browseSel defaults to row 0, the first visible row at the tree top.
	rowY := s.browseTreeTop + s.browseRows[s.browseSel].top - s.browseScroll.offset + s.m.sideItemH/2

	th := s.theme
	tintRight := s.scrollClampRight(s.W, s.W, 0, true)
	// Left of the gutter the selected row is tinted (SurfaceAlt).
	if c := px(buf, s.W, tintRight-4, rowY); c.R != th.SurfaceAlt.R || c.G != th.SurfaceAlt.G || c.B != th.SurfaceAlt.B {
		t.Fatalf("selected row not tinted left of the gutter: %+v", c)
	}
	// In the gap between the gutter and the bar's left edge, the row must show the
	// browse background, proving the tint was clipped and does not run under the bar.
	gap := (tintRight + (s.scrollbarRightX(s.W, 0) - s.scrollbarW())) / 2
	if c := px(buf, s.W, gap, rowY); c.R == th.SurfaceAlt.R && c.G == th.SurfaceAlt.G && c.B == th.SurfaceAlt.B {
		t.Fatalf("tint still paints in the scrollbar gutter at x=%d (should be background)", gap)
	}
}
