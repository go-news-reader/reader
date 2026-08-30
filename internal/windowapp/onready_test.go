package windowapp

import (
	"testing"

	"github.com/go-news-reader/reader/app"
	"github.com/go-news-reader/reader/source"
)

// TestOnReadyFiresOnceAfterFirstFrame proves the seam the reader uses to defer
// its vault read: the callback runs on the SECOND frame — so the first frame is
// already on screen — and exactly once thereafter.
func TestOnReadyFiresOnceAfterFirstFrame(t *testing.T) {
	a := app.New(app.Config{Registry: source.NewRegistry(), Width: 400, Height: 300})
	a.SetRefreshHook(func() {})
	h := New(a)

	calls := 0
	h.SetOnReady(func() { calls++ })

	h.Frame() // frame 1: window not yet shown, must not fire
	if calls != 0 {
		t.Fatalf("onReady fired on the first frame (calls=%d)", calls)
	}
	h.Frame() // frame 2: first frame is blitted, fire now
	h.Frame() // frame 3: must not fire again
	if calls != 1 {
		t.Fatalf("onReady calls = %d, want exactly 1", calls)
	}
}

// TestOnReadyUnsetIsHarmless: with no callback registered, the second frame is a
// normal frame — the nil guard must not panic.
func TestOnReadyUnsetIsHarmless(t *testing.T) {
	a := app.New(app.Config{Registry: source.NewRegistry(), Width: 400, Height: 300})
	a.SetRefreshHook(func() {})
	h := New(a)
	h.Frame()
	h.Frame() // no onReady set: exercises the frames==2 tick with a nil callback
}
