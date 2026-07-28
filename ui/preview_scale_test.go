package ui

import (
	"image"
	"testing"

	"github.com/go-news-reader/reader/source"
)

// TestPreviewImageGrowsWithWindow guards the fix for a down-only resize: the
// default preview pane (no explicit drag) tracks the window width, so the
// preview image grows when the window grows — not only shrinks when it narrows.
func TestPreviewImageGrowsWithWindow(t *testing.T) {
	s := New(1200, 900, ThemeFor(OSLinux, false))
	s.SetSubs(nil)
	it := source.Item{ID: "p", Source: source.Usenet, Title: "pic", Media: []source.Media{{Kind: source.MediaImage}}}
	s.SetItems([]source.Item{it})
	s.SetThumb("p", image.NewRGBA(image.Rect(0, 0, 1200, 700))) // landscape → width-bound
	s.SelectPreview(it)

	measure := func(w int) int {
		s.Resize(w, 900)
		s.Draw(make([]byte, s.W*s.H*4))
		return s.previewImgR.W
	}

	narrow := measure(1200)
	mid := measure(1800)
	wide := measure(2600)
	if !(narrow < mid && mid < wide) {
		t.Fatalf("preview image should grow with the window: %d, %d, %d", narrow, mid, wide)
	}
	// And it still shrinks back when the window narrows again (down direction).
	if back := measure(1200); back != narrow {
		t.Fatalf("narrowing should restore the smaller image: %d != %d", back, narrow)
	}
	// The default pane never drops below the preferred width.
	if s.previewR.W < rpxOf(s, previewPaneW) {
		t.Fatalf("default pane %d below preferred %d", s.previewR.W, rpxOf(s, previewPaneW))
	}
}

// TestPreviewPortraitFillsHeight guards that a tall image now fills the pane
// height (bound by the larger available dimension) instead of the old fixed
// 3/5-of-height cap.
func TestPreviewPortraitFillsHeight(t *testing.T) {
	s := New(1400, 900, ThemeFor(OSLinux, false))
	s.SetSubs(nil)
	it := source.Item{ID: "q", Source: source.Usenet, Title: "tall", Media: []source.Media{{Kind: source.MediaImage}}}
	s.SetItems([]source.Item{it})
	s.SetThumb("q", image.NewRGBA(image.Rect(0, 0, 400, 1600))) // portrait → height-bound
	s.SelectPreview(it)
	s.Draw(make([]byte, s.W*s.H*4))

	// A portrait image should use well over the old 3/5 cap of the pane height.
	if s.previewImgR.H <= s.previewR.H*3/5 {
		t.Fatalf("portrait image %d should exceed the old 3/5 cap %d of pane %d",
			s.previewImgR.H, s.previewR.H*3/5, s.previewR.H)
	}
}

// TestPreviewImageFloorInShortPane covers the minimum-box floor: in a very
// short pane the available height collapses, but the image box keeps a usable
// minimum instead of vanishing.
func TestPreviewImageFloorInShortPane(t *testing.T) {
	s := New(1200, MinH, ThemeFor(OSLinux, false))
	s.SetSubs(nil)
	longTitle := ""
	for i := 0; i < 40; i++ {
		longTitle += "a very long title segment that wraps across many lines "
	}
	it := source.Item{ID: "z", Source: source.Usenet, Title: longTitle, Media: []source.Media{{Kind: source.MediaImage}}}
	s.SetItems([]source.Item{it})
	s.SetThumb("z", image.NewRGBA(image.Rect(0, 0, 1200, 700)))
	s.SelectPreview(it)
	s.Draw(make([]byte, s.W*s.H*4))
	if s.previewImgR.H <= 0 {
		t.Fatal("image box should keep a usable minimum height in a short pane")
	}
}

// TestPreviewUserWidthPinsPane confirms an explicit drag still overrides the
// window-proportional default (so a user-chosen width is honoured).
func TestPreviewUserWidthPinsPane(t *testing.T) {
	s := New(2600, 900, ThemeFor(OSLinux, false))
	s.SetSubs(nil)
	s.previewUserW = rpxOf(s, previewPaneW) // pin to the preferred width
	if got := s.previewWidth(); got != rpxOf(s, previewPaneW) {
		t.Fatalf("user-pinned pane = %d, want %d", got, rpxOf(s, previewPaneW))
	}
}
