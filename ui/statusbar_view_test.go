package ui

import (
	"testing"

	"github.com/go-news-reader/reader/source"
)

func TestGroupCountSegs(t *testing.T) {
	items := []source.Item{
		{Channel: "z.group", GroupCount: 10},
		{Channel: "a.group", GroupCount: 5},
		{Channel: "a.group", GroupCount: 7}, // max wins → 7
		{Channel: "nocount"},                // GroupCount 0 → skipped
		{Channel: "", GroupCount: 3},         // empty channel → skipped
	}
	segs := groupCountSegs(items)
	if len(segs) != 2 || segs[0] != "a.group  7 posts" || segs[1] != "z.group  10 posts" {
		t.Fatalf("segs = %v", segs)
	}
	if groupCountSegs(nil) != nil {
		t.Fatal("no items → nil")
	}
	if groupCountSegs([]source.Item{{Channel: "x"}}) != nil {
		t.Fatal("items with no counts → nil")
	}
}

func TestStatusBarLayoutAndDraw(t *testing.T) {
	s := New(900, 420, ThemeFor(OSMac, false))
	s.SetSubs(nil)
	s.layout()
	if s.statusBarH() != 0 || s.statusBarRect().W != 0 {
		t.Fatal("no group counts → no status bar")
	}
	s.SetItems([]source.Item{
		{ID: "1", Source: source.Usenet, Channel: "alt.test", Title: "x", GroupCount: 99, Score: -1, Comments: -1},
	})
	buf := make([]byte, s.W*s.H*4)
	s.Draw(buf) // computes statusSegs + draws the bar
	if s.statusBarH() <= 0 {
		t.Fatal("group counts → status bar shown")
	}
	if r := s.statusBarRect(); r.W == 0 || r.Y != s.H-s.statusBarH() || r.X != s.m.sidebarW {
		t.Fatalf("status bar rect = %+v", r)
	}
	// It shrinks the feed content area.
	if s.feedBottom() >= s.H {
		t.Fatal("status bar should reduce feedBottom")
	}
	// Hidden outside the feed view.
	s.OpenSettings()
	if s.statusBarH() != 0 || s.statusBarRect().W != 0 {
		t.Fatal("status bar hides outside the feed view")
	}
}
