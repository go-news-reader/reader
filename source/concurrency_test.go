package source

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// concProvider blocks in Feed until its gate is closed, recording how many calls
// are in flight at once — so a test can prove the fan-out never exceeds the cap.
type concProvider struct {
	kind    Kind
	entered chan struct{} // one send per Feed entry
	gate    chan struct{} // closed to release every blocked Feed
	mu      sync.Mutex
	cur     int
	max     int
}

func (c *concProvider) Kind() Kind { return c.kind }

func (c *concProvider) Feed(_ context.Context, _ Query) (Result, error) {
	c.mu.Lock()
	c.cur++
	if c.cur > c.max {
		c.max = c.cur
	}
	c.mu.Unlock()
	c.entered <- struct{}{}
	<-c.gate
	c.mu.Lock()
	c.cur--
	c.mu.Unlock()
	return Result{}, nil
}

func TestConcLimitDefaultAndOverride(t *testing.T) {
	if got := NewRegistry().concLimit(); got != defaultConcurrency {
		t.Errorf("default concLimit = %d, want %d", got, defaultConcurrency)
	}
	if got := (&Registry{MaxConcurrent: 3}).concLimit(); got != 3 {
		t.Errorf("MaxConcurrent=3 concLimit = %d, want 3", got)
	}
}

// TestAggregateStreamBoundsConcurrency proves the fan-out runs at most
// MaxConcurrent source fetches at a time even with many more subscriptions.
func TestAggregateStreamBoundsConcurrency(t *testing.T) {
	const n, k = 20, 4
	p := &concProvider{kind: Reddit, entered: make(chan struct{}, n), gate: make(chan struct{})}
	r := NewRegistry()
	r.MaxConcurrent = k
	r.Register(p)

	subs := make([]Subscription, n)
	for i := range subs {
		subs[i] = Subscription{Source: Reddit, Channel: fmt.Sprintf("c%d", i)}
	}

	done := make(chan struct{})
	go func() { r.Aggregate(context.Background(), subs); close(done) }()

	// Exactly k fetches enter Feed; the rest are parked on the semaphore.
	for i := 0; i < k; i++ {
		<-p.entered
	}
	select {
	case <-p.entered:
		t.Fatalf("a %d-th Feed ran while the concurrency limit is %d", k+1, k)
	case <-time.After(50 * time.Millisecond):
	}

	close(p.gate) // release everything; the remainder run in waves of <= k
	<-done

	p.mu.Lock()
	mx := p.max
	p.mu.Unlock()
	if mx != k {
		t.Errorf("peak concurrent Feed = %d, want exactly %d", mx, k)
	}
}
