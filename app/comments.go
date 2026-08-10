package app

import (
	"context"

	"github.com/go-news-reader/reader/source"
)

// commentFetcher is the optional provider capability that fetches a post's
// comment thread, flattened into display order. The Reddit provider satisfies it
// structurally; other providers do not, so the app type-asserts it and simply
// skips comment fetching for sources that lack it.
type commentFetcher interface {
	Comments(ctx context.Context, postID, subreddit string) ([]source.Comment, error)
}

// commentCacheMax bounds the per-item comment cache. It is a display-side memo,
// not a correctness store, so when it fills it is simply reset rather than
// evicted entry-by-entry.
const commentCacheMax = 64

// maybeFetchComments loads the comment thread for a freshly-selected preview
// item, but only for Reddit posts (the one source whose provider offers
// comments). A cached thread is delivered straight to the pane; otherwise the
// pane is marked loading and an async fetch is kicked off. Runs on the UI thread
// (from selectPreview), the same thread that drains the delivered result, so the
// cache access is race-free.
func (a *App) maybeFetchComments(it source.Item) {
	if it.Source != source.Reddit || it.ID == "" {
		return
	}
	if _, ok := a.commentProvider(); !ok {
		return // no comment-capable provider registered → nothing to fetch or show
	}
	if c, ok := a.commentCache[it.ID]; ok {
		a.scene.SetComments(it.ID, c) // already fetched (possibly empty) — no refetch
		return
	}
	a.scene.SetCommentsLoading(it.ID, true)
	a.commentFetch(it)
}

// commentProvider returns the registered comment-capable provider (today Reddit)
// and whether one exists. maybeFetchComments gates on it so a post is never
// marked loading — nor a fetch goroutine spawned — when nothing could serve the
// comments; fetchComments then uses it to perform the fetch.
func (a *App) commentProvider() (commentFetcher, bool) {
	prov, ok := a.reg.Get(source.Reddit)
	if !ok {
		return nil, false
	}
	cf, ok := prov.(commentFetcher)
	return cf, ok
}

// loadComments fetches it's comments off-thread and delivers them to the scene on
// the UI thread (through post), caching the result so a re-selection does not
// refetch. A fetch failure yields an empty thread (cached, so it is not retried
// on every reselect) rather than an error — comments are a nicety, not core.
func (a *App) loadComments(ctx context.Context, it source.Item) {
	comments := a.fetchComments(ctx, it)
	a.post(func() {
		a.cacheComments(it.ID, comments)
		a.scene.SetComments(it.ID, comments)
	})
}

// fetchComments asks the Reddit provider for it's flattened comment thread. It
// returns nil when the provider is absent, lacks the comment capability, or the
// fetch fails — the caller treats all three the same (no comments shown).
func (a *App) fetchComments(ctx context.Context, it source.Item) []source.Comment {
	cf, ok := a.commentProvider()
	if !ok {
		return nil
	}
	comments, err := cf.Comments(ctx, it.ID, it.Channel)
	if err != nil {
		return nil
	}
	return comments
}

// cacheComments memoises id's comments (UI thread only). When the cache is full
// it is reset wholesale — the entries are a cheap-to-rebuild display memo, so a
// blunt reset is preferable to per-entry eviction bookkeeping.
func (a *App) cacheComments(id string, comments []source.Comment) {
	if len(a.commentCache) >= commentCacheMax {
		a.commentCache = map[string][]source.Comment{}
	}
	a.commentCache[id] = comments
}

// SetCommentFetchHook overrides the async comment fetch (tests use a synchronous
// variant for determinism).
func (a *App) SetCommentFetchHook(f func(it source.Item)) { a.commentFetch = f }
