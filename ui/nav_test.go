package ui

import (
	"strconv"
	"testing"

	"github.com/go-news-reader/reader/source"
)

func navItems(n int) []source.Item {
	items := make([]source.Item, n)
	for i := range items {
		items[i] = source.Item{ID: strconv.Itoa(i), Source: source.Reddit, Title: "t" + strconv.Itoa(i)}
	}
	return items
}

func TestNavItemSelection(t *testing.T) {
	s := New(900, 600, ThemeFor(OSLinux, false))
	items := navItems(3)
	s.SetItems(items)
	s.layout()

	// No selection: down picks the first card, up picks the last.
	if it, ok := s.NavItem(1); !ok || it.ID != "0" {
		t.Fatalf("down-from-none = %q %v, want 0", it.ID, ok)
	}
	if it, ok := s.NavItem(-1); !ok || it.ID != "2" {
		t.Fatalf("up-from-none = %q %v, want 2", it.ID, ok)
	}

	// From a selection, step to the neighbour (NavItem reads the current preview).
	s.SelectPreview(items[0])
	if it, _ := s.NavItem(1); it.ID != "1" {
		t.Fatalf("down = %q, want 1", it.ID)
	}
	s.SelectPreview(items[1])
	if it, _ := s.NavItem(-1); it.ID != "0" {
		t.Fatalf("up = %q, want 0", it.ID)
	}

	// Clamp at both ends.
	s.SelectPreview(items[0])
	if it, _ := s.NavItem(-1); it.ID != "0" {
		t.Fatalf("up-clamp = %q, want 0", it.ID)
	}
	s.SelectPreview(items[2])
	if it, _ := s.NavItem(1); it.ID != "2" {
		t.Fatalf("down-clamp = %q, want 2", it.ID)
	}

	// An empty feed reports no selectable card.
	empty := New(900, 600, ThemeFor(OSLinux, false))
	empty.SetItems(nil)
	empty.layout()
	if _, ok := empty.NavItem(1); ok {
		t.Fatal("empty feed should have no selectable card")
	}
}

func TestNavItemSkipsGroups(t *testing.T) {
	s := New(900, 600, ThemeFor(OSLinux, false))
	// d1..d3 collapse into one Usenet post group; n1 and s1 are standalone cards.
	s.SetItems([]source.Item{
		usenetItem("n1", "just a note"),
		usenetItem("d1", `[1/3] - "release.tar.zst" yEnc (1/1) 1000`),
		usenetItem("d2", `[2/3] - "release.tar.zst.par2" yEnc (1/1) 200`),
		usenetItem("d3", `[3/3] - "release.tar.zst.vol00+01.par2" yEnc (1/1) 300`),
		usenetItem("s1", `[1/1] - "other.tar.zst" yEnc (1/1) 500`),
	})
	s.layout()

	// Navigation visits only the standalone cards, never the group row.
	first, ok := s.NavItem(1)
	if !ok || first.ID != "n1" {
		t.Fatalf("first card = %q %v, want n1", first.ID, ok)
	}
	s.SelectPreview(first)
	if next, _ := s.NavItem(1); next.ID != "s1" {
		t.Fatalf("next card = %q, want s1 (the group is skipped)", next.ID)
	}
}

func TestNavScrollsIntoView(t *testing.T) {
	s := New(900, 400, ThemeFor(OSLinux, false))
	items := navItems(40)
	s.SetItems(items)
	s.layout()

	// Walk to the last item; the feed must scroll to keep it visible.
	var it source.Item
	s.SelectPreview(items[0])
	for range items {
		it, _ = s.NavItem(1)
		s.SelectPreview(it)
	}
	if it.ID != "39" {
		t.Fatalf("did not reach the last item: %q", it.ID)
	}
	if s.feedScroll.offset == 0 {
		t.Fatal("navigating to the last item should scroll the feed down")
	}
	// Walk back to the top; the feed scrolls up so the first card is flush with
	// the top of the viewport (at the first row's content offset).
	for range items {
		it, _ = s.NavItem(-1)
		s.SelectPreview(it)
	}
	if s.feedScroll.offset != s.rows[0].top {
		t.Fatalf("navigating to the top should scroll to the first row (%d), got %d", s.rows[0].top, s.feedScroll.offset)
	}
}
