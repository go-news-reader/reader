package windowapp

import (
	"image"
	"testing"

	"github.com/go-widgets/toolkit"

	"github.com/go-news-reader/reader/app"
	"github.com/go-news-reader/reader/source"
	"github.com/go-news-reader/reader/ui"
)

// webPreviewApp builds an App whose feed has a selected web-linked item with a
// delivered page, so the embedded browser is the active preview.
func webPreviewApp(t *testing.T) *app.App {
	t.Helper()
	a := app.New(app.Config{Registry: source.NewRegistry(), Width: 1000, Height: 700})
	a.Scene().SetScale(1)
	a.SetWebFetchHook(func(string, int) {}) // navigation must not hit the network
	s := a.Scene()
	s.SelectPreview(source.Item{ID: "w", Source: source.HackerNews, Title: "T", Link: "https://site/"})
	page := image.NewRGBA(image.Rect(0, 0, 400, 1200))
	s.Browser().Deliver("https://site/", page.Pix, 400, 1200, 400,
		[]toolkit.BrowserLink{{Rect: image.Rect(0, 0, 400, 1200), Href: "https://site/next"}}, "T")
	a.Frame() // lay out the browser bounds
	return a
}

// TestShortcutZoom checks a Ctrl/Cmd chord on the configured zoom keys drives the
// embedded browser's zoom while the web preview is active.
func TestShortcutZoom(t *testing.T) {
	a := webPreviewApp(t)
	h := New(a)
	s := a.Scene()

	base := s.Browser().Zoom()
	h.Shortcut('=', true, false) // Ctrl + '=' -> zoom in
	in := s.Browser().Zoom()
	if in <= base {
		t.Fatalf("Ctrl+= did not zoom in: %v -> %v", base, in)
	}
	h.Shortcut('-', false, true) // Cmd + '-' -> zoom out
	if out := s.Browser().Zoom(); out >= in {
		t.Fatalf("Cmd+- did not zoom out: %v -> %v", in, out)
	}
}

// TestShortcutCopy checks the Cmd/Ctrl+C copy chord: it copies the current
// article via the installed clipboard writer, works in any view, is ignored
// without a modifier, and does nothing when there is nothing to copy.
func TestShortcutCopy(t *testing.T) {
	a := webPreviewApp(t) // a preview item titled "T" is selected
	h := New(a)
	var got string
	h.SetClipboardWriter(func(text string) { got = text })

	// No modifier → not a shortcut, nothing copied.
	h.Shortcut('c', false, false)
	if got != "" {
		t.Fatalf("copy without a modifier wrote %q", got)
	}
	// Ctrl+C copies the previewed article.
	h.Shortcut('c', true, false)
	if got == "" {
		t.Fatal("Ctrl+C should copy the current article")
	}
	// Cmd+Shift+C (uppercase base rune) also copies.
	got = ""
	h.Shortcut('C', false, true)
	if got == "" {
		t.Fatal("Cmd+C (uppercase base) should copy")
	}

	// A fresh app with nothing selected: the chord copies nothing and falls
	// through harmlessly (no writer call).
	b := newApp(t)
	hb := New(b)
	var wrote bool
	hb.SetClipboardWriter(func(string) { wrote = true })
	hb.Shortcut('c', true, false)
	if wrote {
		t.Fatal("copy with nothing selected should not write the clipboard")
	}
}

// TestShortcutNoOps covers every branch that must NOT zoom: no modifier, an
// unbound key, a non-feed mode, and no active web preview.
func TestShortcutNoOps(t *testing.T) {
	a := webPreviewApp(t)
	h := New(a)
	s := a.Scene()

	assertNoZoom := func(name string, fn func()) {
		before := s.Browser().Zoom()
		fn()
		if s.Browser().Zoom() != before {
			t.Fatalf("%s changed zoom: %v -> %v", name, before, s.Browser().Zoom())
		}
	}

	assertNoZoom("no modifier", func() { h.Shortcut('=', false, false) })
	assertNoZoom("unbound key", func() { h.Shortcut('x', true, false) })

	// A non-feed mode (settings) ignores the chord even with a matching key.
	a.VM().OpenSettings.Execute()
	assertNoZoom("non-feed mode", func() { h.Shortcut('=', true, false) })
	a.VM().CloseView.Execute()

	// Feed mode but no web preview active (plain, no selection): ignored.
	b := newApp(t)
	hb := New(b)
	if b.Scene().WebPreviewActive() {
		t.Fatal("fresh app should not have an active web preview")
	}
	before := b.Scene().Browser().Zoom()
	hb.Shortcut('=', true, false)
	if b.Scene().Browser().Zoom() != before {
		t.Fatal("shortcut zoomed with no active web preview")
	}
}

// TestZoomKeyFieldRouting checks the settings-view zoom-key fields focus on a
// click and that Enter commits them, applying + persisting the binding.
func TestZoomKeyFieldRouting(t *testing.T) {
	a := profApp(t)
	h := New(a)
	s := a.Scene()
	a.VM().OpenSettings.Execute()

	click(t, h, ui.HitFocusZoomIn)
	if s.Focus() != ui.FocusZoomIn {
		t.Fatalf("zoom-in field not focused: %d", s.Focus())
	}
	s.TypeRune('+')
	h.Key("Enter", 0) // commit via commitSettingsField
	if s.BrowserZoomInKey() != "+" {
		t.Fatalf("Enter did not commit zoom-in key: %q", s.BrowserZoomInKey())
	}

	click(t, h, ui.HitFocusZoomOut)
	if s.Focus() != ui.FocusZoomOut {
		t.Fatalf("zoom-out field not focused: %d", s.Focus())
	}
	s.TypeRune('_')
	// Done commits (CommitZoomKeys) and returns to the feed.
	click(t, h, ui.HitCloseSettings)
	if s.Mode() != ui.ModeFeed {
		t.Fatal("Done did not close settings")
	}
	if s.BrowserZoomOutKey() != "_" {
		t.Fatalf("Done did not commit zoom-out key: %q", s.BrowserZoomOutKey())
	}
}
