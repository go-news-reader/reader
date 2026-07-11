package ui

import (
	"image"
	"image/color"
	"sync"

	"github.com/go-widgets/toolkit"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// This file gives the reader real, anti-aliased text. The go-widgets toolkit
// ships a single 5×7 bitmap font that looks blocky when the canvas is scaled
// (zoom or a Retina display); rendering the UI at device resolution with a
// hinted TrueType face removes that aliasing. We rasterise the embedded Go
// fonts (pure Go, CGO=0) directly into the same RGBA buffer the go-widgets
// painter draws shapes into, so text and chrome compose in one surface.

var (
	regularSrc *opentype.Font
	boldSrc    *opentype.Font
	fontOnce   sync.Once

	faceMu     sync.Mutex
	faceCache  = map[faceKey]textFace{}
	sysFontSrc *opentype.Font // host system font; overrides the embedded faces when set
)

// SetSystemFont installs a host UI typeface (e.g. macOS SF from
// /System/Library/Fonts/SFNS.ttf) so the reader matches the native look. SF is
// shipped as a variable font whose only instance the pure-Go rasteriser can
// reach is Regular, so bold weights are synthesised (see textFace.synthBold).
// It returns false and keeps the embedded Go fonts if ttf can't be parsed. The
// face cache is dropped so already-built faces re-derive from the new source.
func SetSystemFont(ttf []byte) bool {
	f, err := opentype.Parse(ttf)
	if err != nil {
		return false
	}
	faceMu.Lock()
	sysFontSrc = f
	faceCache = map[faceKey]textFace{}
	faceMu.Unlock()
	return true
}

type faceKey struct {
	px   int
	bold bool
}

func loadFonts() {
	fontOnce.Do(func() {
		regularSrc, _ = opentype.Parse(goregular.TTF)
		boldSrc, _ = opentype.Parse(gobold.TTF)
	})
}

// textFace bundles a font.Face with its cached vertical metrics so callers can
// position by the line's top-left corner rather than the baseline. synthBold is
// set for "bold" faces derived from a system font that exposes no bold instance;
// draw then over-strikes the glyphs one pixel across to fake the weight.
type textFace struct {
	face      font.Face
	ascent    int
	height    int
	synthBold bool
}

// getFace returns a cached face at px pixels (DPI 72 ⇒ 1pt = 1px), regular or
// bold. Faces are not safe for concurrent use; the scene renders on one
// goroutine.
func getFace(px int, bold bool) textFace {
	if px < 1 {
		px = 1
	}
	loadFonts()
	faceMu.Lock()
	defer faceMu.Unlock()
	k := faceKey{px, bold}
	if f, ok := faceCache[k]; ok {
		return f
	}
	src, synth := regularSrc, false
	if bold {
		src = boldSrc
	}
	if sysFontSrc != nil {
		src = sysFontSrc // one variable font for every weight...
		synth = bold     // ...so fake bold by over-striking.
	}
	face, _ := opentype.NewFace(src, &opentype.FaceOptions{Size: float64(px), DPI: 72, Hinting: font.HintingFull})
	m := face.Metrics()
	tf := textFace{face: face, ascent: m.Ascent.Round(), height: m.Height.Round(), synthBold: synth}
	faceCache[k] = tf
	return tf
}

// width measures the rendered pixel width of s in this face.
func (tf textFace) width(s string) int { return font.MeasureString(tf.face, s).Round() }

// draw renders s with its top-left at (x, top) in col, into img (which must
// alias the scene's RGBA buffer).
func (tf textFace) draw(img *image.RGBA, x, top int, s string, col toolkit.RGBA) {
	d := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(color.RGBA{R: col.R, G: col.G, B: col.B, A: 0xFF}),
		Face: tf.face,
		Dot:  fixed.P(x, top+tf.ascent),
	}
	d.DrawString(s)
	if tf.synthBold {
		// Second pass one pixel right thickens every stem — a passable bold for
		// a font that only offers its Regular instance.
		d.Dot = fixed.P(x+1, top+tf.ascent)
		d.DrawString(s)
	}
}

// drawRight renders s right-aligned so its right edge sits at x.
func (tf textFace) drawRight(img *image.RGBA, x, top int, s string, col toolkit.RGBA) {
	tf.draw(img, x-tf.width(s), top, s, col)
}
