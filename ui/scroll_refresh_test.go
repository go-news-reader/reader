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
// underlying scroller (the window-app layer reads scroll position through it).
func TestScrollYAccessor(t *testing.T) {
	s := New(400, 300, nil)
	many := make([]source.Item, 40)
	for i := range many {
		many[i] = source.Item{ID: string(rune('a' + i%26)), Source: source.Reddit, Title: "t", Score: -1, Comments: -1}
	}
	s.SetItems(many)
	s.Scroll(100000)
	if s.ScrollY() != s.feedScroll.offset || s.ScrollY() <= 0 {
		t.Fatalf("ScrollY()=%d, feedScroll.offset=%d", s.ScrollY(), s.feedScroll.offset)
	}
}

// overflowFeed builds a scene whose feed list overflows its viewport, returning
// it ready for wheel-scroll trigger tests.
func overflowFeed(t *testing.T, n int) *Scene {
	t.Helper()
	s := New(400, 300, nil)
	many := make([]source.Item, n)
	for i := range many {
		many[i] = source.Item{ID: string(rune('a'+i%26)) + itoaTest(i), Source: source.Reddit, Title: "t", Score: -1, Comments: -1}
	}
	s.SetItems(many)
	s.layout()
	if !s.feedScroll.needsBar() {
		t.Fatalf("precondition: feed does not overflow (contentH=%d viewport=%d)", s.feedScroll.contentH, s.feedScroll.viewport)
	}
	return s
}

func itoaTest(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

// TestInfiniteScrollTrigger proves the bottom-of-feed next-page trigger: it fires
// on an at-bottom wheel-down when infinite scroll is on, stays silent when off, and
// never fires on a list that does not overflow.
func TestInfiniteScrollTrigger(t *testing.T) {
	s := overflowFeed(t, 60)
	fired := 0
	s.OnReachBottom = func() { fired++ }
	s.SetInfiniteScroll(true)

	// Wheel to the very bottom: the offset pins to max() and the trigger fires.
	s.Scroll(1 << 20)
	if s.feedScroll.offset != s.feedScroll.max() || s.feedScroll.max() <= 0 {
		t.Fatalf("not pinned to bottom: offset=%d max=%d", s.feedScroll.offset, s.feedScroll.max())
	}
	if fired != 1 {
		t.Fatalf("OnReachBottom fired %d times on reaching bottom, want 1", fired)
	}
	// A further wheel-down while already at the bottom fires again (the app's
	// loadingMore guard is what throttles, not the scene).
	s.Scroll(40)
	if s.feedScroll.offset != s.feedScroll.max() {
		t.Fatalf("offset drifted off bottom: %d (max %d)", s.feedScroll.offset, s.feedScroll.max())
	}
	if fired != 2 {
		t.Fatalf("OnReachBottom fired %d times, want 2 after a second at-bottom wheel", fired)
	}

	// With infinite scroll OFF, an at-bottom wheel-down does not fire.
	s.SetInfiniteScroll(false)
	before := fired
	s.Scroll(40)
	if fired != before {
		t.Fatalf("OnReachBottom fired with infinite scroll off (%d -> %d)", before, fired)
	}

	// A list that does not overflow never fires, even with infinite scroll on.
	s2 := New(400, 300, nil)
	s2.SetItems([]source.Item{{ID: "only", Source: source.Reddit, Title: "t", Score: -1, Comments: -1}})
	s2.layout()
	if s2.feedScroll.needsBar() {
		t.Fatalf("precondition: single-item feed should not overflow")
	}
	got := 0
	s2.OnReachBottom = func() { got++ }
	s2.SetInfiniteScroll(true)
	s2.Scroll(1 << 20)
	if got != 0 {
		t.Fatalf("OnReachBottom fired %d times on a non-overflowing feed, want 0", got)
	}
	if s2.feedScroll.offset != 0 {
		t.Fatalf("non-overflowing feed offset = %d, want 0", s2.feedScroll.offset)
	}
}

// TestPullRefreshTrigger proves the pull-to-refresh accumulator: a single small
// wheel-up at the top does not refresh; insistent upward overscroll crossing the
// threshold fires exactly once and resets; and scrolling away from the top resets
// the accumulator.
func TestPullRefreshTrigger(t *testing.T) {
	s := overflowFeed(t, 60)
	refreshed := 0
	s.OnPullRefresh = func() { refreshed++ }

	if s.feedScroll.offset != 0 {
		t.Fatalf("precondition: feed should start at the top, offset=%d", s.feedScroll.offset)
	}

	// One small wheel-up at the top: accumulates, does not cross the threshold.
	s.Scroll(-20)
	if s.feedScroll.offset != 0 {
		t.Fatalf("wheel-up at top moved offset to %d, want 0 (clamped)", s.feedScroll.offset)
	}
	if s.pullAccum != 20 {
		t.Fatalf("pullAccum = %d after one -20 nudge, want 20", s.pullAccum)
	}
	if refreshed != 0 {
		t.Fatalf("OnPullRefresh fired %d times below threshold, want 0", refreshed)
	}

	// Keep pulling up: 20 + 20 = 40 (< 48), still no fire.
	s.Scroll(-20)
	if refreshed != 0 || s.pullAccum != 40 {
		t.Fatalf("below threshold: fired=%d accum=%d", refreshed, s.pullAccum)
	}
	// Cross 48: fires exactly once and resets the accumulator.
	s.Scroll(-20)
	if refreshed != 1 {
		t.Fatalf("OnPullRefresh fired %d times on crossing threshold, want 1", refreshed)
	}
	if s.pullAccum != 0 {
		t.Fatalf("pullAccum = %d after firing, want 0 (reset)", s.pullAccum)
	}
	// A further single nudge does not re-fire (accumulator restarted).
	s.Scroll(-20)
	if refreshed != 1 || s.pullAccum != 20 {
		t.Fatalf("post-fire: fired=%d accum=%d, want 1 / 20", refreshed, s.pullAccum)
	}

	// Scrolling DOWN (away from the top) resets the accumulator, so a subsequent
	// small pull does not carry stale progress.
	s.Scroll(80) // move off the top
	if s.feedScroll.offset <= 0 {
		t.Fatalf("scroll-down did not leave the top: offset=%d", s.feedScroll.offset)
	}
	if s.pullAccum != 0 {
		t.Fatalf("pullAccum = %d after scrolling down, want 0", s.pullAccum)
	}
}

// TestFeedTriggersNilSafe proves the triggers are nil-safe: firing conditions with
// no callback installed must not panic.
func TestFeedTriggersNilSafe(t *testing.T) {
	s := overflowFeed(t, 60)
	s.SetInfiniteScroll(true)
	s.OnReachBottom = nil
	s.OnPullRefresh = nil
	s.Scroll(1 << 20) // at-bottom: would fire OnReachBottom
	// Back to the top and pull hard: would fire OnPullRefresh.
	s.Scroll(-(1 << 20))
	s.Scroll(-pullRefreshThreshold)
	// No panic == pass.
}
