package source

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestRateLimited(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{ErrRateLimited, true},
		{fmt.Errorf("wrap: %w", ErrRateLimited), true},
		{errors.New("twitter: UserTweets: unexpected status 429: Rate limit exceeded"), true},
		{errors.New("instagram: Rate Limit hit"), true},
		{errors.New("too many requests"), true},
		{errors.New("some other failure"), false},
	}
	for _, c := range cases {
		if got := RateLimited(c.err); got != c.want {
			t.Errorf("RateLimited(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}

// testCache returns a cache with a controllable clock; *at is the current time.
func testCache(ttl time.Duration, interval func(Kind) time.Duration) (*FeedCache, *time.Time) {
	at := time.Unix(1_000_000, 0)
	c := NewFeedCache(ttl, interval)
	c.now = func() time.Time { return at }
	return c, &at
}

func okFetch(marker string) func() (Result, error) {
	return func() (Result, error) { return Result{Items: []Item{{ID: marker}}}, nil }
}

func TestFeedCacheHitMissAndExpiry(t *testing.T) {
	c, at := testCache(time.Minute, nil)
	calls := 0
	fetch := func() (Result, error) { calls++; return Result{Items: []Item{{ID: "v"}}}, nil }

	// Miss → fetch + store.
	if r, err := c.Feed(Reddit, Query{Channel: "a"}, fetch); err != nil || len(r.Items) != 1 {
		t.Fatalf("first feed = %+v, %v", r, err)
	}
	if calls != 1 || !c.Fresh(Reddit, "a") {
		t.Fatalf("after miss: calls=%d fresh=%v", calls, c.Fresh(Reddit, "a"))
	}
	// Hit (within TTL) → no re-fetch.
	if _, err := c.Feed(Reddit, Query{Channel: "a"}, fetch); err != nil || calls != 1 {
		t.Fatalf("cache hit re-fetched: calls=%d err=%v", calls, err)
	}
	// A different channel is a separate entry.
	if _, _ = c.Feed(Reddit, Query{Channel: "b"}, fetch); calls != 2 {
		t.Fatalf("second channel should fetch: calls=%d", calls)
	}
	// Past the TTL → stale → re-fetch.
	*at = at.Add(2 * time.Minute)
	if c.Fresh(Reddit, "a") {
		t.Fatal("entry should be stale past the TTL")
	}
	if _, _ = c.Feed(Reddit, Query{Channel: "a"}, fetch); calls != 3 {
		t.Fatalf("stale entry should re-fetch: calls=%d", calls)
	}
}

func TestFeedCacheStaleWhileError(t *testing.T) {
	c, _ := testCache(0, nil) // TTL 0: every call fetches, but the stale fallback still applies
	// Seed a good result.
	if _, err := c.Feed(Instagram, Query{Channel: "x"}, okFetch("good")); err != nil {
		t.Fatal(err)
	}
	// A later rate-limited fetch returns the stale good result AND records backoff.
	rl := func() (Result, error) { return Result{}, ErrRateLimited }
	r, err := c.Feed(Instagram, Query{Channel: "x"}, rl)
	if err != nil || len(r.Items) != 1 || r.Items[0].ID != "good" {
		t.Fatalf("stale-while-error = %+v, %v", r, err)
	}
	if c.limiters[Instagram] == nil || c.limiters[Instagram].backoff <= 0 {
		t.Fatal("a rate-limited fetch should record backoff")
	}
}

func TestCachedPeek(t *testing.T) {
	c, at := testCache(time.Minute, nil)
	if _, ok := c.Cached(Reddit, "a"); ok {
		t.Fatal("no entry should peek as absent")
	}
	if _, err := c.Feed(Reddit, Query{Channel: "a"}, okFetch("v")); err != nil {
		t.Fatal(err)
	}
	// Cached returns the entry regardless of age (even past the TTL).
	*at = at.Add(time.Hour)
	r, ok := c.Cached(Reddit, "a")
	if !ok || len(r.Items) != 1 || r.Items[0].ID != "v" {
		t.Fatalf("Cached = %+v, ok=%v", r, ok)
	}
}

func TestFeedCacheErrorWithoutCache(t *testing.T) {
	c, _ := testCache(time.Minute, nil)
	sentinel := errors.New("boom")
	_, err := c.Feed(Reddit, Query{Channel: "none"}, func() (Result, error) { return Result{}, sentinel })
	if !errors.Is(err, sentinel) {
		t.Fatalf("first-ever failure should surface: %v", err)
	}
}

func TestFreshTTLDisabled(t *testing.T) {
	c, _ := testCache(0, nil)
	_, _ = c.Feed(Reddit, Query{Channel: "a"}, okFetch("v"))
	if c.Fresh(Reddit, "a") {
		t.Fatal("Fresh must be false when the TTL is disabled")
	}
}

func TestReserveQueuesSlotsAndBackoff(t *testing.T) {
	c, _ := testCache(0, func(Kind) time.Duration { return 2 * time.Second })
	// Successive reservations queue one interval apart: 0, 2s, 4s from now.
	if w := c.reserve(Instagram); w != 0 {
		t.Fatalf("first reserve wait = %v, want 0", w)
	}
	if w := c.reserve(Instagram); w != 2*time.Second {
		t.Fatalf("second reserve wait = %v, want 2s", w)
	}
	if w := c.reserve(Instagram); w != 4*time.Second {
		t.Fatalf("third reserve wait = %v, want 4s", w)
	}
	// A backoff shorter than the already-queued slot leaves the queue unchanged.
	c.noteRateLimited(Instagram) // 1s vs the +6s slot → no extension
	if w := c.reserve(Instagram); w != 6*time.Second {
		t.Fatalf("reserve after a shorter backoff = %v, want 6s", w)
	}
	// A fresh source's first backoff pushes its next slot out, so reserve waits.
	c.noteRateLimited(Reddit)
	if w := c.reserve(Reddit); w <= 0 {
		t.Fatalf("reserve after a fresh backoff should wait, got %v", w)
	}
}

func TestIntervalOf(t *testing.T) {
	// nil MinInterval → 0.
	c, _ := testCache(0, nil)
	if c.intervalOf(Instagram) != 0 {
		t.Fatal("nil MinInterval should give 0")
	}
	// non-positive result → 0; positive → itself.
	c2, _ := testCache(0, func(k Kind) time.Duration {
		if k == Instagram {
			return 3 * time.Second
		}
		return -1
	})
	if c2.intervalOf(Instagram) != 3*time.Second {
		t.Fatal("positive interval not returned")
	}
	if c2.intervalOf(Reddit) != 0 {
		t.Fatal("negative interval should clamp to 0")
	}
}

func TestPaceImmediateAndCancel(t *testing.T) {
	// No interval → immediate.
	c, _ := testCache(0, nil)
	if err := c.Pace(context.Background(), Reddit); err != nil {
		t.Fatalf("unpaced source should return at once: %v", err)
	}
	// With an interval and an already-cancelled context, a waited slot returns the
	// context error rather than sleeping it out.
	c2, _ := testCache(0, func(Kind) time.Duration { return time.Hour })
	c2.now = time.Now                            // real clock so the reserved slot is genuinely in the future
	_ = c2.Pace(context.Background(), Instagram) // first slot is immediate; reserves +1h
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c2.Pace(ctx, Instagram); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Pace = %v, want context.Canceled", err)
	}
}

func TestPaceWaitsThenFires(t *testing.T) {
	c := NewFeedCache(0, func(Kind) time.Duration { return 15 * time.Millisecond })
	// First is immediate (reserves +15ms); second must wait out the slot.
	if err := c.Pace(context.Background(), Instagram); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := c.Pace(context.Background(), Instagram); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) < 10*time.Millisecond {
		t.Fatalf("second Pace returned too early: %v", time.Since(start))
	}
}

func TestBackoffGrowsAndResets(t *testing.T) {
	c, _ := testCache(0, nil)
	c.MaxBackoff = 3 * time.Second // doubling 2s→4s overshoots, exercising the clamp
	c.noteRateLimited(Twitter)
	if got := c.limiters[Twitter].backoff; got != time.Second {
		t.Fatalf("first backoff = %v, want 1s", got)
	}
	c.noteRateLimited(Twitter)
	if got := c.limiters[Twitter].backoff; got != 2*time.Second {
		t.Fatalf("second backoff = %v, want 2s", got)
	}
	c.noteRateLimited(Twitter) // 2s<3s → doubles to 4s → clamped to 3s
	if got := c.limiters[Twitter].backoff; got != 3*time.Second {
		t.Fatalf("clamped backoff = %v, want 3s", got)
	}
	c.noteRateLimited(Twitter) // 3s not < 3s → no double, stays at the 3s cap
	if got := c.limiters[Twitter].backoff; got != 3*time.Second {
		t.Fatalf("capped backoff = %v, want 3s", got)
	}
	c.noteOK(Twitter)
	if got := c.limiters[Twitter].backoff; got != 0 {
		t.Fatalf("backoff after success = %v, want 0", got)
	}
}

func TestFeedCacheDefaultClock(t *testing.T) {
	// A cache with no injected clock uses time.Now (exercises the nil-now branch).
	c := NewFeedCache(time.Minute, nil)
	if _, err := c.Feed(Reddit, Query{Channel: "a"}, okFetch("v")); err != nil {
		t.Fatal(err)
	}
	if !c.Fresh(Reddit, "a") {
		t.Fatal("freshly fetched entry should be fresh under the real clock")
	}
}
