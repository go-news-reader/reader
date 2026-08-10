package windowapp

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/go-news-reader/reader/app"
	"github.com/go-news-reader/reader/internal/settings"
	"github.com/go-news-reader/reader/source"
	"github.com/go-news-reader/reader/ui"
)

// subImporterProv is a Reddit provider that lists a canned set of subscriptions
// and signals (once) when its MySubscriptions method is invoked, so the async
// dispatch can be awaited deterministically.
type subImporterProv struct {
	subs   []string
	called chan struct{}
}

func (p *subImporterProv) Kind() source.Kind { return source.Reddit }
func (p *subImporterProv) Feed(context.Context, source.Query) (source.Result, error) {
	return source.Result{}, nil
}
func (p *subImporterProv) MySubscriptions(context.Context) ([]string, error) {
	select {
	case p.called <- struct{}{}:
	default:
	}
	return p.subs, nil
}

func TestImportRedditSubsRouting(t *testing.T) {
	prov := &subImporterProv{subs: []string{"r/golang", "r/rust"}, called: make(chan struct{}, 1)}
	set := &settings.Settings{
		Profiles: []settings.Profile{{Name: "Home", Subs: []source.Subscription{
			{Source: source.Reddit, Channel: "r/golang"},
		}}},
		Active: 0, Theme: settings.ThemeSystem,
	}
	// A wide surface so all three Reddit buttons fit on the single row and the
	// hit-scan can find the import-subscriptions button.
	a := app.New(app.Config{Registry: newReg(prov), Settings: set, Width: 1500, Height: 800})
	a.Scene().SetScale(1)
	a.SetRefreshHook(func() {})
	// Defer scene writes so the async import only ENQUEUES mutations; the render
	// thread (this goroutine, via Frame) applies them under the queue lock — no race.
	a.DeferSceneWrites()

	h := New(a)
	s := a.Scene()

	click(t, h, ui.HitAccounts)
	h.Frame() // drain the deferred open-accounts mutation so the editor is live
	if s.SelectedAccount() != source.Reddit {
		t.Fatalf("accounts editor should default to Reddit, got %v", s.SelectedAccount())
	}
	click(t, h, ui.HitImportRedditSubs)

	// Wait for the background import to have run its network call.
	select {
	case <-prov.called:
	case <-time.After(2 * time.Second):
		t.Fatal("ImportRedditSubscriptions was not dispatched")
	}

	// Drain the queued scene mutations (bounded) and assert the new subreddit
	// landed and the status line reports the import.
	deadline := time.Now().Add(2 * time.Second)
	for {
		h.Frame() // drains queued scene writes on this goroutine
		if hasSub(s.Settings(), "r/rust") && strings.Contains(a.VM().Status.Get(), "Imported") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("import outcome not observed: subs=%+v status=%q",
				s.Settings().ActiveProfile().Subs, a.VM().Status.Get())
		}
		time.Sleep(5 * time.Millisecond)
	}
	// r/golang was already present, so exactly one new sub (r/rust) was added.
	if got := a.VM().Status.Get(); !strings.Contains(got, "Imported 1 new subreddit") {
		t.Fatalf("status = %q, want 1 import", got)
	}
}

func newReg(provs ...source.Provider) *source.Registry {
	r := source.NewRegistry()
	for _, p := range provs {
		r.Register(p)
	}
	return r
}

func hasSub(set *settings.Settings, channel string) bool {
	for _, s := range set.ActiveProfile().Subs {
		if s.Source == source.Reddit && strings.EqualFold(s.Channel, channel) {
			return true
		}
	}
	return false
}
