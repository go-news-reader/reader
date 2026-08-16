package app

import (
	"testing"

	"github.com/go-news-reader/reader/source"
	"github.com/go-news-reader/reader/ui"
)

func tabApp(subs ...source.Subscription) *App {
	return New(Config{Subscriptions: subs, Width: 400, Height: 300})
}

func TestOpenDefaultTab(t *testing.T) {
	a := tabApp(source.Subscription{Source: source.Reddit, Channel: "golang"})
	a.OpenDefaultTab()
	if got := a.scene.ActiveFilter(); got != 0 {
		t.Errorf("OpenDefaultTab active = %d, want 0 (first source)", got)
	}
	b := tabApp() // no subscriptions
	b.OpenDefaultTab()
	if got := b.scene.ActiveFilter(); got != ui.AllFilter {
		t.Errorf("no subs: active = %d, want ui.AllFilter(%d)", got, ui.AllFilter)
	}
}

func TestTabToLoad(t *testing.T) {
	r := source.Subscription{Source: source.Reddit, Channel: "golang"}
	hn := source.Subscription{Source: source.HackerNews, Channel: "top"}
	a := tabApp(r, hn)

	// A source never fetched needs a single-source load.
	if sub, one, all := a.tabToLoad(0); !one || all || sub != r {
		t.Fatalf("unfetched source: sub=%v one=%v all=%v", sub, one, all)
	}
	// Once its cursor is recorded (even ""), it is loaded — nothing to do.
	a.cursors = map[string]string{source.SubKey(r): ""}
	if _, one, all := a.tabToLoad(0); one || all {
		t.Fatalf("loaded source: one=%v all=%v, want neither", one, all)
	}
	// "All" while another source is still unfetched needs a full aggregate.
	if _, one, all := a.tabToLoad(ui.AllFilter); one || !all {
		t.Fatalf("All partial: one=%v all=%v, want all", one, all)
	}
	// "All" with every source fetched needs nothing.
	a.cursors[source.SubKey(hn)] = ""
	if _, one, all := a.tabToLoad(ui.AllFilter); one || all {
		t.Fatalf("All fully loaded: one=%v all=%v, want neither", one, all)
	}
}

func TestLoadTabDrivesSeams(t *testing.T) {
	r := source.Subscription{Source: source.Reddit, Channel: "golang"}
	hn := source.Subscription{Source: source.HackerNews, Channel: "top"}
	a := tabApp(r, hn)
	var got source.Subscription
	var one, all int
	a.tabFetch = func(sub source.Subscription) { got = sub; one++ }
	a.allFetch = func() { all++ }

	a.loadTab(0) // unfetched source -> one-source fetch
	if one != 1 || got != r || all != 0 {
		t.Fatalf("loadTab(0): one=%d got=%v all=%d", one, got, all)
	}
	a.loadTab(ui.AllFilter) // hn still unfetched -> aggregate
	if all != 1 {
		t.Fatalf("loadTab(All): all=%d, want 1", all)
	}
	// A tab already loaded fetches nothing.
	a.cursors = map[string]string{source.SubKey(r): "", source.SubKey(hn): ""}
	one, all = 0, 0
	a.loadTab(0)
	a.loadTab(ui.AllFilter)
	if one != 0 || all != 0 {
		t.Fatalf("loaded tabs re-fetched: one=%d all=%d", one, all)
	}
}
