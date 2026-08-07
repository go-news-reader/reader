package ui

import (
	"strings"
	"testing"

	"github.com/go-news-reader/reader/source"
)

// wrapScene is a feed with no sidebar filter, ready for card-wrap assertions.
func wrapScene() *Scene {
	s := New(700, 600, ThemeFor(OSLinux, false))
	s.SetSubs(nil)
	return s
}

// TestCardWrapHeightGrows checks that a long title wraps to several lines and
// the card grows by exactly one title line per extra wrapped line, while a short
// title stays at the fixed rowH. cardHeight and rowHeight agree (single source of
// truth), so the laid-out height matches what the sprite rasterises.
func TestCardWrapHeightGrows(t *testing.T) {
	s := wrapScene()
	s.layout() // populate s.m
	line := s.titleLineH()

	short := source.Item{ID: "s", Source: source.Reddit, Title: "Short", Score: -1, Comments: -1}
	long := source.Item{ID: "l", Source: source.Reddit, Score: -1, Comments: -1,
		Title: "A rather long headline that certainly needs to wrap across several lines when the card is narrow"}

	const w = 220 // a narrow card so the long title must wrap
	if got := s.cardTitleLines(short, w); len(got) != 1 {
		t.Fatalf("short title lines = %d (%q), want 1", len(got), got)
	}
	if h := s.cardHeight(short, w); h != s.m.rowH {
		t.Fatalf("short card height = %d, want rowH %d", h, s.m.rowH)
	}

	lines := s.cardTitleLines(long, w)
	if len(lines) < 2 {
		t.Fatalf("long title lines = %d (%q), want >1", len(lines), lines)
	}
	wantH := s.m.rowH + (len(lines)-1)*line
	if h := s.cardHeight(long, w); h != wantH {
		t.Fatalf("long card height = %d, want rowH+%d*line = %d", h, len(lines)-1, wantH)
	}
	if s.cardHeight(long, w) <= s.m.rowH {
		t.Fatal("long card should be taller than a one-line card")
	}
	// rowHeight for a standard entry must equal cardHeight at the same width.
	if rh := s.rowHeight(feedEntry{item: long}, w); rh != wantH {
		t.Fatalf("rowHeight = %d, want cardHeight %d", rh, wantH)
	}
}

// TestCardWrapMaxLinesCap checks the line cap: a very long title at a very narrow
// width is clamped to cardTitleMaxLines with the last shown line ellipsised, so a
// card cannot grow without bound.
func TestCardWrapMaxLinesCap(t *testing.T) {
	s := wrapScene()
	s.layout()
	long := source.Item{ID: "l", Source: source.Reddit, Score: -1, Comments: -1,
		Title: strings.Repeat("wrap ", 60)} // 60 short words → many lines
	const w = 130
	lines := s.cardTitleLines(long, w)
	if len(lines) != cardTitleMaxLines {
		t.Fatalf("capped lines = %d, want %d", len(lines), cardTitleMaxLines)
	}
	if !strings.HasSuffix(lines[cardTitleMaxLines-1], "…") {
		t.Fatalf("last shown line %q should be ellipsised", lines[cardTitleMaxLines-1])
	}
	wantH := s.m.rowH + (cardTitleMaxLines-1)*s.titleLineH()
	if h := s.cardHeight(long, w); h != wantH {
		t.Fatalf("capped card height = %d, want %d", h, wantH)
	}
}

// TestCardWrapEmptyTitle checks an empty/whitespace title still occupies exactly
// one line (never zero), leaving the card at the fixed rowH.
func TestCardWrapEmptyTitle(t *testing.T) {
	s := wrapScene()
	s.layout()
	it := source.Item{ID: "e", Source: source.Reddit, Title: "   ", Score: -1, Comments: -1}
	if got := s.cardTitleLines(it, 220); len(got) != 1 {
		t.Fatalf("empty-title lines = %d, want 1", len(got))
	}
	if h := s.cardHeight(it, 220); h != s.m.rowH {
		t.Fatalf("empty-title card height = %d, want rowH %d", h, s.m.rowH)
	}
}

// TestCardWrapThumbReservesWidth checks the thumbnail column narrows the title's
// wrap width: with media, the same title at the same card width wraps to more
// lines (a taller card) than without media.
func TestCardWrapThumbReservesWidth(t *testing.T) {
	s := wrapScene()
	s.layout()
	title := "A medium length headline that fits one line wide but wraps once narrowed"
	const w = 360
	noThumb := source.Item{ID: "a", Source: source.Reddit, Title: title, Score: -1, Comments: -1}
	withThumb := source.Item{ID: "b", Source: source.Reddit, Title: title, Score: -1, Comments: -1,
		Media: []source.Media{{Kind: source.MediaImage}}}
	n0 := len(s.cardTitleLines(noThumb, w))
	n1 := len(s.cardTitleLines(withThumb, w))
	if n1 <= n0 {
		t.Fatalf("media card lines %d should exceed no-media %d (thumb reserves width)", n1, n0)
	}
	if s.cardHeight(withThumb, w) <= s.cardHeight(noThumb, w) {
		t.Fatal("media card should be at least as tall as the no-media card")
	}
}

// TestFeedNarrowWrapsAndHits is the end-to-end check through layout + hit-test: a
// narrow feed (a widened preview pane) wraps a long title so its card grows, the
// scroll content height sums the taller card, and a click anywhere inside the
// grown card — including below where the old fixed-height card ended — still maps
// to that item.
func TestFeedNarrowWrapsAndHits(t *testing.T) {
	s := wrapScene()
	long := source.Item{ID: "long", Source: source.Reddit, Channel: "news", Score: -1, Comments: -1,
		Title: "A rather long headline that will certainly need to wrap across several lines in a narrow feed"}
	short := source.Item{ID: "short", Source: source.HackerNews, Title: "Short", Score: -1, Comments: -1}
	withThumb := source.Item{ID: "media", Source: source.Reddit, Score: -1, Comments: -1,
		Title: "Another long wrapping headline that grows the card and keeps a top-pinned thumbnail",
		Media: []source.Media{{Kind: source.MediaImage}}}
	s.SetItems([]source.Item{long, short, withThumb})

	// Widen the preview pane so the feed narrows and the long title must wrap.
	s.previewHas = true
	s.previewUserW = s.W // clamped down to the max the pane may claim
	s.layout()

	rowByID := map[string]feedRow{}
	for _, r := range s.rows {
		rowByID[r.item.ID] = r
	}
	longRow, shortRow := rowByID["long"], rowByID["short"]
	if longRow.height <= s.m.rowH {
		t.Fatalf("long card height %d should exceed rowH %d (title must wrap)", longRow.height, s.m.rowH)
	}
	if shortRow.height != s.m.rowH {
		t.Fatalf("short card height %d should equal rowH %d", shortRow.height, s.m.rowH)
	}
	// The scroll content height sums the (variable) row heights.
	wantMin := longRow.height + shortRow.height + rowByID["media"].height + 2*s.m.cardGap
	if s.feedScroll.contentH < wantMin {
		t.Fatalf("contentH %d should be >= summed heights %d", s.feedScroll.contentH, wantMin)
	}

	feedX, _ := s.feedGeom()
	// A click below where the OLD fixed-height card ended, but inside the grown
	// card, still hits it — proving the taller hit area follows the wrapped title.
	if h := s.HitTest(feedX+6, s.m.topbarH+longRow.top+s.m.rowH+2); h.Kind != HitItem || h.Item.ID != "long" {
		t.Fatalf("click below old rowH inside grown card = %+v, want HitItem long", h)
	}
	// A click near the very bottom of the tall card also maps to it.
	if h := s.HitTest(feedX+6, s.m.topbarH+longRow.top+longRow.height-3); h.Kind != HitItem || h.Item.ID != "long" {
		t.Fatalf("click near bottom of tall card = %+v, want HitItem long", h)
	}
	renderPNG(t, s, "narrow-wrap")
}
