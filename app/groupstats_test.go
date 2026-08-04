package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-news-reader/reader/source"
)

// fakeStatProv is a Usenet provider that also samples group stats.
type fakeStatProv struct {
	st    source.GroupStats
	err   error
	calls int
	done  chan struct{}
}

func (f *fakeStatProv) Kind() source.Kind { return source.Usenet }
func (f *fakeStatProv) Feed(context.Context, source.Query) (source.Result, error) {
	return source.Result{}, nil
}
func (f *fakeStatProv) GroupStats(context.Context, string, int) (source.GroupStats, error) {
	f.calls++
	if f.done != nil {
		close(f.done)
	}
	return f.st, f.err
}

func TestLoadGroupStats(t *testing.T) {
	st := source.GroupStats{Sampled: 100, Binaries: 70, Images: 50}
	a := New(Config{Registry: newReg(&fakeStatProv{st: st})})
	a.loadGroupStats(context.Background(), "g")
	if !a.Scene().HasGroupStats("g") {
		t.Fatal("successful scan should cache stats")
	}

	// Scan error → nothing cached.
	aErr := New(Config{Registry: newReg(&fakeStatProv{err: errors.New("x")})})
	aErr.loadGroupStats(context.Background(), "g")
	if aErr.Scene().HasGroupStats("g") {
		t.Fatal("a failed scan must not cache stats")
	}

	// No Usenet provider at all → no-op.
	New(Config{Registry: newReg()}).loadGroupStats(context.Background(), "g")

	// A Usenet provider that isn't a groupStatter → no-op.
	New(Config{Registry: newReg(fakeProv{kind: source.Usenet})}).loadGroupStats(context.Background(), "g")
}

func TestScanBrowseGroup(t *testing.T) {
	a := New(Config{Registry: newReg(&fakeStatProv{})})
	var scanned string
	a.SetGroupStatsFetchHook(func(name string) { scanned = name })

	// No browse selection yet → no-op.
	a.ScanBrowseGroup()
	if scanned != "" {
		t.Fatalf("scan without a selection fired for %q", scanned)
	}

	// Lay out a browser with a selected group.
	s := a.Scene()
	s.SetUsenetServer("news.x:119")
	s.SetBrowseGroups([]source.GroupInfo{{Name: "altbinpics", Count: 9}})
	s.OpenBrowse()
	a.Frame() // render → lays out the browse tree rows
	name, ok := s.SelectedBrowseGroup()
	if !ok {
		t.Fatal("expected a selected browse group after layout")
	}
	a.ScanBrowseGroup()
	if scanned != name {
		t.Fatalf("scan fired for %q, want %q", scanned, name)
	}

	// Already cached → no re-scan.
	scanned = ""
	s.SetGroupStats(name, source.GroupStats{Sampled: 1})
	a.ScanBrowseGroup()
	if scanned != "" {
		t.Fatalf("cached group re-scanned (%q)", scanned)
	}
}

// TestGroupStatsDefaultAsync exercises the default (unhooked) groupStatsFetch
// closure — the `go loadGroupStats` line — with a fake provider (no network).
func TestGroupStatsDefaultAsync(t *testing.T) {
	done := make(chan struct{})
	a := New(Config{Registry: newReg(&fakeStatProv{st: source.GroupStats{Sampled: 10, Binaries: 5}, done: done})})
	a.DeferSceneWrites()
	a.groupStatsFetch("g") // default closure → go loadGroupStats
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("default groupStatsFetch never scanned")
	}
	ok := false
	for i := 0; i < 200 && !ok; i++ {
		a.drainScene()
		ok = a.Scene().HasGroupStats("g")
		if !ok {
			time.Sleep(time.Millisecond)
		}
	}
	if !ok {
		t.Fatal("default async scan did not deliver stats")
	}
}
