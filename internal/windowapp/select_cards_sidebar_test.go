package windowapp

import (
	"testing"

	"github.com/go-news-reader/reader/source"
	"github.com/go-news-reader/reader/ui"
)

// drawFrame renders one scene frame so the cross-surface selectable runs are
// committed before a pointer drag consults them.
func drawFrame(a interface{ Scene() *ui.Scene }) {
	s := a.Scene()
	buf := make([]byte, s.W*s.H*4)
	s.Draw(buf)
}

// TestCardClickStillPreviews confirms a plain click (press+release, no drag)
// still runs the deferred action: the item loads into the preview pane.
func TestCardClickStillPreviews(t *testing.T) {
	a := newApp(t)
	a.Scene().SetSubs(nil)
	a.Scene().SetItems([]source.Item{{ID: "1", Source: source.Reddit, Channel: "chan",
		Title: "CLICKME", Score: -1, Comments: -1}})
	h := New(a)
	drawFrame(a)
	s := a.Scene()

	h.MouseDown(250, 60)
	h.MouseUp(260, 62) // a tiny wobble, still no selection
	it, ok := s.PreviewItem()
	if !ok || it.ID != "1" {
		t.Fatalf("a click without a drag should preview the item; got %+v ok=%v", it, ok)
	}
	if s.HasSelection() {
		t.Fatal("a click without a drag must not leave a selection")
	}
}

// subRowY scans the sidebar column for the screen y of the subscription row
// whose Sub index is want, so the test targets the real row geometry (below the
// profile tab strip) instead of a hard-coded coordinate.
func subRowY(s *ui.Scene, want int) (int, bool) {
	for y := 0; y < s.H; y++ {
		if hit := s.HitTest(8, y); hit.Kind == ui.HitSub && hit.Sub == want {
			return y, true
		}
	}
	return 0, false
}

// TestSidebarRowActsOnPress proves a sidebar row now resolves through the
// TreeView to its action on press (no text-selection deferral): a click on the
// "All Sources" row switches the filter to AllFilter and leaves no selection.
func TestSidebarRowActsOnPress(t *testing.T) {
	a := newApp(t)
	a.Scene().SetSubs([]ui.Subscription{{Source: source.Reddit, Channel: "c", Label: "SUBROW"}})
	a.Scene().SetActive(0) // start on the SUBROW subscription (index 0), not AllFilter
	h := New(a)
	drawFrame(a)
	s := a.Scene()

	y, ok := subRowY(s, ui.AllFilter)
	if !ok {
		t.Fatal("could not locate the All Sources row")
	}
	h.MouseDown(8, y)
	if s.Active != ui.AllFilter {
		t.Fatalf("a press on the All Sources row should switch to AllFilter; Active=%d", s.Active)
	}
	h.MouseUp(8, y)
	if s.HasSelection() {
		t.Fatal("a sidebar row is not selectable text; it must leave no selection")
	}
}
