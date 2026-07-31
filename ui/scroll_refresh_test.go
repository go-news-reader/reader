package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/go-news-reader/reader/source"
)

// TestScrollClampsOnResize proves the Sencha-style refresh clamp: a panel scrolled
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
	if s.detailScrollY == 0 {
		t.Fatal("precondition: detail should be scrolled off zero")
	}

	// Grow the window taller than the whole article — no Scroll() call.
	s.Resize(400, 4000)
	s.layoutDetail()
	if s.detailScrollY != 0 {
		t.Fatalf("resize did not re-clamp detailScrollY: %d (want 0, content now fits)", s.detailScrollY)
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
	if s2.logScrollY == 0 {
		t.Fatal("precondition: log should be scrolled off zero")
	}
	s2.Resize(400, 20000)
	s2.layoutLog()
	if s2.logScrollY != 0 {
		t.Fatalf("resize did not re-clamp logScrollY: %d (want 0)", s2.logScrollY)
	}
}
