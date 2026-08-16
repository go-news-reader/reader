package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/go-news-reader/reader/source"
)

// TestScrollClampsOnResize proves the layout refresh clamp: a panel scrolled
// to its bottom, then resized so its whole content fits, re-clamps its offset to 0
// during layout alone — no wheel event. Before, the clamp lived only in Scroll, so
// a resize left a stale out-of-range offset until the next wheel.
func TestScrollClampsOnResize(t *testing.T) {
	// Detail view: scroll to the bottom in a short window.
	s := New(400, 200, nil)
	body := strings.Repeat("A long sentence of body copy that wraps a lot. ", 120)
	s.OpenDetail(source.Item{ID: "long", Title: "Long", Body: body, Score: -1, Comments: -1})
	buf := make([]byte, s.W*s.H*4)
	s.Draw(buf)
	s.Scroll(1 << 20) // pin to the bottom
	if s.detailScroll.offset == 0 {
		t.Fatal("precondition: detail should be scrolled off zero")
	}

	// Grow the window taller than the whole article — no Scroll() call.
	s.Resize(400, 4000)
	s.layoutDetail()
	if s.detailScroll.offset != 0 {
		t.Fatalf("resize did not re-clamp detailScrollY: %d (want 0, content now fits)", s.detailScroll.offset)
	}

	// Log view: same property.
	s2 := New(400, 160, nil)
	many := make([]LogEntry, 200)
	for i := range many {
		many[i] = LogEntry{Method: "GET", URL: "https://example.com/y", Status: 200, Dur: time.Second}
	}
	s2.SetLogSource(func() []LogEntry { return many })
	s2.OpenLog()
	buf2 := make([]byte, s2.W*s2.H*4)
	s2.Draw(buf2)
	s2.Scroll(1 << 20)
	if s2.logScroll.offset == 0 {
		t.Fatal("precondition: log should be scrolled off zero")
	}
	s2.Resize(400, 20000)
	s2.layoutLog()
	if s2.logScroll.offset != 0 {
		t.Fatalf("resize did not re-clamp logScrollY: %d (want 0)", s2.logScroll.offset)
	}
}

// TestScrollYAccessor checks the exported feed-offset accessor reflects the
// CardList's own scroll offset (the window-app layer reads position through it).
func TestScrollYAccessor(t *testing.T) {
	s := overflowFeed(t, 40)
	if s.ScrollY() != s.FeedScrollOffset() {
		t.Fatalf("ScrollY()=%d, FeedScrollOffset()=%d", s.ScrollY(), s.FeedScrollOffset())
	}
	// The overflowing feed opens at the bottom, so the offset is non-zero.
	if s.ScrollY() <= 0 {
		t.Fatalf("ScrollY()=%d, want a positive bottom offset", s.ScrollY())
	}
}

// overflowFeed builds a scene whose feed CardList overflows its viewport,
// returning it laid out (open at the bottom) ready for wheel tests.
func overflowFeed(t *testing.T, n int) *Scene {
	t.Helper()
	s := New(400, 300, nil)
	many := make([]source.Item, n)
	for i := range many {
		many[i] = source.Item{ID: string(rune('a'+i%26)) + itoa(i), Source: source.Reddit, Title: "t", Score: -1, Comments: -1}
	}
	s.SetItems(many)
	s.layout()
	if !s.feedScrollbarShown() {
		t.Fatalf("precondition: feed does not overflow (content=%d viewport=%d)", s.feedContentH(), s.feedViewportH())
	}
	return s
}

// TestFeedWheelScrolls proves the feed wheel routes into the CardList: a big
// wheel-up moves toward the top, a one-pixel nudge still advances a whole row
// (the min-magnitude-1 branch of FeedWheel), and a zero delta is a no-op.
func TestFeedWheelScrolls(t *testing.T) {
	s := overflowFeed(t, 60)
	bottom := s.FeedScrollOffset()
	if bottom <= 0 {
		t.Fatalf("feed should open at the bottom, offset=%d", bottom)
	}
	// Wheel up (many rows) moves the viewport toward the top.
	s.Scroll(-10000)
	if s.FeedScrollOffset() >= bottom {
		t.Fatalf("wheel up did not move toward the top: %d (bottom %d)", s.FeedScrollOffset(), bottom)
	}
	// A one-pixel nudge down still advances at least one whole row.
	at := s.FeedScrollOffset()
	s.Scroll(1)
	if s.FeedScrollOffset() <= at {
		t.Fatalf("small wheel nudge did not advance a row: %d -> %d", at, s.FeedScrollOffset())
	}
	// A zero wheel delta is a no-op (FeedWheel early return).
	z := s.FeedScrollOffset()
	s.Scroll(0)
	if s.FeedScrollOffset() != z {
		t.Fatalf("zero wheel changed offset: %d -> %d", z, s.FeedScrollOffset())
	}
}
