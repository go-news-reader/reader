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
func (s *Scene) spinnerAt(r toolkit.Rect, style toolkit.SpinnerStyle) *toolkit.Spinner {
	sp := &toolkit.Spinner{Active: true, Phase: s.spinnerPhase(), Style: style}
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
	s.spinnerAt(r, toolkit.SpinnerRing).Draw(p, s.theme)
}
