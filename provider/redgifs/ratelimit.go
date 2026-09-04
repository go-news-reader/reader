package redgifs

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v7"
)

// RedGIFs answers bursts of requests — exactly what aggregating many
// subscriptions produces — with HTTP 429 "Too Many Requests". The client paces
// its own traffic through a [limiter] so it stays under RedGIFs' budget and,
// when a 429 slips through anyway, honours the Retry-After header and retries.
// All of this lives below get(), so every endpoint benefits and concurrent
// callers (the per-subscription goroutines) serialise through the one shared
// limiter.

// defaultRequestsPerMinute is the self-imposed request rate a fresh client
// uses. 60/min (one request per second) sits well under what the RedGIFs API
// tolerates, so a first burst does not trip a 429.
const defaultRequestsPerMinute = 60

// maxRetries bounds how many times get() re-sends a request that came back 429
// before giving up and returning the error, so a persistently rate-limited or
// wedged endpoint cannot loop forever.
const maxRetries = 4

// baseBackoff and maxBackoff configure the [backoff.ExponentialBackOff] fallback
// used when a 429 response carries no usable Retry-After header: 500ms, 1s, 2s, …
// capped at maxBackoff.
const (
	baseBackoff = 500 * time.Millisecond
	maxBackoff  = 4 * time.Second
)

// limiter paces outbound requests to at most one per minInterval and lets a
// response push the next-allowed instant further out (a 429's Retry-After). It
// is safe for concurrent use: the per-subscription fetch goroutines all wait on
// the same limiter, so they leave the client one at a time rather than
// stampeding RedGIFs at once.
type limiter struct {
	mu          sync.Mutex
	minInterval time.Duration // baseline spacing between requests
	next        time.Time     // earliest instant the next request may go out

	// now/sleep are seams so tests drive timing deterministically. sleep must
	// return early with ctx.Err() when ctx is cancelled.
	now   func() time.Time
	sleep func(ctx context.Context, d time.Duration) error
}

// newLimiter builds a limiter pacing to perMinute requests (falling back to
// [defaultRequestsPerMinute] when perMinute <= 0), with wall-clock timing.
func newLimiter(perMinute int) *limiter {
	if perMinute <= 0 {
		perMinute = defaultRequestsPerMinute
	}
	return &limiter{
		minInterval: time.Minute / time.Duration(perMinute),
		now:         time.Now,
		sleep:       sleepCtx,
	}
}

// sleepCtx sleeps for d unless ctx is cancelled first, in which case it returns
// ctx.Err() immediately. A non-positive d returns nil (or ctx.Err()) without
// blocking.
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// wait blocks until this request's turn, then reserves the following slot. It
// returns ctx.Err() if the context is cancelled while waiting.
func (l *limiter) wait(ctx context.Context) error {
	l.mu.Lock()
	now := l.now()
	start := l.next
	if start.Before(now) {
		start = now
	}
	// Reserve the slot after this one before unlocking, so concurrent callers
	// each get a distinct, spaced turn instead of all reading the same "next".
	l.next = start.Add(l.minInterval)
	l.mu.Unlock()
	return l.sleep(ctx, start.Sub(now))
}

// pauseFor pushes the next-allowed instant out to at least now+d (never pulling
// it in), so a 429 Retry-After delays every queued request, not just the one
// that saw the header.
func (l *limiter) pauseFor(d time.Duration) {
	if d <= 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	until := l.now().Add(d)
	if until.After(l.next) {
		l.next = until
	}
}

// retryAfter returns the wait a 429 response asks for via its Retry-After header
// — either delta-seconds ("5") or an HTTP-date — as a [backoff.RetryAfterError],
// the reference library's signal that a server-supplied delay overrides the
// exponential schedule. It returns nil when the header is absent or unparseable,
// so the caller falls back to [backoff.ExponentialBackOff].
func (l *limiter) retryAfter(h http.Header) *backoff.RetryAfterError {
	v := strings.TrimSpace(h.Get("Retry-After"))
	if v == "" {
		return nil
	}
	if secs, err := strconv.Atoi(v); err == nil {
		if secs < 0 {
			secs = 0
		}
		return &backoff.RetryAfterError{Duration: time.Duration(secs) * time.Second}
	}
	if t, err := http.ParseTime(v); err == nil {
		d := t.Sub(l.now())
		if d < 0 {
			d = 0
		}
		return &backoff.RetryAfterError{Duration: d}
	}
	return nil
}

// newExpBackoff builds the exponential fallback schedule for a single request's
// 429 retries: baseBackoff·2ⁿ (500ms, 1s, 2s, …) capped at maxBackoff, with no
// jitter so pacing stays deterministic. A fresh instance is used per request
// because [backoff.ExponentialBackOff] is stateful and not safe for concurrent
// use.
func newExpBackoff() *backoff.ExponentialBackOff {
	b := &backoff.ExponentialBackOff{
		InitialInterval:     baseBackoff,
		RandomizationFactor: 0,
		Multiplier:          2,
		MaxInterval:         maxBackoff,
	}
	b.Reset()
	return b
}

// nextDelay decides how long to wait before re-sending a 429'd request. It
// mirrors what [backoff.Retry] does internally: it advances the exponential
// schedule, but when the response carries a usable Retry-After the
// server-supplied [backoff.RetryAfterError] duration wins and the exponential
// schedule is reset.
func (l *limiter) nextDelay(exp backoff.BackOff, h http.Header) time.Duration {
	next := exp.NextBackOff()
	if ra := l.retryAfter(h); ra != nil {
		next = ra.Duration
		exp.Reset()
	}
	return next
}
