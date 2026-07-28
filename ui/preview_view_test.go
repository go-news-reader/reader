package ui

import (
	"image"
	"testing"

	"github.com/go-news-reader/reader/source"
)

func previewScene() *Scene {
	s := New(1200, 700, ThemeFor(OSMac, false))
	s.SetSubs([]Subscription{{Source: source.Usenet, Channel: "alt.binaries.pictures"}})
	s.SetItems([]source.Item{
		{ID: "1", Source: source.Usenet, Channel: "alt.binaries.pictures",
			Title: "sunset.jpg", Body: "a nice picture", Media: []source.Media{{Kind: source.MediaImage}}},
		{ID: "2", Source: source.HackerNews, Title: "Show HN"},
	})
	return s
}

func synthRGBA(w, h int) *image.RGBA {
	im := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(im.Pix); i += 4 {
		im.Pix[i], im.Pix[i+1], im.Pix[i+2], im.Pix[i+3] = 0x30, 0x90, 0xE0, 0xFF
	}
	return im
}

func TestPreviewWidthVisibility(t *testing.T) {
	s := previewScene()
	s.layout()
	if s.previewWidth() <= 0 {
		t.Fatal("preview pane should show in a wide feed window")
	}
	// Not the feed view → no pane.
	s.OpenSettings()
	if s.previewWidth() != 0 {
		t.Fatal("preview pane must hide outside the feed view")
	}
	// Narrow window → pane hides to keep a usable feed.
	narrow := New(MinW, 700, ThemeFor(OSMac, false))
	narrow.layout()
	if narrow.previewWidth() != 0 {
		t.Fatalf("narrow window should hide the pane, got %d", narrow.previewWidth())
	}
}

func TestFeedGeomSubtractsPane(t *testing.T) {
	s := previewScene()
	s.layout()
	_, w := s.feedGeom()
	full := s.W - s.m.sidebarW - 2*s.m.pad
	if w >= full {
		t.Fatalf("feed width %d not reduced by the pane (full %d)", w, full)
	}
	if w < 0 {
		t.Fatal("feed width went negative")
	}
}

func TestSelectAndFinishPreview(t *testing.T) {
	s := previewScene()
	s.layout()
	if _, ok := s.PreviewItem(); ok {
		t.Fatal("nothing should be selected initially")
	}
	s.SelectPreview(s.rows[0].item)
	it, ok := s.PreviewItem()
	if !ok || it.ID != "1" {
		t.Fatalf("preview item = %+v ok=%v", it, ok)
	}
	s.SetPreviewLoading(true)
	if !s.previewImgPending {
		t.Fatal("loading flag not set")
	}
	// A finished image for the current item clears loading and caches the thumb.
	s.FinishPreviewImage("1", synthRGBA(40, 40))
	if s.previewImgPending {
		t.Fatal("loading not cleared for the current item")
	}
	if !s.HasThumb("1") {
		t.Fatal("thumb not cached")
	}
	// A finished image (nil) for a different id caches nothing and leaves the
	// current selection's loading state untouched.
	s.SetPreviewLoading(true)
	s.FinishPreviewImage("other", nil)
	if !s.previewImgPending {
		t.Fatal("loading for the current item must not be cleared by another id")
	}
}

func TestGroupPreviewItem(t *testing.T) {
	s := New(1200, 700, ThemeFor(OSMac, false))
	s.SetSubs(nil)
	s.SetItems([]source.Item{
		usenetItem("d1", `[1/2] - "movie.rar" yEnc (1/1) 100`),
		usenetItem("d2", `[2/2] - "movie.rar.par2" yEnc (1/1) 90`),
	})
	it, ok := s.GroupPreviewItem("movie")
	if !ok || it.ID != "movie" || it.Source != source.Usenet || len(it.Media) == 0 {
		t.Fatalf("group preview item = %+v ok=%v", it, ok)
	}
	if _, ok := s.GroupPreviewItem("nope"); ok {
		t.Fatal("unknown base must not resolve")
	}
}

func TestPreviewDrawStates(t *testing.T) {
	s := previewScene()
	buf := make([]byte, s.W*s.H*4)
	// Empty state (no selection): draws the prompt without panicking.
	s.Draw(buf)
	// Selected, pending image → spinner path.
	s.SelectPreview(s.rows[0].item)
	s.SetPreviewLoading(true)
	s.Draw(buf)
	// Selected, no thumb, not pending → media-kind label path.
	s.SetPreviewLoading(false)
	s.Draw(buf)
	// Selected, thumb present → toolkit Image widget path.
	s.SetThumb("1", synthRGBA(200, 120))
	s.Draw(buf)
	// A HackerNews item (no media) → no image box, body path only.
	s.SelectPreview(s.rows[1].item)
	s.Draw(buf)
}

func TestPreviewHitTest(t *testing.T) {
	s := previewScene()
	s.layout()
	s.SelectPreview(s.rows[0].item)
	s.layoutPreview()
	// The Open button (via the exported accessor) resolves to HitOpenPreview.
	oc, shown := s.PreviewOpenButton()
	if !shown {
		t.Fatal("Open button should be shown for a selection")
	}
	if h := s.HitTest(oc.X+oc.W/2, oc.Y+oc.H/2); h.Kind != HitOpenPreview || h.Item.ID != "1" {
		t.Fatalf("open button hit = %+v, want HitOpenPreview id=1", h)
	}
	// Elsewhere in the pane is inert (handled, HitNone).
	if h := s.HitTest(s.previewR.X+s.previewR.W/2, s.previewR.Y+s.previewR.H-4); h.Kind != HitNone {
		t.Fatalf("pane body hit = %+v, want HitNone", h)
	}
	// With no selection the Open button is absent.
	s2 := previewScene()
	s2.layout()
	s2.layoutPreview()
	if _, shown := s2.PreviewOpenButton(); shown {
		t.Fatal("Open button should be absent with no selection")
	}
}

func TestPreviewScroll(t *testing.T) {
	s := New(1200, 300, ThemeFor(OSMac, false))
	s.SetSubs(nil)
	long := ""
	for i := 0; i < 200; i++ {
		long += "word "
	}
	s.SetItems([]source.Item{{ID: "1", Source: source.Usenet, Title: "big", Body: long, Media: []source.Media{{Kind: source.MediaImage}}}})
	s.SelectPreview(source.Item{ID: "1", Source: source.Usenet, Title: "big", Body: long, Media: []source.Media{{Kind: source.MediaImage}}})
	s.layout()
	s.layoutPreview()
	if s.previewContentH <= s.previewR.H {
		t.Skip("content fits; window too tall to exercise pane scroll")
	}
	s.MouseMove(s.previewR.X+10, s.previewR.Y+10) // pointer over the pane
	before := s.previewScrollY
	s.Scroll(120)
	if s.previewScrollY <= before {
		t.Fatalf("preview did not scroll: %d -> %d", before, s.previewScrollY)
	}
}

func TestGroupHeaderPreviewVsChevron(t *testing.T) {
	s := groupScene()
	r, feedX, feedW := groupRow(s)
	// The chevron toggles (click its centre, in screen coords).
	chev := s.chevronRect(feedX, r.top)
	chevY := s.m.topbarH + chev.Y + chev.H/2
	if h := s.HitTest(chev.X+chev.W/2, chevY); h.Kind != HitToggleGroup {
		t.Fatalf("chevron hit = %+v, want HitToggleGroup", h)
	}
	// The header body (right of the chevron, left of the pill) previews.
	rr := s.reconstructRect(feedX, r.top, feedW)
	bx := (chev.X + chev.W + rr.X) / 2
	if h := s.HitTest(bx, s.m.topbarH+r.top+s.m.groupHeadH/2); h.Kind != HitPreviewGroup || h.Value != "release" {
		t.Fatalf("header body hit = %+v, want HitPreviewGroup release", h)
	}
}

func TestTightPix(t *testing.T) {
	// Origin-0, tightly strided → same backing slice.
	full := synthRGBA(4, 3)
	if &tightPix(full)[0] != &full.Pix[0] {
		t.Fatal("tight image should be passed through, not copied")
	}
	// A sub-image (non-zero origin / wide stride) → packed copy of the right size.
	sub := full.SubImage(image.Rect(1, 1, 3, 3)).(*image.RGBA)
	got := tightPix(sub)
	if len(got) != 2*2*4 {
		t.Fatalf("packed copy len = %d, want %d", len(got), 2*2*4)
	}
}
