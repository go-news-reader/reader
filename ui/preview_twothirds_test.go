package ui

import (
	"testing"

	"github.com/go-news-reader/reader/source"
)

// TestPreviewDefaultTwoThirds locks the "wide reading pane by default" intent: with
// no user drag the preview pane defaults to exactly 2/3 of the feed+preview area
// (the sidebar excluded), leaving the card column the complementary ~1/3.
func TestPreviewDefaultTwoThirds(t *testing.T) {
	s := New(1500, 800, ThemeFor(OSMac, false))
	s.SetSubs([]Subscription{{Source: source.Usenet, Channel: "alt.bin"}})
	s.layout()

	if s.previewUserW != 0 {
		t.Fatalf("no drag expected, previewUserW=%d", s.previewUserW)
	}
	area := s.W - s.m.sidebarW // the feed+preview area, sidebar excluded
	want := area * 2 / 3

	// Guard the window actually exercises the 2/3 default branch: it must beat the
	// preferred width and still fit inside the available space (neither clamp fires),
	// so the assertion below tests the 2/3 rule and not a clamp.
	if want <= rpxOf(s, previewPaneW) {
		t.Fatalf("window too narrow to exercise the 2/3 default: 2/3=%d preferred=%d", want, rpxOf(s, previewPaneW))
	}
	if avail := area - rpxOf(s, feedKeepW); want > avail {
		t.Fatalf("2/3 default should fit avail: 2/3=%d avail=%d", want, avail)
	}

	if got := s.previewWidth(); got != want {
		t.Fatalf("default preview width = %d, want 2/3 of %d = %d", got, area, want)
	}
	// The card column keeps the complementary ~1/3 and stays positive; the reading
	// pane dominates it (it is ~twice as wide).
	_, feedW := s.feedGeom()
	if feedW <= 0 {
		t.Fatalf("card column must stay positive, got %d", feedW)
	}
	if s.previewWidth() < 2*feedW {
		t.Fatalf("reading pane %d should dominate the ~1/3 card column %d (≈2×)", s.previewWidth(), feedW)
	}
}

// TestSelectedTabBand proves the active sidebar row paints the soft accent-tinted
// band plus the solid accent left edge, at exact pixels, and that a non-selected row
