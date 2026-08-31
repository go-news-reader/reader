package windowapp

import (
	"testing"

	"github.com/go-widgets/toolkit"
)

// TestNativeSettingsFieldCommitsOnEnter proves a settings text field backed by a
// native OS control drives the app the same way the drawn field does: its OnText
// writes the buffer, and its OnActivate (the native entry's Enter) commits through
// commitSettingsField — persisting the value and re-aggregating — without the
// keyboard ever reaching the Surface. It mirrors TestCacheBackendFieldRouting, but
// exercises the native path the Handler wires in NativeControls.
func TestNativeSettingsFieldCommitsOnEnter(t *testing.T) {
	a := profApp(t)
	h := New(a)
	s := a.Scene()
	a.VM().OpenSettings.Execute()

	// Draw so the settings view accumulates its native controls, then read them
	// through the Handler, which wires each entry's Enter to commitSettingsField.
	h.Frame()
	var backend toolkit.NativeControl
	found := false
	for _, c := range h.NativeControls() {
		if c.Key == "set:cachebackend" {
			backend, found = c, true
			break
		}
	}
	if !found {
		t.Fatal("settings view published no native control for the cache-backend field")
	}
	if backend.OnText == nil || backend.OnActivate == nil {
		t.Fatalf("cache-backend native entry missing callbacks: OnText=%v OnActivate=%v",
			backend.OnText != nil, backend.OnActivate != nil)
	}

	// A person types a plugin path into the native field, then presses Enter.
	backend.OnText("/opt/shared")
	backend.OnActivate()

	if got := s.CacheBackend(); got != "/opt/shared" {
		t.Fatalf("native Enter did not commit the cache backend: %q", got)
	}
}
