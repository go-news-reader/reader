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

func TestSelectableAt(t *testing.T) {
	s := New(1000, 700, ThemeFor(OSMac, false))
	s.SetScale(1)

	// Detail (reading) view: selectable anywhere.
	s.OpenDetail(source.Item{Source: source.HackerNews, Title: "T", Body: "b"})
	if !s.SelectableAt(5, 5) {
		t.Fatal("detail view should be selectable")
	}
	s.CloseDetail()

	// Feed + a non-web preview (text summary): the preview text pane, the feed
	// list of cards and the sidebar labels are all drag-selectable.
	s.SetSubs([]Subscription{{Source: source.Reddit, Channel: "golang"}})
	s.SetItems([]source.Item{{ID: "1", Source: source.Reddit, Title: "hi", Score: -1, Comments: -1}})
	s.SelectPreview(source.Item{Source: source.HackerNews, Title: "T", Body: "body text"})
	buf := make([]byte, s.W*s.H*4)
	s.Draw(buf)
	pr := s.previewR
	if pr.W == 0 {
		t.Fatal("preview pane not laid out")
	}
	if !s.SelectableAt(pr.X+pr.W/2, pr.Y+pr.H/2) {
		t.Fatal("inside the preview text pane should be selectable")
	}
	// Left of the preview pane is now the feed list of cards — selectable so a
	// drag over a card title selects it.
	if !s.SelectableAt(pr.X-5, pr.Y+pr.H/2) {
		t.Fatal("the feed list of cards should be selectable")
	}
	// The sidebar's middle list is a TreeView whose rows resolve to actions, not
	// selectable text, so a press over it is not one of ours.
	if s.SelectableAt(s.m.sidebarW/2, s.sideBandTop+s.m.sideItemH/2) {
		t.Fatal("the sidebar TreeView band must not text-select")
	}
	// The topbar (above the feed/sidebar) is chrome, not a reading surface.
	if s.SelectableAt(s.m.sidebarW/2, 5) {
		t.Fatal("the topbar must not text-select")
	}

	// Feed + a web preview: the embedded browser handles its own selection, so a
	// press inside the pane is not one of ours (the feed list around it still is).
	s.SelectPreview(source.Item{Source: source.HackerNews, Title: "W", Link: "https://ex.com/a"})
	s.Draw(buf)
	pr = s.previewR
	if s.SelectableAt(pr.X+pr.W/2, pr.Y+pr.H/2) {
		t.Fatal("a web preview must not start a text selection")
	}

	// A non-feed, non-detail view (Settings) is never selectable (default branch).
	s2 := New(800, 600, ThemeFor(OSMac, false))
	s2.OpenSettings()
	if s2.SelectableAt(400, 300) {
		t.Fatal("the settings view is not a reading surface → not selectable")
	}
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
