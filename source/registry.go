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
		return Result{}, fmt.Errorf("source: no provider registered for %q", kind)
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

// Aggregate fetches every subscription concurrently and merges the results
// newest-first (by [Item.Created], descending; ties broken by ID for a stable
// order). A failing subscription does not abort the others: its error is
// returned in errs and its items are simply absent. The returned slices are
// never nil.
func (r *Registry) Aggregate(ctx context.Context, subs []Subscription) (items []Item, errs []error) {
	items = []Item{}
	errs = []error{}

	type outcome struct {
		items []Item
		err   error
	}
	results := make([]outcome, len(subs))
	var wg sync.WaitGroup
	for i, sub := range subs {
		wg.Add(1)
		go func(i int, sub Subscription) {
			defer wg.Done()
			res, err := r.Feed(ctx, sub.Source, Query{
				Channel: sub.Channel,
				Sort:    sub.Sort,
				Limit:   sub.Limit,
			})
			if err != nil {
				results[i] = outcome{err: &SubscriptionError{Sub: sub, Err: err}}
				return
			}
			results[i] = outcome{items: res.Items}
		}(i, sub)
	}
	wg.Wait()

	for _, o := range results {
		if o.err != nil {
			errs = append(errs, o.err)
			continue
		}
		items = append(items, o.items...)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Created != items[j].Created {
			return items[i].Created > items[j].Created // newest first
		}
		return items[i].ID < items[j].ID
	})
	return items, errs
}
