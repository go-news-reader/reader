package redgifs

import (
	"context"
	"net/http"
	"testing"
	"time"
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
	if d, ok := l.retryAfter(http.Header{"Retry-After": {"7"}}); !ok || d != 7*time.Second {
		t.Fatalf("retryAfter seconds = %v,%v, want 7s,true", d, ok)
	}
	if d, ok := l.retryAfter(http.Header{"Retry-After": {"-3"}}); !ok || d != 0 {
		t.Fatalf("negative retryAfter = %v,%v, want 0,true", d, ok)
	}
}

func TestRetryAfterHTTPDate(t *testing.T) {
	l, fc := newFakeLimiter(600)
	future := fc.t.Add(90 * time.Second).UTC().Format(http.TimeFormat)
	if d, ok := l.retryAfter(http.Header{"Retry-After": {future}}); !ok || d != 90*time.Second {
		t.Fatalf("retryAfter date = %v,%v, want 90s,true", d, ok)
	}
	past := fc.t.Add(-time.Minute).UTC().Format(http.TimeFormat)
	if d, ok := l.retryAfter(http.Header{"Retry-After": {past}}); !ok || d != 0 {
		t.Fatalf("past retryAfter = %v,%v, want 0,true", d, ok)
	}
}

func TestRetryAfterAbsentOrJunk(t *testing.T) {
	l, _ := newFakeLimiter(600)
	if _, ok := l.retryAfter(http.Header{}); ok {
		t.Fatal("absent Retry-After should be ok=false")
	}
	if _, ok := l.retryAfter(http.Header{"Retry-After": {"soon"}}); ok {
		t.Fatal("junk Retry-After should be ok=false")
	}
}

func TestBackoff(t *testing.T) {
	if d := backoff(0); d != baseBackoff {
		t.Fatalf("backoff(0) = %v, want %v", d, baseBackoff)
	}
	if d := backoff(1); d != 2*baseBackoff {
		t.Fatalf("backoff(1) = %v, want %v", d, 2*baseBackoff)
	}
	// A large attempt clamps at maxBackoff (both the > cap and the overflow<=0
	// paths land here).
	if d := backoff(10); d != maxBackoff {
		t.Fatalf("backoff(10) = %v, want %v", d, maxBackoff)
	}
	if d := backoff(1000); d != maxBackoff {
		t.Fatalf("backoff(1000) = %v, want %v (overflow)", d, maxBackoff)
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
