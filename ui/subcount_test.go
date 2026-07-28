package ui

import (
	"testing"

	"github.com/go-news-reader/reader/source"
)

func TestSubCountsAndMarker(t *testing.T) {
	s := New(900, 500, ThemeFor(OSMac, false))
	s.SetSubs([]Subscription{
		{Source: source.Usenet, Channel: "g"},
		{Source: source.Reddit, Channel: "r"},
		{Source: source.Usenet, Channel: "h"},
		{Source: source.Usenet, Channel: "empty"},
	})
	s.SetItems([]source.Item{
		{Source: source.Usenet, Channel: "g", GroupCount: 100, GroupHigh: 500},
		{Source: source.Reddit, Channel: "r"}, {Source: source.Reddit, Channel: "r"}, // no counts → item count
		{Source: source.Usenet, Channel: "h", GroupCount: 50}, // count but no high → marker falls back to total
	})

	// Usenet with a high-water: unseen = high - seen, total = count.
	s.SetSeen(map[string]int{"usenet|g": 480})
	if total, unseen := s.subCounts(Subscription{Source: source.Usenet, Channel: "g"}); total != 100 || unseen != 20 {
		t.Fatalf("g = %d new / %d total, want 20/100", unseen, total)
	}
	// unseen clamps to total when the marker jumped far past the seen mark.
	s.SetSeen(map[string]int{})
	if _, unseen := s.subCounts(Subscription{Source: source.Usenet, Channel: "g"}); unseen != 100 {
		t.Fatalf("clamp-high unseen = %d, want 100", unseen)
	}
	// unseen clamps to 0 when the seen mark is ahead of the marker.
	s.SetSeen(map[string]int{"usenet|g": 9999})
	if _, unseen := s.subCounts(Subscription{Source: source.Usenet, Channel: "g"}); unseen != 0 {
		t.Fatalf("clamp-low unseen = %d, want 0", unseen)
	}
	// A source without a group count uses the fetched-item count as total+marker.
	s.SetSeen(map[string]int{})
	if total, unseen := s.subCounts(Subscription{Source: source.Reddit, Channel: "r"}); total != 2 || unseen != 2 {
		t.Fatalf("reddit = %d/%d, want 2/2", unseen, total)
	}
	// A group with a count but no high water: marker falls back to the total.
	if total, unseen := s.subCounts(Subscription{Source: source.Usenet, Channel: "h"}); total != 50 || unseen != 50 {
		t.Fatalf("h = %d/%d, want 50/50", unseen, total)
	}

	// SubMarker: valid index yields the key + marker; bad indices report false.
	if key, m, ok := s.SubMarker(0); !ok || key != "usenet|g" || m != 500 {
		t.Fatalf("SubMarker(0) = %q %d %v", key, m, ok)
	}
	if _, _, ok := s.SubMarker(-1); ok {
		t.Fatal("SubMarker(-1) should be false")
	}
	if _, _, ok := s.SubMarker(99); ok {
		t.Fatal("SubMarker(oob) should be false")
	}
}
