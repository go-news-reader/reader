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
