package app

import (
	"context"
	"errors"
	"testing"

	"github.com/go-news-reader/reader/feeds"
	"github.com/go-news-reader/reader/internal/settings"
	"github.com/go-news-reader/reader/source"
	"github.com/go-news-reader/reader/ui"
)

// fakeGrouper is a Usenet provider that also implements the grouper capability,
// recording whether the cached or the force-refresh path was taken.
type fakeGrouper struct {
	names       []string
	err         error
	groupsCalls int
	refreshCall int
}

func (f *fakeGrouper) Kind() source.Kind { return source.Usenet }
func (f *fakeGrouper) Feed(context.Context, source.Query) (source.Result, error) {
	return source.Result{}, nil
}
func (f *fakeGrouper) Groups(context.Context) ([]source.GroupInfo, error) {
	f.groupsCalls++
	return fakeInfos(f.names), f.err
}
func (f *fakeGrouper) RefreshGroups(context.Context) ([]source.GroupInfo, error) {
	f.refreshCall++
	return fakeInfos(f.names), f.err
}

// fakeInfos wraps bare names as GroupInfo (zero counts) for the fake grouper.
func fakeInfos(names []string) []source.GroupInfo {
	if names == nil {
		return nil
	}
	out := make([]source.GroupInfo, len(names))
	for i, n := range names {
		out[i] = source.GroupInfo{Name: n}
	}
	return out
}

// syncGroups wires a synchronous group-load hook for determinism.
func syncGroups(a *App) {
	a.SetLoadGroupsHook(func(force bool) { a.doLoadGroups(context.Background(), force) })
}

func TestLoadGroupsSuccess(t *testing.T) {
	fg := &fakeGrouper{names: []string{"alt.test", "comp.lang.go"}}
	a := New(Config{Registry: newReg(fg), Width: 500, Height: 400})
	syncGroups(a)

	a.LoadGroups()
	if fg.groupsCalls != 1 || fg.refreshCall != 0 {
		t.Fatalf("Groups=%d Refresh=%d, want 1/0", fg.groupsCalls, fg.refreshCall)
	}
	if got := a.Scene().BrowseGroups(); len(got) != 2 || got[0].Name != "alt.test" {
		t.Fatalf("browse groups = %v", got)
	}

	a.RefreshGroups()
	if fg.refreshCall != 1 {
		t.Fatalf("RefreshGroups path not taken: %d", fg.refreshCall)
	}
}

func TestLoadGroupsNoUsenetProvider(t *testing.T) {
	a := New(Config{Registry: newReg(fakeProv{kind: source.Reddit}), Width: 400, Height: 300})
	syncGroups(a)
	a.LoadGroups()
	if got := a.Scene().Status; got != "Usenet server not configured" {
		t.Fatalf("status = %q", got)
	}
}

func TestLoadGroupsProviderCannotList(t *testing.T) {
	// A Usenet provider that does NOT implement grouper.
	a := New(Config{Registry: newReg(fakeProv{kind: source.Usenet}), Width: 400, Height: 300})
	syncGroups(a)
	a.LoadGroups()
	if got := a.Scene().Status; got != "Usenet provider cannot list groups" {
		t.Fatalf("status = %q", got)
	}
}

func TestLoadGroupsError(t *testing.T) {
	fg := &fakeGrouper{err: errors.New("503 program fault")}
	a := New(Config{Registry: newReg(fg), Width: 400, Height: 300})
	syncGroups(a)
	a.LoadGroups()
	if got := a.Scene().Status; got == "" || got[:17] != "Group list failed" {
		t.Fatalf("status = %q", got)
	}
	if a.VM().Load.Get().Active {
		t.Fatal("loading indicator left on after error")
	}
}

func TestLoadGroupsAuthError(t *testing.T) {
	fg := &fakeGrouper{err: source.NeedsAuth(source.Usenet, "credentials required")}
	a := New(Config{Registry: newReg(fg), Width: 400, Height: 300})
	syncGroups(a)
	a.LoadGroups()
	if got := a.Scene().AuthPrompts(); len(got) != 1 || got[0].Kind != source.Usenet {
		t.Fatalf("auth prompts = %v", got)
	}
	if a.Scene().Status != "" {
		t.Fatalf("auth error must not set status: %q", a.Scene().Status)
	}
}

func TestSubscribeGroupAddsAndPersists(t *testing.T) {
	set := &settings.Settings{
		Profiles: []settings.Profile{{Name: "Home", Subs: nil}},
		Active:   0, Theme: settings.ThemeSystem,
	}
	a := New(Config{Registry: newReg(&fakeGrouper{}), Settings: set, Width: 400, Height: 300})
	refreshed := 0
	a.SetRefreshHook(func() { refreshed++ })

	a.SubscribeGroup("alt.binaries.test")
	if !a.Scene().IsSubscribed(source.Usenet, "alt.binaries.test") {
		t.Fatal("group not subscribed")
	}
	if refreshed != 1 {
		t.Fatalf("expected one re-aggregate, got %d", refreshed)
	}
	// A duplicate subscribe is a no-op (no extra refresh).
	a.SubscribeGroup("alt.binaries.test")
	if refreshed != 1 {
		t.Fatalf("duplicate subscribe re-aggregated: %d", refreshed)
	}
	// Unsubscribing removes it and re-aggregates once more.
	a.UnsubscribeGroup("alt.binaries.test")
	if a.Scene().IsSubscribed(source.Usenet, "alt.binaries.test") {
		t.Fatal("group still subscribed after unsubscribe")
	}
	if refreshed != 2 {
		t.Fatalf("expected re-aggregate on unsubscribe, got %d", refreshed)
	}
	// Unsubscribing an absent group is a no-op.
	a.UnsubscribeGroup("alt.binaries.test")
	if refreshed != 2 {
		t.Fatalf("absent unsubscribe re-aggregated: %d", refreshed)
	}
}

func TestNewAppliesUsenetServer(t *testing.T) {
	a := New(Config{
		Registry: newReg(&fakeGrouper{}),
		Options:  feeds.Options{UsenetAddr: "news.free.fr:119"},
		Width:    400, Height: 300,
	})
	if got := a.Scene().UsenetServer(); got != "news.free.fr:119" {
		t.Fatalf("scene usenet server = %q", got)
	}
}

func TestLoadGroupsDefaultHookAsync(t *testing.T) {
	// Exercise the default (goroutine) hook installed by New; no assertion on the
	// async result, just that the trigger path runs without panicking (mirrors
	// TestDefaultReconstructHook). The provider returns immediately.
	a := New(Config{Registry: newReg(&fakeGrouper{names: []string{"a.b"}}), Width: 400, Height: 300})
	a.LoadGroups() // spawns go a.doLoadGroups(..., false)
	_ = ui.ModeBrowse
}
