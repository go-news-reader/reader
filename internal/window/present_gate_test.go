package window

import "testing"

// fakePresenter is a scripted presenter for the gated-loop tests.
type fakePresenter struct{ need, immediate, throttle bool }

func (p fakePresenter) NeedsPresent() bool     { return p.need }
func (p fakePresenter) PresentImmediate() bool { return p.immediate }
func (p fakePresenter) PresentThrottle() bool  { return p.throttle }

// TestShouldRepaintUngatedAlwaysBlits checks a handler that cannot answer the
// presenter contract keeps the pre-gating behaviour: every tick repaints, and
// both counters stay reset.
func TestShouldRepaintUngatedAlwaysBlits(t *testing.T) {
	st := presentState{idle: 5, spinner: 3}
	if !shouldRepaint(false, nil, &st) {
		t.Fatal("an ungated loop must repaint on every tick")
	}
	if st.idle != 0 || st.spinner != 0 {
		t.Fatalf("counters should reset on a repaint, got %+v", st)
	}
}

// TestShouldRepaintImmediateBlitsAtOnce checks a queued content write blits this
// tick regardless of the throttle, and resets both counters.
func TestShouldRepaintImmediateBlitsAtOnce(t *testing.T) {
	st := presentState{idle: 4, spinner: 2}
	if !shouldRepaint(true, fakePresenter{need: true, immediate: true, throttle: true}, &st) {
		t.Fatal("a queued content write must blit at once")
	}
	if st.idle != 0 || st.spinner != 0 {
		t.Fatalf("counters should reset on an immediate blit, got %+v", st)
	}
}

// TestShouldRepaintFullCadenceWhenNotThrottleable checks that a present the
// handler needs but cannot throttle (a GIF, a debounce) blits every tick.
func TestShouldRepaintFullCadenceWhenNotThrottleable(t *testing.T) {
	st := presentState{spinner: 2}
	np := fakePresenter{need: true, immediate: false, throttle: false}
	if !shouldRepaint(true, np, &st) {
		t.Fatal("a non-throttleable present must blit every tick")
	}
	if st.spinner != 0 {
		t.Fatalf("the spinner counter should reset, got %d", st.spinner)
	}
}

// TestShouldRepaintSpinnerThrottle checks the spinner cadence: while only the
// loading spinner moves, ticks are skipped until every spinnerTicks-th one,
// which blits and resets the run.
func TestShouldRepaintSpinnerThrottle(t *testing.T) {
	var st presentState
	np := fakePresenter{need: true, immediate: false, throttle: true}
	for i := 1; i < spinnerTicks; i++ {
		if shouldRepaint(true, np, &st) {
			t.Fatalf("spinner tick %d should be skipped, not blit", i)
		}
	}
	if !shouldRepaint(true, np, &st) {
		t.Fatal("the spinnerTicks-th tick should blit")
	}
	if st.spinner != 0 {
		t.Fatalf("the spinner counter should reset after a blit, got %d", st.spinner)
	}
}

// TestShouldRepaintGatedHeartbeat checks the idle backstop: while the handler
// needs nothing, ticks are skipped until the heartbeat forces one, then reset.
func TestShouldRepaintGatedHeartbeat(t *testing.T) {
	var st presentState
	np := fakePresenter{need: false}
	for i := 1; i < heartbeatTicks; i++ {
		if shouldRepaint(true, np, &st) {
			t.Fatalf("idle tick %d should be skipped, not blit", i)
		}
	}
	if !shouldRepaint(true, np, &st) {
		t.Fatal("the heartbeat tick should force a repaint")
	}
	if st.idle != 0 {
		t.Fatalf("idle counter should reset after the heartbeat, got %d", st.idle)
	}
}
