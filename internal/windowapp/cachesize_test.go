package windowapp

import (
	"testing"

	"github.com/go-news-reader/reader/ui"
)

// TestCacheSizeFieldRouting proves a click on the media-cache size field routes
// through HitFocusCacheSize to FocusCacheSize, and that Enter commits the typed
// limit via commitSettingsField.
func TestCacheSizeFieldRouting(t *testing.T) {
	a := profApp(t)
	h := New(a)
	s := a.Scene()
	a.VM().OpenSettings.Execute()

	// The media-cache section sits near the bottom; scroll it into the viewport
	// so the field is clickable in this window size.
	for i := 0; i < 40; i++ {
		s.Scroll(120)
	}

	click(t, h, ui.HitFocusCacheSize)
	if s.Focus() != ui.FocusCacheSize {
		t.Fatalf("cache-size field not focused: %d", s.Focus())
	}
	// Clear the seeded buffer, retype a fresh limit, then Enter commits it
	// (CommitCacheSize) via commitSettingsField.
	for i := 0; i < 10; i++ {
		h.Key("Backspace", 0)
	}
	for _, r := range "1024" {
		s.TypeRune(r)
	}
	h.Key("Enter", 0)
	if s.MediaCacheMB() != 1024 {
		t.Fatalf("Enter did not commit cache size: %d", s.MediaCacheMB())
	}
}
