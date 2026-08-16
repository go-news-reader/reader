package app

import (
	"testing"

	"github.com/go-news-reader/reader/source"
	"github.com/go-news-reader/reader/ui"
)

func TestSelectAdjacent(t *testing.T) {
	a := New(Config{Registry: newReg()})
	a.scene.SetSubs(nil)
	a.scene.SetItems([]source.Item{
		{ID: "0", Source: source.HackerNews, Title: "a"},
		{ID: "1", Source: source.HackerNews, Title: "b"},
		{ID: "2", Source: source.HackerNews, Title: "c"},
	})
	a.scene.Draw(make([]byte, a.scene.W*a.scene.H*4)) // lays out the feed rows

	// The chat-style feed is newest-at-bottom, so items [0,1,2] (newest-first) lay
	// out oldest→newest as rows [2,1,0]. Down from no selection lands on the top
	// (oldest) row, item "2", and advances downward from there.
	a.SelectAdjacent(1)
	if it, ok := a.scene.PreviewItem(); !ok || it.ID != "2" {
		t.Fatalf("first select = %+v ok=%v, want 2", it, ok)
	}
	// Down again advances; up returns.
	a.SelectAdjacent(1)
	if it, _ := a.scene.PreviewItem(); it.ID != "1" {
		t.Fatalf("down = %q, want 1", it.ID)
	}
	a.SelectAdjacent(-1)
	if it, _ := a.scene.PreviewItem(); it.ID != "2" {
		t.Fatalf("up = %q, want 2", it.ID)
	}
}

func TestSelectAdjacentEmptyFeed(t *testing.T) {
	a := New(Config{Registry: newReg()})
	a.scene.SetItems(nil)
	a.scene.Draw(make([]byte, a.scene.W*a.scene.H*4))
	a.SelectAdjacent(1) // no cards: must be a no-op, not a panic
	if _, ok := a.scene.PreviewItem(); ok {
		t.Fatal("nothing should be selected in an empty feed")
	}
}

func TestOpenSelected(t *testing.T) {
	a := New(Config{Registry: newReg()})
	// Nothing selected: OpenSelected does nothing and stays on the feed.
	a.OpenSelected()
	if a.scene.Mode() != ui.ModeFeed {
		t.Fatalf("mode = %v, want feed", a.scene.Mode())
	}
	// With a selection, OpenSelected opens its reading view.
	a.SelectPreview(source.Item{ID: "x", Source: source.HackerNews, Title: "hello"})
	a.OpenSelected()
	if a.scene.Mode() != ui.ModeDetail || a.scene.Detail().ID != "x" {
		t.Fatalf("detail not opened: mode=%v item=%+v", a.scene.Mode(), a.scene.Detail())
	}
}
