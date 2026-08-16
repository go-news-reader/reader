package windowapp

import "testing"

// TestHandlerNeedsPresent checks the Handler forwards the app's present-gate
// verdict: idle asks for nothing, an animating scene asks to keep ticking.
func TestHandlerNeedsPresent(t *testing.T) {
	h := New(newApp(t))
	if h.NeedsPresent() {
		t.Fatal("an idle handler should not ask to present")
	}
	h.a.VM().SetLoad(true, 0, 1)
	h.a.Frame()
	if !h.NeedsPresent() {
		t.Fatal("Handler.NeedsPresent should report the app's animating state")
	}
	// A loading spinner is throttle-safe: the Handler forwards that verdict so the
	// present loop redraws it below the tick rate.
	if !h.PresentThrottle() {
		t.Fatal("Handler.PresentThrottle should report the app's spinner-throttle state")
	}
	if h.PresentImmediate() {
		t.Fatal("no queued write, so Handler.PresentImmediate should be false")
	}
}
