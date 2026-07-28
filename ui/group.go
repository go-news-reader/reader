package ui

// Usenet multipart grouping. A binary newsgroup feed is a flood of individual
// article parts; a single post ("release") is usually spread across many
// articles: the data file (possibly split into yEnc parts) plus PAR2 recovery
// files. Their subjects share a common grammar,
//
//	[F/T] - "filename" yEnc (p/P) size
//
// where [F/T] is the file index within the post, "filename" carries the release
// base plus a type/recovery suffix (.tar.zst data, .parN.par2 / .volNN+MM.par2
// recovery), (p/P) is the yEnc part counter of that file, and the trailing
// integer is the article byte size. groupItems collapses consecutive Usenet
// items that share a release base into one [itemGroup] the feed renders as a
// single collapsible card; everything else stays a standalone card.

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-news-reader/reader/source"
)

// Subject-grammar matchers. Each is anchored to the fragment it extracts so a
// malformed subject simply fails to match and the item does not group.
var (
	reFileIdx  = regexp.MustCompile(`\[(\d+)/(\d+)\]`)
	reQuoted   = regexp.MustCompile(`"([^"]+)"`)
	reYencPart = regexp.MustCompile(`(?i)yenc\s*\((\d+)/(\d+)\)`)
	reTrailSz  = regexp.MustCompile(`(\d+)\s*$`)
)

// Release-base derivation: strip the recovery/type/volume suffixes so every
// file and part of one post reduces to the same base, covering both the
// .tar.zst + .par2/.vol style AND classic Usenet split archives
// (.partNN.rar, .rNN/.sNN rar volumes, .NNN numeric splits, plain archive and
// sidecar extensions). reMultiTok matches ONE trailing token; releaseBase
// applies it in a loop so stacked suffixes (e.g. release.part03.rar,
// release.tar.zst.vol00+01.par2) unwind to the shared base. Only known
// multipart/archive/sidecar tokens are stripped — arbitrary extensions and
// release qualifiers (.1080p, .2024) are preserved so distinct releases stay
// distinct.
var reMultiTok = regexp.MustCompile(`(?i)\.(par2|vol\d+\+\d+|part\d+|par\d+|r\d{2}|s\d{2}|\d{3}|rar|zip|7z|tar|gz|bz2|xz|zst|nfo|sfv|nzb|srr|srs)$`)

// subjectInfo is the parsed form of a Usenet binary subject.
type subjectInfo struct {
	Filename  string
	Base      string
	FileIndex int
	FileTotal int
	Part      int
	PartTotal int
	Size      int64
}

// parseSubject extracts the release base and part fields from a Usenet subject.
// It succeeds only when a quoted filename is present (the reliable binary-post
// marker); text posts and uniquely-named reposts fail and stay ungrouped.
func parseSubject(subject string) (subjectInfo, bool) {
	q := reQuoted.FindStringSubmatch(subject)
	if q == nil {
		return subjectInfo{}, false
	}
	info := subjectInfo{Filename: q[1]}
	info.Base = releaseBase(info.Filename)
	if info.Base == "" {
		return subjectInfo{}, false
	}
	if m := reFileIdx.FindStringSubmatch(subject); m != nil {
		info.FileIndex, _ = strconv.Atoi(m[1])
		info.FileTotal, _ = strconv.Atoi(m[2])
	}
	if m := reYencPart.FindStringSubmatch(subject); m != nil {
		info.Part, _ = strconv.Atoi(m[1])
		info.PartTotal, _ = strconv.Atoi(m[2])
	}
	if m := reTrailSz.FindStringSubmatch(subject); m != nil {
		info.Size, _ = strconv.ParseInt(m[1], 10, 64)
	}
	return info, true
}

// releaseBase reduces a filename to the release base shared across the post's
// files by stripping recovery and type suffixes.
func releaseBase(filename string) string {
	b := strings.TrimSpace(filename)
	for {
		nb := reMultiTok.ReplaceAllString(b, "")
		if nb == b {
			break
		}
		b = nb
	}
	return b
}

// usenetBase returns the release base of a Usenet item, false when the item is
// not Usenet or its subject does not parse as a binary post.
func usenetBase(it source.Item) (string, bool) {
	if it.Source != source.Usenet {
		return "", false
	}
	info, ok := parseSubject(it.Title)
	if !ok {
		return "", false
	}
	return info.Base, true
}

// groupMember is one article of a grouped post plus its parsed subject.
type groupMember struct {
	Item source.Item
	Info subjectInfo
}

// itemGroup is a collapsed post: the shared base plus its member articles and
// aggregate metadata.
type itemGroup struct {
	Base    string
	Members []groupMember
	Size    int64 // sum of parseable article sizes
	Files   int   // distinct filenames in the post
}

// newGroup builds an itemGroup from a run of same-base items (all parse, by
// construction in groupItems).
func newGroup(base string, items []source.Item) *itemGroup {
	g := &itemGroup{Base: base}
	names := map[string]bool{}
	for _, it := range items {
		info, _ := parseSubject(it.Title)
		g.Members = append(g.Members, groupMember{Item: it, Info: info})
		g.Size += info.Size
		names[info.Filename] = true
	}
	g.Files = len(names)
	return g
}

// Complete reports whether every declared file and part of the post is present
// in the retained members, so the reassembly can actually run. A binary post
// declares its file count as the T in each subject's "[F/T]" and, per file, its
// part count as the P in the yEnc "(p/P)" counter. The post is complete when:
//
//   - every file index 1..T is present (T = the largest declared file total; 0
//     when no subject carries an "[F/T]" marker, in which case only the parts of
//     the files that ARE present are checked — nothing is known to be missing);
//     and
//   - for every present file, all parts 1..P are present (P = that file's
//     largest declared part total; a single-part "(1/1)" or a subject with no
//     part counter counts as one implicit part, hence always complete for parts).
func (g *itemGroup) Complete() bool {
	fileTotal := 0
	byFile := map[int]map[int]bool{} // file index -> set of present part numbers
	partTotal := map[int]int{}       // file index -> declared part total
	for _, m := range g.Members {
		fi := m.Info.FileIndex
		if m.Info.FileTotal > fileTotal {
			fileTotal = m.Info.FileTotal
		}
		if byFile[fi] == nil {
			byFile[fi] = map[int]bool{}
		}
		part := m.Info.Part
		if part == 0 {
			part = 1 // no yEnc counter => a single implicit part
		}
		byFile[fi][part] = true
		if m.Info.PartTotal > partTotal[fi] {
			partTotal[fi] = m.Info.PartTotal
		}
	}
	// Every present file must have all of its declared parts.
	for fi, parts := range byFile {
		for pp := 1; pp <= partTotal[fi]; pp++ {
			if !parts[pp] {
				return false
			}
		}
	}
	// Every declared file (1..T) must be present.
	for fi := 1; fi <= fileTotal; fi++ {
		if byFile[fi] == nil {
			return false
		}
	}
	return true
}

// feedEntry is one row of the display list: either a standalone item (group
// nil) or a collapsed group.
type feedEntry struct {
	group *itemGroup
	item  source.Item
}

// groupItems turns the filtered feed into a display list, collapsing each run of
// two-or-more consecutive Usenet items that share a release base into one group.
// Non-Usenet items, unparseable subjects, and lone parts stay standalone, and
// the order of the feed (newest-first) is preserved: a group takes the position
// of its newest (first) member.
func groupItems(items []source.Item) []feedEntry {
	var out []feedEntry
	for i := 0; i < len(items); {
		base, ok := usenetBase(items[i])
		if !ok {
			out = append(out, feedEntry{item: items[i]})
			i++
			continue
		}
		j := i + 1
		for j < len(items) {
			b2, ok2 := usenetBase(items[j])
			if !ok2 || b2 != base {
				break
			}
			j++
		}
		if j-i >= 2 {
			out = append(out, feedEntry{group: newGroup(base, items[i:j])})
		} else {
			out = append(out, feedEntry{item: items[i]})
		}
		i = j
	}
	return out
}

// groupMeta renders the "N parts · M files · SIZE" subtitle for a group card.
func groupMeta(g *itemGroup) string {
	parts := []string{fmt.Sprintf("%d parts", len(g.Members))}
	if g.Files > 1 {
		parts = append(parts, fmt.Sprintf("%d files", g.Files))
	}
	if g.Size > 0 {
		parts = append(parts, humanSize(g.Size))
	}
	return strings.Join(parts, " · ")
}

// humanSize formats a byte count with a binary unit suffix.
func humanSize(n int64) string {
	switch {
	case n < 1<<10:
		return fmt.Sprintf("%d B", n)
	case n < 1<<20:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	case n < 1<<30:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	default:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	}
}

// ReconstructPart names one article to fetch for a reconstruction: the bare
// message-id (no "news:" scheme, no angle brackets) and the file it belongs to.
type ReconstructPart struct {
	MessageID string
	Filename  string
}

// GroupParts returns the member articles of the currently-displayed group with
// the given base, ready to hand to the Usenet provider's Reconstruct. It reports
// false when no such group is shown.
func (s *Scene) GroupParts(base string) ([]ReconstructPart, bool) {
	var parts []ReconstructPart
	for _, e := range groupItems(s.filtered()) {
		if e.group == nil || e.group.Base != base {
			continue
		}
		for _, mem := range e.group.Members {
			id := strings.TrimPrefix(mem.Item.Permalink, "news:")
			id = strings.Trim(id, "<>")
			parts = append(parts, ReconstructPart{MessageID: id, Filename: mem.Info.Filename})
		}
	}
	return parts, len(parts) > 0
}
