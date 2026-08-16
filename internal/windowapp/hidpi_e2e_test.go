package windowapp

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"

	"github.com/go-news-reader/reader/app"
	"github.com/go-news-reader/reader/source"
	"github.com/go-widgets/toolkit"
)

// The whole HiDPI stack, on a real application frame.
//
// Every layer has been proven on its own: the painter strokes the width it is
// given, the toolkit's widgets follow SetMetricScale, the back-ends set it for a
// NativeScale window. What none of those says is whether an APPLICATION comes
// out doubled -- and that is the only claim a user can see.
//
// So: the reader's own frame, rendered at one point per pixel and again at two,
// with the toolkit told each time, and asked whether the second is the first at
// twice the size. Not pixel-for-pixel -- a doubled interface is not a doubled
// image, since text re-rasterises and a rounded corner is redrawn rather than
// stretched -- but structurally, on where the interface's own boundaries fall.
func TestReaderInterfaceDoublesEndToEnd(t *testing.T) {
	frame := func(scale int) ([]byte, int, int) {
		t.Helper()
		defer toolkit.SetMetricScale(1)
		toolkit.SetMetricScale(float64(scale))

		a := app.New(app.Config{Registry: source.NewRegistry(), Width: 800, Height: 600})
		a.SetRefreshHook(func() {})
		h := New(a)
		// What internal/window does for a NativeScale window: the framebuffer is
		// the logical size times the backing scale, and the scene is told both.
		h.Resize(800*scale, 600*scale, float64(scale))
		buf, w, hh, _ := h.Frame()
		out := make([]byte, len(buf))
		copy(out, buf)
		return out, w, hh
	}

	one, w1, h1 := frame(1)
	two, w2, h2 := frame(2)
	if w2 != 2*w1 || h2 != 2*h1 {
		t.Fatalf("the frame is %dx%d at one point per pixel and %dx%d at two, want twice",
			w1, h1, w2, h2)
	}
	// Saved into the test's own temp dir: they are for a human to look at when
	// this fails, not artefacts to leave in the tree.
	dir := t.TempDir()
	writePNG(t, dir+"/hidpi-1x.png", one, w1, h1)
	writePNG(t, dir+"/hidpi-2x.png", two, w2, h2)

	// The interface's own boundaries: where along a scanline the colour changes.
	// A doubled interface puts them at the same FRACTION of the frame; one whose
	// chrome stayed logical drifts toward the edge as the frame grows around it.
	//
	// Every eighth row, not three of them: three rows of a mostly flat window is
	// a dozen boundaries, and a dozen agreeing proves very little. Sampling the
	// height gives hundreds, including the sidebar's rows, the toolbar, the
	// panel divider and every piece of text.
	total, matched := 0, 0
	for y := 0; y < h1; y += 8 {
		b1 := boundaries(one, w1, y, 4)
		b2 := boundaries(two, w2, 2*y, 8)
		rowTotal, rowMatched := 0, 0
		for _, a := range b1 {
			total++
			rowTotal++
			for _, b := range b2 {
				if diff := a - b; diff < 0.004 && diff > -0.004 { // ~3 pixels of 800
					matched++
					rowMatched++
					break
				}
			}
		}
		// The rows that disagree, printed rather than summarised: every one of
		// them has text on it, and that is the claim this test can and cannot
		// make. A row of chrome matches exactly.
		if rowTotal > 0 && rowMatched < rowTotal {
			t.Logf("  row %3d: %d of %d", y, rowMatched, rowTotal)
		}
	}
	if total < 100 {
		t.Fatalf("only %d boundaries across the whole frame: it is too empty to say anything", total)
	}
	got := float64(matched) / float64(total)
	t.Logf("%d of %d interface boundaries land where doubling would put them (%.1f%%)",
		matched, total, 100*got)
	// Ninety per cent, not a hundred: a word's outer edges move by a pixel or two
	// when its glyphs are re-rasterised at the larger size, so every row carrying
	// text contributes a few. The rows listed above are exactly the rows with
	// text on them; a chrome boundary that missed would be a real defect and
	// there are none. A stack that had not scaled at all sits near fifty, since
	// only the boundaries at the frame's own edges would still line up.
	if got < 0.9 {
		t.Errorf("only %.1f%% of the interface's boundaries are where doubling would put them -- "+
			"something is laid out at a size the scale did not reach", 100*got)
	}
}

// boundaries returns the positions along row y where a REGION begins or ends, as
// a fraction of the width.
//
// A region, not every colour change: a boundary is kept only when the run on one
// side of it is at least minRun wide. That leaves out the interior of glyphs,
// whose stems are a pixel or three, and it has to -- type at twice the scale is
// RE-RASTERISED, not stretched, so its stems land where the rasteriser puts them
// and not at twice their old fraction. Measured before this line existed, the
// eight rows that disagreed were the eight rows with text on them and every flat
// row matched exactly; a glyph that did land on the doubled fraction would be a
// glyph that had been stretched, which is the thing HiDPI exists to stop.
func boundaries(buf []byte, w, y, minRun int) []float64 {
	at := func(x int) (byte, byte, byte) {
		o := 4 * (y*w + x)
		return buf[o], buf[o+1], buf[o+2]
	}
	// Run boundaries first, then keep the ones bounding something big.
	var edges []int
	pr, pg, pb := at(0)
	for x := 1; x < w; x++ {
		r, g, b := at(x)
		if r != pr || g != pg || b != pb {
			edges = append(edges, x)
			pr, pg, pb = r, g, b
		}
	}
	var out []float64
	for i, x := range edges {
		left := x
		if i > 0 {
			left = x - edges[i-1]
		}
		right := w - x
		if i+1 < len(edges) {
			right = edges[i+1] - x
		}
		if left >= minRun || right >= minRun {
			out = append(out, float64(x)/float64(w))
		}
	}
	return out
}

// writePNG saves a frame so a human can look at what the numbers describe.
func writePNG(t *testing.T, name string, buf []byte, w, h int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i+3 < len(buf) && i/4 < w*h; i += 4 {
		img.Set((i/4)%w, (i/4)/w, color.RGBA{buf[i], buf[i+1], buf[i+2], 255})
	}
	f, err := os.Create(name)
	if err != nil {
		t.Logf("saving %s: %v", name, err)
		return
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Logf("encoding %s: %v", name, err)
		return
	}
	t.Logf("saved %s (%dx%d)", name, w, h)
}
