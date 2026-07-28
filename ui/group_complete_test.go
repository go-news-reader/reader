package ui

import (
	"testing"

	"github.com/go-news-reader/reader/source"
)

// group builds an itemGroup directly from a set of Usenet subjects (all sharing
// a release base), for pure completeness tests.
func groupFrom(titles ...string) *itemGroup {
	items := make([]source.Item, len(titles))
	for i, tt := range titles {
		items[i] = usenetItem("m"+itoa(i), tt)
	}
	return newGroup("release", items)
}

func TestGroupCompleteFully(t *testing.T) {
	g := groupFrom(
		`[1/2] - "release.tar.zst" yEnc (1/1) 100000`,
		`[2/2] - "release.tar.zst.par2" yEnc (1/1) 940`,
	)
	if !g.Complete() {
		t.Fatal("a post with both declared files, each single-part, must be complete")
	}
}

func TestGroupCompleteSinglePart(t *testing.T) {
	// A lone [1/1] (1/1) post is complete on its own.
	g := groupFrom(`[1/1] - "solo.par2" yEnc (1/1) 500`)
	if !g.Complete() {
		t.Fatal("single-part (1/1) post must be complete")
	}
}

func TestGroupCompleteNoYencCounter(t *testing.T) {
	// A subject without a yEnc (p/P) counter counts as one implicit part.
	g := groupFrom(`[1/1] - "plain.bin" 4096`)
	if !g.Complete() {
		t.Fatal("a post with no yEnc part counter must be complete (one implicit part)")
	}
}

func TestGroupIncompleteMissingPart(t *testing.T) {
	// One file declaring 3 yEnc parts but only parts 1 and 2 are present.
	g := groupFrom(
		`[1/1] - "big.bin" yEnc (1/3) 100`,
		`[1/1] - "big.bin" yEnc (2/3) 100`,
	)
	if g.Complete() {
		t.Fatal("a file missing part 3 of 3 must be incomplete")
	}
}

func TestGroupIncompleteMissingFile(t *testing.T) {
	// The post declares 2 files ([x/2]) but only the first is present.
	g := groupFrom(`[1/2] - "a.bin" yEnc (1/1) 100`)
	if g.Complete() {
		t.Fatal("a post missing file 2 of 2 must be incomplete")
	}
}

func TestGroupIncompleteNoFileMarkerDistinctFiles(t *testing.T) {
	// Classic RAR split with NO [F/T] marker: every subject reports FileIndex 0,
	// so parts must be tracked by filename, not file index. movie.r00 is missing
	// its part 2/2 while movie.rar is whole — the post is incomplete even though a
	// file-index-keyed check would merge both files' parts and call it complete.
	g := groupFrom(
		`"movie.rar" yEnc (1/2) 100`,
		`"movie.rar" yEnc (2/2) 100`,
		`"movie.r00" yEnc (1/2) 100`,
	)
	if g.Complete() {
		t.Fatal("movie.r00 missing part 2/2 must be incomplete (parts keyed by filename)")
	}
}

func TestGroupCompleteNoFileMarkerDistinctFiles(t *testing.T) {
	// Same shape, but now every part of both files is present -> complete.
	g := groupFrom(
		`"movie.rar" yEnc (1/2) 100`,
		`"movie.rar" yEnc (2/2) 100`,
		`"movie.r00" yEnc (1/2) 100`,
		`"movie.r00" yEnc (2/2) 100`,
	)
	if !g.Complete() {
		t.Fatal("both files whole must be complete")
	}
}

// incompleteGroupScene builds a feed with one incomplete multipart post.
func incompleteGroupScene() *Scene {
	s := New(900, 600, ThemeFor(OSLinux, false))
	s.SetSubs(nil)
	s.SetItems([]source.Item{
		usenetItem("d1", `[1/3] - "movie.mkv" yEnc (1/1) 100000`),
		usenetItem("d2", `[2/3] - "movie.mkv.par2" yEnc (1/1) 940`),
		// file 3 of 3 is missing -> incomplete.
	})
	return s
}

func TestIncompleteGroupHidesReconstruct(t *testing.T) {
	s := incompleteGroupScene()
	renderPNG(t, s, "group-incomplete")

	r, feedX, feedW := groupRow(s)
	if r.group == nil {
		t.Fatal("no group row")
	}
	if r.group.Complete() {
		t.Fatal("scene group should be incomplete")
	}
	// The reconstruct slot is NOT clickable for an incomplete post: clicking it
	// falls through to the header toggle instead of HitReconstruct.
	rr := s.reconstructRect(feedX, r.top, feedW)
	h := s.hitGroup(r, feedX, feedW, rr.X+rr.W/2, r.top+2)
	if h.Kind == HitReconstruct {
		t.Fatal("incomplete post must not offer Reconstruct")
	}
	// The header body (where the pill would be, but the post is incomplete) now
	// previews the post rather than being a dead slot.
	if h.Kind != HitPreviewGroup {
		t.Fatalf("expected header preview, got %v", h.Kind)
	}
}

func TestCompleteGroupOffersReconstruct(t *testing.T) {
	s := groupScene() // the standard complete 3-part post
	r, feedX, feedW := groupRow(s)
	rr := s.reconstructRect(feedX, r.top, feedW)
	h := s.hitGroup(r, feedX, feedW, rr.X+rr.W/2, rr.Y+rr.H/2)
	if h.Kind != HitReconstruct {
		t.Fatalf("complete post should offer Reconstruct, got %v", h.Kind)
	}
}
