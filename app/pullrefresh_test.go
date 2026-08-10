package app

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/go-news-reader/reader/internal/settings"
	"github.com/go-news-reader/reader/source"
)

// chanProv is a test provider keyed by the requested channel: it returns the
// scripted items for that channel (tagging each with the channel + source, like
// the real Reddit provider) and counts per-channel calls so a test can assert
// exactly which subscription was re-fetched.
type chanProv struct {
	kind   source.Kind
	byChan map[string][]source.Item
	err    error
	mu     sync.Mutex
	calls  map[string]int
}

func (c *chanProv) Kind() source.Kind { return c.kind }
func (c *chanProv) Feed(_ context.Context, q source.Query) (source.Result, error) {
	c.mu.Lock()
	if c.calls == nil {
		c.calls = map[string]int{}
	}
	c.calls[q.Channel]++
	c.mu.Unlock()
	if c.err != nil {
		return source.Result{}, c.err
	}
	src := c.byChan[q.Channel]
	tagged := make([]source.Item, len(src))
	for i, it := range src {
		it.Source = c.kind
		it.Channel = q.Channel
		tagged[i] = it
	}
	return source.Result{Items: tagged, Cursor: "cur-" + q.Channel}, nil
}

func (c *chanProv) callCount(ch string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls[ch]
}

// twoSubApp builds an inline (synchronous) app with two Reddit subscriptions
// r/a and r/b backed by prov.
func twoSubApp(t *testing.T, prov source.Provider) *App {
	t.Helper()
	set := &settings.Settings{
		Profiles: []settings.Profile{{Name: "Home", Subs: []source.Subscription{
			{Source: source.Reddit, Channel: "r/a", Limit: 25},
			{Source: source.Reddit, Channel: "r/b", Limit: 25},
		}}},
	}
	set.Normalize()
	a := New(Config{Settings: set, Width: 400, Height: 300})
	a.reg = source.NewRegistry()
	a.reg.Register(prov)
	return a
}

// TestPullRefreshSingleSubReplacesOnlyThatSub: with the feed filtered to r/a,
// pull-to-refresh re-fetches ONLY r/a — replacing its items, leaving r/b's
// untouched, never consulting r/b's provider, and resetting r/a's cursor.
func TestPullRefreshSingleSubReplacesOnlyThatSub(t *testing.T) {
	prov := &chanProv{kind: source.Reddit, byChan: map[string][]source.Item{
		"r/a": {{ID: "a1", Created: 10}},
		"r/b": {{ID: "b1", Created: 5}},
	}}
	a := twoSubApp(t, prov)
	a.RefreshStreaming(context.Background()) // seed: a1 (r/a) + b1 (r/b)
	if !eqIDs(a.Items(), "a1", "b1") {
		t.Fatalf("seed items = %v, want [a1 b1]", ids(a.Items()))
	}

	// Fresh content lands for r/a; r/b's provider must not be consulted.
	prov.byChan["r/a"] = []source.Item{{ID: "a2", Created: 20}}
	bCalls := prov.callCount("r/b")
	a.Scene().SetActive(0) // select r/a in the sidebar

	if errs := a.PullRefresh(context.Background()); errs != nil {
		t.Fatalf("PullRefresh errs = %v", errs)
	}
	// r/a replaced (a1 -> a2), r/b kept (b1); newest-first: a2(20), b1(5).
	if !eqIDs(a.Items(), "a2", "b1") {
		t.Fatalf("after pull items = %v, want [a2 b1]", ids(a.Items()))
	}
	if got := prov.callCount("r/b"); got != bCalls {
		t.Fatalf("r/b re-fetched (%d -> %d); pull-refresh must scope to r/a", bCalls, got)
	}
	a.moreMu.Lock()
	cur := a.cursors[source.SubKey(source.Subscription{Source: source.Reddit, Channel: "r/a", Limit: 25})]
	a.moreMu.Unlock()
	if cur != "cur-r/a" {
		t.Fatalf("r/a cursor = %q, want cur-r/a (reset from the fresh page)", cur)
	}
}

// TestPullRefreshAllFilterFullRefresh: with no single subscription selected
// ("All"), pull-to-refresh re-aggregates every source.
func TestPullRefreshAllFilterFullRefresh(t *testing.T) {
	prov := &chanProv{kind: source.Reddit, byChan: map[string][]source.Item{
		"r/a": {{ID: "a1", Created: 10}},
		"r/b": {{ID: "b1", Created: 5}},
	}}
	a := twoSubApp(t, prov)
	a.RefreshStreaming(context.Background())
	a.Scene().SetActive(-1) // AllFilter: no single sub in view
	aBefore, bBefore := prov.callCount("r/a"), prov.callCount("r/b")

	a.PullRefresh(context.Background())

	if prov.callCount("r/a") <= aBefore || prov.callCount("r/b") <= bBefore {
		t.Fatalf("All-filter pull-refresh must re-fetch every sub: r/a %d->%d, r/b %d->%d",
			aBefore, prov.callCount("r/a"), bBefore, prov.callCount("r/b"))
	}
}

// TestPullRefreshOutOfRangeFilterFullRefresh: an Active index past the current
// subs falls back to the full refresh rather than indexing out of bounds.
func TestPullRefreshOutOfRangeFilterFullRefresh(t *testing.T) {
	prov := &chanProv{kind: source.Reddit, byChan: map[string][]source.Item{
		"r/a": {{ID: "a1", Created: 10}}, "r/b": {{ID: "b1", Created: 5}},
	}}
	a := twoSubApp(t, prov)
	a.RefreshStreaming(context.Background())
	a.Scene().SetActive(99) // past len(subs)
	aBefore := prov.callCount("r/a")
	a.PullRefresh(context.Background())
	if prov.callCount("r/a") <= aBefore {
		t.Fatalf("out-of-range filter should full-refresh (re-fetch r/a), calls %d unchanged", aBefore)
	}
}

// TestPullRefreshSingleSubErrorKeepsFeed: a failed single-sub refresh leaves the
// feed as it was and returns the error.
func TestPullRefreshSingleSubErrorKeepsFeed(t *testing.T) {
	prov := &chanProv{kind: source.Reddit, byChan: map[string][]source.Item{
		"r/a": {{ID: "a1", Created: 10}}, "r/b": {{ID: "b1", Created: 5}},
	}}
	a := twoSubApp(t, prov)
	a.RefreshStreaming(context.Background())
	prov.err = errors.New("429 Too Many Requests")
	a.Scene().SetActive(0)

	errs := a.PullRefresh(context.Background())
	if len(errs) == 0 {
		t.Fatal("want the fetch error surfaced")
	}
	if !eqIDs(a.Items(), "a1", "b1") {
		t.Fatalf("after failed pull items = %v, want unchanged [a1 b1]", ids(a.Items()))
	}
}

// TestPullRefreshSingleSubInitializesCursors: refreshing a single sub before any
// full refresh (cursors map still nil) initializes the map and records the
// fresh page's cursor without panicking.
func TestPullRefreshSingleSubInitializesCursors(t *testing.T) {
	prov := &chanProv{kind: source.Reddit, byChan: map[string][]source.Item{
		"r/a": {{ID: "a1", Created: 10}},
	}}
	a := twoSubApp(t, prov)
	a.moreMu.Lock()
	a.cursors = nil // no prior aggregation
	a.moreMu.Unlock()
	a.Scene().SetActive(0)

	if errs := a.PullRefresh(context.Background()); errs != nil {
		t.Fatalf("PullRefresh errs = %v", errs)
	}
	if !eqIDs(a.Items(), "a1") {
		t.Fatalf("items = %v, want [a1]", ids(a.Items()))
	}
	a.moreMu.Lock()
	cur := a.cursors[source.SubKey(source.Subscription{Source: source.Reddit, Channel: "r/a", Limit: 25})]
	a.moreMu.Unlock()
	if cur != "cur-r/a" {
		t.Fatalf("cursor = %q, want cur-r/a (map initialized)", cur)
	}
}
