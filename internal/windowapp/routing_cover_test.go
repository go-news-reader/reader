package windowapp

import (
	"testing"

	"github.com/go-news-reader/reader/source"
	"github.com/go-news-reader/reader/ui"
)

// TestRouteClearDownloads covers the HitClearDownloads route: the panel's Clear
// button drops finished rows.
func TestRouteClearDownloads(t *testing.T) {
	a := newApp(t)
	a.Scene().SetSubs(nil)
	a.Scene().SetDownloads([]ui.DownloadItem{{ID: "x", Name: "x", Status: ui.DLDone}})
	h := New(a)
	click(t, h, ui.HitClearDownloads)
	if len(a.Scene().Downloads()) != 0 {
		t.Fatalf("Clear should empty the panel, still %d", len(a.Scene().Downloads()))
	}
}

// TestRouteClearRenderCache covers the HitClearRenderCache route: the Settings
// view's "Clear render cache" button invokes ClearRenderCache (which reports on
// the status line and leaves the cache empty).
func TestRouteClearRenderCache(t *testing.T) {
	a := profApp(t)
	h := New(a)
	a.VM().OpenSettings.Execute()
	click(t, h, ui.HitClearRenderCache)
	if pages, _ := a.RenderCacheStats(); pages != 0 {
		t.Fatalf("cache should be empty after clear: pages=%d", pages)
	}
	if got := a.Scene().Status; got != "Render cache cleared" {
		t.Fatalf("status = %q, want the clear confirmation", got)
	}
}

// TestRouteTypeRune covers the Key default: a printable rune is inserted into the
// focused search field.
func TestRouteTypeRune(t *testing.T) {
	a := newApp(t)
	h := New(a)
	a.VM().FocusSearch(true)
	h.Key("", 'q')
	if got := a.Scene().Search(); got != "q" {
		t.Fatalf("typed rune = %q, want q", got)
	}
}

// TestRouteRedditSearch covers the Reddit search view's Hit routes in one light
// pass (no heavy render): open from the sidebar, focus the query/regex fields,
// switch tabs, run the search (seam), subscribe to a subreddit result and save a
// post keyword search, then return to the feed.
func TestRouteRedditSearch(t *testing.T) {
	a := profApp(t)
	searchFired := false
	a.SetSearchFetchHook(func(string, bool) { searchFired = true })
	h := New(a)
	s := a.Scene()

	// Sidebar "Search Reddit" entry opens the search view.
	click(t, h, ui.HitSearchReddit)
	if s.Mode() != ui.ModeSearch {
		t.Fatalf("mode = %v, want search", s.Mode())
	}
	// Focus the query field.
	click(t, h, ui.HitFocusSearchQuery)
	if !s.SearchQueryFocused() {
		t.Fatal("query field not focused")
	}
	// Switch to posts and back to subreddits.
	clickValue(t, h, ui.HitSearchTab, "posts")
	if !s.SearchTabPosts() {
		t.Fatal("posts tab not selected")
	}
	clickValue(t, h, ui.HitSearchTab, "subreddits")
	if s.SearchTabPosts() {
		t.Fatal("subreddits tab not selected")
	}
	// Focus the regexp filter (subreddits tab only).
	click(t, h, ui.HitFocusSearchRegex)
	if !s.SearchRegexFocused() {
		t.Fatal("regex field not focused")
	}
	// Type a query, then run the search (fires the seam).
	s.FocusSearchQuery(true)
	h.Key("", 'g')
	h.Key("", 'o')
	click(t, h, ui.HitRunSearch)
	if !searchFired {
		t.Fatal("Run did not fire the search seam")
	}
	// Subscribe to a subreddit result.
	s.SetSubredditResults([]source.SubredditResult{{Name: "golang"}})
	click(t, h, ui.HitSubscribeSubreddit)
	if !routeHasSub(s, "r/golang") {
		t.Fatalf("r/golang not subscribed: %+v", s.ActiveProfile().Subs)
	}
	// Save the post keyword search (posts tab, query present).
	clickValue(t, h, ui.HitSearchTab, "posts")
	click(t, h, ui.HitSubscribePostSearch)
	if !routeHasSub(s, "search:go") {
		t.Fatalf("search:go not subscribed: %+v", s.ActiveProfile().Subs)
	}
	// Back to the feed.
	click(t, h, ui.HitCloseSearch)
	if s.Mode() != ui.ModeFeed {
		t.Fatalf("mode = %v, want feed", s.Mode())
	}
}

func routeHasSub(s *ui.Scene, channel string) bool {
	for _, su := range s.ActiveProfile().Subs {
		if su.Source == source.Reddit && su.Channel == channel {
			return true
		}
	}
	return false
}
