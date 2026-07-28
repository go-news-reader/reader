package ui

import (
	"testing"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
)

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

// TestTTFontRendersCJK proves the script-fallback chain is wired: a Chinese
// string measures non-zero and actually paints glyphs (routed to the Noto face
// the Latin primary lacks), so mixed-script text like a Chinese headline shows.
func TestTTFontRendersCJK(t *testing.T) {
	f := ttFont(false, 20)
	if f.Measure("中文标题") <= 0 {
		t.Fatal("CJK text measured 0 — fallback chain not wired")
	}
	const w, h = 140, 30
	buf := make([]byte, w*h*4)
	f.Draw(painter.NewPixelPainter(buf, w, h), 2, 4, "中文", toolkit.RGBA{R: 0, G: 0, B: 0, A: 255})
	for _, b := range buf {
		if b != 0 {
			return // painted at least one glyph pixel
		}
	}
	t.Fatal("CJK text drew nothing — the primary lacks the glyphs and no fallback ran")
}
