package source

import (
	"context"
	"sync"
	"testing"
	"time"
)

// countProvider records how many times Feed was actually called (race-safe, since
// AggregateStream fans out across goroutines).
type countProvider struct {
	kind Kind
	mu   sync.Mutex
	n    int
}

func (p *countProvider) Kind() Kind { return p.kind }

func (p *countProvider) Feed(_ context.Context, q Query) (Result, error) {
	p.mu.Lock()
	p.n++
	p.mu.Unlock()
	return Result{Items: []Item{{ID: q.Channel}}}, nil
}

func (p *countProvider) calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.n
}

// TestRegistryFeedThroughCache: with a cache, repeated identical fetches hit the
// provider once.
func TestRegistryFeedThroughCache(t *testing.T) {
	r := NewRegistry()
	p := &countProvider{kind: Reddit}
	r.Register(p)
	r.Cache = NewFeedCache(time.Minute, nil)
	for i := 0; i < 3; i++ {
		if _, err := r.Feed(context.Background(), Reddit, Query{Channel: "a"}); err != nil {
			t.Fatalf("feed %d: %v", i, err)
		}
	}
	if p.calls() != 1 {
		t.Fatalf("provider called %d times, want 1 (cached)", p.calls())
	}
}

// TestAggregateStreamCachedAndPaced: the first pass fetches and caches every sub
// (through the pacing gate); a second pass finds them all fresh and re-fetches
// nothing (covers the fresh-skip branch).
func TestAggregateStreamCachedAndPaced(t *testing.T) {
	r := NewRegistry()
	p := &countProvider{kind: Instagram}
	r.Register(p)
	r.Cache = NewFeedCache(time.Minute, func(Kind) time.Duration { return time.Millisecond })
	subs := []Subscription{{Source: Instagram, Channel: "x"}, {Source: Instagram, Channel: "y"}}
	drain := func() { r.AggregateStream(context.Background(), subs, func(StreamUpdate) {}) }

	drain()
	if p.calls() != 2 {
		t.Fatalf("first pass calls = %d, want 2", p.calls())
	}
	drain()
	if p.calls() != 2 {
		t.Fatalf("second pass re-fetched: calls = %d, want 2 (all cached)", p.calls())
	}
}

// TestAggregateStreamPaceCancelled: when a paced fetch cannot proceed before the
// context ends, the subscription surfaces an error rather than fetching.
func TestAggregateStreamPaceCancelled(t *testing.T) {
	r := NewRegistry()
	p := &countProvider{kind: Instagram}
	r.Register(p)
	r.Cache = NewFeedCache(0, func(Kind) time.Duration { return time.Hour }) // long pace, no freshness
	r.Cache.now = time.Now
	_ = r.Cache.Pace(context.Background(), Instagram) // reserve a slot an hour out

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var errs int
	r.AggregateStream(ctx, []Subscription{{Source: Instagram, Channel: "z"}}, func(u StreamUpdate) {
		if u.Err != nil {
			errs++
		}
	})
	if errs == 0 {
		t.Fatal("a cancelled pace should surface a subscription error")
	}
	if p.calls() != 0 {
		t.Fatalf("no fetch should happen when pacing is cancelled: calls = %d", p.calls())
	}
}
