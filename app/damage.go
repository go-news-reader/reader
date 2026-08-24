package app

import "github.com/go-widgets/toolkit"

// DamageRects reports which rectangles of the current frame differ from the one
// before it, in the framebuffer's pixel coordinates, so a damage-aware host
// (toolkit.Surface.Damage) presents only what changed instead of re-blitting and
// re-presenting the whole window. During a streaming load that is the difference
// between re-presenting a small spinner and re-presenting the entire feed on
// every animation tick — the reader's single biggest load-time cost.
//
// It is exact by construction: rather than enumerate which widgets animate (and
// risk freezing one it forgot), it diffs the two frame buffers the double-
// buffered Frame already keeps, via toolkit.DiffRects. Every changed pixel is
// inside a returned rectangle, so a host can never miss an update.
//
// It must be called right after Frame, on the same (render) goroutine, so the
// "current" buffer is the frame Frame just produced. A size mismatch — the tick
// straddling a resize, when the buffers were reallocated — returns nil, which a
// host reads as "assume everything changed" and presents in full. Nothing
// changed returns no rectangles.
func (a *App) DamageRects() []toolkit.Rect {
	// Only when the last drawn frame changed nothing but the animation is a diff
	// worth its cost. On a content or input frame most of the surface changed
	// anyway, so scanning the whole buffer to discover that — and presenting it as
	// scattered rectangles — is pure overhead over a single full present; return
	// nil so the host presents in full. This gate is a performance choice only:
	// whatever it returns, the host renders correct pixels, because a nil is a
	// full present and a diff is the exact set of changed pixels.
	if !a.frameAnimOnly {
		return nil
	}
	s := a.scene
	cur, prev := a.bufs[a.cur], a.bufs[1-a.cur]
	return toolkit.DiffRects(cur, prev, s.W, s.H)
}
