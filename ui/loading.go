package ui

// Live-loading feedback drawn with go-widgets/toolkit widgets instead of
// hand-drawn primitives:
//
//   - the empty-feed placeholder is a "Loading…" label above an animated
//     toolkit.Spinner, so a slow first fetch never looks like a broken
//     empty state;
//   - the slim top-of-feed progress strip is a "Loading N/M sources…"
//     label with a small trailing toolkit.Spinner above a determinate
//     toolkit.ProgressBar (done/total), so the user sees more is coming
//     without losing what has already landed.
//
// Every Spinner's hand rotates off the scene animation frame (see
// spinnerPhase); the present loop advances that frame once per tick (see
// Scene.AdvanceAnim) ONLY while loading, so an idle scene never animates.

import (
	"fmt"
	"image"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
)

// spinnerPeriod is how many animation frames one full spinner revolution takes.
const spinnerPeriod = 60

// spinnerPhase maps the scene's animation frame to a [0,1) spinner phase, so
// the toolkit.Spinner hand advances one revolution every spinnerPeriod frames.
func (s *Scene) spinnerPhase() float64 {
	p := s.animFrame % spinnerPeriod
	if p < 0 {
		p += spinnerPeriod
	}
	return float64(p) / float64(spinnerPeriod)
}

// spinnerAt builds an Active toolkit.Spinner phased to the current animation
// frame, bounded to r. Shared by the empty-feed placeholder, the strip and the
// sidebar pending marker so every indicator spins in lock-step.
func (s *Scene) spinnerAt(r toolkit.Rect) *toolkit.Spinner {
	sp := &toolkit.Spinner{Active: true, Phase: s.spinnerPhase()}
	sp.SetBounds(r)
	return sp
}

// drawLoadingPlaceholder paints the centred empty-feed indicator inside the feed
// viewport [feedX, feedX+feedW) starting below the topbar. It is drawn in screen
// coordinates (it does not scroll) so it stays put while the first items arrive.
func (s *Scene) drawLoadingPlaceholder(p *painter.PixelPainter, img *image.RGBA, feedX, feedW int, muteS toolkit.RGBA) {
	m := s.m
	msg := "Loading…"
	cx := feedX + (feedW-m.title.width(msg))/2
	cy := s.H/2 - m.title.height
	m.title.draw(img, cx, cy, msg, muteS)

	// Animated spinner centred below the label.
	d := rpxOf(s, 52)
	r := toolkit.Rect{X: feedX + (feedW-d)/2, Y: cy + m.title.height + m.pad, W: d, H: d}
	s.spinnerAt(r).Draw(p, s.theme)
}

// drawLoadStrip paints the slim progress strip at the top of the feed content
// (scrolls with it): a "Loading N/M sources…" label with a small trailing
// spinner above a determinate ProgressBar (done/total). x/y are the strip's
// top-left in screen coordinates; w its width.
func (s *Scene) drawLoadStrip(p *painter.PixelPainter, img *image.RGBA, x, y, w int, muteS toolkit.RGBA) {
	m := s.m
	lbl := fmt.Sprintf("Loading %d/%d sources…", s.loadDone, s.loadTotal)
	m.side.draw(img, x, y, lbl, muteS)

	// Small spinner at the label row's trailing edge keeps the strip visibly
	// alive while sources stream in.
	sd := m.side.height
	s.spinnerAt(toolkit.Rect{X: x + w - sd, Y: y, W: sd, H: sd}).Draw(p, s.theme)

	// Determinate ProgressBar (completed/total sources) below the label.
	pb := toolkit.NewProgressBar()
	if s.loadTotal > 0 {
		pb.SetFraction(float64(s.loadDone) / float64(s.loadTotal))
	}
	trackH := rpxOf(s, 6)
	ty := y + m.side.height + rpxOf(s, 4)
	pb.SetBounds(toolkit.Rect{X: x, Y: ty, W: w, H: trackH})
	pb.Draw(p, s.theme)
}
