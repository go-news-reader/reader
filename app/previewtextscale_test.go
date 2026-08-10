package app

import (
	"path/filepath"
	"testing"

	"github.com/go-news-reader/reader/internal/settings"
	"github.com/go-news-reader/reader/ui"
)

// TestAdjustPreviewTextScalePersistsAndReports drives the A−/A+ app action: it
// nudges the reader-text scale by ±PreviewTextStep, persists the pure-display
// preference through the store (without re-aggregating), and reports the new
// size on the status line.
func TestAdjustPreviewTextScalePersistsAndReports(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	a := New(Config{
		Registry: newReg(), Store: settings.NewStore(path), OS: ui.OSMac,
	})
	var refreshed int
	a.SetRefreshHook(func() { refreshed++ })

	start := a.Scene().PreviewTextScale() // seeded default 1.25
	a.AdjustPreviewTextScale(PreviewTextStep)

	if got := a.Scene().PreviewTextScale(); got != start+PreviewTextStep {
		t.Fatalf("scale after +step = %v, want %v", got, start+PreviewTextStep)
	}
	if refreshed != 0 {
		t.Fatalf("a pure-display scale change must not re-aggregate (refreshed=%d)", refreshed)
	}
	if got := a.Scene().Status; got != "Reader text size 135%" {
		t.Fatalf("status = %q, want %q", got, "Reader text size 135%")
	}
	// The change was persisted to disk.
	loaded, err := settings.NewStore(path).Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.PreviewTextScale != start+PreviewTextStep {
		t.Fatalf("persisted scale = %v, want %v", loaded.PreviewTextScale, start+PreviewTextStep)
	}

	// Decreasing reflects too.
	a.AdjustPreviewTextScale(-PreviewTextStep)
	if got := a.Scene().PreviewTextScale(); got != start {
		t.Fatalf("scale after -step = %v, want %v", got, start)
	}
}

// TestAdjustPreviewTextScaleClampsAtBounds drives the scale hard against each
// bound and asserts the scene clamps (and reports) at the limits.
func TestAdjustPreviewTextScaleClampsAtBounds(t *testing.T) {
	a := New(Config{Registry: newReg()}) // no store → persistSettings no-op branch
	a.SetRefreshHook(func() {})

	for i := 0; i < 100; i++ {
		a.AdjustPreviewTextScale(PreviewTextStep)
	}
	if got := a.Scene().PreviewTextScale(); got != settings.MaxPreviewTextScale {
		t.Fatalf("scale did not clamp at max: %v", got)
	}
	if got := a.Scene().Status; got != "Reader text size 250%" {
		t.Fatalf("max status = %q", got)
	}

	for i := 0; i < 100; i++ {
		a.AdjustPreviewTextScale(-PreviewTextStep)
	}
	if got := a.Scene().PreviewTextScale(); got != settings.MinPreviewTextScale {
		t.Fatalf("scale did not clamp at min: %v", got)
	}
	if got := a.Scene().Status; got != "Reader text size 80%" {
		t.Fatalf("min status = %q", got)
	}
}
