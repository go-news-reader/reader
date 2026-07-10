package source

import (
	"context"
	"errors"
	"testing"
)

// fakeProvider is a test Provider returning canned items or an error.
type fakeProvider struct {
	kind  Kind
	items []Item
	err   error
	seen  Query // last query received
}

func (f *fakeProvider) Kind() Kind { return f.kind }

func (f *fakeProvider) Feed(_ context.Context, q Query) (Result, error) {
	f.seen = q
	if f.err != nil {
		return Result{}, f.err
	}
	return Result{Items: f.items}, nil
}

func TestRegistryRegisterGetKinds(t *testing.T) {
	r := NewRegistry()
	if _, ok := r.Get(Reddit); ok {
		t.Fatal("empty registry returned a provider")
	}
	if got := r.Kinds(); len(got) != 0 {
		t.Fatalf("empty Kinds() = %v", got)
	}

	r.Register(&fakeProvider{kind: Mastodon})
	r.Register(&fakeProvider{kind: Reddit})
	// Re-register Reddit: replaces, does not duplicate.
	replacement := &fakeProvider{kind: Reddit, items: []Item{{ID: "x"}}}
	r.Register(replacement)

	p, ok := r.Get(Reddit)
	if !ok || p != replacement {
		t.Fatalf("Get(Reddit) = %v, %v; want the replacement", p, ok)
	}
	// Kinds are sorted lexically: mastodon < reddit.
	want := []Kind{Mastodon, Reddit}
	got := r.Kinds()
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("Kinds() = %v, want %v", got, want)
	}
}

func TestRegistryFeed(t *testing.T) {
	r := NewRegistry()
	fp := &fakeProvider{kind: Reddit, items: []Item{{ID: "a"}}}
	r.Register(fp)

	res, err := r.Feed(context.Background(), Reddit, Query{Channel: "golang", Sort: "hot", Limit: 5})
	if err != nil {
		t.Fatalf("Feed: %v", err)
	}
	if len(res.Items) != 1 || res.Items[0].ID != "a" {
		t.Fatalf("Feed items = %v", res.Items)
	}
	if fp.seen.Channel != "golang" || fp.seen.Sort != "hot" || fp.seen.Limit != 5 {
		t.Fatalf("provider saw query %+v", fp.seen)
	}

	// Unregistered kind errors.
	if _, err := r.Feed(context.Background(), TikTok, Query{}); err == nil {
		t.Fatal("Feed on unregistered kind: want error")
	}
}

func TestAggregateMergesNewestFirst(t *testing.T) {
	r := NewRegistry()
	r.Register(&fakeProvider{kind: Reddit, items: []Item{
		{ID: "r1", Source: Reddit, Created: 100},
		{ID: "r2", Source: Reddit, Created: 300},
	}})
	r.Register(&fakeProvider{kind: Mastodon, items: []Item{
		{ID: "m1", Source: Mastodon, Created: 200},
		{ID: "m2", Source: Mastodon, Created: 300}, // ties r2 on Created
	}})

	items, errs := r.Aggregate(context.Background(), []Subscription{
		{Source: Reddit, Channel: "golang"},
		{Source: Mastodon, Channel: "@go"},
	})
	if len(errs) != 0 {
		t.Fatalf("unexpected errs: %v", errs)
	}
	// Newest first; the 300-tie breaks by ID (m2 < r2).
	wantOrder := []string{"m2", "r2", "m1", "r1"}
	if len(items) != len(wantOrder) {
		t.Fatalf("got %d items, want %d", len(items), len(wantOrder))
	}
	for i, id := range wantOrder {
		if items[i].ID != id {
			t.Fatalf("items[%d].ID = %q, want %q (full: %+v)", i, items[i].ID, id, items)
		}
	}
}

func TestAggregatePartialFailure(t *testing.T) {
	r := NewRegistry()
	boom := errors.New("boom")
	r.Register(&fakeProvider{kind: Reddit, items: []Item{{ID: "ok", Created: 1}}})
	r.Register(&fakeProvider{kind: Instagram, err: boom})

	subs := []Subscription{
		{Source: Reddit, Channel: "golang"},
		{Source: Instagram, Channel: "nasa"},
		{Source: TikTok, Channel: "x"}, // unregistered -> also errors
	}
	items, errs := r.Aggregate(context.Background(), subs)

	if len(items) != 1 || items[0].ID != "ok" {
		t.Fatalf("items = %+v; want the one good item", items)
	}
	if len(errs) != 2 {
		t.Fatalf("errs = %v; want 2", errs)
	}
	// The Instagram error wraps boom and is a *SubscriptionError.
	var se *SubscriptionError
	found := false
	for _, e := range errs {
		if errors.As(e, &se) && errors.Is(e, boom) {
			found = true
			if se.Sub.Source != Instagram {
				continue
			}
			if se.Error() == "" {
				t.Fatal("SubscriptionError.Error() empty")
			}
		}
	}
	if !found {
		t.Fatalf("no wrapped boom in errs: %v", errs)
	}
}

func TestAggregateEmpty(t *testing.T) {
	r := NewRegistry()
	items, errs := r.Aggregate(context.Background(), nil)
	if items == nil || errs == nil {
		t.Fatal("Aggregate returned nil slices; must be non-nil")
	}
	if len(items) != 0 || len(errs) != 0 {
		t.Fatalf("Aggregate(nil) = %v, %v", items, errs)
	}
}
