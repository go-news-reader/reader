package windowapp

import (
	"testing"

	"github.com/go-news-reader/reader/app"
	"github.com/go-news-reader/reader/internal/settings"
	"github.com/go-news-reader/reader/source"
	"github.com/go-news-reader/reader/ui"
)

// browseHandler builds a Handler whose scene has a configured Usenet server (so
// the sidebar "Browse newsgroups" entry appears) and a small loaded group tree.
func browseHandler(t *testing.T) *Handler {
	t.Helper()
	set := &settings.Settings{
		Profiles: []settings.Profile{{Name: "Home"}},
		Active:   0, Theme: settings.ThemeSystem,
	}
	a := app.New(app.Config{Registry: source.NewRegistry(), Settings: set, Width: 1000, Height: 700})
	a.Scene().SetScale(1)
	a.SetRefreshHook(func() {})
	a.SetLoadGroupsHook(func(bool) {}) // no-op; individual tests override to observe
	a.Scene().SetUsenetServer("news.free.fr:119")
	a.Scene().SetBrowseGroups([]string{"alt.test", "alt.binaries.test", "comp.lang.go"})
	return New(a)
}

func TestRouteOpenBrowse(t *testing.T) {
	h := browseHandler(t)
	loaded := 0
	h.a.SetLoadGroupsHook(func(force bool) {
		if force {
			t.Fatal("opening the browser must not force-refresh")
		}
		loaded++
	})
	click(t, h, ui.HitBrowse) // the sidebar entry (feed mode)
	if h.a.Scene().Mode() != ui.ModeBrowse {
		t.Fatalf("mode = %v, want browse", h.a.Scene().Mode())
	}
	if loaded != 1 {
		t.Fatalf("LoadGroups called %d times, want 1", loaded)
	}
}

func TestRouteBrowseFilterFocus(t *testing.T) {
	h := browseHandler(t)
	h.a.VM().OpenBrowse.Execute()
	click(t, h, ui.HitBrowseFilter)
	if !h.a.Scene().BrowseFocused() {
		t.Fatal("filter field not focused")
	}
}

func TestRouteBrowseToggleNode(t *testing.T) {
	h := browseHandler(t)
	h.a.VM().OpenBrowse.Execute()
	click(t, h, ui.HitToggleBrowseNode) // top-level "alt" hierarchy
	if !h.a.Scene().BrowseNodeExpanded("alt") {
		t.Fatal("node not expanded after toggle")
	}
}

func TestRouteBrowseSubscribe(t *testing.T) {
	h := browseHandler(t)
	h.a.VM().OpenBrowse.Execute()
	h.a.Scene().ToggleBrowseNode("alt") // reveal the alt.* leaves
	click(t, h, ui.HitSubscribeGroup)
	if !h.a.Scene().IsSubscribed(source.Usenet, "alt.test") {
		t.Fatalf("leaf not subscribed; subs=%v", h.a.Scene().ActiveProfile().Subs)
	}
}

func TestRouteBrowseRefresh(t *testing.T) {
	h := browseHandler(t)
	h.a.VM().OpenBrowse.Execute()
	forced := false
	h.a.SetLoadGroupsHook(func(force bool) { forced = force })
	click(t, h, ui.HitBrowseRefresh)
	if !forced {
		t.Fatal("Refresh did not force a cache-bypassing re-fetch")
	}
	if h.a.Scene().BrowseFocused() {
		t.Fatal("Refresh should defocus the filter")
	}
}

func TestRouteBrowseBack(t *testing.T) {
	h := browseHandler(t)
	h.a.VM().OpenBrowse.Execute()
	h.a.Scene().FocusBrowseFilter(true)
	click(t, h, ui.HitCloseBrowse)
	if h.a.Scene().Mode() != ui.ModeFeed {
		t.Fatalf("mode = %v, want feed", h.a.Scene().Mode())
	}
	if h.a.Scene().BrowseFocused() {
		t.Fatal("back should defocus the filter")
	}
}

func TestRouteBrowseKeys(t *testing.T) {
	h := browseHandler(t)
	h.a.VM().OpenBrowse.Execute()
	h.a.Scene().FocusBrowseFilter(true)
	h.Key("Enter", 0) // defocuses the live-filtered field
	if h.a.Scene().BrowseFocused() {
		t.Fatal("Enter should defocus the filter")
	}
	h.a.Scene().FocusBrowseFilter(true)
	h.Key("Escape", 0) // returns to the feed
	if h.a.Scene().Mode() != ui.ModeFeed {
		t.Fatalf("Escape did not return to feed: %v", h.a.Scene().Mode())
	}
}
