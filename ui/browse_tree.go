package ui

// Pure model for the newsgroup browser's hierarchical tree. A Usenet server
// carries tens of thousands of groups named with a dotted grammar
// (alt.binaries.cd.image); this model collapses that flat list into a tree where
// each dot-segment is a node and a leaf is a real newsgroup. A node can be BOTH
// an internal node and a real group (e.g. "alt.test" exists and "alt.test.foo"
// too). The model is rendering-agnostic and fully unit-testable; the browse view
// (browse_view.go) lays it out and the Scene draws + hit-tests it.

import (
	"regexp"
	"sort"
	"strings"
)

// groupNode is one node of the newsgroup hierarchy.
type groupNode struct {
	Name     string       // full dotted path to this node, e.g. "alt.binaries"
	Segment  string       // this node's last dotted segment, e.g. "binaries"
	Children []*groupNode // child nodes, sorted by Segment
	IsGroup  bool         // a real newsgroup exists at exactly Name
	Leaves   int          // number of real groups in this subtree (incl. self)
}

// buildGroupTree assembles the hierarchy from a flat list of full group names.
// Blank names are ignored; the result's children are the top-level hierarchies
// (root itself is a synthetic, unnamed holder that is never rendered). Every
// node's Leaves count is populated.
func buildGroupTree(names []string) *groupNode {
	root := &groupNode{}
	index := map[string]*groupNode{"": root}
	for _, name := range names {
		if name == "" {
			continue
		}
		ensureNode(index, name).IsGroup = true
	}
	sortNode(root)
	countLeaves(root)
	return root
}

// ensureNode returns the node for the full dotted path, creating it (and every
// missing ancestor) and linking it under its parent on first sight.
func ensureNode(index map[string]*groupNode, full string) *groupNode {
	if n, ok := index[full]; ok {
		return n
	}
	seg, parentPath := full, ""
	if i := strings.LastIndex(full, "."); i >= 0 {
		parentPath, seg = full[:i], full[i+1:]
	}
	parent := ensureNode(index, parentPath)
	n := &groupNode{Name: full, Segment: seg}
	parent.Children = append(parent.Children, n)
	index[full] = n
	return n
}

// sortNode orders a node's children by segment, recursively.
func sortNode(n *groupNode) {
	sort.Slice(n.Children, func(i, j int) bool { return n.Children[i].Segment < n.Children[j].Segment })
	for _, c := range n.Children {
		sortNode(c)
	}
}

// countLeaves fills Leaves for n and its subtree (post-order) and returns it.
func countLeaves(n *groupNode) int {
	total := 0
	if n.IsGroup {
		total = 1
	}
	for _, c := range n.Children {
		total += countLeaves(c)
	}
	n.Leaves = total
	return total
}

// filterGroupTree returns a copy of root containing only the branches that lead
// to a real group whose full name matches re (partial match). A retained node is
// marked IsGroup only when it itself matches, so the filtered tree presents a
// Subscribe leaf exactly for the matching groups; ancestors are kept purely as
// containers. Leaves counts are recomputed over the filtered shape.
func filterGroupTree(root *groupNode, re *regexp.Regexp) *groupNode {
	out := &groupNode{Name: root.Name, Segment: root.Segment}
	for _, c := range root.Children {
		if fc := filterNode(c, re); fc != nil {
			out.Children = append(out.Children, fc)
		}
	}
	countLeaves(out)
	return out
}

// filterNode returns a filtered copy of n, or nil when neither n nor any
// descendant is a matching group.
func filterNode(n *groupNode, re *regexp.Regexp) *groupNode {
	var kids []*groupNode
	for _, c := range n.Children {
		if fc := filterNode(c, re); fc != nil {
			kids = append(kids, fc)
		}
	}
	selfMatch := n.IsGroup && re.MatchString(n.Name)
	if !selfMatch && len(kids) == 0 {
		return nil
	}
	return &groupNode{Name: n.Name, Segment: n.Segment, IsGroup: selfMatch, Children: kids}
}

// browseRow is one visible row of the flattened tree: a node plus its indent
// depth (0 for a top-level hierarchy).
type browseRow struct {
	node  *groupNode
	depth int
}

// flattenBrowse walks root's subtree in display order, emitting a row per node
// and descending into a node's children only when it has any and expanded
// reports it open. root itself is not emitted (it is the synthetic holder).
func flattenBrowse(root *groupNode, expanded func(name string) bool) []browseRow {
	var out []browseRow
	var walk func(n *groupNode, depth int)
	walk = func(n *groupNode, depth int) {
		for _, c := range n.Children {
			out = append(out, browseRow{node: c, depth: depth})
			if len(c.Children) > 0 && expanded(c.Name) {
				walk(c, depth+1)
			}
		}
	}
	walk(root, 0)
	return out
}
