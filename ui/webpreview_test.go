package ui

import (
	"image"
	"testing"

	"github.com/go-widgets/toolkit"

	"github.com/go-news-reader/reader/source"
)

func webTestItem() source.Item {
	return source.Item{ID: "h1", Source: source.HackerNews, Channel: "news", Title: "Title", Body: "summary text", Link: "https://example.com/a"}
}

func TestWebPreviewURL(t *testing.T) {
	if got := webPreviewURL(webTestItem()); got != "https://example.com/a" {
		t.Errorf("Link URL = %q", got)
	}
	// No Link → falls back to Permalink.
	if got := webPreviewURL(source.Item{Source: source.Reddit, Permalink: "https://p"}); got != "https://p" {
		t.Errorf("Permalink URL = %q", got)
	}
	// Usenet never web-renders.
	if got := webPreviewURL(source.Item{Source: source.Usenet, Link: "https://x"}); got != "" {
		t.Errorf("Usenet URL = %q, want empty", got)
	}
	// Non-http scheme → empty.
	if got := webPreviewURL(source.Item{Source: source.Reddit, Link: "ftp://x"}); got != "" {
		t.Errorf("ftp URL = %q, want empty", got)
	}
	// Exported wrapper.
	s := New(760, 460, ThemeFor(OSMac, false))
	if s.WebPreviewURL(webTestItem()) == "" {
		t.Error("WebPreviewURL wrapper returned empty")
	}
}

func TestPreviewWebLifecycleAndDraw(t *testing.T) {
	s := New(900, 560, ThemeFor(OSMac, false))
	buf := make([]byte, s.W*s.H*4)

	// No web yet.
	if s.HasWeb("h1") || s.webImg("h1") != nil {
		t.Fatal("fresh scene should have no web image")
	}
	s.SelectPreview(webTestItem())

	// Pending: draws a spinner box, no summary.
	s.SetPreviewWebLoading(true)
	if !s.WebLoading() {
		t.Fatal("SetPreviewWebLoading(true) should set WebLoading")
	}
	s.Draw(buf) // exercises previewContent pending branch + previewImage spinner
	if d := s.previewContent(); d.imgH <= 0 || len(d.bodyLines) != 0 {
		t.Fatalf("pending: imgH=%d bodyLines=%d, want imgH>0 and no body", d.imgH, len(d.bodyLines))
	}

	// Deliver a tall rendered page.
	page := image.NewRGBA(image.Rect(0, 0, 400, 1600))
	s.SetPreviewWeb("h1", page, []WebLink{{Rect: image.Rect(10, 20, 120, 40), Href: "https://example.com/next"}}, page.Bounds().Dx())
	if !s.HasWeb("h1") || s.WebLoading() {
		t.Fatalf("after SetPreviewWeb: has=%v loading=%v", s.HasWeb("h1"), s.WebLoading())
	}
	d := s.previewContent()
	if d.imgH <= 0 || len(d.bodyLines) != 0 {
		t.Fatalf("web present: imgH=%d bodyLines=%d, want full image + no body", d.imgH, len(d.bodyLines))
	}
	s.Draw(buf) // exercises previewContent web branch + previewImage web draw + clip

	// A failed render (nil image) only clears the loading flag.
	s.SelectPreview(source.Item{ID: "e", Source: source.Reddit, Title: "x", Link: "https://y"})
	s.SetPreviewWebLoading(true)
	s.SetPreviewWeb("e", nil, nil, 0)
	if s.HasWeb("e") || s.WebLoading() {
		t.Fatalf("nil delivery: has=%v loading=%v, want false,false", s.HasWeb("e"), s.WebLoading())
	}

	// PreviewWebWidth is floored + positive.
	if s.PreviewWebWidth() < 320 {
		t.Errorf("PreviewWebWidth = %d, want >= 320", s.PreviewWebWidth())
	}

	// A zero-width rendered image reserves no box (guards against /0).
	s.SelectPreview(source.Item{ID: "z", Source: source.Reddit, Title: "z", Link: "https://z"})
	s.SetPreviewWeb("z", image.NewRGBA(image.Rect(0, 0, 0, 10)), nil, 0)
	if dz := s.previewContent(); dz.imgH != 0 {
		t.Errorf("zero-width web image: imgH=%d, want 0", dz.imgH)
	}
}

// TestPreviewWebWidthFloor covers the width floor on a scene whose preview pane
// hasn't been laid out yet (inner width non-positive).
func TestPreviewWebWidthFloor(t *testing.T) {
	s := New(400, 300, ThemeFor(OSMac, false)) // no Draw → previewR unset
	if got := s.PreviewWebWidth(); got != 320 {
		t.Errorf("un-laid-out PreviewWebWidth = %d, want floor 320", got)
	}
}

// TestPreviewWebPendingShortPane covers the pending image box's minimum-height
// floor, hit when the pane is too short for the natural available height.
func TestPreviewWebPendingShortPane(t *testing.T) {
	s := New(900, 150, ThemeFor(OSMac, false)) // very short window → short pane
	s.SelectPreview(webTestItem())
	s.SetPreviewWebLoading(true)
	buf := make([]byte, s.W*s.H*4)
	s.Draw(buf) // lays out the (short) pane, then previewContent floors availH
	if d := s.previewContent(); d.imgH <= 0 {
		t.Fatalf("short-pane pending imgH=%d, want the floored box", d.imgH)
	}
}

func TestWebHistory(t *testing.T) {
	// PushWebURL on a fresh scene lazily creates the history map.
	fresh := New(900, 560, ThemeFor(OSMac, false))
	fresh.PushWebURL("x", "https://a/")
	if fresh.WebCanBack("x") {
		t.Fatal("a single pushed entry is not enough to go back")
	}

	s := New(900, 560, ThemeFor(OSMac, false))
	s.InitWebHistory("h1", "https://a/")
	if s.WebCanBack("h1") {
		t.Fatal("fresh history should not allow back")
	}
	if _, ok := s.WebBackURL("h1"); ok {
		t.Fatal("back on a single-entry history should fail")
	}
	s.PushWebURL("h1", "https://a/b")
	s.PushWebURL("h1", "https://a/b/c")
	if !s.WebCanBack("h1") {
		t.Fatal("should allow back after pushes")
	}
	if u, ok := s.WebBackURL("h1"); !ok || u != "https://a/b" {
		t.Fatalf("back = %q,%v want https://a/b,true", u, ok)
	}
	if u, ok := s.WebBackURL("h1"); !ok || u != "https://a/" {
		t.Fatalf("back = %q,%v want https://a/,true", u, ok)
	}
	if s.WebCanBack("h1") {
		t.Fatal("back to root should exhaust the stack")
	}
}

func TestWebLinkAt(t *testing.T) {
	s := New(900, 560, ThemeFor(OSMac, false))
	s.SelectPreview(webTestItem())
	img := image.NewRGBA(image.Rect(0, 0, 400, 1200))
	links := []WebLink{{Rect: image.Rect(10, 20, 110, 60), Href: "https://x/a"}}
	s.SetPreviewWeb("h1", img, links, 400)

	// Guard: no display box yet → miss.
	if _, ok := s.webLinkAt("h1", 5, 5); ok {
		t.Fatal("no box should miss")
	}
	// Scale 1: box == render size. A click at render (30,40) hits the link.
	s.previewImgR = toolkit.Rect{X: 100, Y: 200, W: 400, H: 1200}
	if href, ok := s.webLinkAt("h1", 130, 240); !ok || href != "https://x/a" {
		t.Fatalf("hit = %q,%v want https://x/a,true", href, ok)
	}
	// A click outside every link rect misses.
	if _, ok := s.webLinkAt("h1", 400, 800); ok {
		t.Fatal("click outside a link should miss")
	}
	// Scale 2: box half the render width → a display click at box+ (15,20) maps to
	// render (30,40), still inside the link.
	s.previewImgR = toolkit.Rect{X: 0, Y: 0, W: 200, H: 600}
	if _, ok := s.webLinkAt("h1", 15, 20); !ok {
		t.Fatal("scaled click should hit the link")
	}
}

func TestPreviewWebClickRouting(t *testing.T) {
	s := New(900, 560, ThemeFor(OSMac, false))
	s.SelectPreview(webTestItem())
	img := image.NewRGBA(image.Rect(0, 0, 400, 1200))
	links := []WebLink{{Rect: image.Rect(0, 0, 80, 30), Href: "https://x/a"}}
	s.SetPreviewWeb("h1", img, links, 400)
	buf := make([]byte, s.W*s.H*4)
	s.Draw(buf) // records previewImgR (and previewBackR, empty until we can go back)

	// A click on the link's mapped rect routes to HitWebLink with the href.
	b := s.previewImgR
	if hit, _ := s.previewHitTest(b.X+2, b.Y+2); hit.Kind != HitWebLink || hit.Value != "https://x/a" {
		t.Fatalf("web link hit = %+v, want HitWebLink https://x/a", hit)
	}

	// After navigating (history depth >1), a Back chip appears and routes to
	// HitWebBack. (The app seeds the initial history entry on preview.)
	s.InitWebHistory("h1", "https://example.com/a")
	s.PushWebURL("h1", "https://x/a")
	s.Draw(buf) // now webCanBackCurrent → previewBackR laid out
	if s.previewBackR.W == 0 {
		t.Fatal("back chip should be shown after navigation")
	}
	if hit, _ := s.previewHitTest(s.previewBackR.X+2, s.previewBackR.Y+2); hit.Kind != HitWebBack {
		t.Fatalf("back chip hit = %+v, want HitWebBack", hit)
	}
}
