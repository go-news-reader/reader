package app

import (
	"context"
	"strings"

	"github.com/go-news-reader/reader/source"
)

// redditSearcher is the narrow slice of the Reddit provider the search view
// drives: discover subreddits by keyword and search posts by keyword. The
// provider registry's Reddit entry implements it.
type redditSearcher interface {
	SearchSubreddits(ctx context.Context, query string) ([]source.SubredditResult, error)
	SearchPosts(ctx context.Context, query, subreddit string) ([]source.Item, error)
}

// RunSearch reads the search view's current query + tab on the render thread,
// marks the view loading, and kicks off the async search through the (test-
// substitutable) searchFetch seam. A blank query just reports a friendly hint
// instead of hitting the network.
func (a *App) RunSearch() {
	query := a.scene.SearchQuery()
	posts := a.scene.SearchTabPosts()
	if query == "" {
		a.scene.SetSearchStatus("Type something to search for")
		return
	}
	a.scene.SetSearchLoading(true)
	a.searchFetch(query, posts)
}

// RunRedditSearch performs a subreddit- or post-search against the Reddit
// provider and delivers the results to the search view on the render thread (via
// a.post). A missing/incompatible Reddit provider, or a search error, is reported
// on the view's status line. The default searchFetch runs this on its own
// goroutine; tests call it directly.
func (a *App) RunRedditSearch(ctx context.Context, query string, posts bool) {
	prov, ok := a.reg.Get(source.Reddit)
	se, isSe := prov.(redditSearcher)
	if !ok || !isSe {
		a.post(func() { a.scene.SetSearchStatus("Reddit is not configured") })
		return
	}
	if strings.TrimSpace(query) == "" {
		a.post(func() { a.scene.SetSearchStatus("Type something to search for") })
		return
	}
	if posts {
		items, err := se.SearchPosts(ctx, query, "")
		a.post(func() {
			if err != nil {
				a.scene.SetSearchStatus("Search failed: " + err.Error())
				return
			}
			a.scene.SetPostResults(items)
		})
		return
	}
	subs, err := se.SearchSubreddits(ctx, query)
	a.post(func() {
		if err != nil {
			a.scene.SetSearchStatus("Search failed: " + err.Error())
			return
		}
		a.scene.SetSubredditResults(subs)
	})
}

// SubscribeSubreddit adds a subreddit result as an r/<name> subscription on the
// active profile (dedup + normalization handled by the scene), then persists and
// re-aggregates. A no-op (already subscribed / no active profile) skips the
// re-aggregate.
func (a *App) SubscribeSubreddit(name string) {
	if a.scene.SubscribeActive(source.Reddit, "r/"+name) {
		a.ApplySceneSettings()
	}
}

// SubscribePostSearch saves a post keyword search as a live "search:<query>"
// subscription on the active profile so fresh matches keep flowing into the
// aggregated feed, then persists and re-aggregates. A blank query, an existing
// subscription, or no active profile is a no-op.
func (a *App) SubscribePostSearch(query string) {
	query = strings.TrimSpace(query)
	if query == "" {
		return
	}
	if a.scene.SubscribeActive(source.Reddit, "search:"+query) {
		a.ApplySceneSettings()
	}
}

// SetSearchFetchHook overrides the async Reddit search (tests use a synchronous
// variant for determinism).
func (a *App) SetSearchFetchHook(f func(query string, posts bool)) { a.searchFetch = f }
