package windowapp

import (
	"testing"

	"github.com/go-news-reader/reader/app"
	"github.com/go-news-reader/reader/source"
	"github.com/go-news-reader/reader/ui"
)

// feedApp builds a handler over a feed with three cards laid out.
func feedApp(t *testing.T) *Handler {
	t.Helper()
	a := app.New(app.Config{Registry: source.NewRegistry(), Width: 1000, Height: 700})
	a.Scene().SetScale(1)
	a.Scene().SetSubs(nil)
	a.Scene().SetItems([]source.Item{
		{ID: "0", Source: source.HackerNews, Title: "a"},
		{ID: "1", Source: source.HackerNews, Title: "b"},
		{ID: "2", Source: source.HackerNews, Title: "c"},
	})
	a.Scene().Draw(make([]byte, a.Scene().W*a.Scene().H*4)) // lay out rows
	return New(a)
}

func TestRouteFeedArrowKeys(t *testing.T) {
	h := feedApp(t)
	s := h.a.Scene()

	// The feed is chat-style: newest at the bottom, so the display order is
	// oldest→newest. Items [0,1,2] (newest-first) become rows [2,1,0]; Down from no
	// selection lands on the top (oldest) row, item "2", then advances downward.
	h.Key("Down", 0)
	if it, ok := s.PreviewItem(); !ok || it.ID != "2" {
		t.Fatalf("after Down = %+v ok=%v, want 2 (top row)", it, ok)
	}
	h.Key("Down", 0)
	if it, _ := s.PreviewItem(); it.ID != "1" {
		t.Fatalf("after 2x Down = %q, want 1", it.ID)
	}
	// Up returns to the previous (top) card.
	h.Key("Up", 0)
	if it, _ := s.PreviewItem(); it.ID != "2" {
		t.Fatalf("after Up = %q, want 2", it.ID)
	}

	// Enter opens the selected post's reading view.
	h.Key("Enter", 0)
	if s.Mode() != ui.ModeDetail {
		t.Fatalf("Enter should open detail, mode = %v", s.Mode())
	}
	// Enter again (now in the reading view) is a harmless no-op — it must not
	// crash or leave the detail view.
	h.Key("Enter", 0)
	if s.Mode() != ui.ModeDetail {
		t.Fatalf("Enter in detail should stay in detail, mode = %v", s.Mode())
	}
}

func TestRouteFeedEnterNoSelection(t *testing.T) {
	h := feedApp(t)
	s := h.a.Scene()
	// No selection: Enter just defocuses search and stays on the feed.
	h.Key("Enter", 0)
	if s.Mode() != ui.ModeFeed {
		t.Fatalf("Enter with no selection should stay on the feed, mode = %v", s.Mode())
	}
}

func TestRouteArrowsIgnoredOutsideFeed(t *testing.T) {
	h := feedApp(t)
	s := h.a.Scene()
	h.a.VM().OpenSettings.Execute() // leave the feed
	if s.Mode() != ui.ModeSettings {
		t.Fatalf("setup: mode = %v", s.Mode())
	}
	h.Key("Down", 0) // must not select a feed item while in Settings
	h.Key("Up", 0)
	if _, ok := s.PreviewItem(); ok {
		t.Fatal("arrow keys must not drive the feed outside feed mode")
	}
}
