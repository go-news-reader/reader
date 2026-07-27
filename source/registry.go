package source

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// Registry holds the configured providers, one per [Kind], and fans queries out
// across them. It is safe for concurrent use.
type Registry struct {
	mu        sync.RWMutex
	providers map[Kind]Provider
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
}

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
		index int
		sub   Subscription
		items []Item
		err   error
	}
	ch := make(chan outcome, total)
	for i, sub := range subs {
		go func(i int, sub Subscription) {
			res, err := r.Feed(ctx, sub.Source, Query{
				Channel: sub.Channel,
				Sort:    sub.Sort,
				Limit:   sub.Limit,
			})
			if err != nil {
				ch <- outcome{index: i, sub: sub, err: &SubscriptionError{Sub: sub, Err: err}}
				return
			}
			ch <- outcome{index: i, sub: sub, items: res.Items}
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
		onUpdate(StreamUpdate{Items: merged, Done: done, Total: total, Index: o.index, Sub: o.sub, Err: o.err})
	}
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
