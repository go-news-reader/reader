package ui

import (
	"image"
	"testing"

	"github.com/go-iconoir/iconoir"
	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
)

// iconCanvas returns a painter over a fresh w×h RGBA buffer (zeroed, so every
// pixel starts fully transparent — any non-zero alpha afterwards is icon ink).
func iconCanvas(w, h int) (*painter.PixelPainter, *image.RGBA, []byte) {
	buf := make([]byte, w*h*4)
	p := painter.NewPixelPainter(buf, w, h)
	img := &image.RGBA{Pix: buf, Stride: w * 4, Rect: image.Rect(0, 0, w, h)}
	return p, img, buf
}

var iconInk = toolkit.RGBA{R: 200, G: 100, B: 50, A: 0xFF}

// hasInk reports whether the composited pixel at (x,y) carries any icon ink
// (non-zero alpha; the buffer started fully transparent).
func hasInk(buf []byte, w, x, y int) bool { return buf[(y*w+x)*4+3] > 0 }

// vRuns counts the maximal runs of ink pixels down the column at x in [y0,y1).
func vRuns(buf []byte, w, x, y0, y1 int) int {
	runs, in := 0, false
	for y := y0; y < y1; y++ {
		ink := hasInk(buf, w, x, y)
		if ink && !in {
			runs++
		}
		in = ink
	}
	return runs
}

// inkStats returns how many pixels in r carry ink, and whether any pixel is
// partially covered (0 < alpha < 255) — proof the Iconoir mask is anti-aliased.
func inkStats(buf []byte, w int, r toolkit.Rect) (inked int, aa bool) {
	for y := r.Y; y < r.Y+r.H; y++ {
		for x := r.X; x < r.X+r.W; x++ {
			a := buf[(y*w+x)*4+3]
			if a > 0 {
				inked++
			}
			if a > 0 && a < 0xFF {
				aa = true
			}
		}
	}
	return
}

func TestIconStroke(t *testing.T) {
	s := &Scene{Scale: 1}
	if got := s.iconStroke(); got != 2 {
		t.Fatalf("iconStroke@1 = %d, want 2", got)
	}
	s.Scale = 0.5
	if got := s.iconStroke(); got != 1 {
		t.Fatalf("iconStroke@0.5 = %d, want 1 (floor at 1px)", got)
	}
	s.Scale = 2
	if got := s.iconStroke(); got != 3 {
		t.Fatalf("iconStroke@2 = %d, want 3", got)
	}
}

// TestMenuIconThreeBars proves the burger renders as three distinct horizontal
// bars (the real Iconoir "menu" glyph), not a single filled/empty tofu box.
func TestMenuIconThreeBars(t *testing.T) {
	w, h := 48, 48
	p, _, buf := iconCanvas(w, h)
	box := toolkit.Rect{X: 0, Y: 0, W: w, H: h}
	drawMenuIcon(p, box, iconInk, 3)
	// A vertical scan through the icon's centre must cross exactly three bars.
	if runs := vRuns(buf, w, w/2, 0, h); runs != 3 {
		t.Fatalf("menu icon vertical runs = %d, want 3 distinct bars", runs)
	}
	// The centre column has gaps between the bars (not a solid tofu column).
	inked := 0
	for y := 0; y < h; y++ {
		if hasInk(buf, w, w/2, y) {
			inked++
		}
	}
	if inked >= h {
		t.Fatal("menu icon column fully inked (tofu box), want gaps between bars")
	}
}

// TestDrawIconsPaintAA proves every nav/chrome helper blits a real Iconoir mask:
// it lands anti-aliased ink inside the target rect, and does not flood-fill the
// whole cell (an outline glyph, never a solid box).
func TestDrawIconsPaintAA(t *testing.T) {
	box := toolkit.Rect{X: 2, Y: 2, W: 40, H: 40}
	for name, fn := range map[string]func(painter.Painter, toolkit.Rect, toolkit.RGBA, int){
		"lock":    drawLockIcon,
		"user":    drawUserIcon,
		"sliders": drawSlidersIcon,
		"list":    drawListIcon,
		"menu":    drawMenuIcon,
	} {
		p, _, buf := iconCanvas(44, 44)
		fn(p, box, iconInk, 2)
		inked, aa := inkStats(buf, 44, box)
		if inked == 0 {
			t.Fatalf("%s icon drew no ink", name)
		}
		if !aa {
			t.Fatalf("%s icon drew no anti-aliased edge (not an Iconoir mask?)", name)
		}
		if inked >= box.W*box.H {
			t.Fatalf("%s icon flood-filled its cell (tofu box)", name)
		}
	}
}

// TestDrawSearchIcon covers the topbar SearchEntry magnifier callback: it paints
// anti-aliased ink into the prefix slot.
func TestDrawSearchIcon(t *testing.T) {
	box := toolkit.Rect{X: 0, Y: 0, W: 24, H: 24}
	p, _, buf := iconCanvas(24, 24)
	drawSearchIcon(p, box, iconInk)
	inked, aa := inkStats(buf, 24, box)
	if inked == 0 || !aa {
		t.Fatalf("search icon: inked=%d aa=%v, want ink with AA edge", inked, aa)
	}
}

// TestUnknownIconName documents that an unknown Iconoir name paints nothing (the
// helpers here use verified names, so this guards the adapter's miss path).
func TestUnknownIconName(t *testing.T) {
	p, _, buf := iconCanvas(24, 24)
	if iconoir.Draw(p, painter.Rect{X: 0, Y: 0, W: 24, H: 24}, "definitely-not-an-icon", iconInk) {
		t.Fatal("iconoir.Draw reported an unknown name as present")
	}
	for _, b := range buf {
		if b != 0 {
			t.Fatal("unknown icon name painted ink")
		}
	}
}
