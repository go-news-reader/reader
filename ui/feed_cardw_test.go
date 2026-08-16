package ui

import (
	"testing"

	"github.com/go-news-reader/reader/source"
)

// TestFeedCardW checks the feed content width reserves a gutter for the
// scrollbar only when the CardList content overflows, and that the gutter tracks
// the bar's grip state: wider (a scrollGripGap clear of the preview divider) when
// the preview pane is open, else flush against the feed's right edge.
func TestFeedCardW(t *testing.T) {
	s := New(1200, 700, ThemeFor(OSMac, false))
	const feedW = 800

	// No items → no overflow → no scrollbar → full width, whatever the preview state.
	s.SetItems(nil)
	s.layout()
	if got := s.feedCardW(feedW); got != feedW {
		t.Fatalf("no scrollbar: cardW=%d, want full %d", got, feedW)
	}

	// Many items so the CardList content overflows its viewport → the scrollbar shows.
	many := make([]source.Item, 60)
	for i := range many {
		many[i] = source.Item{ID: string(rune('a'+i%26)) + itoa(i), Source: source.Reddit, Title: "a headline", Score: -1, Comments: -1}
	}
	s.SetItems(many)
	s.layout()
	if !s.feedScrollbarShown() {
		t.Fatal("precondition: feed should overflow so the scrollbar shows")
	}

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
