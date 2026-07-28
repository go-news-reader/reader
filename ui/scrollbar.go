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

// drawVScrollbar draws a vertical toolkit.Scrollbar at the right edge of area
// when total content exceeds the area's height (else nothing — a bar that always
// fills would be noise). offset is the current scroll position.
func (s *Scene) drawVScrollbar(p *painter.PixelPainter, area toolkit.Rect, total, offset int) {
	view := area.H
	if view <= 0 || total <= view {
		return
	}
	w := s.scrollbarW()
	inset := rpxOf(s, 2)
	bar := toolkit.Rect{X: area.X + area.W - w - inset, Y: area.Y + inset, W: w, H: area.H - 2*inset}
	sb := &toolkit.Scrollbar{Total: total, Viewport: view, Offset: offset}
	sb.SetBounds(bar)
	// Draw the track + thumb ourselves (via the widget's ThumbRect geometry) so the
	// thumb uses a clearly-visible muted grey — the toolkit's default Border thumb
	// is too faint on the sidebar, whose background is already SurfaceAlt.
	th := s.theme
	radius := w / 2
	p.FillRoundRect(painter.Rect(bar), radius, th.SurfaceAlt)
	t := sb.ThumbRect()
	p.FillRoundRect(painter.Rect(t), radius, mute(th.OnSurface, th.SurfaceAlt))
}
