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

// scrollbarNeeded reports whether a panel whose content is contentH tall needs a
// vertical scrollbar for a viewport of viewportH — the same test drawVScrollbar
// applies before it paints anything. Panels call it to decide whether to reserve
// the content gutter below.
func (s *Scene) scrollbarNeeded(contentH, viewportH int) bool {
	return viewportH > 0 && contentH > viewportH
}

// scrollClampRight lowers a panel's content right edge so it stops just before the
// vertical scrollbar when one is shown (else returns naturalRight unchanged).
// panelRight is where the bar is anchored (area.X+area.W) and gripX its grip, so
// the gutter is derived from the one placement rule scrollbarRightX defines. Every
// scrollable panel routes its content right edge through this — one definition of
// "where content stops relative to the bar" instead of per-panel incidental
// padding — so nothing paints under the bar and the gutter is identical everywhere.
func (s *Scene) scrollClampRight(naturalRight, panelRight, gripX int, shown bool) int {
	if !shown {
		return naturalRight
	}
	limit := s.scrollbarRightX(panelRight, gripX) - s.scrollbarW() - rpxOf(s, 6)
	if limit < naturalRight {
		return limit
	}
	return naturalRight
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
