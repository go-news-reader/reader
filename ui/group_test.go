package ui

import (
	"testing"

	"github.com/go-news-reader/reader/source"
)

// usenetItem builds a Usenet feed item with the given subject and message-id.
func usenetItem(id, subject string) source.Item {
	return source.Item{ID: id, Source: source.Usenet, Channel: "alt.binaries.test",
		Title: subject, Permalink: "news:" + id}
}

func TestReleaseBase(t *testing.T) {
	cases := map[string]string{
		"release.tar.zst":                 "release",
		"release.tar.zst.par2":            "release",
		"release.tar.zst.vol03+01.par2":   "release",
		"release.tar.gz":                  "release",
		"release.part2":                   "release",
		"release.par3":                    "release",
		"release.nfo":                     "release",
		"1785160182_x_sm11028111.tar.zst": "1785160182_x_sm11028111",
		"plainname":                       "plainname",
		// Classic Usenet split-archive naming (real groups like a.b.cd.image).
		"flt-iwd2.rar":         "flt-iwd2", // rar volume
		"flt-iwd2.r07":         "flt-iwd2", // .rNN old rar volume
		"movie.part03.rar":     "movie",    // .partNN.rar (two-level)
		"vty-0183.045":         "vty-0183", // .NNN numeric split
		"rld-p2aw.r04":         "rld-p2aw",
		"The.Movie.2024.1080p": "The.Movie.2024.1080p", // qualifiers preserved (no over-strip)
	}
	for in, want := range cases {
		if got := releaseBase(in); got != want {
			t.Errorf("releaseBase(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseSubject(t *testing.T) {
	info, ok := parseSubject(`[1/4] - "release.tar.zst" yEnc (2/4) 2382597`)
	if !ok {
		t.Fatal("expected parse ok")
	}
	if info.Filename != "release.tar.zst" || info.Base != "release" {
		t.Fatalf("filename/base = %q / %q", info.Filename, info.Base)
	}
	if info.FileIndex != 1 || info.FileTotal != 4 {
		t.Fatalf("file idx = %d/%d", info.FileIndex, info.FileTotal)
	}
	if info.Part != 2 || info.PartTotal != 4 {
		t.Fatalf("part = %d/%d", info.Part, info.PartTotal)
	}
	if info.Size != 2382597 {
		t.Fatalf("size = %d", info.Size)
	}
}

func TestParseSubjectNoQuotes(t *testing.T) {
	if _, ok := parseSubject("just a normal text post about Go generics"); ok {
		t.Fatal("unquoted subject must not parse as a binary post")
	}
}

func TestParseSubjectEmptyBase(t *testing.T) {
	// A quoted filename that reduces to nothing after stripping yields no base.
	if _, ok := parseSubject(`re: "" yEnc`); ok {
		t.Fatal("empty filename must not parse")
	}
	if _, ok := parseSubject(`x ".par2" y`); ok {
		t.Fatal("filename that strips to empty must not parse")
	}
}

func TestGroupItemsWellFormedMultiFile(t *testing.T) {
	items := []source.Item{
		{ID: "n1", Source: source.Reddit, Title: "not usenet"},
		usenetItem("d1", `[1/3] - "release.tar.zst" yEnc (1/1) 1000`),
		usenetItem("d2", `[2/3] - "release.tar.zst.par2" yEnc (1/1) 200`),
		usenetItem("d3", `[3/3] - "release.tar.zst.vol00+01.par2" yEnc (1/1) 300`),
		usenetItem("s1", `[1/1] - "other.tar.zst" yEnc (1/1) 500`), // lone -> standalone
	}
	entries := groupItems(items)
	if len(entries) != 3 {
		t.Fatalf("entries = %d, want 3", len(entries))
	}
	// Order preserved: the non-usenet standalone comes first.
	if entries[0].group != nil || entries[0].item.ID != "n1" {
		t.Fatalf("entry[0] = %+v", entries[0])
	}
	g := entries[1].group
	if g == nil || g.Base != "release" {
		t.Fatalf("entry[1] group = %+v", entries[1])
	}
	if len(g.Members) != 3 {
		t.Fatalf("members = %d", len(g.Members))
	}
	if g.Files != 3 {
		t.Fatalf("distinct files = %d, want 3", g.Files)
	}
	if g.Size != 1500 {
		t.Fatalf("aggregate size = %d, want 1500", g.Size)
	}
	// The lone "other" part stays a standalone card (singletons are not wrapped).
	if entries[2].group != nil || entries[2].item.ID != "s1" {
		t.Fatalf("entry[2] = %+v", entries[2])
	}
}

func TestGroupItemsUniqueNamesDoNotGroup(t *testing.T) {
	// Reposters that give each part a unique base never group.
	items := []source.Item{
		usenetItem("a", `[1/1] - "aaa.tar.zst" yEnc (1/1) 1`),
		usenetItem("b", `[1/1] - "bbb.tar.zst" yEnc (1/1) 1`),
	}
	entries := groupItems(items)
	if len(entries) != 2 || entries[0].group != nil || entries[1].group != nil {
		t.Fatalf("unique-named parts should not group: %+v", entries)
	}
}

func TestGroupItemsMalformedStandalone(t *testing.T) {
	// A malformed (unquoted) Usenet subject between two groupable parts breaks the
	// run, so nothing groups.
	items := []source.Item{
		usenetItem("a", `[1/2] - "rel.tar.zst" yEnc (1/1) 1`),
		usenetItem("m", `garbage subject no quotes`),
		usenetItem("b", `[2/2] - "rel.tar.zst.par2" yEnc (1/1) 1`),
	}
	entries := groupItems(items)
	if len(entries) != 3 {
		t.Fatalf("entries = %d, want 3 standalone", len(entries))
	}
	for i, e := range entries {
		if e.group != nil {
			t.Fatalf("entry[%d] should be standalone", i)
		}
	}
}

func TestGroupMetaAndHumanSize(t *testing.T) {
	g := &itemGroup{Base: "r", Members: make([]groupMember, 2), Files: 1, Size: 0}
	if got := groupMeta(g); got != "2 parts" {
		t.Fatalf("meta no-size/one-file = %q", got)
	}
	g.Files = 2
	g.Size = 2048
	if got := groupMeta(g); got != "2 parts · 2 files · 2.0 KB" {
		t.Fatalf("meta = %q", got)
	}
	for in, want := range map[int64]string{
		512:         "512 B",
		2048:        "2.0 KB",
		3 * 1 << 20: "3.0 MB",
		5 * 1 << 30: "5.0 GB",
	} {
		if got := humanSize(in); got != want {
			t.Errorf("humanSize(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestGroupPartsForApp(t *testing.T) {
	s := newScene()
	s.SetSubs(nil)
	s.SetItems([]source.Item{
		usenetItem("d1", `[1/2] - "rel.tar.zst" yEnc (1/1) 1000`),
		usenetItem("d2", `[2/2] - "rel.tar.zst.par2" yEnc (1/1) 200`),
	})
	parts, ok := s.GroupParts("rel")
	if !ok || len(parts) != 2 {
		t.Fatalf("GroupParts = %v, %v", parts, ok)
	}
	if parts[0].MessageID != "d1" || parts[0].Filename != "rel.tar.zst" {
		t.Fatalf("part[0] = %+v", parts[0])
	}
	if parts[1].MessageID != "d2" || parts[1].Filename != "rel.tar.zst.par2" {
		t.Fatalf("part[1] = %+v", parts[1])
	}
	if _, ok := s.GroupParts("does-not-exist"); ok {
		t.Fatal("unknown base should report not-found")
	}
}
