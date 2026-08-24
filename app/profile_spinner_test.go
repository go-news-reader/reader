package app

import (
	"fmt"
	"time"

	"github.com/go-news-reader/reader/source"
	"testing"
)

// BenchmarkFramePopulatedSpinner profiles the exact hot path the live
// measurement pointed at: a feed already full of cards while the loading spinner
// keeps ticking. Each iteration advances the animation clock one interval and
// draws a frame, so the whole scene (all cards) is repainted per spinner step —
// the immediate-mode cost we want to characterise.
//
//	go test ./app/ -run x -bench FramePopulatedSpinner -benchmem -cpuprofile /tmp/spin.prof
func BenchmarkFramePopulatedSpinner(b *testing.B) {
	a := New(Config{Registry: newReg(), Width: 1000, Height: 700})
	clock := time.Unix(0, 0)
	a.now = func() time.Time { return clock }

	items := make([]source.Item, 40)
	for i := range items {
		items[i] = source.Item{
			ID:       fmt.Sprintf("id-%d", i),
			Source:   source.HackerNews,
			Title:    fmt.Sprintf("A representative post title number %d that wraps a little", i),
			Author:   "someone",
			Body:     "A paragraph of body text so the card has real content to lay out and paint.",
			Score:    i * 3,
			Comments: i,
			Created:  int64(1_700_000_000 + i*3600),
		}
	}
	a.Scene().SetItems(items)
	a.Scene().SetLoading(true, 1, 3) // populated feed AND still loading -> spinner ticks
	a.Frame()                        // establish the animation clock

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		clock = clock.Add(animFrameInterval) // step the spinner one frame
		a.Frame()
	}
}
