package app

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-news-reader/reader/feeds"
	"github.com/go-news-reader/reader/internal/settings"
	"github.com/go-news-reader/reader/source"
	"github.com/go-news-reader/reader/ui"
)

// fakeSubProv is a Reddit provider that also lists the account's subscriptions,
// so it satisfies both source.Provider and the app's redditSubImporter seam.
type fakeSubProv struct {
	subs []string
	err  error
}

func (f fakeSubProv) Kind() source.Kind { return source.Reddit }
func (f fakeSubProv) Feed(context.Context, source.Query) (source.Result, error) {
	return source.Result{}, nil
}
func (f fakeSubProv) MySubscriptions(context.Context) ([]string, error) {
	return f.subs, f.err
}

func newImportApp(t *testing.T, prov source.Provider, subs ...source.Subscription) (*App, string) {
	t.Helper()
	set := &settings.Settings{
		Profiles: []settings.Profile{{Name: "Home", Subs: subs}},
		Active:   0, Theme: settings.ThemeSystem,
	}
	path := filepath.Join(t.TempDir(), "s.json")
	a := New(Config{
		Registry: newReg(prov), Settings: set, Store: settings.NewStore(path),
		Options: feeds.Options{}, OS: ui.OSMac,
	})
	a.SetRefreshHook(func() {})
	return a, path
}

func TestImportRedditSubscriptionsSuccess(t *testing.T) {
	// r/golang is already subscribed, so importing it is a no-op; r/rust is new.
	prov := fakeSubProv{subs: []string{"r/golang", "r/rust"}}
	a, path := newImportApp(t, prov, source.Subscription{Source: source.Reddit, Channel: "r/golang"})

	n, err := a.ImportRedditSubscriptions(context.Background())
	if err != nil {
		t.Fatalf("import error: %v", err)
	}
	if n != 1 {
		t.Fatalf("added = %d, want 1 (r/rust new, r/golang dup)", n)
	}
	if got := a.VM().Status.Get(); !strings.Contains(got, "Imported 1 new subreddit") {
		t.Fatalf("status = %q, want to report 1 import", got)
	}
	// a.subs re-synced through ApplySceneSettings and persisted to disk.
	var hasRust bool
	for _, s := range a.subs {
		if s.Source == source.Reddit && strings.EqualFold(s.Channel, "r/rust") {
			hasRust = true
		}
	}
	if !hasRust {
		t.Fatalf("r/rust not in a.subs: %+v", a.subs)
	}
	loaded, err := settings.NewStore(path).Load()
	if err != nil {
		t.Fatal(err)
	}
	var persisted bool
	for _, s := range loaded.ActiveProfile().Subs {
		if s.Source == source.Reddit && strings.EqualFold(s.Channel, "r/rust") {
			persisted = true
		}
	}
	if !persisted {
		t.Fatalf("r/rust not persisted: %+v", loaded.ActiveProfile().Subs)
	}
}

func TestImportRedditSubscriptionsEmpty(t *testing.T) {
	a, _ := newImportApp(t, fakeSubProv{subs: nil})
	n, err := a.ImportRedditSubscriptions(context.Background())
	if err != nil || n != 0 {
		t.Fatalf("import = %d, %v; want 0, nil", n, err)
	}
	if got := a.VM().Status.Get(); !strings.Contains(got, "No Reddit subscriptions found") {
		t.Fatalf("status = %q, want the empty message", got)
	}
}

func TestImportRedditSubscriptionsNoProvider(t *testing.T) {
	// A plain Reddit provider that does NOT implement redditSubImporter.
	a, _ := newImportAppPlain(t)
	n, err := a.ImportRedditSubscriptions(context.Background())
	if !errors.Is(err, errNoRedditProvider) {
		t.Fatalf("err = %v, want errNoRedditProvider", err)
	}
	if n != 0 {
		t.Fatalf("added = %d, want 0", n)
	}
	if got := a.VM().Status.Get(); !strings.Contains(got, "Connect a Reddit account first") {
		t.Fatalf("status = %q, want the connect prompt", got)
	}
}

// newImportAppPlain builds an app whose only Reddit provider is a fakeProv,
// which lacks MySubscriptions, exercising the non-importer branch.
func newImportAppPlain(t *testing.T) (*App, string) {
	t.Helper()
	set := &settings.Settings{
		Profiles: []settings.Profile{{Name: "Home"}}, Active: 0, Theme: settings.ThemeSystem,
	}
	path := filepath.Join(t.TempDir(), "s.json")
	a := New(Config{
		Registry: newReg(fakeProv{kind: source.Reddit}), Settings: set,
		Store: settings.NewStore(path), Options: feeds.Options{}, OS: ui.OSMac,
	})
	a.SetRefreshHook(func() {})
	return a, path
}

func TestImportRedditSubscriptionsDeferred(t *testing.T) {
	// The window front-end defers scene writes: ImportRedditSubscriptions then
	// only enqueues the mutation (returning 0), which the render thread applies on
	// the next drain.
	prov := fakeSubProv{subs: []string{"r/rust"}}
	a, _ := newImportApp(t, prov)
	a.DeferSceneWrites()

	n, err := a.ImportRedditSubscriptions(context.Background())
	if err != nil {
		t.Fatalf("import error: %v", err)
	}
	if n != 0 {
		t.Fatalf("deferred import should return 0 (count applied on drain), got %d", n)
	}
	// Nothing applied yet — the mutation is still queued.
	if a.VM().Status.Get() != "" {
		t.Fatalf("deferred import must not mutate before drain: %q", a.VM().Status.Get())
	}
	a.drainScene() // render thread applies the queued import
	if got := a.VM().Status.Get(); !strings.Contains(got, "Imported 1 new subreddit") {
		t.Fatalf("status after drain = %q, want 1 import", got)
	}
}

func TestImportRedditSubscriptionsError(t *testing.T) {
	a, _ := newImportApp(t, fakeSubProv{err: errors.New("network down")})
	n, err := a.ImportRedditSubscriptions(context.Background())
	if err == nil {
		t.Fatal("want error propagated")
	}
	if n != 0 {
		t.Fatalf("added = %d, want 0", n)
	}
	if got := a.VM().Status.Get(); !strings.Contains(got, "Reddit import failed: network down") {
		t.Fatalf("status = %q, want the failure message", got)
	}
}
