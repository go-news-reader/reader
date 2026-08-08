package ui

import (
	"image"
	"sync"

	"github.com/go-opentype/fonts/goregular"
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
	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
)

// This file gives the reader real, anti-aliased text. The go-widgets toolkit
// ships a single 5x7 bitmap font that looks blocky when the canvas is scaled
// (zoom or a Retina display); rendering the UI at device resolution with a
// hinted TrueType face removes that aliasing.
//
// There is exactly ONE text stack: every string the reader measures or paints
// goes through a go-widgets toolkit font, which shapes the run with
// github.com/go-opentype/shape. The reader used to carry a second, private
// rasteriser over x/image/font — a duplicate fallback chain that routed runes to
// faces itself and drew them one at a time. That path applied no GSUB at all, so
// it measured and drew Arabic in unjoined isolated forms, left Indic clusters
// unreordered, and could not compose an emoji ZWJ sequence. textFace is now a
// thin adapter over the toolkit font, keeping the top-left-corner positioning
// its callers expect.

// faceKey identifies a cached font by its pixel size and weight.
type faceKey struct {
	px   int
	bold bool
}

// ttCache memoises the built font chains (keyed by size + weight): parsing ten
// faces, one of which is a 17 MB CJK Noto, is far too costly to redo per frame.
// sysFontTTF, when set, replaces the embedded Latin primary with a host UI
// typeface. Both are guarded by ttMu.
var (
	ttMu       sync.Mutex
	ttCache    = map[faceKey]toolkit.Font{}
	sysFontTTF []byte
)

// SetSystemFont installs a host UI typeface (e.g. macOS SF from
// /System/Library/Fonts/SFNS.ttf) so the reader matches the native look. SF is
// shipped as a variable font whose only instance the pure-Go rasteriser can
// reach is Regular, so bold weights are synthesised by over-striking (see
// toolkit.NewSyntheticBoldFont). It returns false and keeps the embedded Go
// fonts if ttf cannot be parsed. The font cache is dropped so already-built
// chains re-derive from the new primary.
func SetSystemFont(ttf []byte) bool {
	if _, err := toolkit.NewTrueTypeFont(ttf, 16); err != nil {
		return false
	}
	ttMu.Lock()
	defer ttMu.Unlock()
	sysFontTTF = ttf
	ttCache = map[faceKey]toolkit.Font{}
	return true
}

// scriptFallbackTTFs are the per-script Noto faces chained after the Latin UI
// font so the reader can display text in any of them — no single font covers
// every script, and Noto is split per script, so mixed text (e.g. a Chinese or
// Arabic title next to Latin) needs per-grapheme font routing. go-opentype
// parses each lazily (~1ms), and go:embed shares one copy of the bytes, so the
// chain is cheap in time and memory despite the large CJK face.
//
// Noto Emoji is LAST, and must stay last. Routing sends a rune to the FIRST face
// that covers it, so a face this late can only ever claim runes every text face
// declined. That matters here because the emoji family does map a handful of
// ASCII — "#", "*" and the digits, the bases of the keycap emoji sequences —
// which a text face must keep serving.
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

// ttFont returns the cached shaped font at px pixels, regular or bold: the UI
// primary (the host typeface when one is installed, else the embedded Go face)
// followed by the per-script Noto faces, chained through
// toolkit.NewFallbackFont so glyphs the primary lacks render from the face that
// has them. Assign it to a widget's Base.Font, or wrap it in a textFace to draw
// by the text's top-left corner.
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
	f := buildFont(bold, px)
	ttCache[k] = f
	return f
}

// buildFont assembles one chain. Callers must hold ttMu (it reads sysFontTTF).
func buildFont(bold bool, px int) toolkit.Font {
	primary, synth := uiPrimary(bold, px)
	fonts := []toolkit.Font{primary}
	for _, ttf := range scriptFallbackTTFs {
		if fb, err := toolkit.NewTrueTypeFont(ttf, px); err == nil {
			fonts = append(fonts, fb)
		}
	}
	// The primary always parses, so the chain always has a valid first font and
	// NewFallbackFont never errors here.
	f, _ := toolkit.NewFallbackFont(fonts...)
	if !synth {
		return f
	}
	// Embolden the WHOLE chain, not just the primary: a CJK or Arabic run inside
	// a heading would otherwise stay at body weight beside bold Latin.
	// NewSyntheticBoldFont rejects only a nil font, and f is never nil here.
	b, _ := toolkit.NewSyntheticBoldFont(f)
	return b
}

// uiPrimary returns the primary face and whether its bold must be synthesised.
// A host typeface is one variable font for every weight — the rasteriser reaches
// only its default (Regular) master — so bold is faked over it; the embedded Go
// family ships a real bold and needs no faking.
func uiPrimary(bold bool, px int) (f toolkit.Font, synthBold bool) {
	if sysFontTTF != nil {
		if p, err := toolkit.NewTrueTypeFont(sysFontTTF, px); err == nil {
			return p, bold
		}
	}
	src := goregular.TTF
	if bold {
		src = goregular.BoldTTF
	}
	p, _ := toolkit.NewTrueTypeFont(src, px)
	return p, false
}

// textFace adapts a toolkit font to the reader's drawing convention: callers
// position by the line's TOP-LEFT corner, and size their rows by the line
// height, so the height is cached alongside the font. The baseline is the
// font's own business — Draw derives it — so this carries no ascent.
type textFace struct {
	font   toolkit.Font
	height int
}

// getFace returns the face at px pixels, regular or bold. It is the same cached
// font ttFont hands to widgets, so a string measured here and drawn by a widget
// (or vice versa) can never disagree.
func getFace(px int, bold bool) textFace {
	f := ttFont(bold, px)
	return textFace{font: f, height: f.Height()}
}

// width is the rendered pixel width of s, shaped: ligatures, cursive joining and
// kerning are all reflected, so it matches what draw paints exactly.
func (tf textFace) width(s string) int { return tf.font.Measure(s) }

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
// alias the scene's RGBA buffer: a tightly packed, origin-zero surface, which is
// how every buffer in this package is built).
func (tf textFace) draw(img *image.RGBA, x, top int, s string, col toolkit.RGBA) {
	b := img.Bounds()
	tf.font.Draw(painter.NewPixelPainter(img.Pix, b.Dx(), b.Dy()), x, top, s, col)
}

// drawRight renders s right-aligned so its right edge sits at x.
func (tf textFace) drawRight(img *image.RGBA, x, top int, s string, col toolkit.RGBA) {
	tf.draw(img, x-tf.width(s), top, s, col)
}
