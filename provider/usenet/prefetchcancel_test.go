package usenet

import (
	"context"
	"sync"
	"testing"

	gonntp "github.com/go-newsgroups/nntp"
)

// A prefetch whose context is ALREADY cancelled must feed nothing.
//
// This is the reproducer for #163, and it names the cause the original test could
// not. The feeder's
//
//	select { case tasks <- r: case <-ctx.Done(): }
//
// has no notion of priority: when a worker is ready to receive AND the context is
// done, both cases are ready and Go picks uniformly at random. So the guarantee
// "a cancelled feed dispatches nothing" was never a guarantee at all — it held
// only because the feeder usually reached its offer before any worker had finished
// dialling and parked. A loaded runner under -race removes that head start, which
// is why CI saw it once and an idle laptop never did.
//
// The arrangement is chosen to make the window WIDE rather than to wait for luck:
// eight workers, so the earliest of them is parked and ready to receive while the
// feeder is still spawning the rest, and a hundred rounds. Measured against the
// unmodified feeder: 8/8 plain runs and 6/6 under -race failed. With the context
// checked before the offer: 10/10 and 6/6 passed.
func TestPrefetchImagesFeedsNothingWhenAlreadyCancelled(t *testing.T) {
	const rounds = 100
	for round := 0; round < rounds; round++ {
		p := NewWithDial(func(context.Context) (conn, error) {
			return &reconFakeConn{articles: map[string]*gonntp.Article{}}, nil
		})
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // done before the first request is ever offered

		reqs := make([]ImageRequest, 8)
		for i := range reqs {
			reqs[i] = ImageRequest{ID: "a", Parts: []ReconstructPart{{MessageID: "a@h"}}}
		}
		var mu sync.Mutex
		n := 0
		p.PrefetchImages(ctx, reqs, 8, 16, func(ImageResult) { mu.Lock(); n++; mu.Unlock() })
		if n != 0 {
			t.Fatalf("round %d: a prefetch cancelled before it started delivered %d results, want 0",
				round, n)
		}
	}
}
