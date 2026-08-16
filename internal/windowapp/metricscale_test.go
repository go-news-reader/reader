package windowapp

import (
	"testing"

	"github.com/go-news-reader/reader/app"
	"github.com/go-news-reader/reader/source"
	"github.com/go-widgets/toolkit"
)

// Does telling the toolkit its metric scale change a reader frame?
//
// This is the measurement go-widgets/window#49 asked for before connecting the
// two. The reader threads its OWN scale through Scene.SetScale, and it also
// embeds toolkit widgets; if a metric were multiplied by both, every one of
// those widgets would come out twice the size it should be, quietly, on exactly
// the screens the change is meant to improve.
//
// The frame is rendered at scale 2 with the toolkit told nothing, and again with
// it told 2, and the two are compared pixel for pixel.
func TestReaderFrameUnderMetricScale(t *testing.T) {
	frame := func(metric float64) ([]byte, int, int) {
		t.Helper()
		defer toolkit.SetMetricScale(1)
		toolkit.SetMetricScale(metric)

		a := app.New(app.Config{Registry: source.NewRegistry(), Width: 800, Height: 600})
		a.SetRefreshHook(func() {})
		h := New(a)
		h.Resize(800, 600, 2) // the window's own scale, as internal/window passes it
		buf, w, hh, _ := h.Frame()
		out := make([]byte, len(buf))
		copy(out, buf)
		return out, w, hh
	}

	base, bw, bh := frame(1)
	told, tw, th := frame(2)

	if bw != tw || bh != th {
		t.Fatalf("the frame is %dx%d with the toolkit told nothing and %dx%d with it told 2: "+
			"the scene itself changed size, which is not what this was measuring", bw, bh, tw, th)
	}
	diff := 0
	minX, minY, maxX, maxY := bw, bh, -1, -1
	rows := map[int]int{}
	for i := 0; i+3 < len(base); i += 4 {
		if base[i] == told[i] && base[i+1] == told[i+1] && base[i+2] == told[i+2] {
			continue
		}
		diff++
		px := (i / 4) % bw
		py := (i / 4) / bw
		if px < minX {
			minX = px
		}
		if px > maxX {
			maxX = px
		}
		if py < minY {
			minY = py
		}
		if py > maxY {
			maxY = py
		}
		rows[py]++
	}
	pct := 100 * float64(diff) / float64(bw*bh)
	t.Logf("%dx%d frame: %d of %d pixels differ (%.2f%%) when the toolkit is told the scale",
		bw, bh, diff, bw*bh, pct)
	if diff > 0 {
		t.Logf("they lie within (%d,%d)-(%d,%d), across %d of %d rows",
			minX, minY, maxX, maxY, len(rows), bh)
	}

	// Not an assertion that the two are IDENTICAL: telling the toolkit is meant
	// to change something, or there would be no point in telling it. The
	// question is WHAT.
	//
	// A double scale would move the layout: a widget whose metrics were
	// multiplied by both the scene's scale and the toolkit's would be four times
	// its logical size, taking its neighbours with it, and a large share of the
	// frame would differ. What is measured instead is under two percent, in one
	// band -- the thickness of chrome that now draws its borders at the size the
	// screen deserves, with nothing moved.
	//
	// The bound is deliberately loose. It is not a golden image; it is the
	// difference between "a border got thicker" and "the interface was laid out
	// twice as large", and those are two orders of magnitude apart.
	if pct > 2 {
		t.Errorf("%.2f%% of the frame changed when the toolkit was told the scale: that is a "+
			"LAYOUT moving, not chrome thickening -- something is being scaled twice", pct)
	}
}
