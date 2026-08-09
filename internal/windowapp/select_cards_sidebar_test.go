package windowapp

import (
	"strings"
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

// TestCardDragSelectsAndSuppressesClick proves the deferred routing: a drag
// across a feed card's text selects the title AND suppresses the card's
// click-to-preview action, while a plain click still previews (covered
// elsewhere). Ctrl+C then copies the selection.
func TestCardDragSelectsAndSuppressesClick(t *testing.T) {
	a := newApp(t)
	a.Scene().SetSubs(nil)
	a.Scene().SetItems([]source.Item{{ID: "1", Source: source.Reddit, Channel: "chan",
		Title: "DRAGME", Score: -1, Comments: -1}})
	h := New(a)
	var clip memClip
	h.SetSystemClipboard(&clip)
	drawFrame(a)
	s := a.Scene()

	// Press inside the card, drag diagonally across it (spanning the title row),
	// release: the press deferred the preview, the drag turns it into a selection.
	h.MouseDown(250, 52)
	h.MouseMove(600, 128)
	h.MouseUp(600, 128)

	if _, ok := s.PreviewItem(); ok {
		t.Fatal("a drag over the card must suppress the click-to-preview action")
	}
	if !s.HasSelection() {
		t.Fatal("a drag over the card should produce a text selection")
	}
	h.Shortcut('c', true, false)
	if !strings.Contains(clip.text, "DRAGME") {
		t.Fatalf("card drag-selection copy = %q, want it to contain DRAGME", clip.text)
	}
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

// TestSidebarDragSelectsAndSuppressesSwitch proves a drag across a sidebar row
// selects its label and suppresses the filter switch, while a plain click still
// switches (TestMouseDownSub covers the click).
func TestSidebarDragSelectsAndSuppressesSwitch(t *testing.T) {
	a := newApp(t)
	a.Scene().SetSubs([]ui.Subscription{{Source: source.Reddit, Channel: "c", Label: "SUBDRAG"}})
	a.Scene().SetActive(0) // start on the SUBDRAG subscription (index 0), not AllFilter
	h := New(a)
	drawFrame(a)
	s := a.Scene()

	// Locate the "All Sources" row (AllFilter). A plain click there would switch
	// Active to AllFilter; the drag must suppress that and select the label.
	y, ok := subRowY(s, ui.AllFilter)
	if !ok {
		t.Fatal("could not locate the All Sources row")
	}
	h.MouseDown(8, y)
	h.MouseMove(150, y)
	h.MouseUp(150, y)

	if s.Active != 0 {
		t.Fatalf("a drag over a sidebar row must suppress the filter switch; Active=%d", s.Active)
	}
	if !s.HasSelection() {
		t.Fatal("a drag over the sidebar label should produce a selection")
	}
}

// TestSidebarClickStillSwitches confirms a plain click (no drag) on a sidebar
// row still runs the deferred filter switch.
func TestSidebarClickStillSwitches(t *testing.T) {
	a := newApp(t)
	a.Scene().SetSubs([]ui.Subscription{{Source: source.Reddit, Channel: "c", Label: "SUBDRAG"}})
	a.Scene().SetActive(0)
	h := New(a)
	drawFrame(a)
	s := a.Scene()

	y, ok := subRowY(s, ui.AllFilter)
	if !ok {
		t.Fatal("could not locate the All Sources row")
	}
	h.MouseDown(8, y)
	h.MouseUp(8, y) // no drag: the deferred switch runs
	if s.Active != ui.AllFilter {
		t.Fatalf("a click on the All Sources row should switch to AllFilter; Active=%d", s.Active)
	}
	if s.HasSelection() {
		t.Fatal("a click without a drag must not leave a selection")
	}
}
