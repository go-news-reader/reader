package ui

import (
	"image"
	"testing"

	"github.com/go-news-reader/reader/internal/settings"
	"github.com/go-news-reader/reader/source"
)

func TestUnsubscribeActive(t *testing.T) {
	s := New(1200, 700, ThemeFor(OSMac, false))
	s.SetProfiles([]settings.Profile{{Name: "Home", Subs: []source.Subscription{
		{Source: source.Usenet, Channel: "control"},
		{Source: source.Reddit, Channel: "golang"},
	}}}, 0)
	if !s.IsSubscribed(source.Usenet, "CONTROL") { // case-insensitive
		t.Fatal("expected control subscribed")
	}
	if !s.UnsubscribeActive(source.Usenet, "control") {
		t.Fatal("unsubscribe should report removed")
	}
	if s.IsSubscribed(source.Usenet, "control") {
		t.Fatal("control still subscribed after removal")
	}
	if s.UnsubscribeActive(source.Usenet, "control") {
		t.Fatal("removing an absent sub should report false")
	}
	// No active profile → false.
	s2 := New(1200, 700, ThemeFor(OSMac, false))
	if s2.UnsubscribeActive(source.Usenet, "x") {
		t.Fatal("no active profile should report false")
	}
}

func TestBrowseUnsubscribeHit(t *testing.T) {
	s := New(760, 460, ThemeFor(OSMac, false))
	s.SetProfiles([]settings.Profile{{Name: "Home", Subs: []source.Subscription{
		{Source: source.Usenet, Channel: "control"},
	}}}, 0)
	s.SetUsenetServer("news.free.fr:119")
	s.SetBrowseGroups(gis("control", "junk"))
	s.OpenBrowse()
	s.layoutBrowse()
	// The subscribed "control" leaf's ✓ marker unsubscribes; the unsubscribed
	// "junk" leaf's ＋ subscribes.
	for _, r := range s.browseRows {
		top := s.browseTreeTop + r.top - s.browseScrollY
		sr := s.browseSubscribeRect(s.m.pad, top, s.W-2*s.m.pad)
		h := s.browseHitTest(sr.X+sr.W/2, sr.Y+sr.H/2)
		switch r.node.Name {
		case "control":
			if h.Kind != HitUnsubscribeGroup || h.Value != "control" {
				t.Fatalf("subscribed leaf = %+v, want HitUnsubscribeGroup", h)
			}
		case "junk":
			if h.Kind != HitSubscribeGroup || h.Value != "junk" {
				t.Fatalf("unsubscribed leaf = %+v, want HitSubscribeGroup", h)
			}
		}
	}
	// A childless subscribed leaf row (not on the marker) also unsubscribes.
	r := s.browseRows[0] // "control"
	top := s.browseTreeTop + r.top - s.browseScrollY
	if h := s.browseHitTest(s.m.pad+2, top+s.m.sideItemH/2); h.Kind != HitUnsubscribeGroup {
		t.Fatalf("subscribed leaf row = %+v, want HitUnsubscribeGroup", h)
	}
}

func TestImagePrefetch(t *testing.T) {
	s := New(1200, 700, ThemeFor(OSMac, false))
	s.SetSubs(nil)
	s.SetItems([]source.Item{
		usenetItem("d1", `[1/2] - "release.tar.zst" yEnc (1/1) 10`), // groups → base "release"
		usenetItem("d2", `[2/2] - "release.tar.zst.par2" yEnc (1/1) 10`),
		{ID: "img", Source: source.Usenet, Permalink: "news:<imgid>", Title: `"photo.jpg" yEnc (1/1)`},
		{ID: "txt", Source: source.Usenet, Permalink: "news:txtid", Title: "just a discussion"},
		{ID: "r", Source: source.Reddit, Title: "pic.jpg"},
	})
	ids := map[string][]ReconstructPart{}
	for _, r := range s.ImagePrefetch() {
		ids[r.ID] = r.Parts
	}
	if len(ids["release"]) != 2 {
		t.Fatalf("group prefetch parts = %d, want 2", len(ids["release"]))
	}
	if len(ids["img"]) != 1 || ids["img"][0].MessageID != "imgid" {
		t.Fatalf("standalone image prefetch = %+v", ids["img"])
	}
	if _, ok := ids["txt"]; ok {
		t.Fatal("a text Usenet post must not be prefetched")
	}
	if _, ok := ids["r"]; ok {
		t.Fatal("a non-Usenet item must not be prefetched")
	}
}

func TestPreviewDividerResize(t *testing.T) {
	s := previewScene()
	s.layout()
	s.layoutPreview()
	// The grip at the pane's left edge starts a resize drag.
	if h := s.HitTest(s.previewR.X, s.previewR.Y+20); h.Kind != HitPreviewDivider {
		t.Fatalf("pane grip hit = %+v, want HitPreviewDivider", h)
	}
	before := s.previewWidth()
	s.BeginPreviewResize()
	if !s.DraggingPreview() {
		t.Fatal("should be dragging the pane")
	}
	s.MouseMove(s.previewR.X-60, s.previewR.Y+20) // drag left => wider pane
	s.EndPreviewResize()
	if s.DraggingPreview() {
		t.Fatal("drag should have ended")
	}
	if s.previewWidth() <= before {
		t.Fatalf("pane did not widen: %d -> %d", before, s.previewWidth())
	}
	// Over-wide drag clamps to the available width; too-narrow clamps to the min.
	s.previewUserW = 100000
	s.layout()
	wideAvail := s.W - s.m.sidebarW - rpxOf(s, feedKeepW)
	if s.previewWidth() != wideAvail {
		t.Fatalf("over-wide pane = %d, want avail %d", s.previewWidth(), wideAvail)
	}
	s.previewUserW = 1
	if s.previewWidth() != rpxOf(s, previewMinW) {
		t.Fatalf("too-narrow pane = %d, want min %d", s.previewWidth(), rpxOf(s, previewMinW))
	}
}

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
