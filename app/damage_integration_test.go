package app

import (
	"testing"
	"time"
)

// The point of the damage path is that a spinner tick during a load re-presents
// only the spinner, not the whole window. Unit tests pin diffRects on crafted
// buffers; this pins the property that matters end to end — on the REAL scene, a
// spinner-only tick reports damage that is a small fraction of the surface, so
// the incremental present actually engages instead of quietly degrading to a
// full blit every tick.
func TestDamageIsSmallDuringASpinnerLoad(t *testing.T) {
	const w, h = 360, 240
	a := New(Config{Registry: newReg(), Width: w, Height: h})
	clock := time.Unix(0, 0)
	a.now = func() time.Time { return clock }

	// Enter loading and draw the first animated frame; this establishes the
	// animation clock (see TestFrameAnimatesWhileLoading).
	a.Scene().SetLoading(true, 0, 2)
	if _, changed := a.Frame(); !changed {
		t.Fatal("entering loading did not draw a frame")
	}

	// Advance real time so the spinner steps, then draw again. Now the two frame
	// buffers differ ONLY where the spinner moved — nothing else changed.
	clock = clock.Add(4 * animFrameInterval)
	if _, changed := a.Frame(); !changed {
		t.Fatal("the spinner stepped but no frame was drawn")
	}

	rects := a.DamageRects()
	if len(rects) == 0 {
		t.Fatal("a spinner step reported no damage — the present would show nothing move")
	}
	area := 0
	for _, r := range rects {
		if r.X < 0 || r.Y < 0 || r.X+r.W > w || r.Y+r.H > h {
			t.Fatalf("damage %v escapes the %dx%d surface", r, w, h)
		}
		area += r.W * r.H
	}
	full := w * h
	// The animated indicators (a centred ring, the strip's trailing spinner) are a
	// small part of the window. A generous ceiling still proves the win: if the
	// whole scene were redrawing each tick this would be ~100%.
	if area*100/full > 33 {
		t.Fatalf("spinner-tick damage = %d px (%d%% of the surface) across %d rect(s); expected a small fraction, not a near-full repaint",
			area, area*100/full, len(rects))
	}
	t.Logf("spinner-tick damage = %d px (%d%% of surface) across %d rect(s)", area, area*100/full, len(rects))
}
