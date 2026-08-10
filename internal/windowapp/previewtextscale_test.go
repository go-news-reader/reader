package windowapp

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-news-reader/reader/app"
	"github.com/go-news-reader/reader/internal/settings"
	"github.com/go-news-reader/reader/source"
	"github.com/go-news-reader/reader/ui"
)

// textScaleApp builds an App with a store and a selected text post so the
// preview pane (and its A−/A+ pills) is laid out.
func textScaleApp(t *testing.T) (*app.App, string) {
	t.Helper()
	set := &settings.Settings{
		Profiles: []settings.Profile{{Name: "Home", Subs: []source.Subscription{{Source: source.Reddit, Channel: "golang"}}}},
		Active:   0, Theme: settings.ThemeSystem,
	}
	path := filepath.Join(t.TempDir(), "s.json")
	a := app.New(app.Config{
		Registry: source.NewRegistry(), Settings: set, Store: settings.NewStore(path),
		Width: 1200, Height: 800, OS: ui.OSMac,
	})
	a.Scene().SetScale(1)
	a.SetRefreshHook(func() {})
	it := source.Item{
		ID: "t", Source: source.Reddit, Title: "A headline", Author: "u", Channel: "golang",
		Score: 1, Body: strings.Repeat("word ", 50),
	}
	a.Scene().SetItems([]source.Item{it})
	a.Scene().SelectPreview(it)
	a.Scene().Draw(make([]byte, a.Scene().W*a.Scene().H*4)) // lay out the pills
	return a, path
}

// TestRouteTextZoomButtons proves a click on each A−/A+ pill routes through
// MouseDown to App.AdjustPreviewTextScale: the scale changes in the right
// direction and the change persists to the store.
func TestRouteTextZoomButtons(t *testing.T) {
	a, path := textScaleApp(t)
	h := New(a)
	s := a.Scene()

	smaller, larger, shown := s.TextZoomButtons()
	if !shown {
		t.Fatal("pills not shown")
	}
	start := s.PreviewTextScale()

	// A+ grows the reader text and persists.
	h.MouseDown(larger.X+larger.W/2, larger.Y+larger.H/2)
	up := s.PreviewTextScale()
	if up <= start {
		t.Fatalf("A+ did not grow scale: %v !> %v", up, start)
	}
	if loaded, err := settings.NewStore(path).Load(); err != nil {
		t.Fatal(err)
	} else if loaded.PreviewTextScale != up {
		t.Fatalf("A+ not persisted: %v != %v", loaded.PreviewTextScale, up)
	}

	// A− shrinks it again.
	h.MouseDown(smaller.X+smaller.W/2, smaller.Y+smaller.H/2)
	if down := s.PreviewTextScale(); down >= up {
		t.Fatalf("A− did not shrink scale: %v !< %v", down, up)
	}
}

// TestRouteTextZoomPlainClickUnaffected checks a click elsewhere in the pane
// (not on a pill) leaves the reader-text scale untouched.
func TestRouteTextZoomPlainClickUnaffected(t *testing.T) {
	a, _ := textScaleApp(t)
	h := New(a)
	s := a.Scene()
	before := s.PreviewTextScale()

	// A point in the pane body well below the pills is a passive text surface, not
	// a zoom control. Derive it from a pill's rect (no public pane accessor).
	smaller, _, _ := s.TextZoomButtons()
	px, py := smaller.X+smaller.W/2, smaller.Y+300
	if s.HitTest(px, py).Kind == ui.HitPreviewTextSmaller {
		t.Fatal("body point unexpectedly hit a pill")
	}
	h.MouseDown(px, py)
	h.MouseUp(px, py)

	if after := s.PreviewTextScale(); after != before {
		t.Fatalf("plain click changed the scale: %v != %v", after, before)
	}
}
