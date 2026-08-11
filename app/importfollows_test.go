package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/go-news-reader/reader/internal/settings"
	"github.com/go-news-reader/reader/source"
)

// fakeFollowProv is a provider that lists the connected account's follows as
// ready subscriptions, so it satisfies source.FollowImporter.
type fakeFollowProv struct {
	kind source.Kind
	subs []source.Subscription
	err  error
}

func (f fakeFollowProv) Kind() source.Kind { return f.kind }
func (f fakeFollowProv) Feed(context.Context, source.Query) (source.Result, error) {
	return source.Result{}, nil
}
func (f fakeFollowProv) MyFollows(context.Context) ([]source.Subscription, error) {
	return f.subs, f.err
}

func TestImportFollowsSuccess(t *testing.T) {
	// r/golang is already subscribed (dup); r/rust and u/spez are new.
	prov := fakeFollowProv{kind: source.Reddit, subs: []source.Subscription{
		{Source: source.Reddit, Channel: "r/golang"},
		{Source: source.Reddit, Channel: "r/rust"},
		{Source: source.Reddit, Channel: "u/spez"},
	}}
	a, path := newImportApp(t, prov, source.Subscription{Source: source.Reddit, Channel: "r/golang"})

	n, err := a.ImportFollows(context.Background(), source.Reddit)
	if err != nil {
		t.Fatalf("import error: %v", err)
	}
	if n != 2 {
		t.Fatalf("added = %d, want 2 (r/rust + u/spez new, r/golang dup)", n)
	}
	if got := a.VM().Status.Get(); !strings.Contains(got, "Imported 2 new subscription(s) from Reddit") {
		t.Fatalf("status = %q, want to report 2 imports from Reddit", got)
	}
	// Both new subscriptions re-synced through ApplySceneSettings and persisted.
	if !hasSubIn(a.subs, source.Reddit, "r/rust") || !hasSubIn(a.subs, source.Reddit, "u/spez") {
		t.Fatalf("new subs not in a.subs: %+v", a.subs)
	}
	loaded, err := settings.NewStore(path).Load()
	if err != nil {
		t.Fatal(err)
	}
	if !hasSubIn(loaded.ActiveProfile().Subs, source.Reddit, "u/spez") {
		t.Fatalf("u/spez not persisted: %+v", loaded.ActiveProfile().Subs)
	}
}

func TestImportFollowsEmpty(t *testing.T) {
	a, _ := newImportApp(t, fakeFollowProv{kind: source.Reddit, subs: nil})
	n, err := a.ImportFollows(context.Background(), source.Reddit)
	if err != nil || n != 0 {
		t.Fatalf("import = %d, %v; want 0, nil", n, err)
	}
	if got := a.VM().Status.Get(); !strings.Contains(got, "No Reddit subscriptions found") {
		t.Fatalf("status = %q, want the empty message", got)
	}
}

func TestImportFollowsNoImporter(t *testing.T) {
	// A plain provider that does NOT implement source.FollowImporter, and a kind
	// with no provider at all — both take the same connect-prompt branch.
	a, _ := newImportAppPlain(t)
	n, err := a.ImportFollows(context.Background(), source.Reddit)
	if !errors.Is(err, errNoFollowImporter) {
		t.Fatalf("err = %v, want errNoFollowImporter", err)
	}
	if n != 0 {
		t.Fatalf("added = %d, want 0", n)
	}
	if got := a.VM().Status.Get(); !strings.Contains(got, "Connect a Reddit account first to import subscriptions") {
		t.Fatalf("status = %q, want the connect prompt", got)
	}

	// An unregistered kind hits the same branch, with that kind's label.
	if _, err := a.ImportFollows(context.Background(), source.Mastodon); !errors.Is(err, errNoFollowImporter) {
		t.Fatalf("unregistered kind err = %v, want errNoFollowImporter", err)
	}
	if got := a.VM().Status.Get(); !strings.Contains(got, "Connect a Mastodon account first") {
		t.Fatalf("status = %q, want the Mastodon connect prompt", got)
	}
}

func TestImportFollowsError(t *testing.T) {
	a, _ := newImportApp(t, fakeFollowProv{kind: source.Reddit, err: errors.New("network down")})
	n, err := a.ImportFollows(context.Background(), source.Reddit)
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

func TestImportFollowsDeferred(t *testing.T) {
	prov := fakeFollowProv{kind: source.Reddit, subs: []source.Subscription{
		{Source: source.Reddit, Channel: "u/spez"},
	}}
	a, _ := newImportApp(t, prov)
	a.DeferSceneWrites()

	n, err := a.ImportFollows(context.Background(), source.Reddit)
	if err != nil {
		t.Fatalf("import error: %v", err)
	}
	if n != 0 {
		t.Fatalf("deferred import should return 0 (count applied on drain), got %d", n)
	}
	if a.VM().Status.Get() != "" {
		t.Fatalf("deferred import must not mutate before drain: %q", a.VM().Status.Get())
	}
	a.drainScene()
	if got := a.VM().Status.Get(); !strings.Contains(got, "Imported 1 new subscription(s) from Reddit") {
		t.Fatalf("status after drain = %q, want 1 import", got)
	}
}

func TestFollowKindLabel(t *testing.T) {
	if got := followKindLabel(source.Reddit); got != "Reddit" {
		t.Fatalf("label(reddit) = %q, want Reddit", got)
	}
	if got := followKindLabel(source.Mastodon); got != "Mastodon" {
		t.Fatalf("label(mastodon) = %q, want Mastodon", got)
	}
	// A kind with no credential-schema entry falls back to the raw kind.
	if got := followKindLabel(source.Redgifs); got != string(source.Redgifs) {
		t.Fatalf("label(redgifs) = %q, want the raw kind", got)
	}
}

// hasSubIn reports whether subs contains a subscription of source kind k with
// channel ch (case-insensitive).
func hasSubIn(subs []source.Subscription, k source.Kind, ch string) bool {
	for _, s := range subs {
		if s.Source == k && strings.EqualFold(s.Channel, ch) {
			return true
		}
	}
	return false
}
