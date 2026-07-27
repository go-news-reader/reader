package ui

import (
	"testing"

	"github.com/go-widgets/toolkit"

	"github.com/go-news-reader/reader/source"
)

// groupScene builds a feed with one groupable 3-part Usenet post followed by a
// standalone (non-usenet) card, on a scene with no sidebar filter.
func groupScene() *Scene {
	s := New(900, 600, ThemeFor(OSLinux, false))
	s.SetSubs(nil)
	s.SetItems([]source.Item{
		usenetItem("d1", `[1/3] - "release.tar.zst" yEnc (1/1) 100000`),
		usenetItem("d2", `[2/3] - "release.tar.zst.par2" yEnc (1/1) 940`),
		usenetItem("d3", `[3/3] - "release.tar.zst.vol00+01.par2" yEnc (1/1) 2048`),
		{ID: "r1", Source: source.Reddit, Channel: "golang", Title: "a normal card", Author: "gopher", Score: 3, Comments: 1},
	})
	return s
}

// groupRow returns the first laid-out group row and the feed origin.
func groupRow(s *Scene) (feedRow, int, int) {
	s.layout()
	for _, r := range s.rows {
		if r.group != nil {
			return r, s.m.sidebarW + s.m.pad, s.W - s.m.sidebarW - 2*s.m.pad
		}
	}
	return feedRow{}, 0, 0
}

// regionHas reports whether the RGBA buffer has a pixel of colour c inside rect.
func regionHas(buf []byte, w int, r toolkit.Rect, c toolkit.RGBA) bool {
	for y := r.Y; y < r.Y+r.H; y++ {
		for x := r.X; x < r.X+r.W; x++ {
			if y < 0 || x < 0 {
				continue
			}
			p := px(buf, w, x, y)
			if p.R == c.R && p.G == c.G && p.B == c.B {
				return true
			}
		}
	}
	return false
}

func TestGroupCollapsedSnapshotAndHits(t *testing.T) {
	s := groupScene()
	buf := renderPNG(t, s, "group-collapsed")

	r, feedX, feedW := groupRow(s)
	if r.group == nil {
		t.Fatal("no group row laid out")
	}
	// Collapsed: the row is exactly the header height.
	if r.height != s.m.groupHeadH {
		t.Fatalf("collapsed height = %d, want groupHeadH %d", r.height, s.m.groupHeadH)
	}
	collapsedContentH := s.contentH

	// The Usenet badge (source colour) and the accent Reconstruct pill both drew.
	rowY := s.m.topbarH + r.top
	headerBand := toolkit.Rect{X: feedX, Y: rowY, W: feedW, H: s.m.groupHeadH}
	if !regionHas(buf, s.W, headerBand, sourceColor(source.Usenet)) {
		t.Fatal("Usenet source badge did not render in the group header")
	}
	rr := s.reconstructRect(feedX, r.top, feedW)
	pill := toolkit.Rect{X: rr.X, Y: s.m.topbarH + rr.Y, W: rr.W, H: rr.H}
	if !regionHas(buf, s.W, pill, s.theme.Accent) {
		t.Fatal("Reconstruct pill (accent) did not render")
	}
	// The base name renders: some OnSurface text pixels sit in the name band.
	nameBand := toolkit.Rect{X: feedX + s.m.pad, Y: rowY + s.m.badgeH, W: feedW / 2, H: s.m.groupHeadH - s.m.badgeH}
	if !regionHas(buf, s.W, nameBand, s.theme.OnSurface) {
		t.Fatal("base name text did not render")
	}

	// A click on the header (left of the pill) toggles the group.
	hHeader := s.HitTest(feedX+s.m.pad+2, rowY+s.m.groupHeadH/2)
	if hHeader.Kind != HitToggleGroup || hHeader.Value != "release" {
		t.Fatalf("header hit = %+v, want HitToggleGroup base=release", hHeader)
	}
	// A click on the Reconstruct pill returns the group's base.
	hRec := s.HitTest(rr.X+rr.W/2, s.m.topbarH+rr.Y+rr.H/2)
	if hRec.Kind != HitReconstruct || hRec.Value != "release" {
		t.Fatalf("reconstruct hit = %+v, want HitReconstruct base=release", hRec)
	}
	// The standalone (non-grouped) Reddit card still hit-tests as a normal item.
	var reddit feedRow
	for _, fr := range s.rows {
		if fr.group == nil {
			reddit = fr
			break
		}
	}
	hCard := s.HitTest(feedX+20, s.m.topbarH+reddit.top+reddit.height/2)
	if hCard.Kind != HitItem || hCard.Item.ID != "r1" {
		t.Fatalf("standalone card hit = %+v, want HitItem r1", hCard)
	}

	// Expand and confirm the group grows and now lists its members.
	s.ToggleGroup("release")
	er, _, _ := groupRow(s)
	if !s.GroupExpanded("release") {
		t.Fatal("group not expanded after toggle")
	}
	if er.height <= r.height {
		t.Fatalf("expanded height %d not greater than collapsed %d", er.height, r.height)
	}
	if s.contentH <= collapsedContentH {
		t.Fatalf("content height did not grow on expand: %d <= %d", s.contentH, collapsedContentH)
	}
}

func TestGroupExpandedSnapshotAndMemberHit(t *testing.T) {
	s := groupScene()
	s.ToggleGroup("release")
	buf := renderPNG(t, s, "group-expanded")

	r, feedX, feedW := groupRow(s)
	if r.group == nil {
		t.Fatal("no group row")
	}
	if r.height != s.m.groupHeadH+len(r.group.Members)*s.m.memberH {
		t.Fatalf("expanded height = %d", r.height)
	}
	// A click on the second member row opens that article's detail.
	mr := s.memberRect(feedX, r.top, feedW, 1)
	h := s.HitTest(mr.X+mr.W/2, s.m.topbarH+mr.Y+mr.H/2)
	if h.Kind != HitItem || h.Item.ID != "d2" {
		t.Fatalf("member hit = %+v, want HitItem d2", h)
	}
	// A click below the members but inside the row is a no-op (dead zone guard is
	// unreachable here; assert the empty-gap after members returns none via a
	// point just past the last member but still inside the card is covered by the
	// member loop). Instead exercise the "expanded, click header" toggle-back.
	rowY := s.m.topbarH + r.top
	if hh := s.HitTest(feedX+s.m.pad+2, rowY+s.m.groupHeadH/2); hh.Kind != HitToggleGroup {
		t.Fatalf("expanded header hit = %+v", hh)
	}
	// The member text drew (OnSurface pixels within the member band).
	memBand := toolkit.Rect{X: mr.X, Y: s.m.topbarH + mr.Y, W: mr.W, H: mr.H}
	if !regionHas(buf, s.W, memBand, s.theme.OnSurface) {
		t.Fatal("member row text did not render")
	}
}

func TestGroupHitNoneInsideExpandedGap(t *testing.T) {
	// With a group expanded, a click inside the card but not on the header, the
	// pill, or any member row returns HitNone. Force this by clicking the far
	// left of a member row's vertical span but outside memberRect's X inset.
	s := groupScene()
	s.ToggleGroup("release")
	r, feedX, feedW := groupRow(s)
	mr := s.memberRect(feedX, r.top, feedW, 0)
	// x just left of the member inset (inside the card, below the header).
	h := s.HitTest(feedX+1, s.m.topbarH+mr.Y+mr.H/2)
	if h.Kind != HitNone {
		t.Fatalf("gap hit = %+v, want HitNone", h)
	}
}

func TestGroupScrolledDrawSkips(t *testing.T) {
	// Exercise the offscreen-skip path with a group present and a large scroll.
	s := groupScene()
	s.ToggleGroup("release")
	s.Scroll(10000) // clamps to bottom; top rows scroll offscreen
	renderPNG(t, s, "group-scrolled")
}
