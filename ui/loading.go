package ui

// Live-loading feedback drawn straight with the painter (no arc primitive):
//
//   - a centred placeholder ("Loading…" plus an indeterminate bar) when the feed
//     is still empty, so a slow first fetch never looks like a broken empty state;
//   - a slim top-of-feed progress strip ("Loading N/M sources…" plus a
//     determinate fill and a moving highlight) once some sources have already
//     landed, so the user sees more is coming without losing what is there.
//
// Both indicators share drawSweep, a highlight segment whose position is a
// triangle-wave of the scene's animation frame, so the bar visibly moves while
// loading and is pixel-identical to itself only every full period. The present
// loop advances the frame once per tick (see Scene.AdvanceAnim) only while
// loading, so an idle scene never animates.

import (
	"fmt"
	"image"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
)

// sweepPeriod is how many animation frames one full there-and-back sweep of the
// indeterminate highlight takes.
const sweepPeriod = 60

// triWave maps a frame counter to a [0,1] triangle wave (0 at phase 0, 1 at the
// half period, back to 0), so the highlight bounces inside its track edge-to-edge.
func triWave(frame int) float64 {
	p := frame % sweepPeriod
	if p < 0 {
		p += sweepPeriod
	}
	half := sweepPeriod / 2
	if p <= half {
		return float64(p) / float64(half)
	}
	return float64(sweepPeriod-p) / float64(half)
}

// drawSweep paints a rounded track and a highlight segment (a third of the track
// wide) whose left edge sweeps between the track ends as the animation frame
// advances. base fills the whole track; hi paints the moving segment.
func (s *Scene) drawSweep(p *painter.PixelPainter, track toolkit.Rect, base, hi toolkit.RGBA) {
	rad := track.H / 2
	p.FillRoundRect(painter.Rect(track), rad, base)
	segW := track.W / 3
	if segW < 2*rad {
		segW = 2 * rad
	}
	if segW > track.W {
		segW = track.W
	}
	off := int(float64(track.W-segW)*triWave(s.animFrame) + 0.5)
	p.FillRoundRect(painter.Rect{X: track.X + off, Y: track.Y, W: segW, H: track.H}, rad, hi)
}

// drawLoadingPlaceholder paints the centred empty-feed indicator inside the feed
// viewport [feedX, feedX+feedW) starting below the topbar. It is drawn in screen
// coordinates (it does not scroll) so it stays put while the first items arrive.
func (s *Scene) drawLoadingPlaceholder(p *painter.PixelPainter, img *image.RGBA, feedX, feedW int, muteS toolkit.RGBA) {
	m := s.m
	th := s.theme
	msg := "Loading…"
	cx := feedX + (feedW-m.title.width(msg))/2
	cy := s.H/2 - m.title.height
	m.title.draw(img, cx, cy, msg, muteS)

	trackW := feedW / 3
	if trackW < rpxOf(s, 80) {
		trackW = rpxOf(s, 80)
	}
	if trackW > feedW-2*m.pad {
		trackW = feedW - 2*m.pad
	}
	trackH := rpxOf(s, 6)
	tr := toolkit.Rect{X: feedX + (feedW-trackW)/2, Y: cy + m.title.height + m.pad, W: trackW, H: trackH}
	s.drawSweep(p, tr, mute(th.Border, th.Background), th.Accent)
}

// drawLoadStrip paints the slim progress strip at the top of the feed content
// (scrolls with it): a "Loading N/M sources…" label above a determinate fill
// (done/total) overlaid with the moving highlight. x/y are the strip's top-left
// in screen coordinates; w its width.
func (s *Scene) drawLoadStrip(p *painter.PixelPainter, img *image.RGBA, x, y, w int, muteS toolkit.RGBA) {
	m := s.m
	th := s.theme
	lbl := fmt.Sprintf("Loading %d/%d sources…", s.loadDone, s.loadTotal)
	m.side.draw(img, x, y, lbl, muteS)

	trackH := rpxOf(s, 5)
	ty := y + m.side.height + rpxOf(s, 4)
	track := toolkit.Rect{X: x, Y: ty, W: w, H: trackH}
	rad := trackH / 2
	p.FillRoundRect(painter.Rect(track), rad, mute(th.Border, th.Background))
	// Determinate fill proportional to completed sources.
	if s.loadTotal > 0 {
		fw := w * s.loadDone / s.loadTotal
		if fw > 0 {
			p.FillRoundRect(painter.Rect{X: x, Y: ty, W: fw, H: trackH}, rad, mute(th.Accent, th.Background))
		}
	}
	// Moving highlight rides on top so the strip is visibly animated too.
	segW := w / 4
	if segW < 2*rad {
		segW = 2 * rad
	}
	off := int(float64(w-segW)*triWave(s.animFrame) + 0.5)
	p.FillRoundRect(painter.Rect{X: x + off, Y: ty, W: segW, H: trackH}, rad, th.Accent)
}
