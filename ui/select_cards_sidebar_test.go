package ui

import (
	"testing"

	"github.com/go-widgets/toolkit"

	"github.com/go-news-reader/reader/source"
)

// findRun returns the accumulated selectable run whose text equals want (after a
// Draw populated s.selAccum), so a test can drag across the exact on-screen
// glyphs. Fatal if no such run was collected.
func findRun(t *testing.T, s *Scene, want string) toolkit.TextRun {
	t.Helper()
	for _, r := range s.selAccum {
		if r.Text == want {
			return r
		}
	}
	t.Fatalf("no selectable run with text %q (have %d runs)", want, len(s.selAccum))
	return toolkit.TextRun{}
}

// changedBBox returns the bounding box of the pixels that differ between a and b
// (same W×H RGBA buffers) and how many differed.
func changedBBox(a, b []byte, w, h int) (bx0, by0, bx1, by1, n int) {
	bx0, by0, bx1, by1 = w, h, -1, -1
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := (y*w + x) * 4
			if a[i] != b[i] || a[i+1] != b[i+1] || a[i+2] != b[i+2] {
				n++
				if x < bx0 {
					bx0 = x
				}
				if x > bx1 {
					bx1 = x
				}
				if y < by0 {
					by0 = y
				}
				if y > by1 {
					by1 = y
				}
			}
		}
	}
	return
}

// TestFeedRegionSelectable proves a press over the feed list region is treated as
// selectable (so a front-end defers the click for a possible drag), and that the
// card's composed title text IS a drag-selectable run — the reader's card draws
// its title through real toolkit Labels, so toolkit.CollectRuns lifts it into the
// cross-surface selection accumulator (restoring the pre-CardList behaviour the
// toolkit content cards had dropped).
func TestFeedRegionSelectable(t *testing.T) {
	s := New(1000, 700, ThemeFor(OSMac, false))
	s.SetScale(1)
	s.SetSubs(nil)
	s.SetItems([]source.Item{{ID: "1", Source: source.Reddit, Channel: "chan",
		Title: "TITLERUN", Score: -1, Comments: -1}})

	buf := make([]byte, s.W*s.H*4)
	s.Draw(buf) // lays out + commits the selectable runs

	// A point inside the feed list region is selectable, so a press there defers
	// its click to a possible drag.
	feed := s.feedListRegion()
	if !s.SelectableAt(feed.X+feed.W/2, feed.Y+feed.H/2) {
		t.Fatal("a press over the feed list region should be selectable")
	}
	// The card title IS a drag-selectable run.
	findRun(t, s, "TITLERUN")
}

// TestCollapsedFeedRegionSelectable covers the feed-region branch of
// SelectableAt with the sidebar collapsed: a press where the sidebar used to be
// now falls in the feed list region (the sidebar's own TreeView rows resolve to
// actions, not selectable text, so it no longer participates in selection).
func TestCollapsedFeedRegionSelectable(t *testing.T) {
	s := New(1000, 700, ThemeFor(OSMac, false))
	s.SetScale(1)
	s.SetSubs([]Subscription{{Source: source.Reddit, Channel: "c", Label: "X"}})
	s.ToggleSidebar() // collapse
	buf := make([]byte, s.W*s.H*4)
	s.Draw(buf)
	if !s.SelectableAt(5, 300) {
		t.Fatal("with the sidebar collapsed the feed list starts at the left edge")
	}
}
