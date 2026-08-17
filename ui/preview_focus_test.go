package ui

import (
	"strings"
	"testing"

	"github.com/go-news-reader/reader/source"
)

// longPreviewScene builds a feed scene whose single selected text post overflows
// a short pane, so previewScroll has room to move (needsBar is true).
func longPreviewScene() *Scene {
	s := New(1200, 300, ThemeFor(OSMac, false))
	s.SetSubs(nil)
	body := strings.Repeat("word ", 400)
	it := source.Item{ID: "1", Source: source.Usenet, Title: "big", Body: body,
		Media: []source.Media{{Kind: source.MediaImage}}}
	s.SetItems([]source.Item{it})
	s.SelectPreview(it)
	s.layout()
	s.layoutPreview()
	return s
}

// TestPreviewFocusToggleTextScroll covers the Tab focus model over a text preview:
// focusability, the toggle on/off, the scroll-key routing (arrow + page) into
// previewScroll, and the no-op paths (dir 0, unfocused).
func TestPreviewFocusToggleTextScroll(t *testing.T) {
	s := longPreviewScene()
	if !s.previewScroll.needsBar() {
		t.Fatalf("setup: preview content should overflow (contentH %d, viewport %d)",
			s.previewScroll.contentH, s.previewScroll.viewport)
	}

	// Focused when nothing yet toggled: false. Scrolling a non-focused pane is a
	// no-op that does not consume the key.
	if s.PreviewFocused() {
		t.Fatal("a fresh pane must not hold focus")
	}
	if s.ScrollPreviewKey(1, false) {
		t.Fatal("scroll must not be consumed while the pane is unfocused")
	}

	// Tab focuses the (focusable) pane.
	if !s.PreviewFocusable() {
		t.Fatal("a selected, visible pane should be focusable")
	}
	if !s.TogglePreviewFocus() || !s.PreviewFocused() {
		t.Fatal("Tab should focus the pane")
	}

	// An arrow-down step scrolls the pane down; an up step brings it back.
	if !s.ScrollPreviewKey(1, false) {
		t.Fatal("a focused arrow key should be consumed")
	}
	down := s.previewScroll.offset
	if down <= 0 {
		t.Fatalf("arrow-down did not scroll: offset %d", down)
	}
	s.ScrollPreviewKey(-1, false)
	if s.previewScroll.offset >= down {
		t.Fatalf("arrow-up did not scroll back: %d -> %d", down, s.previewScroll.offset)
	}

	// A page-down moves further than a single arrow step.
	s.previewScroll.offset = 0
	s.ScrollPreviewKey(1, false)
	oneStep := s.previewScroll.offset
	s.previewScroll.offset = 0
	s.ScrollPreviewKey(1, true)
	if s.previewScroll.offset <= oneStep {
		t.Fatalf("page-down (%d) should exceed one arrow step (%d)", s.previewScroll.offset, oneStep)
	}

	// dir 0 is a no-op (not consumed).
	if s.ScrollPreviewKey(0, false) {
		t.Fatal("a zero-direction scroll must not be consumed")
	}

	// Tab again returns focus to the feed.
	if s.TogglePreviewFocus() || s.PreviewFocused() {
		t.Fatal("a second Tab should drop the pane focus")
	}

	// The focus ring is really painted, in the accent colour, on the pane's own
	// two vertical edges -- and only while focused. Comparing the accent pixels
	// inside the pane before and after, on a row halfway down it, is the
	// falsifiable form: a ring that silently stopped being drawn fails it, and so
	// does one that flooded the pane, where a bare "Draw did not panic" would not.
	// The row is taken mid-pane so the horizontal runs of the ring's top and
	// bottom edges cannot be mistaken for its sides.
	buf := make([]byte, s.W*s.H*4)
	s.Draw(buf)
	mid := s.previewR.Y + s.previewR.H/2
	if n := accentInPane(buf, s, mid); n != 0 {
		t.Fatalf("an unfocused pane painted %d accent pixels across its middle", n)
	}
	s.TogglePreviewFocus()
	s.Draw(buf)
	xs := accentXsInPane(buf, s, mid)
	// Two edges, each StrokeWidth wide (2 logical px at scale 1), and nothing in
	// between: an outline, not a fill.
	if len(xs) != 4 {
		t.Fatalf("focus ring on the middle row painted %d accent pixels at %v, want 4 (two 2px edges)", len(xs), xs)
	}
	left, right := s.previewR.X, s.previewR.X+s.previewR.W-1
	if xs[0] != left || xs[1] != left+1 {
		t.Errorf("ring's left edge at x=%d,%d, want %d,%d", xs[0], xs[1], left, left+1)
	}
	if xs[3] != right || xs[2] != right-1 {
		t.Errorf("ring's right edge at x=%d,%d, want %d,%d", xs[2], xs[3], right-1, right)
	}
}

// accentInPane counts the theme-accent pixels of row y that lie inside the
// preview pane.
func accentInPane(buf []byte, s *Scene, y int) int { return len(accentXsInPane(buf, s, y)) }

// accentXsInPane returns the x of every theme-accent pixel of row y inside the
// preview pane, left to right.
func accentXsInPane(buf []byte, s *Scene, y int) []int {
	a := s.theme.Accent
	var xs []int
	for x := s.previewR.X; x < s.previewR.X+s.previewR.W; x++ {
		i := (y*s.W + x) * 4
		if buf[i] == a.R && buf[i+1] == a.G && buf[i+2] == a.B {
			xs = append(xs, x)
		}
	}
	return xs
}

// TestPreviewFocusContentFits covers the "focused but nothing to scroll" path: a
// short post in a tall pane fits, so a scroll key is consumed (focus stays on the
// pane) but the offset does not move.
func TestPreviewFocusContentFits(t *testing.T) {
	s := previewScene()
	s.layout()
	s.SelectPreview(s.Items[0])
	s.layoutPreview()
	if s.previewScroll.needsBar() {
		t.Skip("content overflows; window too short to exercise the fits path")
	}
	if !s.TogglePreviewFocus() {
		t.Fatal("pane should focus")
	}
	if !s.ScrollPreviewKey(1, false) {
		t.Fatal("a focused key is consumed even when the content fits")
	}
	if s.previewScroll.offset != 0 {
		t.Fatalf("content that fits must not scroll: offset %d", s.previewScroll.offset)
	}
}

// TestPreviewNotFocusable covers TogglePreviewFocus refusing focus when the pane
// has no selection or is hidden.
func TestPreviewNotFocusable(t *testing.T) {
	// Visible pane, no selection → not focusable.
	s := previewScene()
	s.layout()
	s.layoutPreview()
	if s.PreviewFocusable() {
		t.Fatal("a pane with no selection is not focusable")
	}
	if s.TogglePreviewFocus() || s.PreviewFocused() {
		t.Fatal("Tab must not focus an unselectable pane")
	}

	// Selecting then hiding the pane (leaving the feed) drops the focus at layout.
	s.SelectPreview(s.Items[0])
	s.layoutPreview()
	if !s.TogglePreviewFocus() {
		t.Fatal("selected + visible pane should focus")
	}
	s.OpenSettings() // mode != feed → the pane hides
	s.layoutPreview()
	if s.PreviewFocused() {
		t.Fatal("a hidden pane must not keep keyboard focus")
	}
}

// TestPreviewFocusWebScroll covers the web-preview focus path: focusing a
// web-linked item arms browserFocused, and the scroll keys route into the
// embedded browser (arrow + page), including the not-yet-laid-out guard.
func TestPreviewFocusWebScroll(t *testing.T) {
	s := New(1200, 700, ThemeFor(OSMac, false))
	s.SetSubs(nil)

	// Not yet laid out: focus the web preview directly, then a scroll key is
	// swallowed by the zero-bounds browser guard.
	s.SelectPreview(webTestItem())
	if !s.webPreviewItem() {
		t.Fatal("setup: a web-linked item should preview in the browser")
	}
	s.setPreviewFocused(true)
	if !s.BrowserFocused() {
		t.Fatal("focusing a web preview should arm browserFocused")
	}
	if !s.ScrollPreviewKey(1, false) {
		t.Fatal("a focused web scroll should be consumed even before layout")
	}

	// Lay it out with a delivered page, then the scroll routes into the browser.
	deliverPage(s, "https://example.com/a", "Title", 400, 2000)
	buf := make([]byte, s.W*s.H*4)
	s.Draw(buf) // lay out the browser + paint the focus ring (web path)
	before := s.Rev()
	if !s.ScrollPreviewKey(1, false) || s.Rev() == before {
		t.Fatal("arrow over a laid-out web preview should scroll it")
	}
	if !s.ScrollPreviewKey(1, true) {
		t.Fatal("page over a laid-out web preview should scroll it")
	}

	// Dropping focus clears browserFocused too.
	s.setPreviewFocused(false)
	if s.BrowserFocused() {
		t.Fatal("leaving preview focus should clear browserFocused")
	}
}
