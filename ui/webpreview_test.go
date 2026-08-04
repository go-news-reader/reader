package ui

import (
	"image"
	"testing"

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
	s.SetPreviewWeb("h1", page)
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
	s.SetPreviewWeb("e", nil)
	if s.HasWeb("e") || s.WebLoading() {
		t.Fatalf("nil delivery: has=%v loading=%v, want false,false", s.HasWeb("e"), s.WebLoading())
	}

	// PreviewWebWidth is floored + positive.
	if s.PreviewWebWidth() < 320 {
		t.Errorf("PreviewWebWidth = %d, want >= 320", s.PreviewWebWidth())
	}

	// A zero-width rendered image reserves no box (guards against /0).
	s.SelectPreview(source.Item{ID: "z", Source: source.Reddit, Title: "z", Link: "https://z"})
	s.SetPreviewWeb("z", image.NewRGBA(image.Rect(0, 0, 0, 10)))
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
