package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/go-news-reader/reader/internal/settings"
	"github.com/go-news-reader/reader/source"
)

// pageProv is a paging test provider: the incoming Query.Cursor selects a scripted
// Result (items + next cursor). It records the cursors it saw and counts calls so a
// test can assert pagination advanced and no extra fetch happened.
type pageProv struct {
	kind  source.Kind
	byCur map[string]source.Result
	mu    sync.Mutex
	seen  []string
	calls int
}

func (p *pageProv) Kind() source.Kind { return p.kind }
func (p *pageProv) Feed(_ context.Context, q source.Query) (source.Result, error) {
	p.mu.Lock()
	p.seen = append(p.seen, q.Cursor)
	p.calls++
	res := p.byCur[q.Cursor]
	p.mu.Unlock()
	return res, nil
}

func ids(items []source.Item) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.ID
	}
	return out
}

func eqIDs(a []source.Item, want ...string) bool {
	got := ids(a)
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// newPagingApp builds an app with a single paging Reddit subscription, its
// infinite-scroll effective setting forced to want, and inline (synchronous)
// aggregation.
func newPagingApp(t *testing.T, prov source.Provider, infinite bool) *App {
	t.Helper()
	on := infinite
	set := &settings.Settings{
		Profiles:       []settings.Profile{{Name: "Home", Subs: []source.Subscription{{Source: source.Reddit, Channel: "golang", Limit: 25}}}},
		InfiniteScroll: &on,
	}
	set.Normalize()
	a := New(Config{Settings: set, Width: 400, Height: 300})
	a.reg = source.NewRegistry()
	a.reg.Register(prov)
	return a
}

func TestLoadMoreAppendsAndDedups(t *testing.T) {
	prov := &pageProv{kind: source.Reddit, byCur: map[string]source.Result{
		"":   {Items: []source.Item{{ID: "a", Source: source.Reddit, Created: 3}, {ID: "b", Source: source.Reddit, Created: 2}}, Cursor: "c1"},
		"c1": {Items: []source.Item{{ID: "b", Source: source.Reddit, Created: 2}, {ID: "c", Source: source.Reddit, Created: 1}}, Cursor: "c2"},
	}}
	a := newPagingApp(t, prov, true)

	// First page via the streaming refresh: [a, b] and a live cursor c1.
	a.RefreshStreaming(context.Background())
	if !eqIDs(a.Items(), "a", "b") {
		t.Fatalf("after refresh items = %v, want [a b]", ids(a.Items()))
	}
	a.moreMu.Lock()
	if a.cursors[source.SubKey(source.Subscription{Source: source.Reddit, Channel: "golang", Limit: 25})] != "c1" {
		t.Fatalf("cursor after refresh = %v, want c1", a.cursors)
	}
	a.moreMu.Unlock()

	// LoadMore fetches page c1 ([b,c]); b is a duplicate and is dropped, c appended.
	errs := a.LoadMore(context.Background())
	if len(errs) != 0 {
		t.Fatalf("LoadMore errs = %v", errs)
	}
	if !eqIDs(a.Items(), "a", "b", "c") {
		t.Fatalf("after load-more items = %v, want [a b c] (deduped)", ids(a.Items()))
	}
	// The cursor advanced to c2, and the provider was asked for exactly "" then c1.
	a.moreMu.Lock()
	got := a.cursors[source.SubKey(source.Subscription{Source: source.Reddit, Channel: "golang", Limit: 25})]
	a.moreMu.Unlock()
	if got != "c2" {
		t.Fatalf("cursor after load-more = %q, want c2", got)
	}
	if len(prov.seen) != 2 || prov.seen[0] != "" || prov.seen[1] != "c1" {
		t.Fatalf("provider saw cursors %v, want [\"\" c1]", prov.seen)
	}
}

func TestLoadMoreDisabledIsNoop(t *testing.T) {
	prov := &pageProv{kind: source.Reddit, byCur: map[string]source.Result{
		"":   {Items: []source.Item{{ID: "a", Source: source.Reddit, Created: 3}}, Cursor: "c1"},
		"c1": {Items: []source.Item{{ID: "b", Source: source.Reddit, Created: 2}}, Cursor: ""},
	}}
	a := newPagingApp(t, prov, false) // infinite scroll OFF
	a.RefreshStreaming(context.Background())
	before := prov.calls
	if errs := a.LoadMore(context.Background()); errs != nil {
		t.Fatalf("LoadMore with infinite scroll off returned %v, want nil", errs)
	}
	if prov.calls != before {
		t.Fatalf("LoadMore fetched (%d -> %d) with infinite scroll off", before, prov.calls)
	}
	if !eqIDs(a.Items(), "a") {
		t.Fatalf("items changed while disabled: %v", ids(a.Items()))
	}
}

func TestLoadMoreNoCursorsIsNoop(t *testing.T) {
	prov := &pageProv{kind: source.Reddit, byCur: map[string]source.Result{}}
	a := newPagingApp(t, prov, true)
	// No RefreshStreaming yet -> no cursors recorded.
	if errs := a.LoadMore(context.Background()); errs != nil {
		t.Fatalf("LoadMore with no cursors returned %v, want nil", errs)
	}
	if prov.calls != 0 {
		t.Fatalf("LoadMore fetched with no live cursor (calls=%d)", prov.calls)
	}
}

func TestLoadMoreGuardBlocksReentry(t *testing.T) {
	prov := &pageProv{kind: source.Reddit, byCur: map[string]source.Result{
		"":   {Items: []source.Item{{ID: "a", Source: source.Reddit, Created: 3}}, Cursor: "c1"},
		"c1": {Items: []source.Item{{ID: "b", Source: source.Reddit, Created: 2}}, Cursor: ""},
	}}
	a := newPagingApp(t, prov, true)
	a.RefreshStreaming(context.Background())
	// Simulate an in-flight load-more: the guard makes a second call a no-op.
	a.moreMu.Lock()
	a.loadingMore = true
	a.moreMu.Unlock()
	before := prov.calls
	if errs := a.LoadMore(context.Background()); errs != nil {
		t.Fatalf("guarded LoadMore returned %v, want nil", errs)
	}
	if prov.calls != before {
		t.Fatalf("guarded LoadMore still fetched (%d -> %d)", before, prov.calls)
	}
}

func TestLoadMoreErrorsPropagate(t *testing.T) {
	// The first page is fine (records a cursor); the second page errors.
	prov := &errPageProv{kind: source.Reddit, boom: errors.New("boom")}
	a := newPagingApp(t, prov, true)
	a.RefreshStreaming(context.Background())
	errs := a.LoadMore(context.Background())
	if len(errs) != 1 {
		t.Fatalf("LoadMore errs = %v, want 1", errs)
	}
	var se *source.SubscriptionError
	if !errors.As(errs[0], &se) || !errors.Is(errs[0], prov.boom) {
		t.Fatalf("err = %v, want a SubscriptionError wrapping boom", errs[0])
	}
	// The feed is unchanged (the erroring page contributes nothing).
	if !eqIDs(a.Items(), "a") {
		t.Fatalf("items = %v, want [a] (error page dropped)", ids(a.Items()))
	}
}

// errPageProv returns a first page with a cursor, then errors on the next page.
type errPageProv struct {
	kind source.Kind
	boom error
}

func (p *errPageProv) Kind() source.Kind { return p.kind }
func (p *errPageProv) Feed(_ context.Context, q source.Query) (source.Result, error) {
	if q.Cursor == "" {
		return source.Result{Items: []source.Item{{ID: "a", Source: source.Reddit, Created: 3}}, Cursor: "c1"}, nil
	}
	return source.Result{}, p.boom
}

func TestPullRefreshResetsPagination(t *testing.T) {
	prov := &pageProv{kind: source.Reddit, byCur: map[string]source.Result{
		"":   {Items: []source.Item{{ID: "a", Source: source.Reddit, Created: 3}, {ID: "b", Source: source.Reddit, Created: 2}}, Cursor: "c1"},
		"c1": {Items: []source.Item{{ID: "c", Source: source.Reddit, Created: 1}}, Cursor: "c2"},
	}}
	a := newPagingApp(t, prov, true)
	a.RefreshStreaming(context.Background())
	a.LoadMore(context.Background())
	if !eqIDs(a.Items(), "a", "b", "c") {
		t.Fatalf("pre-refresh items = %v, want [a b c]", ids(a.Items()))
	}

	// PullRefresh re-aggregates from the first page and resets pagination.
	a.PullRefresh(context.Background())
	if !eqIDs(a.Items(), "a", "b") {
		t.Fatalf("after pull-refresh items = %v, want [a b] (reset)", ids(a.Items()))
	}
	a.moreMu.Lock()
	got := a.cursors[source.SubKey(source.Subscription{Source: source.Reddit, Channel: "golang", Limit: 25})]
	a.moreMu.Unlock()
	if got != "c1" {
		t.Fatalf("cursor after pull-refresh = %q, want c1 (first page again)", got)
	}
}

// TestFeedLoadingSeamsWired proves app.New installs the scene's OnReachBottom /
// OnPullRefresh callbacks and routes them through the substitutable seams.
func TestFeedLoadingSeamsWired(t *testing.T) {
	prov := &pageProv{kind: source.Reddit, byCur: map[string]source.Result{}}
	a := newPagingApp(t, prov, true)
	loadMore, pull := 0, 0
	a.SetLoadMoreHook(func() { loadMore++ })
	a.SetPullRefreshHook(func() { pull++ })

	if a.Scene().OnReachBottom == nil || a.Scene().OnPullRefresh == nil {
		t.Fatal("app.New did not install the scene feed-loading callbacks")
	}
	a.Scene().OnReachBottom()
	a.Scene().OnPullRefresh()
	if loadMore != 1 || pull != 1 {
		t.Fatalf("seams fired loadMore=%d pull=%d, want 1/1", loadMore, pull)
	}
}

// TestFeedLoadingDefaultSeams exercises the unhooked default seams installed by
// app.New — the `go a.LoadMore` / `go a.PullRefresh` lines.
func TestFeedLoadingDefaultSeams(t *testing.T) {
	// LoadMore default: with infinite scroll OFF, LoadMore returns at its first
	// guard, so the spawned goroutine touches no shared state.
	off := newPagingApp(t, &pageProv{kind: source.Reddit, byCur: map[string]source.Result{}}, false)
	off.loadMoreFetch() // default closure -> go a.LoadMore

	// PullRefresh default: spawn the goroutine and wait for the aggregation to reach
	// the provider. DeferSceneWrites keeps the background scene writes off the render
	// state the test never touches.
	fed := make(chan struct{}, 1)
	a := newPagingApp(t, signalProv{kind: source.Reddit, fed: fed}, true)
	a.DeferSceneWrites()
	a.pullRefreshFetch() // default closure -> go a.PullRefresh -> RefreshStreaming
	select {
	case <-fed:
	case <-time.After(5 * time.Second):
		t.Fatal("default pullRefresh never aggregated")
	}
}
