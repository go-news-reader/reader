package windowapp

import (
	"strings"
	"testing"

	"github.com/go-news-reader/reader/app"
	"github.com/go-news-reader/reader/source"
	"github.com/go-news-reader/reader/ui"
)

// TestRoutePreviewKeyboardFocus covers the Tab focus model routed through
// Handler.Key: Tab swaps keyboard focus between the feed card list and the
// preview pane; while the pane holds focus the scroll keys (arrows, PageUp/Down,
// Space) drive the pane and must NOT move the feed selection; a second Tab
// returns focus to the feed so the arrows advance the selection again.
func TestRoutePreviewKeyboardFocus(t *testing.T) {
	a := app.New(app.Config{Registry: source.NewRegistry(), Width: 1300, Height: 700})
	a.Scene().SetScale(1)
	a.Scene().SetSubs(nil)
	long := strings.Repeat("word ", 400)
	a.Scene().SetItems([]source.Item{
		{ID: "0", Source: source.HackerNews, Title: "a", Body: long},
		{ID: "1", Source: source.HackerNews, Title: "b", Body: long},
	})
	a.Frame() // lay out the feed rows + preview pane
	h := New(a)
	s := a.Scene()

	// Down (unfocused) selects the top card. The feed is chat-style — newest at
	// the bottom — so items [0,1] (newest-first) become rows [1,0] and the top row
	// is item "1".
	h.Key("Down", 0)
	first, ok := s.PreviewItem()
	if !ok || first.ID != "1" {
		t.Fatalf("after Down, selection = %+v ok=%v, want 1 (top row)", first, ok)
	}

	// Tab focuses the preview pane.
	h.Key("Tab", 0)
	if !s.PreviewFocused() {
		t.Fatal("Tab should focus the preview pane")
	}

	// Every scroll key now drives the pane, leaving the feed selection put.
	h.Key("Down", 0)
	h.Key("Up", 0)
	h.Key("PageDown", 0)
	h.Key("PageUp", 0)
	h.Key("", ' ') // Space pages the preview down
	if it, _ := s.PreviewItem(); it.ID != "1" {
		t.Fatalf("preview focus must not move the feed selection (now %q)", it.ID)
	}

	// A non-space printable rune with the pane focused still does nothing to the
	// selection (search is unfocused), and does not crash.
	h.Key("", 'x')
	if it, _ := s.PreviewItem(); it.ID != "1" {
		t.Fatalf("a stray rune must not move the selection (now %q)", it.ID)
	}

	// Tab again hands focus back to the feed; Down advances the selection down a
	// row, to item "0".
	h.Key("Tab", 0)
	if s.PreviewFocused() {
		t.Fatal("a second Tab should drop the preview focus")
	}
	h.Key("Down", 0)
	if it, _ := s.PreviewItem(); it.ID != "0" {
		t.Fatalf("after unfocus, Down should advance to 0 (got %q)", it.ID)
	}
}

// TestRoutePreviewFocusKeysFollowFocus covers the guard branches: with the
// preview unfocused the paging keys belong to the feed card list (and Space,
// which is only ever a preview gesture, stays inert); outside the feed view
// Tab and the paging keys are ignored entirely.
func TestRoutePreviewFocusKeysFollowFocus(t *testing.T) {
	h := feedApp(t)
	s := h.a.Scene()
	// Home/PageUp reach the top of the card list, which fires OnPullRefresh. Its
	// default closure runs the aggregation in a goroutine that mutates the scene
	// this test is also driving -- the front-end serialises that through
	// DeferSceneWrites, a test cannot. Count the trigger instead of running it.
	pulls := 0
	h.a.SetPullRefreshHook(func() { pulls++ })

	// Feed view, nothing focused: Space is inert -- it is the preview's "read on"
	// gesture and has no meaning for the card list.
	h.Key("", ' ')
	if _, ok := s.PreviewItem(); ok {
		t.Fatal("Space must not select a feed item while the preview is unfocused")
	}

	// PageDown/PageUp, on the other hand, page the CardList: they move its
	// selection cursor, which fires the select hook and fills the preview.
	// Three cards fit in one viewport, so a page is the whole list: PageDown lands
	// on the bottom row and PageUp back on the top one. Rows are [2,1,0] (newest
	// at the bottom), so that is item "0" then item "2".
	h.Key("PageDown", 0)
	if it, ok := s.PreviewItem(); !ok || it.ID != "0" {
		t.Fatalf("PageDown unfocused = %q ok=%v, want the bottom row 0", it.ID, ok)
	}
	h.Key("PageUp", 0)
	if it, ok := s.PreviewItem(); !ok || it.ID != "2" {
		t.Fatalf("PageUp unfocused = %q ok=%v, want the top row 2", it.ID, ok)
	}
	// End/Home jump to the ends of the list, same rows.
	h.Key("End", 0)
	if it, ok := s.PreviewItem(); !ok || it.ID != "0" {
		t.Fatalf("End unfocused = %q ok=%v, want the bottom row 0", it.ID, ok)
	}
	h.Key("Home", 0)
	if it, ok := s.PreviewItem(); !ok || it.ID != "2" {
		t.Fatalf("Home unfocused = %q ok=%v, want the top row 2", it.ID, ok)
	}

	// With the pane focused, Home/End are consumed rather than moving the feed
	// selection out from under the article being read: there is no preview
	// counterpart for them yet, and silently re-selecting would be worse than
	// doing nothing.
	if !s.TogglePreviewFocus() {
		t.Fatal("setup: the pane should be focusable with a selection")
	}
	h.Key("End", 0)
	h.Key("Home", 0)
	if it, ok := s.PreviewItem(); !ok || it.ID != "2" {
		t.Fatalf("Home/End with the preview focused moved the selection to %q", it.ID)
	}
	h.Key("Tab", 0) // back to the feed for the rest of the test

	// Outside the feed, Tab and the page keys are ignored entirely.
	h.a.VM().OpenSettings.Execute()
	if s.Mode() != ui.ModeSettings {
		t.Fatalf("setup: mode = %v", s.Mode())
	}
	h.Key("Tab", 0)
	h.Key("PageDown", 0)
	h.Key("PageUp", 0)
	if s.PreviewFocused() {
		t.Fatal("Tab must not focus the preview outside the feed view")
	}

	// Reaching the top of the feed did ask for a pull-refresh -- proof the hook
	// above stood in for real work rather than hiding a path that never ran.
	if pulls == 0 {
		t.Error("paging to the top of the feed never triggered a pull-refresh")
	}
}
