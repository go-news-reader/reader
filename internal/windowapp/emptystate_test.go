package windowapp

import (
	"testing"

	"github.com/go-news-reader/reader/app"
	"github.com/go-news-reader/reader/source"
)

// The feed's empty state must fit inside the feed pane.
//
// It was centred across everything right of the sidebar, which includes the
// preview pane -- so the label sat under the divider, and the chrome drawn after
// it cut the word in half. What the user saw was "No it" with a scrollbar over
// the wound (#199).
//
// This measures where the label's ink actually lands, rather than what the
// layout intended: render a frame with no items and find the columns carrying
// the muted label colour on its row.
func TestEmptyStateFitsInsideTheFeedPane(t *testing.T) {
	a := app.New(app.Config{Registry: source.NewRegistry(), Width: 900, Height: 600})
	a.SetRefreshHook(func() {})
	h := New(a)
	h.Resize(900, 600, 1)
	buf, w, height, _ := h.Frame()
	if w != 900 || height != 600 {
		t.Fatalf("frame is %dx%d, want 900x600", w, height)
	}

	// Where the label's ink actually lands, measured INSIDE the feed pane. The
	// preview pane on the same row carries its own centred label, so the scan is
	// bounded by the pane rather than by the window -- and the pane is the thing
	// the label is supposed to be centred in.
	feedX, feedW := a.Scene().FeedGeom()
	feedRight := feedX + feedW
	// A band of rows, not one: the label's box starts at the pane's vertical
	// centre and its glyphs sit a few rows below, so the centre row itself is
	// blank. A single scanline through a blank row is how this test first
	// reported the label missing when it was there and correct.
	bg := pixel(buf, w, feedX+4, height/2)
	minX, maxX := -1, -1
	for y := height/2 - 4; y < height/2+24; y++ {
		for x := feedX; x < feedRight; x++ {
			if pixel(buf, w, x, y) == bg {
				continue
			}
			if minX < 0 || x < minX {
				minX = x
			}
			if x > maxX {
				maxX = x
			}
		}
	}
	if minX < 0 {
		t.Fatal("no label ink anywhere in the feed pane: the empty state is drawn outside the pane " +
			"it belongs to, which is the whole defect")
	}

	paneMid := feedX + feedW/2
	inkMid := (minX + maxX) / 2
	t.Logf("label ink spans x=%d..%d (centre %d); the feed pane is x=%d..%d (centre %d)",
		minX, maxX, inkMid, feedX, feedRight, paneMid)
	if diff := inkMid - paneMid; diff < -8 || diff > 8 {
		t.Errorf("the empty-state label is centred on x=%d and the feed pane on x=%d: it is laid "+
			"out across the window instead of across its pane, so the preview pane's chrome cuts it",
			inkMid, paneMid)
	}
	if maxX >= feedRight-1 {
		t.Errorf("the label's ink reaches x=%d, at or past the pane's right edge (%d): it is being cut",
			maxX, feedRight)
	}
}

func pixel(buf []byte, w, x, y int) [3]byte {
	o := 4 * (y*w + x)
	return [3]byte{buf[o], buf[o+1], buf[o+2]}
}
