package source

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

// Registry holds the configured providers, one per [Kind], and fans queries out
// across them. It is safe for concurrent use.
type Registry struct {
	mu        sync.RWMutex
	providers map[Kind]Provider

	// MaxConcurrent bounds how many source Feed calls run at once across an
	// aggregate fan-out. Without it a profile with hundreds of subscriptions
	// would open hundreds of simultaneous HTTP requests on every refresh —
	// exhausting the connection pool and tripping remote rate limits. Zero means
	// defaultConcurrency; the fan-out still enqueues every subscription, only the
	// number fetching at any instant is capped.
	MaxConcurrent int

	// Cache, when non-nil, adds a result cache and per-source pacing/backoff in
	// front of every [Registry.Feed] (see [FeedCache]): a subscription re-served
	// from cache is not re-fetched, a failed fetch falls back to the last good
	// result, and the aggregate fan-outs pace each source so a profile with
	// hundreds of subscriptions does not burst a source's API. Nil fetches
	// straight through, unchanged.
	Cache *FeedCache

	// MaxFetchPerAggregate caps how many subscriptions one aggregate fan-out
	// (AggregateStream / AggregateMore) actually NETWORK-fetches: past the cap, a
	// still-fresh subscription is served from cache as always, but a stale one is
	// shown from its last cached result rather than re-fetched, so viewing a
	// profile that follows thousands of accounts never walks the whole list at
	// once (the behaviour a platform reads as a bot). Requires Cache. Zero means
	// no cap — every subscription is fetched, as before.
	MaxFetchPerAggregate int
}

// defaultConcurrency bounds simultaneous source fetches when a Registry sets no
// MaxConcurrent — brisk enough to fill the feed quickly without flooding remote
// APIs (or the local socket pool) when a profile carries many subscriptions.
const defaultConcurrency = 8

// overFetchBudget reports whether a network fetch must be skipped because the
// aggregate has already used its MaxFetchPerAggregate network fetches; it consumes
// one budget slot per call. A zero cap means unlimited (never over budget).
func (r *Registry) overFetchBudget(used *int32) bool {
	if r.MaxFetchPerAggregate <= 0 {
		return false
	}
	return atomic.AddInt32(used, 1) > int32(r.MaxFetchPerAggregate)
}

// concLimit is the effective fan-out width: MaxConcurrent when positive, else the
// default. Always at least 1.
func (r *Registry) concLimit() int {
	if r.MaxConcurrent > 0 {
		return r.MaxConcurrent
	}
	return defaultConcurrency
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{providers: make(map[Kind]Provider)}
}

// Register adds p, replacing any existing provider for the same [Kind].
func (r *Registry) Register(p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[p.Kind()] = p
}

// Get returns the provider for kind and whether one is registered.
func (r *Registry) Get(kind Kind) (Provider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[kind]
	return p, ok
}

// Kinds returns the registered kinds sorted lexically, for stable iteration.
func (r *Registry) Kinds() []Kind {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ks := make([]Kind, 0, len(r.providers))
	for k := range r.providers {
		ks = append(ks, k)
	}
	sort.Slice(ks, func(i, j int) bool { return ks[i] < ks[j] })
	return ks
}

// Feed dispatches a single query to the provider for kind. It errors if no
// provider is registered for that kind.
func (r *Registry) Feed(ctx context.Context, kind Kind, q Query) (Result, error) {
	p, ok := r.Get(kind)
	if !ok {
		// An unregistered kind means an endpoint-gated provider (Mastodon
		// instance, Usenet server, …) was subscribed to but never configured, so
		// report it as a typed "needs configuration" signal the UI can act on.
		return Result{}, NeedsAuth(kind, "not configured")
	}
	// The cache keys on (kind, channel) only, so it holds a channel's FIRST page. A
	// paginated read (Cursor set) asks for a later page and must never be served —
	// or stored — under that key, or it would return the cached first page again
	// and stall infinite scroll; page it straight from the provider. Pacing/backoff
	// still apply: the aggregate worker calls Pace before this.
	if r.Cache != nil && q.Cursor == "" {
		return r.Cache.Feed(kind, q, func() (Result, error) { return p.Feed(ctx, q) })
	}
	return p.Feed(ctx, q)
}

// Subscription names one provider+channel the aggregator pulls from.
type Subscription struct {
	Source  Kind
	Channel string
	Sort    string
	Limit   int
}

// SubscriptionError reports that one subscription failed during [Registry.Aggregate].
type SubscriptionError struct {
	Sub Subscription
	Err error
}

func (e *SubscriptionError) Error() string {
	return fmt.Sprintf("source: %s/%s: %v", e.Sub.Source, e.Sub.Channel, e.Err)
}

// Unwrap exposes the underlying error for errors.Is/As.
func (e *SubscriptionError) Unwrap() error { return e.Err }

// StreamUpdate is one incremental result delivered by [Registry.AggregateStream]
// as a subscription completes. Items is the FULL merged+sorted (newest-first)
// feed accumulated across every subscription that has finished so far; Done and
// Total report progress; Index is the position in the subs slice of the
// subscription that just completed (-1 for the terminal update when there were
// no subscriptions); Sub is that completed subscription; and Err is its failure
// (a [*SubscriptionError]) or nil on success.
type StreamUpdate struct {
	Items       []Item
	Done, Total int
	Index       int
	Sub         Subscription
	Err         error
	// Cursor is the just-completed subscription's next-page token (from its
	// [Result.Cursor]) — feed it back through [Registry.AggregateMore] to append
	// the following page. Empty when the source is exhausted or the fetch failed.
	Cursor string
}

// SubKey is a stable per-subscription pagination key: the source kind and
// channel joined by a NUL, which cannot occur in either. Cursors returned by
// [Registry.AggregateStream]/[Registry.AggregateMore] are keyed by it so a
// caller can carry each subscription's next-page token across load-more calls.
func SubKey(s Subscription) string {
	return string(s.Source) + "\x00" + s.Channel
}

// Matches reports whether it belongs to this subscription: same source, and —
// for a channel-scoped subscription — the same channel (case-insensitive). A
// blank channel matches every item of the source. It is the item↔subscription
// predicate a single-subscription refresh uses to replace only that
// subscription's items in the merged feed.
func (s Subscription) Matches(it Item) bool {
	if it.Source != s.Source {
		return false
	}
	return s.Channel == "" || strings.EqualFold(it.Channel, s.Channel)
}

// SortItems orders items newest-first (by [Item.Created] descending; ties broken
// by ID for a stable order), in place — the same order [AggregateStream]
// produces, exported so a caller that merges a single subscription's fresh page
// into the feed can re-establish it.
func SortItems(items []Item) { sortItems(items) }

// sortItems orders items newest-first (by [Item.Created] descending; ties broken
// by ID for a stable order), in place.
func sortItems(items []Item) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Created != items[j].Created {
			return items[i].Created > items[j].Created // newest first
		}
		return items[i].ID < items[j].ID
	})
}

// AggregateStream fetches every subscription concurrently and streams the
// merged feed as each one completes. It invokes onUpdate exactly once per
// completed subscription — serialized on the calling goroutine, so onUpdate
// needs no locking — passing the full merged+sorted feed accumulated so far, the
// running done/total counts, and the subscription that just finished (with its
// error, if any). A failing subscription does not abort the others: its items
// are simply absent and its error rides along in the update. When subs is empty
// a single terminal update (Done==Total==0, Index -1) is delivered so callers
// can still clear any "loading" state. AggregateStream returns after the final
// onUpdate.
func (r *Registry) AggregateStream(ctx context.Context, subs []Subscription, onUpdate func(StreamUpdate)) {
	total := len(subs)
	if total == 0 {
		onUpdate(StreamUpdate{Items: []Item{}, Done: 0, Total: 0, Index: -1})
		return
	}

	type outcome struct {
		index  int
		sub    Subscription
		items  []Item
		cursor string
		err    error
	}
	ch := make(chan outcome, total)
	sem := make(chan struct{}, r.concLimit())
	var fetches int32 // network fetches used against MaxFetchPerAggregate
	for i, sub := range subs {
		go func(i int, sub Subscription) {
			if r.Cache != nil && !r.Cache.Fresh(sub.Source, sub.Channel) {
				// A stale sub past the per-aggregate fetch budget is served from its
				// last cached result instead of being re-fetched, so a profile that
				// follows thousands of accounts never walks the whole list at once.
				if r.overFetchBudget(&fetches) {
					res, _ := r.Cache.Cached(sub.Source, sub.Channel)
					ch <- outcome{index: i, sub: sub, items: res.Items, cursor: res.Cursor}
					return
				}
				// Pace real fetches per source BEFORE taking a concurrency slot, so a
				// paced wait never occupies the fan-out semaphore.
				if err := r.Cache.Pace(ctx, sub.Source); err != nil {
					ch <- outcome{index: i, sub: sub, err: &SubscriptionError{Sub: sub, Err: err}}
					return
				}
			}
			sem <- struct{}{}
			defer func() { <-sem }()
			res, err := r.Feed(ctx, sub.Source, Query{
				Channel: sub.Channel,
				Sort:    sub.Sort,
				Limit:   sub.Limit,
			})
			if err != nil {
				ch <- outcome{index: i, sub: sub, err: &SubscriptionError{Sub: sub, Err: err}}
				return
			}
			ch <- outcome{index: i, sub: sub, items: res.Items, cursor: res.Cursor}
		}(i, sub)
	}

	acc := []Item{}
	for done := 1; done <= total; done++ {
		o := <-ch
		if o.err == nil {
			acc = append(acc, o.items...)
		}
		merged := make([]Item, len(acc))
		copy(merged, acc)
		sortItems(merged)
		onUpdate(StreamUpdate{Items: merged, Done: done, Total: total, Index: o.index, Sub: o.sub, Err: o.err, Cursor: o.cursor})
	}
}

// AggregateMore fetches the NEXT page of every subscription that still has an
// unexhausted cursor, concurrently, and returns the new page merged newest-first.
// A subscription is paged only when cursors[SubKey(sub)] is a non-empty token
// (from a prior [StreamUpdate.Cursor] or an earlier AggregateMore); subscriptions
// with an empty or missing cursor are exhausted or never loaded and are skipped.
//
// items is the merged+sorted new page (never nil). next maps every paged
// subscription's [SubKey] to its fresh [Result.Cursor], so the caller learns
// exhaustion (a token that came back "") as well as advancement — merge it over
// the running cursor map. errs holds each paged subscription's failure in
// subscription order, wrapped in the same [*SubscriptionError] shape
// AggregateStream uses (never nil). A failing subscription does not abort the
// others: its items are absent and its next cursor is left "" (exhausted).
func (r *Registry) AggregateMore(ctx context.Context, subs []Subscription, cursors map[string]string) (items []Item, next map[string]string, errs []error) {
	items = []Item{}
	next = map[string]string{}
	errs = []error{}

	type outcome struct {
		index  int
		sub    Subscription
		items  []Item
		cursor string
		err    error
	}
	// Page only the subscriptions carrying a live cursor, preserving their order.
	type paged struct {
		index int
		sub   Subscription
		cur   string
	}
	var todo []paged
	for i, sub := range subs {
		if cur := cursors[SubKey(sub)]; cur != "" {
			todo = append(todo, paged{index: i, sub: sub, cur: cur})
		}
	}
	if len(todo) == 0 {
		return items, next, errs
	}

	ch := make(chan outcome, len(todo))
	sem := make(chan struct{}, r.concLimit())
	for _, p := range todo {
		go func(p paged) {
			// A next-page fetch always hits the network (it is never cache-served),
			// so pace it per source before taking a slot, exactly like the first
			// page — a scroll must not burst a source any more than a refresh does.
			if r.Cache != nil {
				if err := r.Cache.Pace(ctx, p.sub.Source); err != nil {
					ch <- outcome{index: p.index, sub: p.sub, err: &SubscriptionError{Sub: p.sub, Err: err}}
					return
				}
			}
			sem <- struct{}{}
			defer func() { <-sem }()
			res, err := r.Feed(ctx, p.sub.Source, Query{
				Channel: p.sub.Channel,
				Sort:    p.sub.Sort,
				Limit:   p.sub.Limit,
				Cursor:  p.cur,
			})
			if err != nil {
				ch <- outcome{index: p.index, sub: p.sub, err: &SubscriptionError{Sub: p.sub, Err: err}}
				return
			}
			ch <- outcome{index: p.index, sub: p.sub, items: res.Items, cursor: res.Cursor}
		}(p)
	}

	byIndex := make([]error, len(subs))
	acc := []Item{}
	for range todo {
		o := <-ch
		// Record the next-page cursor for every paged sub (even a failed one, which
		// stays "" = exhausted) so the caller's map converges on the real state.
		next[SubKey(o.sub)] = o.cursor
		if o.err != nil {
			byIndex[o.index] = o.err
			continue
		}
		acc = append(acc, o.items...)
	}
	sortItems(acc)
	items = acc

	for _, e := range byIndex {
		if e != nil {
			errs = append(errs, e)
		}
	}
	return items, next, errs
}

// Aggregate fetches every subscription concurrently and merges the results
// newest-first (by [Item.Created], descending; ties broken by ID for a stable
// order). A failing subscription does not abort the others: its error is
// returned in errs (in subscription order) and its items are simply absent. The
// returned slices are never nil. It is the blocking form of [AggregateStream].
func (r *Registry) Aggregate(ctx context.Context, subs []Subscription) (items []Item, errs []error) {
	items = []Item{}
	errs = []error{}

	// Collect errors by subscription index so the returned order is stable
	// (subscription order) regardless of completion order.
	byIndex := make([]error, len(subs))
	r.AggregateStream(ctx, subs, func(u StreamUpdate) {
		if u.Err != nil && u.Index >= 0 {
			byIndex[u.Index] = u.Err
		}
		if u.Done >= u.Total {
			items = u.Items
		}
	})
	for _, e := range byIndex {
		if e != nil {
			errs = append(errs, e)
		}
	}
	return items, errs
}
