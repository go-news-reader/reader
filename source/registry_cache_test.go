package source

import (
	"context"
	"strconv"
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

// pageProvider returns a distinct item per page so a test can tell a cached first
// page from a freshly fetched next page.
type pageProvider struct {
	kind Kind
	mu   sync.Mutex
	n    int
}

func (p *pageProvider) Kind() Kind { return p.kind }

func (p *pageProvider) Feed(_ context.Context, q Query) (Result, error) {
	p.mu.Lock()
	p.n++
	p.mu.Unlock()
	if q.Cursor == "" {
		return Result{Items: []Item{{ID: "p1"}}, Cursor: "c1"}, nil // first page + next cursor
	}
	return Result{Items: []Item{{ID: "p2-" + q.Cursor}}}, nil // a later page
}

// TestPaginationBypassesCache: a fresh first-page cache entry must not be served
// for a paginated (cursor-set) read — that would stall infinite scroll.
func TestPaginationBypassesCache(t *testing.T) {
	r := NewRegistry()
	p := &pageProvider{kind: Reddit}
	r.Register(p)
	r.Cache = NewFeedCache(time.Hour, nil) // long TTL: the first page stays fresh
	// Prime + freshen the first page.
	if res, err := r.Feed(context.Background(), Reddit, Query{Channel: "a"}); err != nil || res.Items[0].ID != "p1" {
		t.Fatalf("first page = %+v, %v", res, err)
	}
	// A direct paginated Feed bypasses the fresh cache and returns the next page.
	res, err := r.Feed(context.Background(), Reddit, Query{Channel: "a", Cursor: "c1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 1 || res.Items[0].ID != "p2-c1" {
		t.Fatalf("paginated Feed = %+v, want the second page (cache must be bypassed)", res.Items)
	}
	// End to end, AggregateMore returns the next page, not the cached first page.
	sub := Subscription{Source: Reddit, Channel: "a"}
	items, _, errs := r.AggregateMore(context.Background(), []Subscription{sub}, map[string]string{SubKey(sub): "c1"})
	if len(errs) != 0 {
		t.Fatalf("AggregateMore errs: %v", errs)
	}
	if len(items) != 1 || items[0].ID != "p2-c1" {
		t.Fatalf("AggregateMore = %+v, want the second page", items)
	}
}

// TestAggregateMorePaceCancelled: AggregateMore paces next-page fetches, so a
// cancelled context during a paced wait surfaces as that subscription's error.
func TestAggregateMorePaceCancelled(t *testing.T) {
	r := NewRegistry()
	p := &pageProvider{kind: Instagram}
	r.Register(p)
	r.Cache = NewFeedCache(0, func(Kind) time.Duration { return time.Hour }) // long pace, real clock
	sub := Subscription{Source: Instagram, Channel: "a"}
	_ = r.Cache.Pace(context.Background(), Instagram) // reserve the immediate slot; next is +1h
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, errs := r.AggregateMore(ctx, []Subscription{sub}, map[string]string{SubKey(sub): "c1"})
	if len(errs) == 0 {
		t.Fatal("a cancelled paced fetch should surface an error, not silently drop")
	}
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

// TestAggregateStreamFetchBudget: past MaxFetchPerAggregate, stale subs are not
// re-fetched (here they have no cache, so they contribute nothing).
func TestAggregateStreamFetchBudget(t *testing.T) {
	r := NewRegistry()
	p := &countProvider{kind: Instagram}
	r.Register(p)
	r.Cache = NewFeedCache(0, nil) // nothing fresh → every sub is subject to the budget
	r.MaxFetchPerAggregate = 2
	subs := []Subscription{
		{Source: Instagram, Channel: "a"}, {Source: Instagram, Channel: "b"},
		{Source: Instagram, Channel: "c"}, {Source: Instagram, Channel: "d"},
	}
	r.AggregateStream(context.Background(), subs, func(StreamUpdate) {})
	if p.calls() != 2 {
		t.Fatalf("budget=2 network-fetched %d of 4 subs, want 2", p.calls())
	}
}

// TestAggregateStreamOverBudgetServesCache: over-budget stale subs are shown from
// their last cached result, so the merge stays complete while the network fetches
// stay capped.
func TestAggregateStreamOverBudgetServesCache(t *testing.T) {
	r := NewRegistry()
	p := &countProvider{kind: Instagram}
	r.Register(p)
	r.Cache = NewFeedCache(0, nil)
	subs := []Subscription{
		{Source: Instagram, Channel: "a"}, {Source: Instagram, Channel: "b"}, {Source: Instagram, Channel: "c"},
	}
	for _, s := range subs { // prime all three into the cache
		if _, err := r.Feed(context.Background(), s.Source, Query{Channel: s.Channel}); err != nil {
			t.Fatal(err)
		}
	}
	before := p.calls()
	r.MaxFetchPerAggregate = 1
	var merged int
	r.AggregateStream(context.Background(), subs, func(u StreamUpdate) { merged = len(u.Items) })
	if p.calls()-before != 1 {
		t.Fatalf("over-budget re-fetched: %d network fetches, want 1", p.calls()-before)
	}
	if merged != 3 {
		t.Fatalf("merged items = %d, want 3 (1 fetched + 2 from cache)", merged)
	}
}

// TestAggregateSpreadsBudgetAcrossSources: the budget is shared fairly across
// sources, so a source whose subscriptions sit LAST in the list is not starved.
func TestAggregateSpreadsBudgetAcrossSources(t *testing.T) {
	r := NewRegistry()
	ri := &countProvider{kind: Reddit}
	ig := &countProvider{kind: Instagram}
	r.Register(ri)
	r.Register(ig)
	r.Cache = NewFeedCache(0, nil) // nothing fresh → every sub competes for the budget
	r.MaxFetchPerAggregate = 4
	var subs []Subscription
	for i := 0; i < 10; i++ { // reddit first…
		subs = append(subs, Subscription{Source: Reddit, Channel: "r" + strconv.Itoa(i)})
	}
	for i := 0; i < 10; i++ { // …instagram entirely after the by-order budget.
		subs = append(subs, Subscription{Source: Instagram, Channel: "i" + strconv.Itoa(i)})
	}
	r.AggregateStream(context.Background(), subs, func(StreamUpdate) {})
	if ig.calls() == 0 {
		t.Fatalf("the spread starved instagram: reddit=%d instagram=%d", ri.calls(), ig.calls())
	}
	if got := ri.calls() + ig.calls(); got != 4 {
		t.Fatalf("total fetches = %d, want 4 (the budget)", got)
	}
}

// TestSpreadBudgetSkipsFreshSubs: a cache-fresh subscription costs no fetch and is
// excluded from the budget, so the slot goes to a stale one.
func TestSpreadBudgetSkipsFreshSubs(t *testing.T) {
	r := NewRegistry()
	p := &countProvider{kind: Reddit}
	r.Register(p)
	r.Cache = NewFeedCache(time.Hour, nil) // long TTL so a primed sub stays fresh
	r.MaxFetchPerAggregate = 1
	if _, err := r.Feed(context.Background(), Reddit, Query{Channel: "a"}); err != nil {
		t.Fatal(err) // prime "a" fresh
	}
	before := p.calls()
	// "a" is fresh (served free), "b" is stale — the single budget slot goes to "b".
	subs := []Subscription{{Source: Reddit, Channel: "a"}, {Source: Reddit, Channel: "b"}}
	r.AggregateStream(context.Background(), subs, func(StreamUpdate) {})
	if got := p.calls() - before; got != 1 {
		t.Fatalf("fetches = %d, want 1 (only the stale 'b'; 'a' was fresh)", got)
	}
}

// TestAggregateBudgetExceedsSubs: a budget larger than the (stale) subs fetches
// them all and stops (the round-robin's exhaustion break).
func TestAggregateBudgetExceedsSubs(t *testing.T) {
	r := NewRegistry()
	p := &countProvider{kind: Instagram}
	r.Register(p)
	r.Cache = NewFeedCache(0, nil)
	r.MaxFetchPerAggregate = 50
	subs := []Subscription{
		{Source: Instagram, Channel: "a"}, {Source: Instagram, Channel: "b"}, {Source: Instagram, Channel: "c"},
	}
	r.AggregateStream(context.Background(), subs, func(StreamUpdate) {})
	if p.calls() != 3 {
		t.Fatalf("budget above the sub count should fetch all 3, got %d", p.calls())
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
