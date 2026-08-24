package app

import (
	"context"
	"strings"

	"github.com/go-news-reader/reader/source"
)

// redditSearcher is the slice of the Reddit provider the posts tab drives:
// keyword post-search. Channel discovery goes through the platform-agnostic
// source.Searcher instead, so it is not part of this interface.
type redditSearcher interface {
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

// RunSearchFetch performs the active search and delivers the results to the
// search view on the render thread (via a.post): on the posts tab a Reddit
// keyword post-search; otherwise channel discovery across EVERY provider that
// implements source.Searcher (Reddit subreddits, X accounts/hashtags, Instagram
// accounts/tags, ...), merged into one list — each source.ChannelResult carries
// its own platform, so a mixed list stays unambiguous. The default searchFetch
// runs this on its own goroutine; tests call it directly.
func (a *App) RunSearchFetch(ctx context.Context, query string, posts bool) {
	if strings.TrimSpace(query) == "" {
		a.post(func() { a.scene.SetSearchStatus("Type something to search for") })
		return
	}
	if posts {
		prov, ok := a.reg.Get(source.Reddit)
		se, isSe := prov.(redditSearcher)
		if !ok || !isSe {
			a.post(func() { a.scene.SetSearchStatus("Reddit is not configured") })
			return
		}
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
	var merged []source.ChannelResult
	var firstErr error
	found := false
	for _, k := range a.reg.Kinds() {
		prov, _ := a.reg.Get(k)
		se, ok := prov.(source.Searcher)
		if !ok {
			continue
		}
		found = true
		rs, err := se.SearchChannels(ctx, query)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		merged = append(merged, rs...)
	}
	a.post(func() {
		switch {
		case !found:
			a.scene.SetSearchStatus("No searchable sources are configured")
		case len(merged) == 0 && firstErr != nil:
			a.scene.SetSearchStatus("Search failed: " + firstErr.Error())
		default:
			a.scene.SetChannelResults(merged)
		}
	})
}

// SubscribeChannel adds a channel-discovery result — its handle already carrying
// the platform form (r/<name>, @account, #tag) — as a subscription on the active
// profile (dedup + normalization handled by the scene), then persists and
// re-aggregates. A no-op (already subscribed / no active profile) skips the
// re-aggregate.
func (a *App) SubscribeChannel(src source.Kind, channel string) {
	if a.scene.SubscribeActive(src, channel) {
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
