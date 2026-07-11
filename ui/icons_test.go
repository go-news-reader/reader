package ui

import (
	"image"
	"testing"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
)

// iconCanvas returns a painter over a fresh w×h RGBA buffer.
func iconCanvas(w, h int) (*painter.PixelPainter, *image.RGBA, []byte) {
	buf := make([]byte, w*h*4)
	p := painter.NewPixelPainter(buf, w, h)
	img := &image.RGBA{Pix: buf, Stride: w * 4, Rect: image.Rect(0, 0, w, h)}
	return p, img, buf
}

var iconInk = toolkit.RGBA{R: 200, G: 100, B: 50, A: 0xFF}

func isInk(buf []byte, w, x, y int) bool {
	i := (y*w + x) * 4
	return buf[i] == iconInk.R && buf[i+1] == iconInk.G && buf[i+2] == iconInk.B && buf[i+3] == iconInk.A
}

// vRuns counts the maximal runs of ink pixels down the column at x in [y0,y1).
func vRuns(buf []byte, w, x, y0, y1 int) int {
	runs, in := 0, false
	for y := y0; y < y1; y++ {
		ink := isInk(buf, w, x, y)
		if ink && !in {
			runs++
		}
		in = ink
	}
	return runs
}

func anyInk(buf []byte) bool {
	for i := 0; i+3 < len(buf); i += 4 {
		if buf[i] == iconInk.R && buf[i+1] == iconInk.G && buf[i+2] == iconInk.B && buf[i+3] == iconInk.A {
			return true
		}
	}
	return false
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

// TestMenuIconThreeBars proves the burger renders three distinct horizontal
// bars (the reported tofu bug), not a single filled/empty box.
func TestMenuIconThreeBars(t *testing.T) {
	w, h := 48, 48
	p, _, buf := iconCanvas(w, h)
	box := toolkit.Rect{X: 0, Y: 0, W: w, H: h}
	drawMenuIcon(p, box, iconInk, 3)
	// A vertical scan through the icon's centre must cross exactly three bars.
	if runs := vRuns(buf, w, w/2, 0, h); runs != 3 {
		t.Fatalf("menu icon vertical runs = %d, want 3 distinct bars", runs)
	}
	// The box is not a single solid tofu rectangle: its centre column has gaps.
	inked := 0
	for y := 0; y < h; y++ {
		if isInk(buf, w, w/2, y) {
			inked++
		}
	}
	if inked >= h {
		t.Fatal("menu icon column fully inked (tofu box), want gaps between bars")
	}
}

func TestDrawIconsPaint(t *testing.T) {
	box := toolkit.Rect{X: 2, Y: 2, W: 40, H: 40}
	for name, fn := range map[string]func(*painter.PixelPainter, toolkit.Rect, toolkit.RGBA, int){
		"lock":    drawLockIcon,
		"user":    drawUserIcon,
		"sliders": drawSlidersIcon,
		"list":    drawListIcon,
		"menu":    drawMenuIcon,
	} {
		p, _, buf := iconCanvas(44, 44)
		fn(p, box, iconInk, 2)
		if !anyInk(buf) {
			t.Fatalf("%s icon drew no ink", name)
		}
	}
}
