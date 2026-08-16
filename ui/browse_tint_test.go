package ui

import (
	"fmt"
	"testing"

	"github.com/go-news-reader/reader/source"
)

// TestBrowseSelectionRendersOnAccent renders the newsgroup browser with a tree
// long enough to overflow (so the TreeView shows its scrollbar) and checks the
// keyboard-selected row is painted with the accent selection while unselected
// rows blend into the Backdrop ground — and that the overflowing tree insets its
// rows by the scrollbar gutter, so the selection fill can never run under the bar
// (the concern the old hand-drawn full-width tint had to clip for).
func TestBrowseSelectionRendersOnAccent(t *testing.T) {
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

	// 300 flat groups overflow the viewport, so the TreeView virtualizes + shows a
	// scrollbar (fewer window rows than total rows).
	if s.browseWindowRows() >= len(s.browseRows) {
		t.Fatal("300 groups should overflow the browse viewport")
	}
	th := s.theme
	rowW := s.browseRightEdge()
	sampleX := rowW / 2 // between the label and the right-edge marker: plain fill

	// Row 0 is the keyboard selection: its fill is the accent selection.
	y0, ok := s.browseRowScreenY(0)
	if !ok {
		t.Fatal("row 0 should be visible")
	}
	if c := px(buf, s.W, sampleX, y0+s.m.sideItemH/2); c.R != th.Accent.R || c.G != th.Accent.G || c.B != th.Accent.B {
		t.Fatalf("selected row 0 not accent-filled: %+v", c)
	}
	// Row 1 is unselected: the browse ground (Background) shows through, proving the
	// selection is per-row and unselected rows blend into the Backdrop.
	y1, ok := s.browseRowScreenY(1)
	if !ok {
		t.Fatal("row 1 should be visible")
	}
	if c := px(buf, s.W, sampleX, y1+s.m.sideItemH/2); c.R != th.Background.R || c.G != th.Background.G || c.B != th.Background.B {
		t.Fatalf("unselected row 1 not the background ground: %+v", c)
	}
	// The overflowing tree insets its rows by the scrollbar gutter, so the row fill
	// (selection included) stops before the bar rather than running under it.
	if rowW >= s.W {
		t.Fatalf("overflowing tree should inset rows by the scrollbar gutter: rowW=%d W=%d", rowW, s.W)
	}
}
