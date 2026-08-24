package app

import (
	"reflect"
	"testing"
	"time"

	"github.com/go-widgets/toolkit"
)

// setPx flips pixel (x,y) of a w-wide RGBA buffer to a non-zero value, so a diff
// against an all-zero buffer sees exactly that pixel change.
func setPx(buf []byte, w, x, y int) {
	o := (y*w + x) * 4
	buf[o], buf[o+1], buf[o+2], buf[o+3] = 1, 2, 3, 4
}

// DamageRects diffs the two buffers Frame keeps, in the scene's coordinates.
func TestDamageRectsReadsTheFrameBuffers(t *testing.T) {
	a := New(Config{Registry: newReg(), Width: 8, Height: 6})
	w, hh := a.Scene().W, a.Scene().H
	a.bufs[0] = make([]byte, w*hh*4)
	a.bufs[1] = make([]byte, w*hh*4)
	a.cur = 0
	a.frameAnimOnly = true    // an animation-only frame: the diff path is taken
	setPx(a.bufs[0], w, 2, 3) // "current" differs from "previous" at (2,3)

	want := []toolkit.Rect{{X: 2, Y: 3, W: 1, H: 1}}
	if got := a.DamageRects(); !reflect.DeepEqual(got, want) {
		t.Fatalf("DamageRects = %v, want %v", got, want)
	}
	// Swapping which buffer is current diffs the other way — same rectangle.
	a.cur = 1
	if got := a.DamageRects(); !reflect.DeepEqual(got, want) {
		t.Fatalf("after swap DamageRects = %v, want %v", got, want)
	}
}

// A tick straddling a resize — the buffers reallocated to a size that no longer
// matches the scene — reports whole-surface damage (nil) so the host presents in
// full rather than reading past a short buffer.
func TestDamageRectsSizeMismatchIsWholeSurface(t *testing.T) {
	a := New(Config{Registry: newReg(), Width: 8, Height: 6})
	a.frameAnimOnly = true      // reach the diff path, so nil is the size guard's doing
	a.bufs[0] = make([]byte, 4) // stale, wrong-sized buffers
	a.bufs[1] = make([]byte, 4)
	a.cur = 0
	if got := a.DamageRects(); got != nil {
		t.Fatalf("DamageRects = %v, want nil on a size mismatch", got)
	}
}

// Frame flags a frame as animation-only exactly when nothing but the spinner
// changed: the loading frame (which mutated the scene) is not, the next frame
// (only the animation clock stepped) is.
func TestFrameFlagsAnimationOnlyFrames(t *testing.T) {
	a := New(Config{Registry: newReg(), Width: 360, Height: 240})
	clock := time.Unix(0, 0)
	a.now = func() time.Time { return clock }

	a.Scene().SetLoading(true, 0, 2)
	if _, changed := a.Frame(); !changed {
		t.Fatal("entering loading did not draw")
	}
	if a.frameAnimOnly {
		t.Fatal("a content frame (SetLoading) must not be flagged animation-only")
	}

	clock = clock.Add(4 * animFrameInterval)
	if _, changed := a.Frame(); !changed {
		t.Fatal("the spinner stepped but no frame was drawn")
	}
	if !a.frameAnimOnly {
		t.Fatal("a spinner-only frame must be flagged animation-only")
	}
}

// A content or input frame (frameAnimOnly false) reports whole-surface damage
// (nil), so the host presents in full instead of scanning the whole buffer to
// discover most of it changed. This is the gate that keeps the diff off the
// frames it would only slow down.
func TestDamageRectsGatedToAnimationOnlyFrames(t *testing.T) {
	a := New(Config{Registry: newReg(), Width: 8, Height: 6})
	w, hh := a.Scene().W, a.Scene().H
	a.bufs[0] = make([]byte, w*hh*4)
	a.bufs[1] = make([]byte, w*hh*4)
	a.cur = 0
	setPx(a.bufs[0], w, 2, 3) // a real difference exists...
	a.frameAnimOnly = false   // ...but this was a content/input frame
	if got := a.DamageRects(); got != nil {
		t.Fatalf("DamageRects = %v, want nil (full present) on a non-animation frame", got)
	}
	// The same buffers on an animation-only frame do report the diff.
	a.frameAnimOnly = true
	want := []toolkit.Rect{{X: 2, Y: 3, W: 1, H: 1}}
	if got := a.DamageRects(); !reflect.DeepEqual(got, want) {
		t.Fatalf("DamageRects = %v, want %v on an animation frame", got, want)
	}
}
