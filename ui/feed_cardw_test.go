package ui

import "testing"

// TestFeedCardW checks the feed content width reserves a gutter for the
// scrollbar only when it is shown, and that the gutter tracks the bar's grip
// state: wider (a scrollGripGap clear of the preview divider) when the preview
// pane is open, else flush against the feed's right edge.
func TestFeedCardW(t *testing.T) {
	s := New(1200, 700, ThemeFor(OSMac, false))
	const feedW = 800

	// No overflow → no scrollbar → full width, whatever the preview state.
	s.contentH = 0
	if got := s.feedCardW(feedW); got != feedW {
		t.Fatalf("no scrollbar: cardW=%d, want full %d", got, feedW)
	}

	// Force overflow so the scrollbar shows.
	s.contentH = s.feedBottom() - s.m.topbarH + 10000

	// Preview closed (no grip): cards stop before a right-flush bar.
	s.previewR.W = 0
	closed := s.feedCardW(feedW)
	if closed >= feedW {
		t.Fatalf("scrollbar shown (no preview): cardW=%d must be < %d", closed, feedW)
	}
	if want := s.scrollbarRightX(feedW, 0) - s.scrollbarW() - rpxOf(s, 6); closed != want {
		t.Fatalf("no-grip cardW=%d, want %d", closed, want)
	}

	// Preview open (grip at the feed's right edge): the bar sits a scrollGripGap
	// left of the divider, so the card gutter is wider.
	s.previewR.W = 300
	open := s.feedCardW(feedW)
	if open >= closed {
		t.Fatalf("preview-open cardW=%d should be narrower than closed %d", open, closed)
	}
	if want := s.scrollbarRightX(feedW, feedW) - s.scrollbarW() - rpxOf(s, 6); open != want {
		t.Fatalf("grip cardW=%d, want %d", open, want)
	}
}
