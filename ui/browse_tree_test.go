package ui

import (
	"regexp"
	"testing"

	"github.com/go-news-reader/reader/source"
)

// gis builds a group list from bare names (zero counts) for tests.
func gis(names ...string) []source.GroupInfo {
	out := make([]source.GroupInfo, len(names))
	for i, n := range names {
		out[i] = source.GroupInfo{Name: n}
	}
	return out
}

// findChild returns the child of n with the given segment (nil if absent).
func findChild(n *groupNode, seg string) *groupNode {
	for _, c := range n.Children {
		if c.Segment == seg {
			return c
		}
	}
	return nil
}

func sampleGroups() []source.GroupInfo {
	return gis(
		"alt.binaries.cd.image",
		"alt.binaries.test",
		"alt.test",
		"comp.lang.go",
		"fr.test",
		"", // blank names are ignored
	)
}

func TestBuildGroupTreeStructureAndCounts(t *testing.T) {
	root := buildGroupTree(sampleGroups())

	// Top-level hierarchies are sorted: alt, comp, fr.
	if len(root.Children) != 3 {
		t.Fatalf("top-level count = %d, want 3", len(root.Children))
	}
	if root.Children[0].Segment != "alt" || root.Children[1].Segment != "comp" || root.Children[2].Segment != "fr" {
		t.Fatalf("top-level order = %v/%v/%v", root.Children[0].Segment, root.Children[1].Segment, root.Children[2].Segment)
	}

	alt := findChild(root, "alt")
	if alt.Name != "alt" || alt.Leaves != 3 {
		t.Fatalf("alt name=%q leaves=%d, want alt/3", alt.Name, alt.Leaves)
	}
	// alt is NOT itself a group (no "alt" newsgroup in the list).
	if alt.IsGroup {
		t.Fatal("alt must not be a group")
	}
	altTest := findChild(alt, "test")
	if !altTest.IsGroup || altTest.Name != "alt.test" || altTest.Leaves != 1 {
		t.Fatalf("alt.test = %+v", altTest)
	}
	bin := findChild(alt, "binaries")
	if bin.Leaves != 2 || bin.Name != "alt.binaries" {
		t.Fatalf("alt.binaries leaves=%d name=%q", bin.Leaves, bin.Name)
	}
	img := findChild(findChild(bin, "cd"), "image")
	if !img.IsGroup || img.Name != "alt.binaries.cd.image" {
		t.Fatalf("deep leaf = %+v", img)
	}
}

func TestBuildGroupTreeGroupAndInternal(t *testing.T) {
	// "alt.test" is both a real group AND an internal node (alt.test.foo exists).
	root := buildGroupTree(gis("alt.test", "alt.test.foo"))
	altTest := findChild(findChild(root, "alt"), "test")
	if !altTest.IsGroup {
		t.Fatal("alt.test should be a group")
	}
	if len(altTest.Children) != 1 || altTest.Children[0].Name != "alt.test.foo" {
		t.Fatalf("alt.test children = %+v", altTest.Children)
	}
	if altTest.Leaves != 2 { // itself + foo
		t.Fatalf("alt.test leaves = %d, want 2", altTest.Leaves)
	}
}

func TestFlattenBrowseRespectsExpand(t *testing.T) {
	root := buildGroupTree(sampleGroups())

	// Collapsed: only the three top-level hierarchies.
	collapsed := flattenBrowse(root, func(string) bool { return false })
	if len(collapsed) != 3 {
		t.Fatalf("collapsed rows = %d, want 3", len(collapsed))
	}
	for _, r := range collapsed {
		if r.depth != 0 {
			t.Fatalf("top-level row depth = %d, want 0", r.depth)
		}
	}

	// Expand only "alt": alt, alt.binaries, alt.test, comp, fr.
	rows := flattenBrowse(root, func(name string) bool { return name == "alt" })
	if len(rows) != 5 {
		t.Fatalf("rows = %d, want 5: %v", len(rows), rows)
	}
	if rows[1].node.Name != "alt.binaries" || rows[1].depth != 1 {
		t.Fatalf("row[1] = %+v", rows[1].node)
	}
}

func TestFilterGroupTree(t *testing.T) {
	root := buildGroupTree(sampleGroups())
	re := regexp.MustCompile("(?i)binaries")
	f := filterGroupTree(root, re)

	// Only the alt.binaries.* branch survives; 2 matching leaves.
	if f.Leaves != 2 {
		t.Fatalf("filtered leaves = %d, want 2", f.Leaves)
	}
	if len(f.Children) != 1 || f.Children[0].Segment != "alt" {
		t.Fatalf("filtered top-level = %+v", f.Children)
	}
	// The retained alt / alt.binaries nodes are containers, not matching groups.
	alt := findChild(f, "alt")
	if alt.IsGroup {
		t.Fatal("container alt must not be marked a group")
	}
	// Auto-expanded flatten reaches the deep leaf.
	rows := flattenBrowse(f, func(string) bool { return true })
	var names []string
	for _, r := range rows {
		names = append(names, r.node.Name)
	}
	want := []string{"alt", "alt.binaries", "alt.binaries.cd", "alt.binaries.cd.image", "alt.binaries.test"}
	if len(names) != len(want) {
		t.Fatalf("filtered rows = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("row[%d] = %q, want %q", i, names[i], want[i])
		}
	}
}

func TestFilterGroupTreeSelfMatchWithChildren(t *testing.T) {
	// A node that is BOTH a matching group and has a matching descendant keeps
	// IsGroup true and its children.
	root := buildGroupTree(gis("a.b", "a.b.c"))
	re := regexp.MustCompile(`(?i)a\.b`)
	f := filterGroupTree(root, re)
	ab := findChild(findChild(f, "a"), "b")
	if !ab.IsGroup {
		t.Fatal("a.b matches and must stay a group")
	}
	if len(ab.Children) != 1 || ab.Children[0].Name != "a.b.c" {
		t.Fatalf("a.b children = %+v", ab.Children)
	}
	if f.Leaves != 2 {
		t.Fatalf("leaves = %d, want 2", f.Leaves)
	}
}

func TestFilterGroupTreeNoMatch(t *testing.T) {
	root := buildGroupTree(sampleGroups())
	f := filterGroupTree(root, regexp.MustCompile("(?i)zzzznope"))
	if f.Leaves != 0 || len(f.Children) != 0 {
		t.Fatalf("no-match filtered tree = %+v", f)
	}
}
