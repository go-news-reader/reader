package ui

import (
	"image"
	"testing"

	"github.com/go-news-reader/reader/source"
)

// TestWebURLFocusEdit covers focusing the address field, typing/backspacing into
// it, and committing (with and without a scheme, and the empty no-op).
func TestWebURLFocusEdit(t *testing.T) {
	s := New(1000, 700, ThemeFor(OSMac, false))

	// Focus on a scene with no preview just flips the flag (nothing to seed).
	s.FocusWebURL(true)
	if !s.WebURLFocused() {
		t.Fatal("FocusWebURL(true) should focus even without a preview")
	}
	s.FocusWebURL(false)
	if s.WebURLFocused() {
		t.Fatal("FocusWebURL(false) should defocus")
	}

	// With a previewed web page, focus seeds the buffer from the current URL and
	// drops any topbar search focus.
	s.SelectPreview(webTestItem())
	s.SetPreviewWeb("h1", image.NewRGBA(image.Rect(0, 0, 400, 800)), nil, 400)
	s.InitWebHistory("h1", "https://example.com/a")
	s.FocusSearch(true)
	s.FocusWebURL(true)
	if s.SearchFocused() {
		t.Fatal("focusing the address field must drop search focus")
	}
	if s.webURLDisplay("h1") != "https://example.com/a" {
		t.Fatalf("seeded display = %q", s.webURLDisplay("h1"))
	}

	// Typing + backspace edit the buffer (feed mode routes here).
	s.Backspace()
	s.TypeRune('X')
	if got := s.webURLDisplay("h1"); got != "https://example.com/X" {
		t.Fatalf("after edit display = %q", got)
	}

	// Commit normalises (already has a scheme here) and defocuses.
	if u, ok := s.CommitWebURL(); !ok || u != "https://example.com/X" {
		t.Fatalf("commit = %q,%v", u, ok)
	}
	if s.WebURLFocused() {
		t.Fatal("commit should defocus")
	}

	// A bare host gains https://.
	s.FocusWebURL(true)
	s.webURLBuf = "news.ycombinator.com"
	if u, ok := s.CommitWebURL(); !ok || u != "https://news.ycombinator.com" {
		t.Fatalf("bare-host commit = %q,%v", u, ok)
	}
	// An http:// URL is left as-is.
	s.FocusWebURL(true)
	s.webURLBuf = "http://plain/"
	if u, _ := s.CommitWebURL(); u != "http://plain/" {
		t.Fatalf("http commit = %q", u)
	}
	// An empty buffer is a no-op.
	s.FocusWebURL(true)
	s.webURLBuf = "   "
	if u, ok := s.CommitWebURL(); ok || u != "" {
		t.Fatalf("empty commit = %q,%v want \"\",false", u, ok)
	}

	// CurrentWebURL on an unknown item is empty.
	if s.CurrentWebURL("nope") != "" {
		t.Fatal("unknown item should have no current URL")
	}
}

// TestAddressFieldDraw exercises the toolbar's address-field drawing: unfocused
// (shows the URL), focused (focus ring + caret), and the empty-history
// placeholder — plus a hit on the field routing to HitWebURL.
func TestAddressFieldDraw(t *testing.T) {
	s := New(1100, 720, ThemeFor(OSMac, false))
	buf := make([]byte, s.W*s.H*4)
	s.SelectPreview(webTestItem())
	s.SetPreviewWeb("h1", image.NewRGBA(image.Rect(0, 0, 500, 1400)), nil, 500)
	s.InitWebHistory("h1", "https://example.com/a")

	// Unfocused: the toolbar (with URL text) lays out; the field is hit-testable.
	s.Draw(buf)
	if s.previewURLR.W == 0 {
		t.Fatal("address field should be shown for a web preview")
	}
	if hit, _ := s.previewHitTest(s.previewURLR.X+3, s.previewURLR.Y+3); hit.Kind != HitWebURL {
		t.Fatalf("address field hit = %+v, want HitWebURL", hit)
	}
	// The reload chip is always present for a web page and routes to HitWebReload.
	if s.previewReloadR.W == 0 {
		t.Fatal("reload chip should be shown for a web preview")
	}
	if hit, _ := s.previewHitTest(s.previewReloadR.X+2, s.previewReloadR.Y+2); hit.Kind != HitWebReload {
		t.Fatalf("reload chip hit = %+v, want HitWebReload", hit)
	}

	// Focused: draws the focus ring + caret and the being-typed buffer.
	s.FocusWebURL(true)
	s.Draw(buf)

	// Empty history → placeholder text path.
	s.SelectPreview(source.Item{ID: "r1", Source: source.Reddit, Title: "x", Link: "https://y"})
	s.SetPreviewWeb("r1", image.NewRGBA(image.Rect(0, 0, 500, 900)), nil, 500)
	// (no InitWebHistory → CurrentWebURL == "")
	s.Draw(buf)
}

// TestClipTextRight covers the address-field URL truncation helper: fits,
// non-positive width, a mid-length cut (ellipsis + tail), and a width so small
// only the ellipsis remains.
func TestClipTextRight(t *testing.T) {
	s := New(900, 600, ThemeFor(OSMac, false))
	s.Draw(make([]byte, s.W*s.H*4)) // lay out metrics so the text face is ready
	f := s.m.side
	const long = "https://example.com/very/long/path/that/does/not/fit"

	if got := clipTextRight(f, "short", 100000); got != "short" {
		t.Fatalf("fits: %q", got)
	}
	if got := clipTextRight(f, "short", 0); got != "short" {
		t.Fatalf("w<=0: %q", got)
	}
	mid := clipTextRight(f, long, f.width(long)/2)
	if mid == long || []rune(mid)[0] != '…' {
		t.Fatalf("mid cut = %q, want ellipsis-prefixed and shorter", mid)
	}
	if got := clipTextRight(f, long, 1); got != "…" {
		t.Fatalf("tiny width = %q, want just the ellipsis", got)
	}
}
