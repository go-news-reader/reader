package ui

import (
	"image"
	"strings"
	"testing"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
)

// TestGetFaceCJKFallback covers the getFace fallback path (the hand-drawn views):
// coverage routing, per-rune width, and mixed-script draw (with + without the
// synthetic-bold pass), while the Latin fast path stays byte-identical.
func TestGetFaceCJKFallback(t *testing.T) {
	f := getFace(16, false)
	if len(f.faces) < 2 {
		t.Fatal("getFace should build a primary + fallback faces")
	}
	// Coverage routing.
	if !f.primaryCovers("Hi!") || f.primaryCovers("中") {
		t.Fatal("primaryCovers: Latin yes, CJK no")
	}
	if f.faceFor('A') != f.face {
		t.Fatal("Latin routes to the primary face")
	}
	if f.faceFor('中') == f.face {
		t.Fatal("CJK routes to a fallback face")
	}
	if f.faceFor('🙂') != f.face {
		t.Fatal("an uncovered rune falls back to the primary")
	}
	// Width: the fallback sum is positive and mixed text is wider than its Latin
	// prefix alone.
	if f.width("中文") <= 0 || f.width("Hi 中文") <= f.width("Hi ") {
		t.Fatal("CJK width should be measured through the fallback")
	}
	// A rune no face covers (emoji) alongside CJK still measures (uncovered → the
	// primary's .notdef advance) without panicking.
	if f.width("中🙂") < f.width("中") {
		t.Fatal("an uncovered rune must not shrink the measured width")
	}
	// Draw the mixed run (fallback path) — expect painted pixels.
	img := image.NewRGBA(image.Rect(0, 0, 220, 30))
	f.draw(img, 2, 4, "Hi 中文", toolkit.RGBA{R: 0, G: 0, B: 0, A: 255})
	if !anyPainted(img.Pix) {
		t.Fatal("mixed Latin+CJK drew nothing")
	}
	// The synthetic-bold fallback branch (a system font exposes only Regular).
	bold := textFace{face: f.face, faces: f.faces, ascent: f.ascent, height: f.height, synthBold: true}
	img2 := image.NewRGBA(image.Rect(0, 0, 120, 30))
	bold.draw(img2, 2, 4, "中A", toolkit.RGBA{R: 0, G: 0, B: 0, A: 255})
	if !anyPainted(img2.Pix) {
		t.Fatal("synth-bold mixed draw painted nothing")
	}
	// A face with no fallback chain uses the primary-only fast path.
	primaryOnly := textFace{face: f.face, ascent: f.ascent, height: f.height}
	if primaryOnly.width("中") < 0 { // no panic; measures via the primary
		t.Fatal("unreachable")
	}
	primaryOnly.draw(image.NewRGBA(image.Rect(0, 0, 30, 30)), 0, 0, "A", toolkit.RGBA{A: 255})
}

func anyPainted(pix []byte) bool {
	for _, b := range pix {
		if b != 0 {
			return true
		}
	}
	return false
}

// TestTruncateFont covers the toolkit-font ellipsis clip used by box-composed
// labels: a fit (early return), a zero width, a mid clip, and the tiny-width
// "…"-only fallback.
func TestTruncateFont(t *testing.T) {
	f := ttFont(false, 14)
	if got := truncateFont(f, "hi", 100000); got != "hi" {
		t.Fatalf("fit = %q, want hi", got)
	}
	if got := truncateFont(f, "hi", 0); got != "hi" {
		t.Fatalf("zero width = %q, want hi (early return)", got)
	}
	const long = "verylongchannelname_that_will_not_fit_here"
	got := truncateFont(f, long, f.Measure("verylong"))
	if got == long || !strings.HasSuffix(got, "…") {
		t.Fatalf("clip = %q, want a shorter …-terminated string", got)
	}
	if got := truncateFont(f, long, 1); got != "…" {
		t.Fatalf("tiny width = %q, want …", got)
	}
}

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
