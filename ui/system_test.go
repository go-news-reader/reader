package ui

import (
	"image"
	"testing"

	"github.com/go-opentype/fonts/goregular"
	"github.com/go-widgets/toolkit"
)

// withSysFont installs a system font for the duration of a test and restores the
// embedded Go fonts (clearing the font cache) afterwards, so the other tests in
// the package keep rendering with the deterministic bundled typeface.
func withSysFont(t *testing.T, ttf []byte) {
	t.Helper()
	ttMu.Lock()
	old := sysFontTTF
	ttMu.Unlock()
	if !SetSystemFont(ttf) {
		t.Fatal("SetSystemFont returned false for a valid font")
	}
	t.Cleanup(func() {
		ttMu.Lock()
		sysFontTTF = old
		ttCache = map[faceKey]toolkit.Font{}
		ttMu.Unlock()
	})
}

func TestSetSystemFontInvalid(t *testing.T) {
	if SetSystemFont([]byte("not a font")) {
		t.Fatal("expected false for unparseable bytes")
	}
	ttMu.Lock()
	got := sysFontTTF
	ttMu.Unlock()
	if got != nil {
		t.Fatal("a failed parse must leave the embedded fonts in place")
	}
}

// TestSystemFontSynthesisesBold checks the host-typeface path. A system font is
// one variable font for every weight and the rasteriser reaches only its Regular
// master, so "bold" has to be over-struck — otherwise every heading would draw
// at body weight. goregular stands in for SFNS here: what matters is that the
// SAME face is asked for both weights.
func TestSystemFontSynthesisesBold(t *testing.T) {
	withSysFont(t, goregular.TTF)
	const s = "Bold"
	reg, bold := getFace(15, false), getFace(15, true)

	// The over-strike widens the run by exactly the extra column it paints.
	if bold.width(s) != reg.width(s)+1 {
		t.Fatalf("bold width %d, regular %d — want one over-strike column more",
			bold.width(s), reg.width(s))
	}
	if bold.height != reg.height {
		t.Fatal("over-striking must not change the line height")
	}

	ink := func(tf textFace) int {
		img := image.NewRGBA(image.Rect(0, 0, 160, 30))
		tf.draw(img, 2, 2, s, toolkit.RGBA{R: 0xFF, A: 0xFF})
		n := 0
		for i := 0; i < len(img.Pix); i += 4 {
			if img.Pix[i] != 0 {
				n++
			}
		}
		return n
	}
	regInk, boldInk := ink(reg), ink(bold)
	if regInk == 0 {
		t.Fatal("the system face drew nothing")
	}
	if boldInk <= regInk {
		t.Fatalf("synth bold painted %d px, regular %d — it must be heavier", boldInk, regInk)
	}
}

// TestSystemFontIsActuallyUsed checks the installed typeface really replaces the
// embedded primary, rather than being accepted and ignored.
func TestSystemFontIsActuallyUsed(t *testing.T) {
	before := getFace(15, true).width("Heading")
	withSysFont(t, goregular.TTF)
	// The embedded bold face and goregular-as-system differ in weight, so the
	// same string cannot measure the same through both.
	if after := getFace(15, true).width("Heading"); after == before {
		t.Fatal("installing a system font did not change what the bold face renders")
	}
}

func TestWithAccent(t *testing.T) {
	// A light accent takes black label text.
	lt := WithAccent(&toolkit.Theme{}, 0xFF, 0xFF, 0x00)
	if lt.Accent != (toolkit.RGBA{R: 0xFF, G: 0xFF, A: 0xFF}) {
		t.Fatalf("accent = %+v", lt.Accent)
	}
	if on := lt.Extra["OnAccent"]; on != (toolkit.RGBA{A: 0xFF}) {
		t.Fatalf("light-accent label = %+v, want black", on)
	}
	// A dark accent takes white label text (and threads through an existing Extra).
	dk := WithAccent(&toolkit.Theme{Extra: map[string]toolkit.RGBA{}}, 0x0D, 0x1B, 0x2A)
	if on := dk.Extra["OnAccent"]; on != (toolkit.RGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}) {
		t.Fatalf("dark-accent label = %+v, want white", on)
	}
}
