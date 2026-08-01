package ui

import (
	"testing"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
)

// TestSettingsButtonDanger covers the local settingsButton's danger branch (still
// used by the accounts view; the settings view has moved to toolkit.Button).
func TestSettingsButtonDanger(t *testing.T) {
	s := New(200, 100, ThemeFor(OSMac, false))
	s.m = s.computeMetrics()
	w := &settingsButton{s: s, label: "Delete", danger: true}
	w.SetBounds(toolkit.Rect{X: 0, Y: 0, W: 80, H: 24})
	buf := make([]byte, 80*24*4)
	w.Draw(painter.NewPixelPainter(buf, 80, 24), s.theme)
}
