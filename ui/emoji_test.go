package ui

import (
	"image"
	"testing"

	"github.com/go-widgets/toolkit"
)

// feedEmoji is a spread of the pictographs a real feed actually carries — moon
// phases, a rocket, gestures, a person, and two BMP dingbats — plus a ZWJ
// sequence's base rune.
var feedEmoji = []rune{'🚀', '🌔', '🌘', '🧑', '👏', '👍', '✨', '🔥', '🗞', '⚠', '🎉'}

// TestEmojiHaveAFaceInTheGetFaceChain is the regression this exists for: with no
// emoji family in the chain, every one of these runes fell through to the
// primary's .notdef and drew a blank gap. Live @NASA tweets are full of them.
func TestEmojiHaveAFaceInTheGetFaceChain(t *testing.T) {
	tf := getFace(16, false)
	for _, r := range feedEmoji {
		f := tf.faceFor(r)
		adv, ok := f.GlyphAdvance(r)
		if !ok {
			t.Errorf("U+%04X %q: no face in the chain covers it", r, r)
			continue
		}
		if adv <= 0 {
			t.Errorf("U+%04X %q: advance = %v, want > 0", r, r, adv)
		}
		// The chosen face must NOT be the primary: the primary is the Latin Go
		// face, and it reporting coverage would mean we measured .notdef.
		if f == tf.face {
			t.Errorf("U+%04X %q resolved to the primary Latin face", r, r)
		}
	}
}

// TestEmojiAreMeasuredAndDrawn checks the whole text path, not just coverage:
// an emoji string measures wider than nothing and actually puts ink on the
// surface.
func TestEmojiAreMeasuredAndDrawn(t *testing.T) {
	tf := getFace(24, false)
	const s = "go 🚀 now"

	if got := tf.width(s); got <= tf.width("go  now") {
		t.Fatalf("width(%q) = %d, should exceed the same string without the emoji", s, got)
	}

	img := image.NewRGBA(image.Rect(0, 0, 200, 48))
	tf.draw(img, 4, 4, "🚀", toolkit.RGBA{R: 0, G: 0, B: 0, A: 0xFF})
	inked := 0
	for i := 3; i < len(img.Pix); i += 4 {
		if img.Pix[i] > 0 {
			inked++
		}
	}
	if inked == 0 {
		t.Fatal("drawing 🚀 left the surface blank")
	}
}

// TestEmojiRenderInTheToolkitChain covers the OTHER font path — the toolkit
// fallback font that stock widgets (Label, Button, the feed card) render with.
// Both paths derive from scriptFallbackTTFs, and both must carry emoji.
func TestEmojiRenderInTheToolkitChain(t *testing.T) {
	f := ttFont(false, 20)
	for _, r := range feedEmoji {
		if w := f.Measure(string(r)); w <= 0 {
			t.Errorf("U+%04X %q measures %d in the toolkit chain, want > 0", r, r, w)
		}
	}
	// An emoji genuinely widens a string rather than measuring as nothing.
	if f.Measure("ab🚀") <= f.Measure("ab") {
		t.Fatal("the toolkit chain measures 🚀 as zero-width")
	}
}

// TestFormatCharactersAreNeverRasterised covers the defect that only becomes
// visible once emoji render: an emoji sequence is held together by invisible
// controls (🧑‍🚀 is person + ZWJ + rocket), and nothing in the chain has a glyph
// for some of them, so routing fell through to the primary's .notdef and painted
// a box mid-word. U+2060 WORD JOINER did exactly that.
func TestFormatCharactersAreNeverRasterised(t *testing.T) {
	tf := getFace(16, false)
	// Every one of these is category Cf: an invisible control, never a glyph.
	for _, r := range []rune{'\u200D', '\uFE0F', '\uFE0E', '\u200B', '\u2060', '\u00AD', '\uFEFF'} {
		if !isFormatRune(r) {
			t.Errorf("U+%04X should be recognised as a format character", r)
		}
		if got := tf.width(string(r)); got != 0 {
			t.Errorf("U+%04X measures %d px, want 0 — it must not rasterise", r, got)
		}
	}
	// A ZWJ sequence measures exactly as its visible components do.
	if joined, bare := tf.width("🧑‍🚀"), tf.width("🧑🚀"); joined != bare {
		t.Fatalf("ZWJ sequence measures %d, bare pair measures %d — the joiner took space", joined, bare)
	}
	// And it draws no extra ink.
	ink := func(s string) int {
		img := image.NewRGBA(image.Rect(0, 0, 240, 40))
		tf.draw(img, 2, 2, s, toolkit.RGBA{R: 0, G: 0, B: 0, A: 0xFF})
		n := 0
		for i := 3; i < len(img.Pix); i += 4 {
			if img.Pix[i] > 0 {
				n++
			}
		}
		return n
	}
	if withWJ, without := ink("ab⁠cd"), ink("abcd"); withWJ != without {
		t.Fatalf("a word joiner added ink: %d px vs %d px", withWJ, without)
	}
}

// TestStripFormatKeepsOrdinaryTextIdentical checks the fast path: text with no
// format characters is returned as-is, so ordinary rendering is untouched.
func TestStripFormatKeepsOrdinaryTextIdentical(t *testing.T) {
	const plain = "Total solar eclipses, 2026 — 中文 🚀"
	if got := stripFormat(plain); got != plain {
		t.Fatalf("stripFormat rewrote text that has nothing to strip: %q", got)
	}
	if got := stripFormat("a‍b️c"); got != "abc" {
		t.Fatalf("stripFormat = %q, want %q", got, "abc")
	}
	if got := stripFormat(""); got != "" {
		t.Fatalf("stripFormat(empty) = %q", got)
	}
}

// TestEmojiFaceIsLastAndNeverShadowsText is the invariant that keeps the emoji
// family safe to chain: it maps "#", "*" and the digits (the keycap-sequence
// bases), so were it ordered before a text face, ordinary digits would start
// rendering as emoji glyphs. Routing picks the FIRST covering face, so the
// primary must still win for all of them.
func TestEmojiFaceIsLastAndNeverShadowsText(t *testing.T) {
	tf := getFace(16, false)
	for _, r := range []rune{'0', '5', '9', '#', '*', ' ', '©', '®', 'A', 'z'} {
		if f := tf.faceFor(r); f != tf.face {
			t.Errorf("U+%04X %q resolved to a fallback face; the primary must serve it", r, r)
		}
	}
	// And a pure-Latin string still takes the kerned fast path.
	if !tf.primaryCovers("Total solar eclipses, 2026.") {
		t.Fatal("a plain Latin string should be fully covered by the primary")
	}
}
