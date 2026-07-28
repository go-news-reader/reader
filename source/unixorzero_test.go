package source

import (
	"testing"
	"time"
)

func TestUnixOrZero(t *testing.T) {
	if got := UnixOrZero(time.Time{}); got != 0 {
		t.Fatalf("zero Time -> %d, want 0 (unknown), not year-1", got)
	}
	ref := time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC)
	if got := UnixOrZero(ref); got != ref.Unix() {
		t.Fatalf("real Time -> %d, want %d", got, ref.Unix())
	}
	// A genuine pre-1970 date must be preserved (negative, not clamped).
	old := time.Date(1969, 1, 1, 0, 0, 0, 0, time.UTC)
	if got := UnixOrZero(old); got != old.Unix() || got >= 0 {
		t.Fatalf("pre-1970 Time -> %d, want %d (negative)", got, old.Unix())
	}
}

// A zero-time item (mapped to Created 0) must not sink below real items — it
// sorts as an unknown, above nothing-in-particular but never at year-1 depth.
func TestSortItemsZeroTimeNotBelowReal(t *testing.T) {
	items := []Item{
		{ID: "real", Created: UnixOrZero(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC))},
		{ID: "unknown", Created: UnixOrZero(time.Time{})}, // 0, not -62135596800
	}
	sortItems(items)
	if items[0].ID != "real" {
		t.Fatalf("real item should sort first, got %q", items[0].ID)
	}
	if items[1].Created != 0 {
		t.Fatalf("unknown item Created = %d, want 0", items[1].Created)
	}
}
