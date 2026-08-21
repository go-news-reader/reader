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

// TestShouldRepaintSpinnerCadenceStepsDown checks the adaptive spinner cadence:
// a load blits at the warm spinnerTicks rate through spinnerWarmTicks, then
// steps down to the sustained spinnerSlowTicks rate, and the age counter that
// drives the step-down keeps climbing across the load (it is not reset by the
// warm-phase blits).
func TestShouldRepaintSpinnerCadenceStepsDown(t *testing.T) {
	var st presentState
	np := fakePresenter{need: true, throttle: true}
	// Drive the whole warm window. Every spinnerTicks-th tick blits; spinner
	// resets on each blit but spinnerAge accumulates every tick.
	for tick := 1; tick <= spinnerWarmTicks; tick++ {
		blit := shouldRepaint(true, np, &st)
		if want := tick%spinnerTicks == 0; blit != want {
			t.Fatalf("warm tick %d: blit=%v, want %v", tick, blit, want)
		}
		if st.spinnerAge != tick {
			t.Fatalf("warm tick %d: spinnerAge=%d, want %d (age must not reset on a spinner blit)", tick, st.spinnerAge, tick)
		}
	}
	// Past the warm window the cadence widens to spinnerSlowTicks. spinner was
	// reset by the last warm blit (spinnerWarmTicks is a multiple of spinnerTicks),
	// so a fresh run of spinnerSlowTicks skips must precede the next blit.
	for i := 1; i < spinnerSlowTicks; i++ {
		if shouldRepaint(true, np, &st) {
			t.Fatalf("sustained tick %d should be skipped at the slow cadence", i)
		}
	}
	if !shouldRepaint(true, np, &st) {
		t.Fatal("the spinnerSlowTicks-th sustained tick should blit")
	}
}

// TestShouldRepaintSpinnerAgeResetsWhenStateBreaks checks that leaving the
// spinner-only state resets spinnerAge, so a later load reopens at the warm
// cadence instead of inheriting a stale age and starting slow.
func TestShouldRepaintSpinnerAgeResetsWhenStateBreaks(t *testing.T) {
	st := presentState{spinnerAge: spinnerWarmTicks + 50}
	// A non-throttleable present (a GIF, a debounce) breaks the spinner state.
	shouldRepaint(true, fakePresenter{need: true, throttle: false}, &st)
	if st.spinnerAge != 0 {
		t.Fatalf("a non-throttle present must reset spinnerAge, got %d", st.spinnerAge)
	}
	// So must going idle.
	st.spinnerAge = spinnerWarmTicks + 50
	shouldRepaint(true, fakePresenter{need: false}, &st)
	if st.spinnerAge != 0 {
		t.Fatalf("going idle must reset spinnerAge, got %d", st.spinnerAge)
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
