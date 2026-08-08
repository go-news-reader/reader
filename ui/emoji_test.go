package ui

import (
	"image"
	"testing"

	"github.com/go-widgets/toolkit"
	"golang.org/x/image/font/gofont/goregular"
)

// feedEmoji is a spread of the pictographs a real feed actually carries — moon
// phases, a rocket, gestures, a person, and two BMP dingbats.
var feedEmoji = []rune{'\U0001F680', '\U0001F314', '\U0001F318', '\U0001F9D1', '\U0001F44F',
	'\U0001F44D', '\u2728', '\U0001F525', '\U0001F5DE', '\u26A0', '\U0001F389'}

// inkOf counts the pixels a string paints on a blank surface.
func inkOf(t *testing.T, tf textFace, s string) int {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 600, 90))
	tf.draw(img, 4, 4, s, toolkit.RGBA{R: 0, G: 0, B: 0, A: 0xFF})
	n := 0
	for i := 3; i < len(img.Pix); i += 4 {
		if img.Pix[i] > 0 {
			n++
		}
	}
	return n
}

// TestEmojiRender is the regression this exists for: with no emoji family in the
// chain every one of these runes drew a blank gap. Live @NASA tweets are full of
// them.
func TestEmojiRender(t *testing.T) {
	tf := getFace(24, false)
	for _, r := range feedEmoji {
		s := string(r)
		if w := tf.width(s); w <= 0 {
			t.Errorf("U+%04X %q measures %d, want > 0", r, r, w)
			continue
		}
		if inkOf(t, tf, s) == 0 {
			t.Errorf("U+%04X %q painted nothing", r, r)
		}
	}
	// An emoji genuinely widens a string rather than measuring as nothing.
	if tf.width("go \U0001F680 now") <= tf.width("go  now") {
		t.Fatal("the emoji added no width")
	}
}

// TestEmojiRenderInTheToolkitChain covers the same font as widgets see it: the
// reader hands ttFont to stock Labels, and textFace wraps that very font, so the
// two can never disagree — this pins that they don't.
func TestEmojiRenderInTheToolkitChain(t *testing.T) {
	f := ttFont(false, 20)
	tf := getFace(20, false)
	for _, r := range feedEmoji {
		if got, want := f.Measure(string(r)), tf.width(string(r)); got != want {
			t.Fatalf("U+%04X: widget font measures %d, textFace %d", r, got, want)
		}
	}
}

// TestZWJSequencesComposeToOneGlyph is the payoff of routing whole graphemes to
// one face: the sequence reaches the shaper intact, so the GSUB ligature fires
// and 🧑‍🚀 is ONE astronaut rather than a person standing next to a rocket.
func TestZWJSequencesComposeToOneGlyph(t *testing.T) {
	tf := getFace(32, false)
	const (
		person = "\U0001F9D1"
		rocket = "\U0001F680"
		zwj    = "\u200D"
	)
	one := tf.width(person)
	if one <= 0 {
		t.Fatal("the emoji face measured nothing")
	}
	if got := tf.width(person + zwj + rocket); got != one {
		t.Fatalf("astronaut measures %d, want one glyph's %d (un-composed would be %d)",
			got, one, tf.width(person+rocket))
	}
	family := "\U0001F468" + zwj + "\U0001F469" + zwj + "\U0001F466"
	if got := tf.width(family); got != one {
		t.Fatalf("family sequence measures %d, want one glyph's %d", got, one)
	}
	// Embedded in text, it still costs one glyph of line width.
	if tf.width("a"+person+zwj+rocket+"b") != tf.width("a"+person+"b") {
		t.Fatal("a sequence embedded in text did not compose")
	}
}

// TestDefaultIgnorablesLeaveNoMark checks the invisible controls an emoji
// sequence carries by construction cost neither width nor a pixel — the shaper
// hides the ones it did not consume, and the toolkit skips them.
func TestDefaultIgnorablesLeaveNoMark(t *testing.T) {
	tf := getFace(20, false)
	baseW := tf.width("abcd")
	baseInk := inkOf(t, tf, "abcd")
	for _, r := range []rune{'\u200D', '\uFE0F', '\uFE0E', '\u200B', '\u2060', '\u00AD', '\uFEFF'} {
		s := "ab" + string(r) + "cd"
		if got := tf.width(s); got != baseW {
			t.Errorf("U+%04X: width %d, want %d — an invisible control must take no space", r, got, baseW)
		}
		if got := inkOf(t, tf, s); got != baseInk {
			t.Errorf("U+%04X: painted %d px, want the plain word's %d", r, got, baseInk)
		}
	}
}

// TestEmojiFaceNeverShadowsText is the invariant that keeps the emoji family
// safe to chain last: it maps "#", "*" and the digits (the keycap-sequence
// bases), so were it ordered before a text face, ordinary digits would start
// rendering as emoji glyphs. Measuring against the bare Latin primary proves the
// chain never reaches the emoji face for them.
func TestEmojiFaceNeverShadowsText(t *testing.T) {
	latinOnly, err := toolkit.NewTrueTypeFont(goregular.TTF, 16)
	if err != nil {
		t.Fatal(err)
	}
	tf := getFace(16, false)
	for _, s := range []string{"0123456789", "#", "*", " ", "\u00A9", "\u00AE", "Az"} {
		if got, want := tf.width(s), latinOnly.Measure(s); got != want {
			t.Errorf("%q measures %d through the chain, %d through the Latin face alone — "+
				"a later face claimed it", s, got, want)
		}
	}
}
