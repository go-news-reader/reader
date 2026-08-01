package ui

import (
	"testing"

	"github.com/go-widgets/painter"
)

// nonPixelPainter is a painter.Painter that is NOT a *PixelPainter, to exercise
// the retained-widget fallback: pixImg reports ok=false and a widget draws
// nothing (a widget's Draw can be handed any Painter back-end).
type nonPixelPainter struct{}

func (nonPixelPainter) FillRect(painter.Rect, painter.RGBA)                  {}
func (nonPixelPainter) StrokeRect(painter.Rect, painter.RGBA, int)           {}
func (nonPixelPainter) FillRoundRect(painter.Rect, int, painter.RGBA)        {}
func (nonPixelPainter) StrokeRoundRect(painter.Rect, int, painter.RGBA, int) {}
func (nonPixelPainter) PutPixel(int, int, painter.RGBA)                      {}
func (nonPixelPainter) Text(int, int, string, painter.RGBA)                  {}
func (nonPixelPainter) Size() (int, int)                                     { return 0, 0 }

func TestPixImgNonPixelPainter(t *testing.T) {
	if _, _, ok := pixImg(nonPixelPainter{}); ok {
		t.Fatal("pixImg on a non-pixel painter should report ok=false")
	}
	// A retained logRow must be a safe no-op when handed a non-pixel painter (it
	// returns before touching any scene state).
	s := New(400, 300, ThemeFor(OSMac, false))
	(&logRow{s: s}).Draw(nonPixelPainter{}, s.theme)
}
