package app

import (
	"testing"

	"github.com/go-news-reader/reader/internal/settings"
	"github.com/go-news-reader/reader/source"
	"github.com/go-news-reader/reader/ui"
)

// TestMediaCacheBudgetWiring verifies the configurable media-cache size limit
// flows from Settings into the package's mediaCacheBudget both at construction
// (New) and on a live edit (ApplySceneSettings).
func TestMediaCacheBudgetWiring(t *testing.T) {
	// Restore the process-wide budget so later tests prune against the default.
	orig := mediaCacheBudget
	t.Cleanup(func() { mediaCacheBudget = orig })

	set := settings.Default()
	set.MediaCacheMB = 512
	a := New(Config{
		Registry: newReg(fakeProv{kind: source.Reddit}),
		Settings: set,
		OS:       ui.OSMac,
		Width:    400, Height: 300,
	})
	if want := int64(512) << 20; mediaCacheBudget != want {
		t.Fatalf("budget after New = %d, want %d", mediaCacheBudget, want)
	}

	// A live edit through the scene re-applies the budget on ApplySceneSettings.
	a.Scene().SetMediaCacheMB(1024)
	a.ApplySceneSettings()
	if want := int64(1024) << 20; mediaCacheBudget != want {
		t.Fatalf("budget after ApplySceneSettings = %d, want %d", mediaCacheBudget, want)
	}
}
