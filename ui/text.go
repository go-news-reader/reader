package ui

import (
	"image"
	"image/color"
	"strings"
	"sync"
	"unicode"

	"github.com/go-opentype/fonts/notoemoji"
	"github.com/go-opentype/fonts/notosansarabic"
	"github.com/go-opentype/fonts/notosansarmenian"
	"github.com/go-opentype/fonts/notosansdevanagari"
	"github.com/go-opentype/fonts/notosansgeorgian"
	"github.com/go-opentype/fonts/notosanshebrew"
	"github.com/go-opentype/fonts/notosansjp"
	"github.com/go-opentype/fonts/notosanskr"
	"github.com/go-opentype/fonts/notosanssc"
	"github.com/go-opentype/fonts/notosansthai"
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
	faces     []font.Face // primary first, then per-script fallbacks (nil ⇒ primary only)
	ascent    int
	height    int
	synthBold bool
}

// fallbackSrcs are the per-script Noto faces (parsed once) chained after the
// primary in the getFace path, mirroring ttFont's toolkit fallback so hand-drawn
// views render CJK/Arabic/Indic too. loadFallbacks parses them lazily.
var (
	fallbackOnce sync.Once
	fallbackSrcs []*opentype.Font
)

func loadFallbacks() {
	fallbackOnce.Do(func() {
		for _, ttf := range scriptFallbackTTFs {
			if f, err := opentype.Parse(ttf); err == nil {
				fallbackSrcs = append(fallbackSrcs, f)
			}
		}
	})
}

// isFormatRune reports whether r is an invisible control rather than a glyph:
// a Unicode format character (category Cf — the zero-width joiner and space, the
// word joiner, the soft hyphen, the BOM) or a variation selector (U+FE00..FE0F
// and the supplement, which are category Mn, NOT Cf, so the Cf test alone misses
// the VS16 every emoji presentation sequence ends with).
//
// Only the variation selectors are taken from Mn, never Mn at large: ordinary
// combining marks carry meaning and must render, or Devanagari, Arabic and
// Hebrew text would silently lose its diacritics.
func isFormatRune(r rune) bool {
	return unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Variation_Selector, r)
}

// stripFormat drops every format character from s.
//
// A renderer must never rasterise these, but nothing in the font chain has a
// glyph for some of them, so the per-rune routing fell through to the primary
// face's .notdef and painted a visible box mid-word — U+2060 WORD JOINER did
// exactly that next to an emoji. Emoji sequences carry these characters by
// construction (🧑‍🚀 is person + ZWJ + rocket), so displaying emoji at all means
// handling them.
//
// It returns s itself when there is nothing to strip, so ordinary text costs one
// scan and no allocation.
func stripFormat(s string) string {
	if !strings.ContainsFunc(s, isFormatRune) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if !isFormatRune(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// faceFor returns the first face in the chain that has a glyph for r, or the
// primary when none does (so an unknown rune lands on the primary's .notdef).
func (tf textFace) faceFor(r rune) font.Face {
	for _, f := range tf.faces {
		if _, ok := f.GlyphAdvance(r); ok {
			return f
		}
	}
	return tf.face
}

// primaryCovers reports whether the primary face has a glyph for every rune of
// s — the fast path that keeps pure-Latin measuring/drawing byte-identical.
func (tf textFace) primaryCovers(s string) bool {
	for _, r := range s {
		if _, ok := tf.face.GlyphAdvance(r); !ok {
			return false
		}
	}
	return true
}

// faceRun is a maximal slice of s rendered by one face.
type faceRun struct {
	text string
	face font.Face
}

// splitRuns splits s into maximal runs, each routed to the covering face.
func (tf textFace) splitRuns(s string) []faceRun {
	var out []faceRun
	var cur font.Face
	var b strings.Builder
	flush := func() {
		if b.Len() > 0 {
			out = append(out, faceRun{text: b.String(), face: cur})
			b.Reset()
		}
	}
	for _, r := range s {
		f := tf.faceFor(r)
		if f != cur {
			flush()
			cur = f
		}
		b.WriteRune(r)
	}
	flush()
	return out
}

// ttFonts caches go-widgets TrueType fonts (keyed by size+weight) so toolkit
// widgets can render their OWN anti-aliased text at the reader's sizes via the
// per-widget Base.Font, instead of the toolkit's built-in 5×7 bitmap font.
var (
	ttMu    sync.Mutex
	ttCache = map[faceKey]toolkit.Font{}
)

// scriptFallbackTTFs are the per-script Noto faces chained after the Latin UI
// font so the reader can display text in any of them — no single font covers
// every script, and Noto is split per script, so mixed text (e.g. a Chinese or
// Arabic title next to Latin) needs per-rune font routing. go-opentype parses
// each lazily (~1ms), and go:embed shares one copy of the bytes, so the chain is
// cheap in time and memory despite the large CJK face.
//
// Noto Emoji is LAST, and must stay last. Both fallback paths route a rune to
// the FIRST face that covers it, so a face this late can only ever claim runes
// every text face declined. That matters here because the emoji family does map
// a handful of ASCII — "#", "*" and the digits, the bases of the keycap emoji
// sequences — which a text face must keep serving.
var scriptFallbackTTFs = [][]byte{
	notosanssc.TTF,         // Chinese + Han ideographs + kana
	notosansjp.TTF,         // Japanese (kana + JP kanji forms)
	notosanskr.TTF,         // Korean (Hangul)
	notosansthai.TTF,       // Thai
	notosansarabic.TTF,     // Arabic
	notosansdevanagari.TTF, // Devanagari (Hindi, …)
	notosanshebrew.TTF,     // Hebrew
	notosansarmenian.TTF,   // Armenian
	notosansgeorgian.TTF,   // Georgian
	notoemoji.TTF,          // emoji + pictographs — keep LAST, see above
}

// ttFont returns a cached toolkit TrueType font at px pixels, regular or bold.
// The Latin UI face (embedded Go font) is the primary; the per-script Noto faces
// are chained after it via toolkit.NewFallbackFont so glyphs the primary lacks
// (CJK, Arabic, Thai, …) render from the face that has them. Assigned to a
// widget's Base.Font so the widget draws crisp AA text itself.
func ttFont(bold bool, px int) toolkit.Font {
	if px < 1 {
		px = 1
	}
	ttMu.Lock()
	defer ttMu.Unlock()
	k := faceKey{px, bold}
	if f, ok := ttCache[k]; ok {
		return f
	}
	src := goregular.TTF
	if bold {
		src = gobold.TTF
	}
	primary, _ := toolkit.NewTrueTypeFont(src, px)
	fonts := []toolkit.Font{primary}
	for _, ttf := range scriptFallbackTTFs {
		if fb, err := toolkit.NewTrueTypeFont(ttf, px); err == nil {
			fonts = append(fonts, fb)
		}
	}
	// The primary (an embedded Go face) always parses, so the chain always has a
	// valid first font and NewFallbackFont never errors here.
	f, _ := toolkit.NewFallbackFont(fonts...)
	ttCache[k] = f
	return f
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
	loadFallbacks()
	faces := []font.Face{face}
	for _, fsrc := range fallbackSrcs {
		if ff, err := opentype.NewFace(fsrc, &opentype.FaceOptions{Size: float64(px), DPI: 72, Hinting: font.HintingFull}); err == nil {
			faces = append(faces, ff)
		}
	}
	tf := textFace{face: face, faces: faces, ascent: m.Ascent.Round(), height: m.Height.Round(), synthBold: synth}
	faceCache[k] = tf
	return tf
}

// width measures the rendered pixel width of s. Pure-primary strings (the common
// case) use font.MeasureString on the primary face verbatim (kerning intact);
// only when a rune falls outside the primary does it sum per-rune advances across
// the fallback chain.
func (tf textFace) width(s string) int {
	s = stripFormat(s)
	if len(tf.faces) <= 1 || tf.primaryCovers(s) {
		return font.MeasureString(tf.face, s).Round()
	}
	total := fixed.Int26_6(0)
	for _, r := range s {
		f := tf.faceFor(r)
		adv, ok := f.GlyphAdvance(r)
		if !ok {
			adv, _ = tf.face.GlyphAdvance(r)
		}
		total += adv
	}
	return total.Round()
}

// clipRight returns the longest trailing run of s (by whole runes) whose
// rendered width does not exceed w. A text field uses it to show the END of an
// over-long value — where the caret sits — so the value scrolls left inside the
// box instead of spilling over the neighbouring controls.
func (tf textFace) clipRight(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if tf.width(s) <= w {
		return s
	}
	rs := []rune(s)
	for i := 1; i < len(rs); i++ {
		if tf.width(string(rs[i:])) <= w {
			return string(rs[i:])
		}
	}
	return ""
}

// draw renders s with its top-left at (x, top) in col, into img (which must
// alias the scene's RGBA buffer).
func (tf textFace) draw(img *image.RGBA, x, top int, s string, col toolkit.RGBA) {
	src := image.NewUniform(color.RGBA{R: col.R, G: col.G, B: col.B, A: 0xFF})
	s = stripFormat(s)
	// Fast path: the primary face covers every rune (all Latin UI text), drawn in
	// one pass exactly as before.
	if len(tf.faces) <= 1 || tf.primaryCovers(s) {
		d := &font.Drawer{Dst: img, Src: src, Face: tf.face, Dot: fixed.P(x, top+tf.ascent)}
		d.DrawString(s)
		if tf.synthBold {
			// Second pass one pixel right thickens every stem — a passable bold for
			// a font that only offers its Regular instance.
			d.Dot = fixed.P(x+1, top+tf.ascent)
			d.DrawString(s)
		}
		return
	}
	// Mixed-script: draw each run with the face that covers it, advancing the pen.
	dot := fixed.P(x, top+tf.ascent)
	for _, run := range tf.splitRuns(s) {
		d := &font.Drawer{Dst: img, Src: src, Face: run.face, Dot: dot}
		d.DrawString(run.text)
		end := d.Dot
		if tf.synthBold {
			b := &font.Drawer{Dst: img, Src: src, Face: run.face, Dot: fixed.Point26_6{X: dot.X + fixed.I(1), Y: dot.Y}}
			b.DrawString(run.text)
		}
		dot = end
	}
}

// drawRight renders s right-aligned so its right edge sits at x.
func (tf textFace) drawRight(img *image.RGBA, x, top int, s string, col toolkit.RGBA) {
	tf.draw(img, x-tf.width(s), top, s, col)
}
