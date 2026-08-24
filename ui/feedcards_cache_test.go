package ui

import (
	"image"
	"testing"

	"github.com/go-news-reader/reader/source"
)

// The raster cache must be invisible: a feed drawn with the card cache on is
// byte-for-byte identical to the same feed drawn with it off — on the second
// (cache-hit) frame, which is where a wrong CacheBackground or a lost draw would
// show. Exercises varied content, a selected row, a read/dimmed row and an
// arrived thumbnail so the cache key's live inputs are all in play.
func TestFeedCacheRastersIdenticallyToUncached(t *testing.T) {
	thumb := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for i := 0; i < len(thumb.Pix); i += 4 {
		thumb.Pix[i], thumb.Pix[i+1], thumb.Pix[i+2], thumb.Pix[i+3] = 0x11, 0xF0, 0x22, 0xFF
	}

	build := func(cacheOn bool) []byte {
		s := New(1000, 700, ThemeFor(OSMac, false))
		s.SetScale(1)
		s.SetSubs(nil)
		s.SetItems([]source.Item{
			{ID: "a", Source: source.Reddit, Channel: "r/go", Title: "First post with a longer headline that wraps", Author: "alice", Score: 12, Comments: 3, Media: []source.Media{{Kind: source.MediaImage}}},
			{ID: "b", Source: source.HackerNews, Title: "Second", Author: "bob", Score: 99, Comments: 40},
			{ID: "c", Source: source.Reddit, Channel: "r/rust", Title: "Third post here", Score: -1, Comments: -1},
			{ID: "d", Source: source.Mastodon, Title: "Fourth, a toot", Author: "dan", Score: -1, Comments: -1},
		})
		s.SetThumb("a", thumb)        // an arrived thumbnail (hasThumb key input)
		s.FeedCardList().Selected = 1 // a selection ring (selected key input)
		if !cacheOn {
			s.FeedCardList().CacheKey = nil
		}
		buf := make([]byte, s.W*s.H*4)
		s.Draw(buf) // frame 1: cache miss (fills tiles) / direct
		s.Draw(buf) // frame 2: cache hit (blits tiles) / direct again
		return buf
	}

	on, off := build(true), build(false)
	for i := range off {
		if on[i] != off[i] {
			px := (i / 4)
			t.Fatalf("cached feed differs from uncached at pixel %d,%d byte %d: cached=%d uncached=%d",
				px%1000, px/1000, i%4, on[i], off[i])
		}
	}
}

// The per-card run cache populates for the visible cards and stays bounded to
// the working set across frames (swept in beginSelectableFrame), so a long feed
// does not grow it without bound — and text selection still sees runs (the
// existing selection tests exercise that the runs are correct).
func TestFeedRunCacheBounded(t *testing.T) {
	s := New(1000, 700, ThemeFor(OSMac, false))
	s.SetScale(1)
	s.SetSubs(nil)
	items := make([]source.Item, 200)
	for i := range items {
		items[i] = source.Item{ID: "id" + string(rune('a'+i%26)) + string(rune('a'+i/26)), Source: source.HackerNews, Title: "Post", Score: -1, Comments: -1}
	}
	s.SetItems(items)

	buf := make([]byte, s.W*s.H*4)
	// A few frames to let the initial scroll (newest at the bottom) settle, after
	// which the on-screen row positions — and so the run-cache keys — are stable.
	for i := 0; i < 4; i++ {
		s.Draw(buf)
	}
	n := len(s.feedRunCache)
	if n == 0 || n > 40 {
		t.Fatalf("run cache holds %d entries once settled, want ~the visible few", n)
	}
	// A further unchanged frame reuses the cache and does not grow it (the sweep
	// keeps it bounded to the working set).
	s.Draw(buf)
	if got := len(s.feedRunCache); got != n {
		t.Fatalf("run cache changed from %d to %d on a settled, unchanged frame", n, got)
	}
}
