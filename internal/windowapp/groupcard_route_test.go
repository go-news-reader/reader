package windowapp

import (
	"testing"

	"github.com/go-news-reader/reader/app"
	"github.com/go-news-reader/reader/source"
	"github.com/go-news-reader/reader/ui"
)

// usenetGroupApp builds an app whose feed shows one complete multipart Usenet
// post (base "rel"), so its group card draws the chevron, the download checkbox
// and the Reconstruct pill. Downloads run inline (SetDownloadSync) so a routed
// toggle never spawns a goroutine that races the assertions.
func usenetGroupApp(t *testing.T) *app.App {
	t.Helper()
	a := newApp(t)
	a.SetDownloadSync()
	s := a.Scene()
	s.SetSubs(nil)
	s.SetItems([]source.Item{
		{ID: "a1", Source: source.Usenet, Channel: "alt.bin", Created: 1000,
			Title: `[1/2] - "rel.part1.rar" yEnc (1/1) 100`},
		{ID: "a2", Source: source.Usenet, Channel: "alt.bin", Created: 2000,
			Title: `[2/2] - "rel.part2.rar" yEnc (1/1) 100`},
	})
	s.HitTest(0, 0) // force a layout so the group row is placed
	return a
}

// TestRouteToggleGroup covers the HitToggleGroup route: clicking a group card's
// disclosure chevron expands (then collapses) the post's member list.
func TestRouteToggleGroup(t *testing.T) {
	a := usenetGroupApp(t)
	h := New(a)
	if a.Scene().GroupExpanded("rel") {
		t.Fatal("the group starts collapsed")
	}
	click(t, h, ui.HitToggleGroup)
	if !a.Scene().GroupExpanded("rel") {
		t.Fatal("clicking the chevron should expand the group")
	}
}

// TestRouteReconstruct covers the HitReconstruct route: clicking a complete
// post's Reconstruct pill fires the app's reconstruction trigger for its base.
func TestRouteReconstruct(t *testing.T) {
	a := usenetGroupApp(t)
	var got string
	a.SetReconstructHook(func(base string) { got = base })
	h := New(a)
	click(t, h, ui.HitReconstruct)
	if got != "rel" {
		t.Fatalf("Reconstruct fired for %q, want %q", got, "rel")
	}
}

// TestRouteToggleDownload covers the HitToggleDownload route: clicking a complete
// post's download checkbox queues it into the download panel.
func TestRouteToggleDownload(t *testing.T) {
	a := usenetGroupApp(t)
	h := New(a)
	if len(a.Scene().Downloads()) != 0 {
		t.Fatal("the download panel starts empty")
	}
	click(t, h, ui.HitToggleDownload)
	dls := a.Scene().Downloads()
	if len(dls) != 1 || dls[0].ID != "rel" {
		t.Fatalf("toggling the checkbox should queue the post; got %+v", dls)
	}
}
