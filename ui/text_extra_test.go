package ui

import "testing"

// TestTTFont covers the toolkit-TrueType font cache: the regular branch (badges
// only use bold), the px<1 clamp, and a cache hit on repeat.
func TestTTFont(t *testing.T) {
	a := ttFont(false, 0) // regular + px<1 clamp
	if a == nil {
		t.Fatal("ttFont(regular) returned nil")
	}
	if b := ttFont(false, 0); b != a {
		t.Fatal("ttFont did not cache (regular)")
	}
	if bold := ttFont(true, 12); bold == nil || bold == a {
		t.Fatal("ttFont(bold) should be a distinct non-nil font")
	}
	// A wider string measures wider (proportional TrueType).
	if ttFont(false, 16).Measure("WWWW") <= ttFont(false, 16).Measure("ii") {
		t.Fatal("expected proportional widths")
	}
}
