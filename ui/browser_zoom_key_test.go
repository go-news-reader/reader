package ui

import (
	"testing"

	"github.com/go-news-reader/reader/internal/settings"
	"github.com/go-news-reader/reader/source"
)

// TestBrowserZoomKeyDefaults checks a fresh scene binds '=' to zoom-in and '-'
// to zoom-out, and reports them as 1-rune strings.
func TestBrowserZoomKeyDefaults(t *testing.T) {
	s := New(900, 640, nil)
	if s.BrowserZoomInKey() != "=" || s.BrowserZoomOutKey() != "-" {
		t.Fatalf("defaults = %q / %q, want = / -", s.BrowserZoomInKey(), s.BrowserZoomOutKey())
	}
	if s.ZoomKeyDir('=') != 1 || s.ZoomKeyDir('-') != -1 {
		t.Fatalf("ZoomKeyDir defaults = %d / %d", s.ZoomKeyDir('='), s.ZoomKeyDir('-'))
	}
	// A rune bound to neither, and the zero rune, are direction 0.
	if s.ZoomKeyDir('x') != 0 || s.ZoomKeyDir(0) != 0 {
		t.Fatalf("ZoomKeyDir unbound/zero = %d / %d", s.ZoomKeyDir('x'), s.ZoomKeyDir(0))
	}
}

// TestSetBrowserZoomKeys reconfigures the bindings, and leaves each binding
// unchanged for a blank or multi-rune value.
func TestSetBrowserZoomKeys(t *testing.T) {
	s := New(900, 640, nil)
	s.SetBrowserZoomKeys("+", "_")
	if s.BrowserZoomInKey() != "+" || s.BrowserZoomOutKey() != "_" {
		t.Fatalf("set = %q / %q", s.BrowserZoomInKey(), s.BrowserZoomOutKey())
	}
	if s.ZoomKeyDir('+') != 1 || s.ZoomKeyDir('_') != -1 {
		t.Fatalf("dir after set = %d / %d", s.ZoomKeyDir('+'), s.ZoomKeyDir('_'))
	}
	// Blank and multi-rune values are ignored, so the previous bindings survive.
	s.SetBrowserZoomKeys("", "ab")
	if s.BrowserZoomInKey() != "+" || s.BrowserZoomOutKey() != "_" {
		t.Fatalf("blank/multi changed bindings: %q / %q", s.BrowserZoomInKey(), s.BrowserZoomOutKey())
	}
}

// TestZoomBrowserInOut drives the MVVM zoom commands through the scene helpers
// and checks the embedded browser's zoom moves in the right direction.
func TestZoomBrowserInOut(t *testing.T) {
	s := New(900, 640, nil)
	base := s.Browser().Zoom()
	s.ZoomBrowserIn()
	in := s.Browser().Zoom()
	if in <= base {
		t.Fatalf("ZoomBrowserIn did not increase zoom: %v -> %v", base, in)
	}
	s.ZoomBrowserOut()
	if out := s.Browser().Zoom(); out >= in {
		t.Fatalf("ZoomBrowserOut did not decrease zoom: %v -> %v", in, out)
	}
}

// TestZoomKeyFieldEditing exercises the settings-view zoom-key fields: focus,
// typing a single rune (which replaces the buffer), backspace clearing it, and
// commit applying to the bindings and persisting through the snapshot.
func TestZoomKeyFieldEditing(t *testing.T) {
	s := New(900, 640, nil)
	s.SetProfiles([]settings.Profile{{Name: "Home", Subs: []source.Subscription{
		{Source: source.Reddit, Channel: "golang"},
	}}}, 0)
	s.OpenSettings()
	// Buffers seed from the current bindings.
	if s.zoomInInput != "=" || s.zoomOutInput != "-" {
		t.Fatalf("seed buffers = %q / %q", s.zoomInInput, s.zoomOutInput)
	}

	// Focus + type into the zoom-in field: a second rune replaces the first.
	s.FocusZoomIn()
	if s.Focus() != FocusZoomIn {
		t.Fatalf("focus = %d, want FocusZoomIn", s.Focus())
	}
	s.TypeRune('a')
	s.TypeRune('+')
	if s.zoomInInput != "+" {
		t.Fatalf("zoom-in buffer = %q, want single '+'", s.zoomInInput)
	}
	// focusedField reaches the zoom-in buffer, so Backspace clears it.
	if f := s.focusedField(); f != &s.zoomInInput {
		t.Fatal("focusedField did not return the zoom-in buffer")
	}
	s.Backspace()
	if s.zoomInInput != "" {
		t.Fatalf("backspace did not clear zoom-in buffer: %q", s.zoomInInput)
	}
	// Restore a value so commit applies it.
	s.TypeRune('+')

	// Focus + type into the zoom-out field.
	s.FocusZoomOut()
	if s.Focus() != FocusZoomOut || s.focusedField() != &s.zoomOutInput {
		t.Fatalf("zoom-out focus/field wrong: %d", s.Focus())
	}
	s.TypeRune('_')
	if s.zoomOutInput != "_" {
		t.Fatalf("zoom-out buffer = %q", s.zoomOutInput)
	}

	// Commit applies both bindings.
	s.CommitZoomKeys()
	if s.BrowserZoomInKey() != "+" || s.BrowserZoomOutKey() != "_" {
		t.Fatalf("commit bindings = %q / %q", s.BrowserZoomInKey(), s.BrowserZoomOutKey())
	}
	// The persistence snapshot round-trips the two keys.
	if set := s.Settings(); set.ZoomInKey != "+" || set.ZoomOutKey != "_" {
		t.Fatalf("snapshot = %q / %q", set.ZoomInKey, set.ZoomOutKey)
	}
}

// TestZoomKeyHitRegions checks a click on each zoom-key field resolves to its
// focus hit, with precise rects inside the laid-out field bounds.
func TestZoomKeyHitRegions(t *testing.T) {
	s := New(900, 640, nil)
	s.SetProfiles([]settings.Profile{{Name: "Home"}}, 0)
	s.OpenSettings()
	s.layoutSettings()

	if s.sZoomInR.W <= 0 || s.sZoomOutR.W <= 0 {
		t.Fatalf("zoom-key rects not laid out: %+v / %+v", s.sZoomInR, s.sZoomOutR)
	}
	// The two fields do not overlap (zoom-out sits to the right of zoom-in).
	if s.sZoomOutR.X <= s.sZoomInR.X+s.sZoomInR.W {
		t.Fatalf("zoom-out field overlaps zoom-in: %+v / %+v", s.sZoomInR, s.sZoomOutR)
	}
	if h := s.hitSettings(s.sZoomInR.X+2, s.sZoomInR.Y+2); h.Kind != HitFocusZoomIn {
		t.Fatalf("zoom-in hit = %+v", h)
	}
	if h := s.hitSettings(s.sZoomOutR.X+2, s.sZoomOutR.Y+2); h.Kind != HitFocusZoomOut {
		t.Fatalf("zoom-out hit = %+v", h)
	}
}

// TestRuneHelpers covers the singleRune / runeStr edge cases directly.
func TestRuneHelpers(t *testing.T) {
	if r, ok := singleRune("="); !ok || r != '=' {
		t.Fatalf("singleRune single = %q %v", r, ok)
	}
	if _, ok := singleRune(""); ok {
		t.Fatal("singleRune empty should be !ok")
	}
	if _, ok := singleRune("ab"); ok {
		t.Fatal("singleRune multi should be !ok")
	}
	if runeStr(0) != "" {
		t.Fatalf("runeStr(0) = %q, want empty", runeStr(0))
	}
	if runeStr('=') != "=" {
		t.Fatalf("runeStr('=') = %q", runeStr('='))
	}
}
