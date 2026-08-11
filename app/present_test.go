package app

import "testing"

// TestNeedsPresentRaisedByQueuedWriteAndClearedOnDrain checks the wake half of
// the present gate: a background scene write enqueued while the window is idle
// asks for exactly one present, and draining it on the render thread drops the
// ask again so the loop can go back to sleep.
func TestNeedsPresentRaisedByQueuedWriteAndClearedOnDrain(t *testing.T) {
	a := New(Config{Registry: newReg(), Width: 400, Height: 300})
	a.DeferSceneWrites()
	if a.NeedsPresent() {
		t.Fatal("a fresh idle app should not ask to be presented")
	}
	drained := false
	a.post(func() { drained = true })
	if !a.NeedsPresent() {
		t.Fatal("a queued scene write should raise the present wake")
	}
	a.Frame() // drains on the render thread, clearing the wake
	if !drained {
		t.Fatal("Frame did not drain the queued write")
	}
	if a.NeedsPresent() {
		t.Fatal("a drained, idle app should drop the present wake")
	}
}

// TestNeedsPresentTracksAnimation checks the busy half: while the scene is
// animating the loop is told to keep ticking, and once it settles the ask drops
// so an idle window stops blitting.
func TestNeedsPresentTracksAnimation(t *testing.T) {
	a := New(Config{Registry: newReg(), Width: 400, Height: 300})
	a.VM().SetLoad(true, 0, 1) // loading -> Scene.Animating
	a.Frame()
	if !a.NeedsPresent() {
		t.Fatal("an animating scene should keep the present loop ticking")
	}
	a.VM().SetLoad(false, 1, 1)
	a.Frame()
	if a.NeedsPresent() {
		t.Fatal("a settled scene should let the present loop idle out")
	}
}
