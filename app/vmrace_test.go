package app

import (
	"context"
	"sync"
	"testing"

	"github.com/go-news-reader/reader/source"
)

// A1: overlapping background operations must not race on the (concurrency-unsafe)
// view-model observables. Before the vmu fix, two goroutines mutating vm.Items /
// vm.Status / vm.Load concurrently tripped `go test -race`. Run with -race.
func TestConcurrentVMMutationsNoRace(t *testing.T) {
	reg := newReg(fakeProv{kind: source.Reddit, items: []source.Item{
		{ID: "a", Source: source.Reddit, Created: 2},
		{ID: "b", Source: source.Reddit, Created: 1},
	}})
	a := New(Config{
		Registry:      reg,
		Subscriptions: []source.Subscription{{Source: source.Reddit, Channel: "golang"}},
	})
	a.DeferSceneWrites() // window-mode marshaling, as the native front-end uses

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); a.Refresh(context.Background()) }()
		go func() { defer wg.Done(); a.RefreshStreaming(context.Background()) }()
	}
	// Drain posted scene writes concurrently, mimicking the render thread.
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			default:
				a.drainScene()
			}
		}
	}()
	wg.Wait()
	close(done)
	a.drainScene()
}
