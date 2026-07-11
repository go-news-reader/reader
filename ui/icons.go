package ui

// Vector nav icons drawn straight with the painter instead of font glyphs.
//
// The embedded Go fonts (goregular/gobold) carry none of the UI symbol glyphs
// (☰ ⚙ 👤 📡 🔒 …), so drawing them as text produced empty "tofu" boxes. These
// helpers redraw the chrome icons as thin line-drawings in the Iconoir style: a
// 24-grid outline aesthetic, a uniform ~1.5px stroke scaled by the scene scale,
// rounded corners/caps. The painter offers only rectangles and rounded
// rectangles (no circle/line/arc), so every shape is built from bars and
// rounded rects — a StrokeRoundRect with radius ≈ min(w,h)/2 reads as a circular
// / pill outline.
//
// Each helper fills the supplied box r; the caller centres r where the glyph
// should sit and passes the stroke width from [Scene.iconStroke].

import (
	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
)

// iconStroke returns the Iconoir ~1.5px outline stroke width for the current
// scene scale (never below one device pixel).
func (s *Scene) iconStroke() int { return max(1, int(1.5*s.Scale+0.5)) }

// iconInset centres a square glyph box of side frac/100 of min(r.W, r.H) inside
// r, so every icon shares one visual weight and alignment within its cell.
func iconInset(r toolkit.Rect, frac int) toolkit.Rect {
	d := min(r.W, r.H) * frac / 100
	return toolkit.Rect{X: r.X + (r.W-d)/2, Y: r.Y + (r.H-d)/2, W: d, H: d}
}

// drawMenuIcon paints the burger/menu glyph: three evenly-spaced thin rounded
// horizontal bars (Iconoir "Menu").
func drawMenuIcon(p *painter.PixelPainter, r toolkit.Rect, col toolkit.RGBA, lineW int) {
	b := iconInset(r, 80)
	bar := func(y int) {
		p.FillRoundRect(toolkit.Rect{X: b.X, Y: y, W: b.W, H: lineW}, lineW/2, col)
	}
	bar(b.Y)                 // top
	bar(b.Y + (b.H-lineW)/2) // middle
	bar(b.Y + b.H - lineW)   // bottom
}

// drawLockIcon paints a padlock: an outlined body (lower ~60%) with a narrower
// outlined shackle loop above whose bottom edge meets the body's top edge.
func drawLockIcon(p *painter.PixelPainter, r toolkit.Rect, col toolkit.RGBA, lineW int) {
	b := iconInset(r, 78)
	bodyY := b.Y + 2*b.H/5
	body := toolkit.Rect{X: b.X, Y: bodyY, W: b.W, H: b.Y + b.H - bodyY}
	p.StrokeRoundRect(body, min(body.W, body.H)/4, col, lineW)
	shW := b.W * 3 / 5
	sh := toolkit.Rect{X: b.X + (b.W-shW)/2, Y: b.Y, W: shW, H: bodyY - b.Y + lineW}
	p.StrokeRoundRect(sh, shW/2, col, lineW)
}

// drawUserIcon paints an account/person glyph: an outlined circular head above a
// wide top-rounded outlined shoulders shape (Iconoir "User").
func drawUserIcon(p *painter.PixelPainter, r toolkit.Rect, col toolkit.RGBA, lineW int) {
	b := iconInset(r, 78)
	headD := b.W * 2 / 5
	head := toolkit.Rect{X: b.X + (b.W-headD)/2, Y: b.Y, W: headD, H: headD}
	p.StrokeRoundRect(head, headD/2, col, lineW)
	shW := b.W * 4 / 5
	shY := head.Y + headD + lineW
	sh := toolkit.Rect{X: b.X + (b.W-shW)/2, Y: shY, W: shW, H: b.Y + b.H - shY}
	p.StrokeRoundRect(sh, min(sh.W, sh.H)/2, col, lineW)
}

// drawSlidersIcon paints a settings/sliders glyph: three thin horizontal rules,
// each with a small outlined knob square at a staggered x (Iconoir "Settings").
func drawSlidersIcon(p *painter.PixelPainter, r toolkit.Rect, col toolkit.RGBA, lineW int) {
	b := iconInset(r, 80)
	knob := max(lineW*3, b.H/5)
	const rows = 3
	for i := 0; i < rows; i++ {
		y := b.Y + i*(b.H-lineW)/(rows-1)
		p.FillRoundRect(toolkit.Rect{X: b.X, Y: y, W: b.W, H: lineW}, lineW/2, col)
		kx := b.X + (b.W-knob)*(i+1)/(rows+1)
		p.StrokeRoundRect(toolkit.Rect{X: kx, Y: y + lineW/2 - knob/2, W: knob, H: knob}, knob/2, col, lineW)
	}
}

// drawListIcon paints a network-log/list glyph: three stacked thin rules, each
// preceded by a small bullet dot (Iconoir "List").
func drawListIcon(p *painter.PixelPainter, r toolkit.Rect, col toolkit.RGBA, lineW int) {
	b := iconInset(r, 80)
	dot := lineW * 2
	gap := dot + lineW*2
	const rows = 3
	for i := 0; i < rows; i++ {
		y := b.Y + i*(b.H-lineW)/(rows-1)
		p.FillRoundRect(toolkit.Rect{X: b.X, Y: y + lineW/2 - dot/2, W: dot, H: dot}, dot/2, col)
		p.FillRoundRect(toolkit.Rect{X: b.X + gap, Y: y, W: b.W - gap, H: lineW}, lineW/2, col)
	}
}
