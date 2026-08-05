package ui

import (
	"image"
	"testing"

	"github.com/go-news-reader/reader/source"
)

func webItemN(id, title string) source.Item {
	return source.Item{ID: id, Source: source.HackerNews, Title: title, Link: "https://ex/" + id}
}

// openTab selects an item and delivers a rendered page for it (which opens its
// tab), seeding its history.
func openTab(s *Scene, it source.Item) {
	s.SelectPreview(it)
	s.SetPreviewWeb(it.ID, image.NewRGBA(image.Rect(0, 0, 400, 900)), nil, 400)
	s.InitWebHistory(it.ID, it.Link)
}

func TestWebTabModel(t *testing.T) {
	s := New(1200, 760, ThemeFor(OSMac, false))
	openTab(s, webItemN("a", "Alpha"))
	if s.webTabsShown() {
		t.Fatal("a single tab should not show the strip")
	}
	openTab(s, webItemN("b", "Beta"))
	if !s.webTabsShown() || len(s.webTabs) != 2 {
		t.Fatalf("two tabs expected, got %d", len(s.webTabs))
	}
	// Re-viewing an open item does not duplicate or reorder it.
	openTab(s, webItemN("a", "Alpha"))
	if len(s.webTabs) != 2 || s.webTabs[0].ID != "a" {
		t.Fatalf("re-view should not add/reorder; tabs=%v", tabIDs(s))
	}
	// WebTabItem finds open tabs and misses unknown ones.
	if _, ok := s.WebTabItem("b"); !ok {
		t.Fatal("WebTabItem(b) should be found")
	}
	if _, ok := s.WebTabItem("zzz"); ok {
		t.Fatal("WebTabItem(zzz) should miss")
	}

	// Filling past the cap evicts the oldest tab and its cached web state.
	for _, id := range []string{"c", "d", "e", "f", "g"} {
		openTab(s, webItemN(id, id))
	}
	if len(s.webTabs) != maxWebTabs {
		t.Fatalf("cap = %d, got %d (%v)", maxWebTabs, len(s.webTabs), tabIDs(s))
	}
	if s.hasWeb("a") { // "a" was oldest → evicted
		t.Fatal("evicted tab's cached page should be dropped")
	}
}

func TestWebTabClose(t *testing.T) {
	// Closing an unknown id is a no-op.
	s := New(1100, 760, ThemeFor(OSMac, false))
	openTab(s, webItemN("a", "A"))
	openTab(s, webItemN("b", "B"))
	openTab(s, webItemN("c", "C")) // active = c
	if _, ok := s.CloseWebTab("zzz"); ok {
		t.Fatal("closing unknown id should report ok=false")
	}

	// Closing a non-active tab drops it without changing the selection.
	if it, ok := s.CloseWebTab("a"); ok || len(s.webTabs) != 2 {
		t.Fatalf("close non-active: ok=%v tabs=%v it=%v", ok, tabIDs(s), it.ID)
	}
	if s.hasWeb("a") {
		t.Fatal("closed tab's page should be evicted")
	}

	// Closing the active tab (c) returns the neighbour that takes its slot (b).
	next, ok := s.CloseWebTab("c")
	if !ok || next.ID != "b" {
		t.Fatalf("close active c → next=%q ok=%v, want b,true", next.ID, ok)
	}

	// Closing the last remaining active tab returns ok=false (nothing to switch to).
	s.SelectPreview(webItemN("b", "B")) // make b active
	if _, ok := s.CloseWebTab("b"); ok {
		t.Fatal("closing the last tab should report ok=false")
	}
	if len(s.webTabs) != 0 {
		t.Fatalf("no tabs should remain, got %v", tabIDs(s))
	}

	// Closing the active tab when it is the LAST in the list picks the new last.
	s2 := New(1100, 760, ThemeFor(OSMac, false))
	openTab(s2, webItemN("x", "X"))
	openTab(s2, webItemN("y", "Y")) // active = y (last)
	next2, ok2 := s2.CloseWebTab("y")
	if !ok2 || next2.ID != "x" {
		t.Fatalf("close last active y → next=%q ok=%v, want x,true", next2.ID, ok2)
	}
}

func TestWebTabDrawAndHit(t *testing.T) {
	s := New(1200, 760, ThemeFor(OSMac, false))
	openTab(s, webItemN("a", "Alpha article with a long title that must be clipped"))
	openTab(s, webItemN("b", "")) // empty title → placeholder path
	openTab(s, webItemN("c", "Gamma"))
	buf := make([]byte, s.W*s.H*4)
	s.Draw(buf)
	if len(s.webTabHits) < 3 {
		t.Fatalf("expected 3 drawn tabs, got %d", len(s.webTabHits))
	}
	// A click on a non-active tab body routes to HitWebTab with its id.
	tb := s.webTabHits[0]
	if hit, _ := s.previewHitTest(tb.body.X+3, tb.body.Y+3); hit.Kind != HitWebTab || hit.Value != "a" {
		t.Fatalf("tab body hit = %+v, want HitWebTab a", hit)
	}
	// A click on the close box routes to HitWebTabClose.
	if hit, _ := s.previewHitTest(tb.closeR.X+1, tb.closeR.Y+2); hit.Kind != HitWebTabClose || hit.Value != "a" {
		t.Fatalf("tab close hit = %+v, want HitWebTabClose a", hit)
	}
	// A miss in the strip's gap between tabs returns no tab hit.
	if hit, ok := s.webTabHitTest(s.previewR.X+s.previewR.W-1, s.previewR.Y-50); ok {
		t.Fatalf("out-of-strip should miss, got %+v", hit)
	}
}

// TestWebTabNarrow forces the tab-width floor and the ran-out-of-room break by
// packing the maximum tabs into a minimal pane.
func TestWebTabNarrow(t *testing.T) {
	s := New(780, 760, ThemeFor(OSMac, false)) // just wide enough to keep a (narrow) pane
	for _, id := range []string{"a", "b", "c", "d", "e", "f"} {
		openTab(s, webItemN(id, id+"-title-long-enough-to-clip"))
	}
	buf := make([]byte, s.W*s.H*4)
	s.Draw(buf) // exercises the tw floor and the tabX+tw>right break
	if !s.webTabsShown() {
		t.Fatal("six tabs should show the strip")
	}
}

func TestClipTextTail(t *testing.T) {
	s := New(900, 600, ThemeFor(OSMac, false))
	s.Draw(make([]byte, s.W*s.H*4))
	f := s.m.side
	const long = "A fairly long article title that will not fit in a narrow tab"
	if got := clipTextTail(f, "hi", 100000); got != "hi" {
		t.Fatalf("fits: %q", got)
	}
	if got := clipTextTail(f, "hi", 0); got != "hi" {
		t.Fatalf("w<=0: %q", got)
	}
	mid := clipTextTail(f, long, f.width(long)/2)
	if mid == long || []rune(mid)[len([]rune(mid))-1] != '…' {
		t.Fatalf("mid cut = %q, want head + trailing ellipsis", mid)
	}
	if got := clipTextTail(f, long, 1); got != "…" {
		t.Fatalf("tiny width = %q, want just the ellipsis", got)
	}
}

func tabIDs(s *Scene) []string {
	ids := make([]string, len(s.webTabs))
	for i, t := range s.webTabs {
		ids[i] = t.ID
	}
	return ids
}
