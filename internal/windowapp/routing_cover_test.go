package windowapp

import (
	"testing"

	"github.com/go-news-reader/reader/provider/usenet"
	"github.com/go-news-reader/reader/ui"
)

// TestRoutePreviewGroup covers the HitPreviewGroup route: clicking a Usenet post
// group's card body previews the reconstructed image in the right pane.
func TestRoutePreviewGroup(t *testing.T) {
	a := groupApp(t)
	previewed := ""
	a.SetPreviewFetchHook(func(id string, _ []usenet.ReconstructPart) { previewed = id }) // deterministic
	h := New(a)
	click(t, h, ui.HitPreviewGroup)
	if previewed != "release" {
		t.Fatalf("preview routed for %q, want release", previewed)
	}
}

// TestRouteToggleDownload covers the HitToggleDownload route: ticking a complete
// group's checkbox queues it in the download manager.
func TestRouteToggleDownload(t *testing.T) {
	a := groupApp(t)
	a.SetDownloadSync() // run the reconstruct inline so the click is deterministic
	h := New(a)
	click(t, h, ui.HitToggleDownload)
	if !a.Scene().IsDownloadQueued("release") && len(a.Scene().Downloads()) == 0 {
		t.Fatal("toggling the checkbox should register the download")
	}
}

// TestRouteClearDownloads covers the HitClearDownloads route: the panel's Clear
// button drops finished rows.
func TestRouteClearDownloads(t *testing.T) {
	a := newApp(t)
	a.Scene().SetSubs(nil)
	a.Scene().SetDownloads([]ui.DownloadItem{{ID: "x", Name: "x", Status: ui.DLDone}})
	h := New(a)
	click(t, h, ui.HitClearDownloads)
	if len(a.Scene().Downloads()) != 0 {
		t.Fatalf("Clear should empty the panel, still %d", len(a.Scene().Downloads()))
	}
}

// TestRouteTypeRune covers the Key default: a printable rune is inserted into the
// focused search field.
func TestRouteTypeRune(t *testing.T) {
	a := newApp(t)
	h := New(a)
	a.VM().FocusSearch(true)
	h.Key("", 'q')
	if got := a.Scene().Search(); got != "q" {
		t.Fatalf("typed rune = %q, want q", got)
	}
}
