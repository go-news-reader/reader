package window

import "testing"

type fakeNeeder bool

func (n fakeNeeder) NeedsPresent() bool { return bool(n) }

// TestShouldRepaintUngatedAlwaysBlits checks a handler that cannot answer
// NeedsPresent keeps the pre-gating behaviour: every tick repaints.
func TestShouldRepaintUngatedAlwaysBlits(t *testing.T) {
	idle := 5
	if !shouldRepaint(false, nil, &idle) {
		t.Fatal("an ungated loop must repaint on every tick")
	}
	if idle != 0 {
		t.Fatalf("idle counter should reset on a repaint, got %d", idle)
	}
}

// TestShouldRepaintGatedRepaintsWhenNeeded checks a gated loop blits the moment
// the handler needs it, and resets its idle run.
func TestShouldRepaintGatedRepaintsWhenNeeded(t *testing.T) {
	idle := 9
	if !shouldRepaint(true, fakeNeeder(true), &idle) {
		t.Fatal("a gated loop must repaint when the handler needs it")
	}
	if idle != 0 {
		t.Fatalf("idle counter should reset on a needed repaint, got %d", idle)
	}
}

// TestShouldRepaintGatedHeartbeat checks the backstop: while the handler needs
// nothing, ticks are skipped until the heartbeat forces one, then the run
// resets.
func TestShouldRepaintGatedHeartbeat(t *testing.T) {
	idle := 0
	np := fakeNeeder(false)
	for i := 1; i < heartbeatTicks; i++ {
		if shouldRepaint(true, np, &idle) {
			t.Fatalf("idle tick %d should be skipped, not blit", i)
		}
	}
	if !shouldRepaint(true, np, &idle) {
		t.Fatal("the heartbeat tick should force a repaint")
	}
	if idle != 0 {
		t.Fatalf("idle counter should reset after the heartbeat, got %d", idle)
	}
}
