package ui

import (
	"strings"
	"testing"

	"github.com/go-widgets/toolkit"

	"github.com/go-news-reader/reader/source"
)

// drawDetailScene opens a detail article and draws one frame so the reading
// view's selectable runs are populated. Returns the scene + its frame buffer.
func drawDetailScene(t *testing.T) *Scene {
	t.Helper()
	toolkit.SetClipboard(nil) // fresh in-process clipboard
	s := New(900, 640, ThemeFor(OSMac, false))
	s.SetScale(1)
	s.OpenDetail(source.Item{
		Source: source.HackerNews, Title: "Hello Detail Title",
		Body: "alpha beta gamma delta\n\nepsilon zeta eta theta", Link: "https://ex.com/a",
	})
	buf := make([]byte, s.W*s.H*4)
	s.Draw(buf) // populates the text-selection runs
	return s
}

func TestDetailDragSelectionCopy(t *testing.T) {
	s := drawDetailScene(t)

	// Drag from above the content (anchors at the first run) to far bottom-right
	// (the last run) → selects the whole article body.
	s.SelectionBegin(20, 30)
	s.MouseMove(3000, 3000) // selecting → extends
	if !s.HasSelection() {
		t.Fatal("a drag across the reading view should produce a selection")
	}
	// Cmd/Ctrl+C copies the SELECTION (not the whole article), so the copied text
	// is drawn from the on-screen runs: title + body words.
	if !s.Copy() {
		t.Fatal("Copy with a selection should report true")
	}
	got := toolkit.ClipboardText()
	if !strings.Contains(got, "Detail") || !strings.Contains(got, "alpha") || !strings.Contains(got, "theta") {
		t.Fatalf("selection copy missing expected words: %q", got)
	}

	// Redraw with the selection active to exercise the highlight-paint path.
	buf := make([]byte, s.W*s.H*4)
	s.Draw(buf)

	s.SelectionEnd()
	if s.selecting {
		t.Fatal("SelectionEnd should stop the drag")
	}
	// The selection survives the release (still copyable).
	if !s.HasSelection() {
		t.Fatal("selection should persist after release")
	}
	s.ClearSelection()
	if s.HasSelection() {
		t.Fatal("ClearSelection should empty the selection")
	}
}

func TestSelectionClickClearsAndFallsBackToArticle(t *testing.T) {
	s := drawDetailScene(t)
	// A press with no drag (a click): the selection is empty, so Copy falls back
	// to the whole article (title + body + link).
	s.SelectionBegin(20, 200)
	s.SelectionEnd() // no MouseMove between → empty
	if s.HasSelection() {
		t.Fatal("a click without a drag must leave the selection empty")
	}
	if !s.Copy() {
		t.Fatal("Copy should fall back to the article")
	}
	if got := toolkit.ClipboardText(); !strings.Contains(got, "https://ex.com/a") {
		t.Fatalf("article fallback missing link: %q", got)
	}
	// SelectionEnd with no active drag is a safe no-op.
	s.SelectionEnd()
}

func TestSelectionFillMix(t *testing.T) {
	s := New(400, 300, ThemeFor(OSMac, false))
	fill := s.selectionFill()
	if fill.A != 0xFF {
		t.Fatalf("selection fill must be opaque, got A=%d", fill.A)
	}
	// mixRGBA endpoints: t=0 → a, t=1 → b.
	a := toolkit.RGBA{R: 10, G: 20, B: 30, A: 0xFF}
	b := toolkit.RGBA{R: 200, G: 100, B: 50, A: 0xFF}
	if got := mixRGBA(a, b, 0); got != a {
		t.Fatalf("mix t=0 = %+v, want %+v", got, a)
	}
	if got := mixRGBA(a, b, 1); got.R != b.R || got.G != b.G || got.B != b.B {
		t.Fatalf("mix t=1 = %+v, want rgb of %+v", got, b)
	}
}
