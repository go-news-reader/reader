package app

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-news-reader/reader/source"
)

// commentProv is a Reddit provider that also serves a canned comment thread,
// counting Comments calls (so a re-selection can be proved not to refetch) and
// optionally signalling a channel each call (so the default async hook can be
// awaited).
type commentProv struct {
	kind     source.Kind
	items    []source.Item
	comments []source.Comment
	err      error
	calls    int32
	fed      chan struct{}
}

func (p *commentProv) Kind() source.Kind { return p.kind }
func (p *commentProv) Feed(context.Context, source.Query) (source.Result, error) {
	return source.Result{Items: p.items}, nil
}
func (p *commentProv) Comments(_ context.Context, _, _ string) ([]source.Comment, error) {
	atomic.AddInt32(&p.calls, 1)
	if p.fed != nil {
		select {
		case p.fed <- struct{}{}:
		default:
		}
	}
	if p.err != nil {
		return nil, p.err
	}
	return p.comments, nil
}

// syncComments makes the app fetch comments synchronously on the calling
// goroutine, for deterministic assertions.
func syncComments(a *App) {
	a.SetCommentFetchHook(func(it source.Item) { a.loadComments(context.Background(), it) })
}

// TestCommentsFetchAndCache: selecting a Reddit post fetches its comments, hands
// them to the scene, caches them, and a re-selection serves the cache without
// refetching.
func TestCommentsFetchAndCache(t *testing.T) {
	cp := &commentProv{kind: source.Reddit, comments: []source.Comment{
		{Author: "a", Body: "hi", Depth: 0}, {Author: "b", Body: "re", Depth: 1},
	}}
	a := New(Config{Registry: newReg(cp), Width: 1200, Height: 800})
	syncComments(a)

	it := source.Item{ID: "p1", Source: source.Reddit, Channel: "golang", Title: "t", Body: "self"}
	a.SelectPreview(it)

	cs, ok := a.Scene().PreviewComments("p1")
	if !ok || len(cs) != 2 || cs[0].Body != "hi" || cs[1].Depth != 1 {
		t.Fatalf("scene comments = %+v ok=%v", cs, ok)
	}
	if a.Scene().CommentsLoading("p1") {
		t.Fatal("loading flag should be cleared after delivery")
	}
	if got := a.commentCache["p1"]; len(got) != 2 {
		t.Fatalf("comments not cached: %+v", got)
	}
	if n := atomic.LoadInt32(&cp.calls); n != 1 {
		t.Fatalf("first select made %d fetches, want 1", n)
	}

	// Re-selecting the same post serves the cache — no second fetch.
	a.SelectPreview(it)
	if n := atomic.LoadInt32(&cp.calls); n != 1 {
		t.Fatalf("re-select refetched: %d calls", n)
	}
	if cs, ok := a.Scene().PreviewComments("p1"); !ok || len(cs) != 2 {
		t.Fatalf("re-select lost comments: %+v ok=%v", cs, ok)
	}
}

// TestCommentsErrorYieldsEmpty: a fetch failure delivers an empty (but present)
// thread, which is cached so it is not retried on every re-selection.
func TestCommentsErrorYieldsEmpty(t *testing.T) {
	cp := &commentProv{kind: source.Reddit, err: errors.New("network down")}
	a := New(Config{Registry: newReg(cp), Width: 1200, Height: 800})
	syncComments(a)

	it := source.Item{ID: "p1", Source: source.Reddit, Body: "self"}
	a.SelectPreview(it)

	cs, ok := a.Scene().PreviewComments("p1")
	if !ok {
		t.Fatal("scene should hold an (empty) fetched entry after an error")
	}
	if len(cs) != 0 {
		t.Fatalf("error should yield no comments, got %+v", cs)
	}
	a.SelectPreview(it) // cached empty → no refetch
	if n := atomic.LoadInt32(&cp.calls); n != 1 {
		t.Fatalf("error result not cached: %d calls", n)
	}
}

// TestCommentsOnlyReddit: a non-Reddit item, and a Reddit item with no id, never
// fetch and never mark the pane loading.
func TestCommentsOnlyReddit(t *testing.T) {
	cp := &commentProv{kind: source.Reddit, comments: []source.Comment{{Body: "x"}}}
	a := New(Config{Registry: newReg(cp), Width: 1200, Height: 800})
	called := false
	a.SetCommentFetchHook(func(source.Item) { called = true })

	a.SelectPreview(source.Item{ID: "h", Source: source.HackerNews, Body: "b"})
	if called {
		t.Fatal("a HackerNews item must not fetch comments")
	}
	if _, ok := a.Scene().PreviewComments("h"); ok {
		t.Fatal("non-Reddit item should have no comment entry")
	}
	if a.Scene().CommentsLoading("h") {
		t.Fatal("non-Reddit item must not mark the pane loading")
	}

	a.SelectPreview(source.Item{Source: source.Reddit, Body: "b"}) // empty id
	if called {
		t.Fatal("a Reddit item with no id must not fetch comments")
	}
}

// TestCommentsNoCapableProvider: selecting a Reddit item when no comment-capable
// provider is registered neither marks the pane loading nor spawns a fetch — the
// guard that keeps a fast, provider-less fetch from writing the scene off the
// render thread.
func TestCommentsNoCapableProvider(t *testing.T) {
	// A Reddit provider WITHOUT a Comments method (fakeProv) — not comment-capable.
	a := New(Config{Registry: newReg(fakeProv{kind: source.Reddit}), Width: 800, Height: 600})
	called := false
	a.SetCommentFetchHook(func(source.Item) { called = true })

	a.SelectPreview(source.Item{ID: "p", Source: source.Reddit, Body: "self"})
	if called {
		t.Fatal("no comment-capable provider → must not fetch")
	}
	if a.Scene().CommentsLoading("p") {
		t.Fatal("no comment-capable provider → must not mark loading")
	}
	if _, ok := a.Scene().PreviewComments("p"); ok {
		t.Fatal("no comment-capable provider → no comment entry")
	}
}

// TestFetchCommentsProviderVariants: with no Reddit provider, or a provider that
// lacks the comment capability, fetchComments returns nil (no comments shown).
func TestFetchCommentsProviderVariants(t *testing.T) {
	it := source.Item{ID: "p", Source: source.Reddit, Channel: "golang"}

	a := New(Config{Registry: newReg()}) // no Reddit provider at all
	if c := a.fetchComments(context.Background(), it); c != nil {
		t.Fatalf("absent provider should yield nil, got %+v", c)
	}

	// fakeProv is a Reddit provider WITHOUT a Comments method.
	a2 := New(Config{Registry: newReg(fakeProv{kind: source.Reddit})})
	if c := a2.fetchComments(context.Background(), it); c != nil {
		t.Fatalf("provider without comment capability should yield nil, got %+v", c)
	}
}

// TestCacheCommentsResetsWhenFull: the bounded cache is reset wholesale when it
// fills, then the new entry is inserted.
func TestCacheCommentsResetsWhenFull(t *testing.T) {
	a := New(Config{Registry: newReg()})
	for i := 0; i < commentCacheMax; i++ {
		a.cacheComments(fmt.Sprintf("k%d", i), nil)
	}
	if len(a.commentCache) != commentCacheMax {
		t.Fatalf("cache size = %d, want %d before overflow", len(a.commentCache), commentCacheMax)
	}
	a.cacheComments("overflow", []source.Comment{{Body: "x"}})
	if len(a.commentCache) != 1 {
		t.Fatalf("cache not reset on overflow: size = %d", len(a.commentCache))
	}
	if _, ok := a.commentCache["overflow"]; !ok {
		t.Fatal("overflow entry missing after reset")
	}
}

// TestDefaultCommentHook exercises the real (goroutine) comment fetch hook the
// window front-end uses: in deferred-scene mode the goroutine only enqueues its
// scene write, so draining via Frame applies it on this goroutine, race-free.
func TestDefaultCommentHook(t *testing.T) {
	fed := make(chan struct{}, 1)
	cp := &commentProv{kind: source.Reddit, comments: []source.Comment{{Body: "async reply"}}, fed: fed}
	a := New(Config{Registry: newReg(cp), Width: 800, Height: 600})
	a.DeferSceneWrites()

	a.SelectPreview(source.Item{ID: "p", Source: source.Reddit, Body: "self"})
	select {
	case <-fed:
	case <-time.After(2 * time.Second):
		t.Fatal("default comment hook did not run")
	}
	waitFor(t, func() bool {
		a.Frame()
		cs, ok := a.Scene().PreviewComments("p")
		return ok && len(cs) == 1 && cs[0].Body == "async reply"
	})
}
