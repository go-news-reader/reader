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
}
