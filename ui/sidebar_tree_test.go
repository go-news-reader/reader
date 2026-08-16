package ui

import (
	"testing"

	"github.com/go-widgets/toolkit"

	"github.com/go-news-reader/reader/internal/settings"
	"github.com/go-news-reader/reader/source"
)

// sideKeyOf is the stable folder-membership key for a subscription, matching
// buildSideTree's grouping.
func sideKeyOf(src source.Kind, channel string) string { return subKey(src, channel) }

// foldersScene builds a 720×420 feed scene with three subscriptions, a Usenet
// server (so the Browse entry shows), one folder holding the first sub, per-sub
// item counts, and a seen marker — exercising every sidebar node kind in one
// layout.
func foldersScene(t *testing.T) *Scene {
	t.Helper()
	s := New(720, 420, ThemeFor(OSLinux, false))
	s.SetScale(1)
	s.SetUsenetServer("news.example:119") // adds the Browse discovery node
	subs := []Subscription{
		{Source: source.Reddit, Channel: "r/golang", Label: "golang"},
		{Source: source.Reddit, Channel: "r/rust", Label: "rust"},
		{Source: source.HackerNews, Channel: "", Label: "HN"},
	}
	s.SetSubs(subs)
	s.SetItems([]source.Item{
		{ID: "a", Source: source.Reddit, Channel: "r/golang", Title: "x"},
		{ID: "b", Source: source.Reddit, Channel: "r/golang", Title: "y"},
		{ID: "c", Source: source.Reddit, Channel: "r/rust", Title: "z"},
	})
	s.SetSeen(map[string]int{sideKeyOf(source.Reddit, "r/golang"): 1}) // 1 seen of 2 → unseen 1
	s.SetSidebarFolders([]settings.Folder{
		{Name: "Langs", Subs: []string{sideKeyOf(source.Reddit, "r/golang")}},
	})
	return s
}

// TestSidebarTreeStructure checks buildSideTree hides the synthetic root and lays
// its children flush at depth 0: "All Sources" first, then the subscriptions
// grouped under their folder, the unclassified ones, and the discovery nodes.
func TestSidebarTreeStructure(t *testing.T) {
	s := foldersScene(t)
	s.layout()
	if !s.sideTree.HideRoot {
		t.Fatal("the sidebar tree must hide its synthetic root")
	}
	root := s.sideTree.Root
	// Children: All Sources, Langs folder, r/rust, HN, Browse, Search Reddit.
	var kinds []sideKind
	for _, c := range root.Children {
		kinds = append(kinds, sideData(c).Kind)
	}
	want := []sideKind{sideAll, sideFolder, sideSub, sideSub, sideBrowse, sideSearchReddit}
	if len(kinds) != len(want) {
		t.Fatalf("root children kinds = %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("child %d kind = %v, want %v", i, kinds[i], want[i])
		}
	}
	// "All Sources" is the first child (depth 0), not the root.
	if sideData(root.Children[0]).Kind != sideAll {
		t.Fatal("the first child must be the All Sources row")
	}
	// The folder holds exactly the golang sub (index 0).
	folder := root.Children[1]
	if len(folder.Children) != 1 || sideData(folder.Children[0]).Sub != 0 {
		t.Fatalf("folder children = %+v, want [sub 0]", folder.Children)
	}
	// The unclassified subs are r/rust (1) and HN (2).
	if sideData(root.Children[2]).Sub != 1 || sideData(root.Children[3]).Sub != 2 {
		t.Fatalf("unclassified subs = %d,%d, want 1,2", sideData(root.Children[2]).Sub, sideData(root.Children[3]).Sub)
	}
}

// TestSidebarTreeNoUsenetNoBrowse: without a Usenet server the Browse node is
// absent, and the tree still ends with Search Reddit.
func TestSidebarTreeNoUsenetNoBrowse(t *testing.T) {
	s := New(720, 420, ThemeFor(OSLinux, false))
	s.SetSubs([]Subscription{{Source: source.Reddit, Channel: "r/a"}})
	s.layout()
	last := s.sideTree.Root.Children
	if sideData(last[len(last)-1]).Kind != sideSearchReddit {
		t.Fatal("last child must be Search Reddit")
	}
	for _, c := range last {
		if sideData(c).Kind == sideBrowse {
			t.Fatal("no Browse node without a Usenet server")
		}
	}
}

// TestSidebarEmptyFolderHidden: a folder whose subscriptions are absent from the
// active profile is not rendered, but stays in the persisted set.
func TestSidebarEmptyFolderHidden(t *testing.T) {
	s := New(720, 420, ThemeFor(OSLinux, false))
	s.SetSubs([]Subscription{{Source: source.Reddit, Channel: "r/a"}})
	s.SetSidebarFolders([]settings.Folder{{Name: "Ghost", Subs: []string{sideKeyOf(source.Reddit, "r/absent")}}})
	s.layout()
	for _, c := range s.sideTree.Root.Children {
		if sideData(c).Kind == sideFolder {
			t.Fatal("an empty-in-profile folder must not render")
		}
	}
	if len(s.SidebarFolders()) != 1 {
		t.Fatal("the empty folder must remain in the persisted set")
	}
}

// TestSidebarSubInFirstFolderOnly: a subscription listed in two folders lands in
// the first one only.
func TestSidebarSubInFirstFolderOnly(t *testing.T) {
	s := New(720, 420, ThemeFor(OSLinux, false))
	key := sideKeyOf(source.Reddit, "r/a")
	s.SetSubs([]Subscription{{Source: source.Reddit, Channel: "r/a"}})
	s.SetSidebarFolders([]settings.Folder{
		{Name: "F1", Subs: []string{key}},
		{Name: "F2", Subs: []string{key}},
	})
	s.layout()
	folders := 0
	for _, c := range s.sideTree.Root.Children {
		if sideData(c).Kind == sideFolder {
			folders++
			if sideData(c).Folder == "F2" {
				t.Fatal("the sub must land in F1, so F2 stays empty and hidden")
			}
		}
	}
	if folders != 1 {
		t.Fatalf("rendered folders = %d, want 1 (F1)", folders)
	}
}

// TestSidebarSelectedNode: the active filter drives the TreeView's Selected
// pointer — the "All Sources" child (not the hidden root) for AllFilter, the
// matching leaf for a sub.
func TestSidebarSelectedNode(t *testing.T) {
	s := foldersScene(t)
	s.SetActive(AllFilter)
	s.layout()
	if sel := s.sideTree.Selected; sel != s.sideTree.Root.Children[0] || sideData(sel).Kind != sideAll {
		t.Fatal("AllFilter should select the All Sources child, not the hidden root")
	}
	s.SetActive(0) // the golang sub, which lives inside the Langs folder
	s.layout()
	if sd := sideData(s.sideTree.Selected); sd.Kind != sideSub || sd.Sub != 0 {
		t.Fatalf("Active=0 selected %+v, want sub 0", sd)
	}
}

// TestSidebarDrawAllRowKinds renders a scene exercising every RowRenderer branch
// (root, folder with count, sub with count, pending sub with spinner, discovery
// entries) plus the selected-sub chip path, and asserts something painted.
func TestSidebarDrawAllRowKinds(t *testing.T) {
	s := foldersScene(t)
	// Mark HN pending so its row draws the spinner slot.
	s.SetPendingSources([]source.Subscription{{Source: source.HackerNews, Channel: ""}})
	s.SetActive(0) // select the golang sub → its count chip renders in the row ink
	buf := make([]byte, s.W*s.H*4)
	s.Draw(buf)
	// The selected sub row paints an accent background somewhere in the band.
	if !regionHas(buf, s.W, toolkit.Rect{X: 0, Y: s.sideBandTop, W: s.m.sidebarW, H: s.sideBandBot - s.sideBandTop}, s.theme.Accent) {
		t.Fatal("no accent selection fill painted in the sidebar band")
	}
}

// TestSidebarWheelScroll drives a wheel over an overflowing sidebar and back,
// covering both wheel directions through the TreeView.
func TestSidebarWheelScroll(t *testing.T) {
	s := New(700, 320, ThemeFor(OSLinux, false))
	var subs []Subscription
	for i := 0; i < 40; i++ {
		subs = append(subs, Subscription{Source: source.Reddit, Channel: "r/" + itoa(i)})
	}
	s.SetSubs(subs)
	s.layout()
	if !s.sidebarListOverflows() {
		t.Fatal("40 subs should overflow")
	}
	s.MouseMove(10, 150)
	s.Scroll(200) // down
	if s.sideTree.ScrollRow == 0 {
		t.Fatal("wheel down did not scroll the tree")
	}
	down := s.sideTree.ScrollRow
	s.Scroll(-200) // up
	if s.sideTree.ScrollRow >= down {
		t.Fatalf("wheel up did not scroll back: %d !< %d", s.sideTree.ScrollRow, down)
	}
}

// TestWheelRows covers the device-pixel→row conversion, including the small-notch
// and unlaid-out (rowH<=0) fallbacks.
func TestWheelRows(t *testing.T) {
	cases := []struct{ dy, rowH, want int }{
		{68, 34, 2},   // two whole rows
		{10, 34, 1},   // small positive notch → one row down
		{-10, 34, -1}, // small negative notch → one row up
		{0, 34, 0},    // no movement
		{40, 0, 40},   // rowH<=0 → one px per row
	}
	for _, c := range cases {
		if got := wheelRows(c.dy, c.rowH); got != c.want {
			t.Fatalf("wheelRows(%d,%d) = %d, want %d", c.dy, c.rowH, got, c.want)
		}
	}
}

// TestSidebarOverflowGuards covers sidebarListOverflows' nil-tree and
// non-positive-band guards.
func TestSidebarOverflowGuards(t *testing.T) {
	z := &Scene{}
	if z.sidebarListOverflows() {
		t.Fatal("a nil TreeView must report no overflow")
	}
	s := foldersScene(t)
	s.layout()
	s.m.sideItemH = 0 // force the rh<=0 guard
	if s.sidebarListOverflows() {
		t.Fatal("a zero row height must report no overflow")
	}
}

// TestSidebarNodeHitAllKinds maps each node kind through sideNodeHit, including
// the unreachable default (a node with an unknown kind).
func TestSidebarNodeHitAllKinds(t *testing.T) {
	s := foldersScene(t)
	cases := []struct {
		node *toolkit.TreeNode
		want HitKind
	}{
		{&toolkit.TreeNode{Data: sideNode{Kind: sideAll}}, HitSub},
		{&toolkit.TreeNode{Data: sideNode{Kind: sideSub, Sub: 2}}, HitSub},
		{&toolkit.TreeNode{Data: sideNode{Kind: sideBrowse}}, HitBrowse},
		{&toolkit.TreeNode{Data: sideNode{Kind: sideSearchReddit}}, HitSearchReddit},
		{&toolkit.TreeNode{Data: sideNode{Kind: sideFolder, Folder: "F"}}, HitToggleFolder},
		{&toolkit.TreeNode{Data: sideNode{Kind: sideKind(99)}}, HitNone},
	}
	for _, c := range cases {
		if got := s.sideNodeHit(c.node).Kind; got != c.want {
			t.Fatalf("sideNodeHit(%+v) = %v, want %v", sideData(c.node), got, c.want)
		}
	}
}

// TestSidebarHitTestFolderRow: a click on a folder row resolves to
// HitToggleFolder carrying the folder name.
func TestSidebarHitTestFolderRow(t *testing.T) {
	s := foldersScene(t)
	s.layout()
	// The folder is the root's first child → the second visible row (after the
	// root), at band top + one row.
	y := s.sideBandTop + s.m.sideItemH + s.m.sideItemH/2
	h := s.HitTest(10, y)
	if h.Kind != HitToggleFolder || h.Value != "Langs" {
		t.Fatalf("folder row hit = %+v, want HitToggleFolder Langs", h)
	}
}

// TestSidebarA11yTree exposes every sidebar row (root, folder, subs, discovery)
// with its count/collapse value, and covers the collapsed-folder value branch.
func TestSidebarA11yTree(t *testing.T) {
	s := foldersScene(t)
	// While expanded, the folder a11y node reads "expanded".
	for _, n := range s.A11yTree() {
		if n.Name == "Langs" && n.Value != "expanded" {
			t.Fatalf("expanded folder a11y value = %q, want expanded", n.Value)
		}
	}
	s.ToggleSidebarFolder("Langs") // collapse it so the a11y value reads "collapsed"
	tree := s.A11yTree()
	var haveFolder, haveSub, haveBrowse, haveSearch bool
	for _, n := range tree {
		switch n.Name {
		case "Langs":
			haveFolder = true
			if n.Value != "collapsed" {
				t.Fatalf("collapsed folder a11y value = %q", n.Value)
			}
		case "rust":
			haveSub = true
			if n.Value == "" {
				t.Fatal("a subscription a11y node should carry its unseen/total count")
			}
		case "Browse newsgroups":
			haveBrowse = true
		case "Search Reddit":
			haveSearch = true
		}
	}
	if !haveFolder || !haveSub || !haveBrowse || !haveSearch {
		t.Fatalf("a11y tree missing rows: folder=%v sub=%v browse=%v search=%v", haveFolder, haveSub, haveBrowse, haveSearch)
	}
}

// TestFlattenSideNil covers flattenSide's nil-tree guard.
func TestFlattenSideNil(t *testing.T) {
	z := &Scene{}
	if got := z.flattenSide(); got != nil {
		t.Fatalf("flattenSide on a nil tree = %v, want nil", got)
	}
}
