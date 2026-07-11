package ui

import (
	"image"
	"testing"

	"github.com/go-widgets/toolkit"
	"golang.org/x/image/font/gofont/goregular"
)

// withSysFont installs a system font for the duration of a test and restores the
// embedded Go fonts (clearing the face cache) afterwards, so the other tests in
// the package keep rendering with the deterministic bundled typeface.
func withSysFont(t *testing.T, ttf []byte) {
	t.Helper()
	faceMu.Lock()
	old := sysFontSrc
	faceMu.Unlock()
	if !SetSystemFont(ttf) {
		t.Fatal("SetSystemFont returned false for a valid font")
	}
	t.Cleanup(func() {
		faceMu.Lock()
		sysFontSrc = old
		faceCache = map[faceKey]textFace{}
		faceMu.Unlock()
	})
}

func TestSetSystemFontInvalid(t *testing.T) {
	if SetSystemFont([]byte("not a font")) {
		t.Fatal("expected false for unparseable bytes")
	}
	faceMu.Lock()
	got := sysFontSrc
	faceMu.Unlock()
	if got != nil {
		t.Fatal("a failed parse must leave the embedded fonts in place")
	}
}

func TestSystemFontFacesAndSynthBold(t *testing.T) {
	withSysFont(t, goregular.TTF)
	if reg := getFace(15, false); reg.synthBold {
		t.Fatal("regular face must not synthesise bold")
	}
	bold := getFace(15, true)
	if !bold.synthBold {
		t.Fatal("bold from a single-instance system font must synthesise bold")
	}
	// The synth-bold over-strike pass must actually render.
	img := image.NewRGBA(image.Rect(0, 0, 120, 24))
	bold.draw(img, 2, 2, "Bold", toolkit.RGBA{R: 0xFF, A: 0xFF})
	lit := 0
	for i := 0; i < len(img.Pix); i += 4 {
		if img.Pix[i] != 0 {
			lit++
		}
	}
	if lit == 0 {
		t.Fatal("synth-bold draw produced no pixels")
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
