package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/go-news-reader/reader/source"
)

// fakeSearchProv is a Reddit provider that also implements redditSearcher.
type fakeSearchProv struct {
	subs       []source.SubredditResult
	subErr     error
	posts      []source.Item
	postErr    error
	sawSubQ    string
	sawPostQ   string
	sawPostSub string
	done       chan struct{}
}

func (f *fakeSearchProv) Kind() source.Kind { return source.Reddit }
func (f *fakeSearchProv) Feed(context.Context, source.Query) (source.Result, error) {
	return source.Result{}, nil
}
func (f *fakeSearchProv) SearchSubreddits(_ context.Context, q string) ([]source.SubredditResult, error) {
	f.sawSubQ = q
	if f.done != nil {
		close(f.done)
	}
	return f.subs, f.subErr
}
func (f *fakeSearchProv) SearchPosts(_ context.Context, q, sub string) ([]source.Item, error) {
	f.sawPostQ, f.sawPostSub = q, sub
	return f.posts, f.postErr
}

// searchApp builds an inline app with the given provider and an active profile,
// its scene already in the search view.
func searchApp(prov source.Provider) *App {
	a := New(Config{
		Registry:      newReg(prov),
		Subscriptions: []source.Subscription{{Source: source.Reddit, Channel: "r/seed"}},
	})
	a.SetRefreshHook(func() {}) // no async re-aggregate in these tests
	a.Scene().OpenSearch()
	return a
}

func TestRunRedditSearchSubreddits(t *testing.T) {
	prov := &fakeSearchProv{subs: []source.SubredditResult{{Name: "golang", Subscribers: 5}}}
	a := searchApp(prov)
	a.RunRedditSearch(context.Background(), "go", false)
	if prov.sawSubQ != "go" {
		t.Fatalf("provider saw query %q", prov.sawSubQ)
	}
	if got := a.Scene().SubredditResults(); len(got) != 1 || got[0].Name != "golang" {
		t.Fatalf("subreddit results not delivered: %+v", got)
	}
	if a.Scene().SearchTabPosts() {
		t.Fatal("delivering subreddits should select the subreddits tab")
	}
}

func TestRunRedditSearchPosts(t *testing.T) {
	prov := &fakeSearchProv{posts: []source.Item{{ID: "p1", Source: source.Reddit, Title: "hit"}}}
	a := searchApp(prov)
	a.RunRedditSearch(context.Background(), "generics", true)
	if prov.sawPostQ != "generics" || prov.sawPostSub != "" {
		t.Fatalf("provider saw q=%q sub=%q", prov.sawPostQ, prov.sawPostSub)
	}
	if got := a.Scene().PostResults(); len(got) != 1 || got[0].ID != "p1" {
		t.Fatalf("post results not delivered: %+v", got)
	}
	if !a.Scene().SearchTabPosts() {
		t.Fatal("delivering posts should select the posts tab")
	}
}

func TestRunRedditSearchErrors(t *testing.T) {
	// Subreddit search error → status line.
	a := searchApp(&fakeSearchProv{subErr: errors.New("boom")})
	a.RunRedditSearch(context.Background(), "go", false)
	if a.Scene().SearchStatus() == "" {
		t.Fatal("subreddit search error should set a status")
	}
	// Post search error → status line.
	a2 := searchApp(&fakeSearchProv{postErr: errors.New("nope")})
	a2.RunRedditSearch(context.Background(), "go", true)
	if a2.Scene().SearchStatus() == "" {
		t.Fatal("post search error should set a status")
	}
}

func TestRunRedditSearchNoProvider(t *testing.T) {
	// A Reddit provider that is not a searcher.
	a := searchApp(fakeProv{kind: source.Reddit})
	a.RunRedditSearch(context.Background(), "go", false)
	if a.Scene().SearchStatus() != "Reddit is not configured" {
		t.Fatalf("status = %q", a.Scene().SearchStatus())
	}
	// No Reddit provider at all.
	b := New(Config{Registry: newReg()})
	b.SetRefreshHook(func() {})
	b.RunRedditSearch(context.Background(), "go", false)
	if b.Scene().SearchStatus() != "Reddit is not configured" {
		t.Fatalf("no-provider status = %q", b.Scene().SearchStatus())
	}
}

func TestRunRedditSearchBlankQuery(t *testing.T) {
	prov := &fakeSearchProv{}
	a := searchApp(prov)
	a.RunRedditSearch(context.Background(), "   ", false)
	if a.Scene().SearchStatus() == "" {
		t.Fatal("blank query should set a status")
	}
	if prov.sawSubQ != "" {
		t.Fatal("blank query must not hit the provider")
	}
}

func TestRunSearchReadsSceneAndFiresSeam(t *testing.T) {
	a := searchApp(&fakeSearchProv{})
	s := a.Scene()
	s.FocusSearchQuery(true)
	for _, r := range "cats" {
		s.TypeRune(r)
	}
	s.SetSearchTab(true) // posts
	var gotQuery string
	var gotPosts bool
	a.SetSearchFetchHook(func(query string, posts bool) { gotQuery, gotPosts = query, posts })
	a.RunSearch()
	if gotQuery != "cats" || !gotPosts {
		t.Fatalf("seam saw query=%q posts=%v", gotQuery, gotPosts)
	}
	if !s.SearchLoading() {
		t.Fatal("RunSearch should mark the view loading")
	}
}

func TestRunSearchBlankQuery(t *testing.T) {
	a := searchApp(&fakeSearchProv{})
	fired := false
	a.SetSearchFetchHook(func(string, bool) { fired = true })
	a.RunSearch() // no query typed
	if fired {
		t.Fatal("blank query must not fire the fetch seam")
	}
	if a.Scene().SearchStatus() == "" {
		t.Fatal("blank query should set a hint")
	}
}

func TestSubscribeSubredditAddsAndReaggregates(t *testing.T) {
	a := searchApp(&fakeSearchProv{})
	refreshed := 0
	a.SetRefreshHook(func() { refreshed++ })
	a.SubscribeSubreddit("golang")
	subs := a.Scene().ActiveProfile().Subs
	if !hasSub(subs, source.Reddit, "r/golang") {
		t.Fatalf("r/golang not added: %+v", subs)
	}
	if refreshed != 1 {
		t.Fatalf("re-aggregate fired %d times, want 1", refreshed)
	}
	// A duplicate is a no-op (no extra re-aggregate).
	a.SubscribeSubreddit("golang")
	if refreshed != 1 {
		t.Fatalf("duplicate subscribe re-aggregated (%d)", refreshed)
	}
}

func TestSubscribePostSearchAddsChannel(t *testing.T) {
	a := searchApp(&fakeSearchProv{})
	refreshed := 0
	a.SetRefreshHook(func() { refreshed++ })
	a.SubscribePostSearch("  go generics  ")
	subs := a.Scene().ActiveProfile().Subs
	if !hasSub(subs, source.Reddit, "search:go generics") {
		t.Fatalf("search channel not added: %+v", subs)
	}
	if refreshed != 1 {
		t.Fatalf("re-aggregate fired %d times, want 1", refreshed)
	}
	// A blank query is a no-op.
	a.SubscribePostSearch("   ")
	if refreshed != 1 {
		t.Fatalf("blank save re-aggregated (%d)", refreshed)
	}
}

// TestSearchFetchDefaultAsync exercises the default (unhooked) searchFetch
// closure — the `go a.RunRedditSearch` line in New — with a fake provider.
func TestSearchFetchDefaultAsync(t *testing.T) {
	done := make(chan struct{})
	prov := &fakeSearchProv{subs: []source.SubredditResult{{Name: "golang"}}, done: done}
	a := New(Config{Registry: newReg(prov), Subscriptions: []source.Subscription{{Source: source.Reddit, Channel: "r/seed"}}})
	a.SetRefreshHook(func() {})
	a.DeferSceneWrites()
	a.Scene().OpenSearch()
	a.searchFetch("go", false) // default closure → go a.RunRedditSearch
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("default searchFetch never queried the provider")
	}
	ok := false
	for i := 0; i < 200 && !ok; i++ {
		a.drainScene()
		ok = len(a.Scene().SubredditResults()) == 1
		if !ok {
			time.Sleep(time.Millisecond)
		}
	}
	if !ok {
		t.Fatal("default async search did not deliver results")
	}
}

// hasSub reports whether subs contains a source+channel (case-insensitive).
func hasSub(subs []source.Subscription, k source.Kind, ch string) bool {
	for _, s := range subs {
		if s.Source == k && strings.EqualFold(s.Channel, ch) {
			return true
		}
	}
	return false
}
