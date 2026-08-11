package windowapp

import (
	"testing"

	"github.com/go-news-reader/reader/ui"
)

// TestCacheBackendFieldRouting proves a click on the cache-backend (plugin path)
// field routes through HitFocusCacheBackend to FocusCacheBackend, and that Enter
// commits the typed path via commitSettingsField.
func TestCacheBackendFieldRouting(t *testing.T) {
	a := profApp(t)
	h := New(a)
	s := a.Scene()
	a.VM().OpenSettings.Execute()

	// The cache-backend field sits near the bottom; scroll it into the viewport so
	// the field is clickable in this window size.
	for i := 0; i < 40; i++ {
		s.Scroll(120)
	}

	click(t, h, ui.HitFocusCacheBackend)
	if s.Focus() != ui.FocusCacheBackend {
		t.Fatalf("cache-backend field not focused: %d", s.Focus())
	}
	// Clear the seeded buffer, type a plugin path, then Enter commits it
	// (CommitCacheBackend) via commitSettingsField.
	for i := 0; i < 40; i++ {
		h.Key("Backspace", 0)
	}
	for _, r := range "/opt/shared" {
		s.TypeRune(r)
	}
	h.Key("Enter", 0)
	if s.CacheBackend() != "/opt/shared" {
		t.Fatalf("Enter did not commit cache backend: %q", s.CacheBackend())
	}
}
