package ui

// A shared scrollbar for every scrollable panel (the feed card list, the
// newsgroup tree, the detail/reading view, the network log, the preview pane).
// It is the go-widgets toolkit.Scrollbar widget — one reusable primitive drawn
// down the right edge of whichever view overflows its viewport, so the user can
// see where they are in content that can grow arbitrarily tall.

import (
	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
)

// scrollbarW is the on-screen width of the vertical scrollbars.
func (s *Scene) scrollbarW() int { return rpxOf(s, 6) }

// scrollGripGap is the single standard gap between a scrollbar and the resize
// grip on its panel's divider — the same everywhere, so the sidebar and the feed
// (and any future panel) line up identically instead of drifting.
func (s *Scene) scrollGripGap() int { return rpxOf(s, 8) }

// scrollbarRightX is where a panel scrollbar's right edge sits: a standard gap
// left of the resize grip centred at gripX when there is one (gripX>0), else
// flush (an inset) inside panelRight. One rule for every panel.
func (s *Scene) scrollbarRightX(panelRight, gripX int) int {
	if gripX > 0 {
		return gripX - s.scrollGripGap()
	}
	return panelRight - rpxOf(s, 2)
}

// drawVScrollbar draws a vertical toolkit.Scrollbar for a panel when its content
// overflows the viewport (else nothing). gripX is the x of the resize grip on the
// panel's right divider (0 = no grip); the bar is positioned by scrollbarRightX
// so every panel uses one standard placement.
func (s *Scene) drawVScrollbar(p *painter.PixelPainter, area toolkit.Rect, gripX, total, offset int) {
	view := area.H
	if view <= 0 || total <= view {
		return
	}
	w := s.scrollbarW()
	inset := rpxOf(s, 2)
	right := s.scrollbarRightX(area.X+area.W, gripX)
	sb := &toolkit.Scrollbar{Total: total, Viewport: view, Offset: offset}
	sb.SetBounds(toolkit.Rect{X: right - w, Y: area.Y + inset, W: w, H: area.H - 2*inset})
	sb.Draw(p, s.theme) // toolkit v0.55+ paints a clearly-visible muted-grey thumb
}
