package windowapp

import (
	"testing"

	"github.com/go-news-reader/reader/app"
	"github.com/go-news-reader/reader/source"
	"github.com/go-news-reader/reader/ui"
)

// groupApp builds an App whose feed holds one groupable Usenet post.
func groupApp(t *testing.T) *app.App {
	t.Helper()
	a := app.New(app.Config{Registry: source.NewRegistry(), Width: 1000, Height: 700})
	a.Scene().SetScale(1)
	a.Scene().SetSubs(nil)
	a.Scene().SetItems([]source.Item{
		{ID: "d1", Source: source.Usenet, Channel: "a.b.test", Permalink: "news:d1",
			Title: `[1/2] - "release.tar.zst" yEnc (1/1) 1000`},
		{ID: "d2", Source: source.Usenet, Channel: "a.b.test", Permalink: "news:d2",
			Title: `[2/2] - "release.tar.zst.par2" yEnc (1/1) 200`},
	})
	return a
}

func TestMouseDownToggleGroup(t *testing.T) {
	a := groupApp(t)
	h := New(a)
	if a.Scene().GroupExpanded("release") {
		t.Fatal("group should start collapsed")
	}
	click(t, h, ui.HitToggleGroup)
	if !a.Scene().GroupExpanded("release") {
		t.Fatal("clicking the header should expand the group")
	}
}

func TestMouseDownReconstruct(t *testing.T) {
	a := groupApp(t)
	var gotBase string
	a.SetReconstructHook(func(base string) { gotBase = base })
	h := New(a)
	click(t, h, ui.HitReconstruct)
	if gotBase != "release" {
		t.Fatalf("reconstruct routed base = %q, want release", gotBase)
	}
}
