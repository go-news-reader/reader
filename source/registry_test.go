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

// gateProvider blocks in Feed until its release channel is closed, so a test can
// deterministically control the order in which subscriptions complete.
type gateProvider struct {
	kind    Kind
	items   []Item
	err     error
	release chan struct{}
}

func (g *gateProvider) Kind() Kind { return g.kind }
func (g *gateProvider) Feed(_ context.Context, _ Query) (Result, error) {
	<-g.release
	if g.err != nil {
		return Result{}, g.err
	}
	return Result{Items: g.items}, nil
}

func TestAggregateStreamIncremental(t *testing.T) {
	r := NewRegistry()
	relR := make(chan struct{})
	relM := make(chan struct{})
	r.Register(&gateProvider{kind: Reddit, items: []Item{{ID: "r", Source: Reddit, Created: 100}}, release: relR})
	r.Register(&gateProvider{kind: Mastodon, items: []Item{{ID: "m", Source: Mastodon, Created: 300}}, release: relM})

	subs := []Subscription{{Source: Reddit}, {Source: Mastodon}}

	type snap struct {
		items       []string
		done, total int
		index       int
		src         Kind
		err         bool
	}
	updates := make(chan snap, 4)
	done := make(chan struct{})
	go func() {
		r.AggregateStream(context.Background(), subs, func(u StreamUpdate) {
			ids := make([]string, len(u.Items))
			for i, it := range u.Items {
				ids[i] = it.ID
			}
			updates <- snap{ids, u.Done, u.Total, u.Index, u.Sub.Source, u.Err != nil}
		})
		close(done)
	}()

	// Release Reddit first: the first update carries only Reddit's item.
	close(relR)
	u1 := <-updates
	if u1.done != 1 || u1.total != 2 || u1.index != 0 || u1.src != Reddit || u1.err {
		t.Fatalf("update 1 = %+v", u1)
	}
	if len(u1.items) != 1 || u1.items[0] != "r" {
		t.Fatalf("update 1 items = %v, want [r]", u1.items)
	}
	// Then Mastodon: the second update carries the full merged+sorted feed
	// (Mastodon@300 newest, then Reddit@100).
	close(relM)
	u2 := <-updates
	if u2.done != 2 || u2.total != 2 || u2.index != 1 || u2.src != Mastodon {
		t.Fatalf("update 2 = %+v", u2)
	}
	if len(u2.items) != 2 || u2.items[0] != "m" || u2.items[1] != "r" {
		t.Fatalf("update 2 items = %v, want [m r]", u2.items)
	}
	<-done
}

func TestAggregateStreamErrorSub(t *testing.T) {
	r := NewRegistry()
	boom := errors.New("boom")
	r.Register(&fakeProvider{kind: Reddit, items: []Item{{ID: "ok", Created: 1}}})
	r.Register(&fakeProvider{kind: Instagram, err: boom})

	subs := []Subscription{{Source: Reddit}, {Source: Instagram}}
	var lastItems []Item
	var sawErr error
	sawErrIdx := -1
	final := 0
	r.AggregateStream(context.Background(), subs, func(u StreamUpdate) {
		if u.Err != nil {
			sawErr = u.Err
			sawErrIdx = u.Index
		}
		if u.Done >= u.Total {
			lastItems = u.Items
			final++
		}
	})
	if final != 1 {
		t.Fatalf("terminal updates = %d, want 1", final)
	}
	// The erroring sub contributes no items; only the good one survives.
	if len(lastItems) != 1 || lastItems[0].ID != "ok" {
		t.Fatalf("final items = %+v", lastItems)
	}
	// The error is a *SubscriptionError wrapping boom, tagged with its sub index.
	var se *SubscriptionError
	if !errors.As(sawErr, &se) || !errors.Is(sawErr, boom) {
		t.Fatalf("err = %v, want wrapped boom", sawErr)
	}
	if sawErrIdx != 1 {
		t.Fatalf("err index = %d, want 1 (Instagram)", sawErrIdx)
	}
}

func TestAggregateStreamEmptyTerminal(t *testing.T) {
	r := NewRegistry()
	got := 0
	var u StreamUpdate
	r.AggregateStream(context.Background(), nil, func(up StreamUpdate) {
		got++
		u = up
	})
	if got != 1 {
		t.Fatalf("updates = %d, want exactly 1 terminal update", got)
	}
	if u.Done != 0 || u.Total != 0 || u.Index != -1 {
		t.Fatalf("terminal update = %+v, want done/total 0 and index -1", u)
	}
	if u.Items == nil || len(u.Items) != 0 {
		t.Fatalf("terminal items = %v, want non-nil empty", u.Items)
	}
}

// pagingProvider is a test Provider that pages by cursor: the incoming
// Query.Cursor selects a scripted Result (items + next cursor). It also records
// the last query it saw so a test can assert the cursor was forwarded, and counts
// calls so a test can assert an exhausted/missing cursor is never fetched.
type pagingProvider struct {
	kind  Kind
	byCur map[string]Result // incoming cursor -> page
	seen  Query
	calls int
}

func (p *pagingProvider) Kind() Kind { return p.kind }
func (p *pagingProvider) Feed(_ context.Context, q Query) (Result, error) {
	p.seen = q
	p.calls++
	return p.byCur[q.Cursor], nil
}

func TestSubKey(t *testing.T) {
	a := SubKey(Subscription{Source: Reddit, Channel: "golang"})
	b := SubKey(Subscription{Source: Reddit, Channel: "golang"})
	if a != b {
		t.Fatalf("SubKey not stable: %q vs %q", a, b)
	}
	// A different channel or source yields a different key, and the NUL separator
	// prevents "reddit"+"xy" colliding with "reddi"+"txy".
	if SubKey(Subscription{Source: Reddit, Channel: "programming"}) == a {
		t.Fatal("SubKey collides across channels")
	}
	if SubKey(Subscription{Source: Mastodon, Channel: "golang"}) == a {
		t.Fatal("SubKey collides across sources")
	}
	if SubKey(Subscription{Source: "reddit", Channel: "xy"}) ==
		SubKey(Subscription{Source: "reddi", Channel: "txy"}) {
		t.Fatal("SubKey lacks a separator (prefix collision)")
	}
}

func TestAggregateStreamCarriesCursor(t *testing.T) {
	r := NewRegistry()
	r.Register(&fakeProviderCursor{kind: Reddit, items: []Item{{ID: "a"}}, cursor: "next-a"})
	var got string
	seen := false
	r.AggregateStream(context.Background(), []Subscription{{Source: Reddit}}, func(u StreamUpdate) {
		if u.Index >= 0 {
			got = u.Cursor
			seen = true
		}
	})
	if !seen {
		t.Fatal("no per-sub update")
	}
	if got != "next-a" {
		t.Fatalf("StreamUpdate.Cursor = %q, want next-a", got)
	}
}

// fakeProviderCursor is fakeProvider plus a returned next-page cursor.
type fakeProviderCursor struct {
	kind   Kind
	items  []Item
	cursor string
}

func (f *fakeProviderCursor) Kind() Kind { return f.kind }
func (f *fakeProviderCursor) Feed(_ context.Context, _ Query) (Result, error) {
	return Result{Items: f.items, Cursor: f.cursor}, nil
}

func TestAggregateMoreAdvancesCursors(t *testing.T) {
	r := NewRegistry()
	rp := &pagingProvider{kind: Reddit, byCur: map[string]Result{
		"rc1": {Items: []Item{{ID: "r3", Source: Reddit, Created: 150}}, Cursor: "rc2"}, // more to come
	}}
	mp := &pagingProvider{kind: Mastodon, byCur: map[string]Result{
		"mc1": {Items: []Item{{ID: "m3", Source: Mastodon, Created: 250}}, Cursor: ""}, // exhausted
	}}
	r.Register(rp)
	r.Register(mp)

	subs := []Subscription{{Source: Reddit, Channel: "golang"}, {Source: Mastodon, Channel: "@go"}}
	cursors := map[string]string{
		SubKey(subs[0]): "rc1",
		SubKey(subs[1]): "mc1",
	}
	items, next, errs := r.AggregateMore(context.Background(), subs, cursors)
	if len(errs) != 0 {
		t.Fatalf("errs = %v", errs)
	}
	// Merge is newest-first: m3@250 before r3@150.
	if len(items) != 2 || items[0].ID != "m3" || items[1].ID != "r3" {
		t.Fatalf("items = %+v, want [m3 r3]", items)
	}
	// The forwarded cursor reached each provider.
	if rp.seen.Cursor != "rc1" || mp.seen.Cursor != "mc1" {
		t.Fatalf("providers saw cursors %q / %q", rp.seen.Cursor, mp.seen.Cursor)
	}
	// next reports each sub's fresh token: Reddit advances, Mastodon exhausts ("").
	if next[SubKey(subs[0])] != "rc2" {
		t.Fatalf("next reddit = %q, want rc2", next[SubKey(subs[0])])
	}
	if v, ok := next[SubKey(subs[1])]; !ok || v != "" {
		t.Fatalf("next mastodon = %q,%v, want present empty", v, ok)
	}
}

func TestAggregateMoreSkipsExhaustedAndMissing(t *testing.T) {
	r := NewRegistry()
	rp := &pagingProvider{kind: Reddit, byCur: map[string]Result{
		"rc1": {Items: []Item{{ID: "r2", Source: Reddit, Created: 10}}, Cursor: ""},
	}}
	mp := &pagingProvider{kind: Mastodon, byCur: map[string]Result{}}
	r.Register(rp)
	r.Register(mp)

	subs := []Subscription{
		{Source: Reddit, Channel: "golang"}, // has a live cursor
		{Source: Mastodon, Channel: "@go"},  // exhausted ("" cursor) -> skipped
		{Source: HackerNews, Channel: ""},   // missing from the map -> skipped
	}
	cursors := map[string]string{
		SubKey(subs[0]): "rc1",
		SubKey(subs[1]): "", // exhausted
	}
	items, next, errs := r.AggregateMore(context.Background(), subs, cursors)
	if len(errs) != 0 {
		t.Fatalf("errs = %v", errs)
	}
	if len(items) != 1 || items[0].ID != "r2" {
		t.Fatalf("items = %+v, want [r2]", items)
	}
	// Only the Reddit provider was fetched; Mastodon (exhausted) was skipped.
	if rp.calls != 1 {
		t.Fatalf("reddit calls = %d, want 1", rp.calls)
	}
	if mp.calls != 0 {
		t.Fatalf("mastodon calls = %d, want 0 (exhausted, skipped)", mp.calls)
	}
	// next only carries the paged sub.
	if len(next) != 1 {
		t.Fatalf("next = %v, want just the reddit key", next)
	}
}

func TestAggregateMoreEmpty(t *testing.T) {
	r := NewRegistry()
	// No subs at all.
	items, next, errs := r.AggregateMore(context.Background(), nil, nil)
	if items == nil || next == nil || errs == nil {
		t.Fatal("AggregateMore returned a nil slice/map; all must be non-nil")
	}
	if len(items) != 0 || len(next) != 0 || len(errs) != 0 {
		t.Fatalf("AggregateMore(nil) = %v, %v, %v", items, next, errs)
	}
	// Subs present but the cursor map is empty -> nothing to page.
	subs := []Subscription{{Source: Reddit, Channel: "golang"}}
	items2, next2, errs2 := r.AggregateMore(context.Background(), subs, map[string]string{})
	if len(items2) != 0 || len(next2) != 0 || len(errs2) != 0 {
		t.Fatalf("AggregateMore(empty map) = %v, %v, %v", items2, next2, errs2)
	}
}

func TestAggregateMoreErrorsInOrder(t *testing.T) {
	r := NewRegistry()
	boom := errors.New("boom")
	// Reddit is registered and pages fine; Instagram errors; TikTok is unregistered
	// (also errors). Their subscription order is Reddit, Instagram, TikTok.
	rp := &pagingProvider{kind: Reddit, byCur: map[string]Result{
		"rc1": {Items: []Item{{ID: "ok", Source: Reddit, Created: 5}}, Cursor: "rc2"},
	}}
	r.Register(rp)
	r.Register(&fakeProvider{kind: Instagram, err: boom})

	subs := []Subscription{
		{Source: Reddit, Channel: "golang"},
		{Source: Instagram, Channel: "nasa"},
		{Source: TikTok, Channel: "x"}, // unregistered
	}
	cursors := map[string]string{
		SubKey(subs[0]): "rc1",
		SubKey(subs[1]): "ic1",
		SubKey(subs[2]): "tc1",
	}
	items, next, errs := r.AggregateMore(context.Background(), subs, cursors)
	if len(items) != 1 || items[0].ID != "ok" {
		t.Fatalf("items = %+v, want [ok]", items)
	}
	if len(errs) != 2 {
		t.Fatalf("errs = %v, want 2", errs)
	}
	// Subscription order: Instagram before TikTok.
	var se0, se1 *SubscriptionError
	if !errors.As(errs[0], &se0) || se0.Sub.Source != Instagram {
		t.Fatalf("errs[0] = %v, want Instagram SubscriptionError", errs[0])
	}
	if !errors.As(errs[1], &se1) || se1.Sub.Source != TikTok {
		t.Fatalf("errs[1] = %v, want TikTok SubscriptionError", errs[1])
	}
	if !errors.Is(errs[0], boom) {
		t.Fatalf("errs[0] does not wrap boom: %v", errs[0])
	}
	// The good sub advanced; the failed subs report an exhausted ("") next cursor.
	if next[SubKey(subs[0])] != "rc2" {
		t.Fatalf("next reddit = %q, want rc2", next[SubKey(subs[0])])
	}
	if next[SubKey(subs[1])] != "" || next[SubKey(subs[2])] != "" {
		t.Fatalf("failed subs should report empty next cursors: %v", next)
	}
}

func TestSubscriptionMatches(t *testing.T) {
	s := Subscription{Source: Reddit, Channel: "r/golang"}
	if !s.Matches(Item{Source: Reddit, Channel: "r/golang"}) {
		t.Error("same source+channel should match")
	}
	if !s.Matches(Item{Source: Reddit, Channel: "R/GoLang"}) {
		t.Error("channel match must be case-insensitive")
	}
	if s.Matches(Item{Source: Reddit, Channel: "r/rust"}) {
		t.Error("different channel must not match")
	}
	if s.Matches(Item{Source: HackerNews, Channel: "r/golang"}) {
		t.Error("different source must not match")
	}
	all := Subscription{Source: Reddit} // blank channel = every item of the source
	if !all.Matches(Item{Source: Reddit, Channel: "anything"}) {
		t.Error("blank channel should match any item of the source")
	}
	if all.Matches(Item{Source: HackerNews}) {
		t.Error("blank channel is still source-scoped")
	}
}

func TestSortItemsExported(t *testing.T) {
	items := []Item{{ID: "a", Created: 1}, {ID: "c", Created: 3}, {ID: "b", Created: 3}}
	SortItems(items)
	// Newest first (Created desc); ties broken by ID ascending: b,c (Created 3) then a (1).
	if items[0].ID != "b" || items[1].ID != "c" || items[2].ID != "a" {
		t.Fatalf("SortItems order = %s%s%s, want b c a", items[0].ID, items[1].ID, items[2].ID)
	}
}
