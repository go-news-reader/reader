package app

import (
	"bytes"

	"github.com/go-widgets/toolkit"
)

// DamageRects reports which rectangles of the current frame differ from the one
// before it, in the framebuffer's pixel coordinates, so a damage-aware host
// (toolkit.Surface.Damage) presents only what changed instead of re-blitting and
// re-presenting the whole window. During a streaming load that is the difference
// between re-presenting a small spinner and re-presenting the entire feed on
// every animation tick — the reader's single biggest load-time cost.
//
// It is exact by construction: rather than enumerate which widgets animate (and
// risk freezing one it forgot), it diffs the two frame buffers the double-
// buffered Frame already keeps. Every changed pixel is inside a returned
// rectangle, so a host can never miss an update; the coalescing groups a
// contiguous band of changed rows into one rectangle, which for an in-place
// animation (a spinner, a caret, a progress bar) is a tight box.
//
// It must be called right after Frame, on the same (render) goroutine, so the
// "current" buffer is the frame Frame just produced. A size mismatch — the tick
// straddling a resize, when the buffers were reallocated — returns nil, which a
// host reads as "assume everything changed" and presents in full. Nothing
// changed returns no rectangles.
func (a *App) DamageRects() []toolkit.Rect {
	s := a.scene
	w, h := s.W, s.H
	cur, prev := a.bufs[a.cur], a.bufs[1-a.cur]
	return diffRects(cur, prev, w, h)
}

// diffRects returns the rectangles covering every pixel that differs between cur
// and prev — two w×h RGBA buffers — coalesced into one rectangle per contiguous
// band of changed rows (its x-span the union of the changed columns in the
// band). It returns nil when a buffer is unusable (a size mismatch or one too
// short for w*h*4), which the caller treats as whole-surface damage; an
// all-identical pair returns no rectangles.
func diffRects(cur, prev []byte, w, h int) []toolkit.Rect {
	stride := w * 4
	if w <= 0 || h <= 0 || len(cur) < stride*h || len(prev) < stride*h {
		return nil
	}
	var out []toolkit.Rect
	bandOpen := false
	var bandY0, bandMinX, bandMaxX int
	flush := func(yEnd int) {
		if bandOpen {
			out = append(out, toolkit.Rect{X: bandMinX, Y: bandY0, W: bandMaxX - bandMinX, H: yEnd - bandY0})
			bandOpen = false
		}
	}
	for y := 0; y < h; y++ {
		rc := cur[y*stride : y*stride+stride]
		rp := prev[y*stride : y*stride+stride]
		if bytes.Equal(rc, rp) {
			flush(y)
			continue
		}
		minX, maxX := 0, 0
		found := false
		for x := 0; x < w; x++ {
			o := x * 4
			if rc[o] != rp[o] || rc[o+1] != rp[o+1] || rc[o+2] != rp[o+2] || rc[o+3] != rp[o+3] {
				if !found {
					minX = x
					found = true
				}
				maxX = x
			}
		}
		if !bandOpen {
			bandOpen, bandY0, bandMinX, bandMaxX = true, y, minX, maxX+1
			continue
		}
		if minX < bandMinX {
			bandMinX = minX
		}
		if maxX+1 > bandMaxX {
			bandMaxX = maxX + 1
		}
	}
	flush(h)
	return out
}
