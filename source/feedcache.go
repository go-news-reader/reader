package source

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

// defaultMaxBackoff caps a source's rate-limit backoff when [FeedCache.MaxBackoff]
// is unset.
const defaultMaxBackoff = 10 * time.Minute

// ErrRateLimited is the typed signal that a source refused a read because the
// client is over its rate limit (an HTTP 429). Providers may return or wrap it;
// [RateLimited] also recognises the plain "429" / "rate limit" / "too many
// requests" phrasings that reach here from providers not yet using it.
var ErrRateLimited = errors.New("source: rate limited")

// RateLimited reports whether err signals that the source is rate-limiting the
// client, so a caller ([FeedCache]) can back off rather than keep hammering.
func RateLimited(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrRateLimited) {
		return true
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "429") ||
		strings.Contains(s, "rate limit") ||
		strings.Contains(s, "too many requests")
}

// FeedCache sits in front of a [Registry]'s provider fetches to keep a profile
// with many subscriptions from behaving like a bot against a source's API. It
// does three things:
//
//   - caches each subscription's last result for [FeedCache.TTL], so re-viewing
//     or refreshing a feed does not re-fetch what was just fetched;
//   - serves the last good result when a fetch fails (stale-while-error), so a
//     rate-limited source shows its previous posts rather than an empty feed;
//   - paces and backs off per source: [FeedCache.Pace] spaces successive fetches
//     to one source by [FeedCache.MinInterval], and a fetch that reports a rate
//     limit ([RateLimited]) pushes an exponentially growing pause onto that
//     source so the client eases off instead of hammering it.
//
// It is opt-in: a Registry with a nil Cache fetches straight through, unchanged.
// All methods are safe for concurrent use.
type FeedCache struct {
	// TTL is how long a fetched result is served from cache without a re-fetch.
	// Zero means every call fetches (the stale fallback and pacing still apply).
	TTL time.Duration
	// MinInterval returns the minimum gap between two fetches to the same source.
	// Nil, or a non-positive result, means that source is not paced.
	MinInterval func(Kind) time.Duration
	// MaxBackoff caps the exponential backoff a rate-limited source accrues. Zero
	// uses [defaultMaxBackoff].
	MaxBackoff time.Duration
	// now is the clock; nil means time.Now (tests inject a fake).
	now func() time.Time

	mu       sync.Mutex
	entries  map[string]feedEntry
	limiters map[Kind]*srcLimiter
}

// feedEntry is one cached subscription result and when it was fetched.
type feedEntry struct {
	res Result
	at  time.Time
}

// srcLimiter is one source's pacing + backoff state.
type srcLimiter struct {
	nextAllowed time.Time     // no fetch to this source before this instant
	backoff     time.Duration // current rate-limit pause length (doubles each 429)
}

// NewFeedCache returns a cache with the given freshness TTL and per-source pacing.
// minInterval may be nil (no pacing).
func NewFeedCache(ttl time.Duration, minInterval func(Kind) time.Duration) *FeedCache {
	return &FeedCache{
		TTL:         ttl,
		MinInterval: minInterval,
		entries:     map[string]feedEntry{},
		limiters:    map[Kind]*srcLimiter{},
	}
}

func (c *FeedCache) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

func cacheKey(kind Kind, channel string) string { return string(kind) + "\x00" + channel }

// Fresh reports whether kind/channel has a cached result younger than the TTL —
// so a scheduler can skip pacing a fetch it will not actually make.
func (c *FeedCache) Fresh(kind Kind, channel string) bool {
	if c.TTL <= 0 {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[cacheKey(kind, channel)]
	return ok && c.clock().Sub(e.at) < c.TTL
}

// Feed returns a cached result when it is younger than the TTL, otherwise calls
// fetch and caches the result. When fetch fails it returns the last good result
// if there is one (so a transient failure or rate limit does not blank the feed),
// and records rate-limit backoff for the source; only a first-ever failure with
// no cached result is returned as an error.
func (c *FeedCache) Feed(kind Kind, q Query, fetch func() (Result, error)) (Result, error) {
	key := cacheKey(kind, q.Channel)

	c.mu.Lock()
	e, has := c.entries[key]
	fresh := c.TTL > 0 && has && c.clock().Sub(e.at) < c.TTL
	c.mu.Unlock()
	if fresh {
		return e.res, nil
	}

	res, err := fetch()
	if err != nil {
		if RateLimited(err) {
			c.noteRateLimited(kind)
		}
		if has {
			return e.res, nil // stale-while-error
		}
		return Result{}, err
	}

	c.noteOK(kind)
	c.mu.Lock()
	c.entries[key] = feedEntry{res: res, at: c.clock()}
	c.mu.Unlock()
	return res, nil
}

// Pace blocks until the caller may fetch from kind: it reserves the next paced
// slot (honouring the per-source minimum interval and any rate-limit backoff) and
// waits until it. It returns ctx.Err() if the context ends first. A source with
// no configured interval and no active backoff returns immediately.
func (c *FeedCache) Pace(ctx context.Context, kind Kind) error {
	wait := c.reserve(kind)
	if wait <= 0 {
		return nil
	}
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// reserve claims kind's next paced slot and returns how long the caller must wait
// until it. Each caller gets a distinct slot one interval after the previous, so
// concurrent callers queue in order rather than all waking at once; a slot in the
// past (an idle source) returns a zero wait.
func (c *FeedCache) reserve(kind Kind) time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	lim := c.limiterLocked(kind)
	now := c.clock()
	start := lim.nextAllowed
	if start.Before(now) {
		start = now
	}
	lim.nextAllowed = start.Add(c.intervalOf(kind))
	return start.Sub(now)
}

// intervalOf returns kind's configured minimum interval (never negative).
func (c *FeedCache) intervalOf(kind Kind) time.Duration {
	if c.MinInterval == nil {
		return 0
	}
	if d := c.MinInterval(kind); d > 0 {
		return d
	}
	return 0
}

// noteRateLimited grows kind's backoff (doubling, capped) and pushes its
// next-allowed instant out by the new backoff.
func (c *FeedCache) noteRateLimited(kind Kind) {
	c.mu.Lock()
	defer c.mu.Unlock()
	lim := c.limiterLocked(kind)
	max := c.MaxBackoff
	if max <= 0 {
		max = defaultMaxBackoff
	}
	if lim.backoff <= 0 {
		lim.backoff = time.Second
	} else if lim.backoff < max {
		lim.backoff *= 2
	}
	if lim.backoff > max {
		lim.backoff = max
	}
	when := c.clock().Add(lim.backoff)
	if when.After(lim.nextAllowed) {
		lim.nextAllowed = when
	}
}

// noteOK clears kind's accrued backoff after a successful fetch.
func (c *FeedCache) noteOK(kind Kind) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.limiterLocked(kind).backoff = 0
}

// limiterLocked returns kind's limiter, creating it. Caller holds c.mu.
func (c *FeedCache) limiterLocked(kind Kind) *srcLimiter {
	lim := c.limiters[kind]
	if lim == nil {
		lim = &srcLimiter{}
		c.limiters[kind] = lim
	}
	return lim
}
