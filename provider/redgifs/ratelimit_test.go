package redgifs

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/cenkalti/backoff/v5"
)

// fakeClock is a controllable clock + sleep for the limiter: sleeping advances
// the clock instantly (no real waiting) and records the requested durations.
type fakeClock struct {
	t      time.Time
	slept  []time.Duration
	cancel bool // when true, sleep reports a cancelled context
}

func (f *fakeClock) now() time.Time { return f.t }

func (f *fakeClock) sleep(ctx context.Context, d time.Duration) error {
	if f.cancel {
		return context.Canceled
	}
	if d > 0 {
		f.slept = append(f.slept, d)
		f.t = f.t.Add(d)
	}
	return ctx.Err()
}

func newFakeLimiter(perMinute int) (*limiter, *fakeClock) {
	fc := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	l := &limiter{
		minInterval: time.Minute / time.Duration(perMinute),
		now:         fc.now,
		sleep:       fc.sleep,
	}
	return l, fc
}

func TestLimiterPacesRequests(t *testing.T) {
	l, fc := newFakeLimiter(60) // 1s spacing
	ctx := context.Background()
	if err := l.wait(ctx); err != nil {
		t.Fatal(err)
	}
	if len(fc.slept) != 0 {
		t.Fatalf("first request slept %v, want none", fc.slept)
	}
	for i := 0; i < 2; i++ {
		if err := l.wait(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if len(fc.slept) != 2 || fc.slept[0] != time.Second || fc.slept[1] != time.Second {
		t.Fatalf("paced sleeps = %v, want [1s 1s]", fc.slept)
	}
}

func TestLimiterWaitRespectsCancellation(t *testing.T) {
	l, fc := newFakeLimiter(60)
	fc.cancel = true
	if err := l.wait(context.Background()); err != context.Canceled {
		t.Fatalf("wait err = %v, want context.Canceled", err)
	}
}

func TestLimiterPauseFor(t *testing.T) {
	l, fc := newFakeLimiter(600) // 100ms baseline
	ctx := context.Background()
	l.pauseFor(5 * time.Second)
	if err := l.wait(ctx); err != nil {
		t.Fatal(err)
	}
	if len(fc.slept) != 1 || fc.slept[0] != 5*time.Second {
		t.Fatalf("after pauseFor, slept = %v, want [5s]", fc.slept)
	}
	before := l.next
	l.pauseFor(0)
	l.pauseFor(-time.Second)
	if !l.next.Equal(before) {
		t.Fatalf("non-positive pauseFor moved next: %v -> %v", before, l.next)
	}
}

func TestRetryAfterSeconds(t *testing.T) {
	l, _ := newFakeLimiter(600)
	if ra := l.retryAfter(http.Header{"Retry-After": {"7"}}); ra == nil || ra.Duration != 7*time.Second {
		t.Fatalf("retryAfter seconds = %v, want 7s", ra)
	}
	if ra := l.retryAfter(http.Header{"Retry-After": {"-3"}}); ra == nil || ra.Duration != 0 {
		t.Fatalf("negative retryAfter = %v, want 0", ra)
	}
}

func TestRetryAfterHTTPDate(t *testing.T) {
	l, fc := newFakeLimiter(600)
	future := fc.t.Add(90 * time.Second).UTC().Format(http.TimeFormat)
	if ra := l.retryAfter(http.Header{"Retry-After": {future}}); ra == nil || ra.Duration != 90*time.Second {
		t.Fatalf("retryAfter date = %v, want 90s", ra)
	}
	past := fc.t.Add(-time.Minute).UTC().Format(http.TimeFormat)
	if ra := l.retryAfter(http.Header{"Retry-After": {past}}); ra == nil || ra.Duration != 0 {
		t.Fatalf("past retryAfter = %v, want 0", ra)
	}
}

func TestRetryAfterAbsentOrJunk(t *testing.T) {
	l, _ := newFakeLimiter(600)
	if ra := l.retryAfter(http.Header{}); ra != nil {
		t.Fatalf("absent Retry-After should be nil, got %v", ra)
	}
	if ra := l.retryAfter(http.Header{"Retry-After": {"soon"}}); ra != nil {
		t.Fatalf("junk Retry-After should be nil, got %v", ra)
	}
}

func TestNewExpBackoffSchedule(t *testing.T) {
	// No jitter: the schedule is deterministic and caps at maxBackoff, matching
	// the historical 500ms, 1s, 2s, 4s, 4s, … fallback.
	b := newExpBackoff()
	want := []time.Duration{baseBackoff, 2 * baseBackoff, 4 * baseBackoff, maxBackoff, maxBackoff}
	for i, w := range want {
		if got := b.NextBackOff(); got != w {
			t.Fatalf("NextBackOff #%d = %v, want %v", i+1, got, w)
		}
	}
}

func TestNextDelay(t *testing.T) {
	l, _ := newFakeLimiter(600)

	// No usable Retry-After: fall back to the exponential schedule, which keeps
	// advancing across calls.
	b := newExpBackoff()
	if d := l.nextDelay(b, http.Header{}); d != baseBackoff {
		t.Fatalf("nextDelay(no header) = %v, want %v", d, baseBackoff)
	}
	if d := l.nextDelay(b, http.Header{"Retry-After": {"junk"}}); d != 2*baseBackoff {
		t.Fatalf("nextDelay(junk header) = %v, want %v", d, 2*baseBackoff)
	}

	// A usable Retry-After overrides the schedule and resets it, so the next
	// fallback starts again from baseBackoff.
	if d := l.nextDelay(b, http.Header{"Retry-After": {"9"}}); d != 9*time.Second {
		t.Fatalf("nextDelay(Retry-After 9) = %v, want 9s", d)
	}
	if d := l.nextDelay(b, http.Header{}); d != baseBackoff {
		t.Fatalf("nextDelay after reset = %v, want %v", d, baseBackoff)
	}

	// The override is surfaced as the reference library's own error type.
	var ra *backoff.RetryAfterError = l.retryAfter(http.Header{"Retry-After": {"1"}})
	if ra == nil || ra.Duration != time.Second {
		t.Fatalf("retryAfter type/value = %v, want 1s *backoff.RetryAfterError", ra)
	}
}

func TestNewLimiterDefaultsOnNonPositive(t *testing.T) {
	l := newLimiter(0)
	if l.minInterval != time.Minute/time.Duration(defaultRequestsPerMinute) {
		t.Fatalf("minInterval = %v, want default", l.minInterval)
	}
	if l2 := newLimiter(120); l2.minInterval != time.Minute/120 {
		t.Fatalf("minInterval = %v, want 500ms", l2.minInterval)
	}
}

func TestSleepCtx(t *testing.T) {
	if err := sleepCtx(context.Background(), 0); err != nil {
		t.Fatalf("sleepCtx(0) = %v, want nil", err)
	}
	if err := sleepCtx(context.Background(), time.Millisecond); err != nil {
		t.Fatalf("sleepCtx(1ms) = %v, want nil", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleepCtx(ctx, 0); err != context.Canceled {
		t.Fatalf("sleepCtx(0, cancelled) = %v, want Canceled", err)
	}
	if err := sleepCtx(ctx, time.Hour); err != context.Canceled {
		t.Fatalf("sleepCtx(1h, cancelled) = %v, want Canceled", err)
	}
}
