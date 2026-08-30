package ui

import (
	"strings"
	"testing"

	simpleicons "github.com/go-icons/simple-icons"
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
	// Children: All Sources, Langs folder, then the unclassified subs grouped
	// under a source header each (Reddit → r/rust, Hacker News → HN), then the
	// discovery nodes.
	var kinds []sideKind
	for _, c := range root.Children {
		kinds = append(kinds, sideData(c).Kind)
	}
	want := []sideKind{sideAll, sideFolder, sideSource, sideSource, sideBrowse, sideSearchReddit}
	if len(kinds) != len(want) {
		t.Fatalf("root children kinds = %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("child %d kind = %v, want %v", i, kinds[i], want[i])
		}
	}
	// The folder holds exactly the golang sub (index 0).
	folder := root.Children[1]
	if len(folder.Children) != 1 || sideData(folder.Children[0]).Sub != 0 {
		t.Fatalf("folder children = %+v, want [sub 0]", folder.Children)
	}
	// The Reddit source group holds r/rust (sub 1); Hacker News holds HN (sub 2).
	reddit := root.Children[2]
	if d := sideData(reddit); d.Source != source.Reddit || d.Count != 1 {
		t.Fatalf("first source group = %+v, want Reddit count 1", d)
	}
	if len(reddit.Children) != 1 || sideData(reddit.Children[0]).Sub != 1 {
		t.Fatalf("Reddit group children = %+v, want [sub 1]", reddit.Children)
	}
	hn := root.Children[3]
	if d := sideData(hn); d.Source != source.HackerNews || d.Count != 1 {
		t.Fatalf("second source group = %+v, want Hacker News count 1", d)
	}
	if len(hn.Children) != 1 || sideData(hn.Children[0]).Sub != 2 {
		t.Fatalf("Hacker News group children = %+v, want [sub 2]", hn.Children)
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
	if sel := s.sideTree.Selected().Get(); sel != s.sideTree.Root.Children[0] || sideData(sel).Kind != sideAll {
		t.Fatal("AllFilter should select the All Sources child, not the hidden root")
	}
	s.SetActive(0) // the golang sub, which lives inside the Langs folder
	s.layout()
	if sd := sideData(s.sideTree.Selected().Get()); sd.Kind != sideSub || sd.Sub != 0 {
		t.Fatalf("Active=0 selected %+v, want sub 0", sd)
	}
}

// TestSidebarDrawAllRowKinds renders a scene exercising every RowRenderer branch
// (root, folder with count, sub with count, pending sub with spinner, discovery
// entries) plus the selected-sub chip path, and asserts something painted.
func TestSidebarDrawAllRowKinds(t *testing.T) {
	s := foldersScene(t)
	// Mark HN pending so its row draws the spinner slot; expand its source group so
	// the HN row (and the source-header row) are visible to paint.
	s.SetPendingSources([]source.Subscription{{Source: source.HackerNews, Channel: ""}})
	s.ToggleSidebarSource(source.HackerNews)
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
	s.ToggleSidebarSource(source.Reddit) // expand the group so its 40 accounts show
	s.layout()
	// The open section overflows its ListBox window, so a wheel over the sidebar
	// scrolls the widget (keeping the headers pinned), not the tree.
	if !s.sectionListOverflows() {
		t.Fatal("40 subs should overflow the open section's ListBox window")
	}
	s.MouseMove(10, 150)
	s.Scroll(200) // down
	if s.sideAccountList.ScrollRow().Get() == 0 {
		t.Fatal("wheel down did not scroll the open section")
	}
	down := s.sideAccountList.ScrollRow().Get()
	s.Scroll(-200) // up
	if s.sideAccountList.ScrollRow().Get() >= down {
		t.Fatalf("wheel up did not scroll back: %d !< %d", s.sideAccountList.ScrollRow().Get(), down)
	}
}

// TestSourceIconName pins each source's brand-glyph name and checks that every
// non-empty name actually resolves in the go-icons/simple-icons pack (so a header
// never asks for a logo the pack cannot draw). Usenet and RedGIFs have no brand
// glyph and fall back to the coloured dot.
func TestSourceIconName(t *testing.T) {
	branded := map[source.Kind]string{
		source.Reddit:      "reddit",
		source.HackerNews:  "ycombinator",
		source.Syndication: "rss",
		source.Mastodon:    "mastodon",
		source.Lemmy:       "lemmy",
		source.Bluesky:     "bluesky",
		source.Twitter:     "x",
		source.Instagram:   "instagram",
		source.TikTok:      "tiktok",
	}
	for k, want := range branded {
		if got := sourceIconName(k); got != want {
			t.Errorf("sourceIconName(%v) = %q, want %q", k, got, want)
		}
		if !simpleicons.Has(want) {
			t.Errorf("simple-icons pack is missing %q for %v", want, k)
		}
	}
	for _, k := range []source.Kind{source.Usenet, source.Redgifs} {
		if got := sourceIconName(k); got != "" {
			t.Errorf("sourceIconName(%v) = %q, want \"\" (no brand glyph)", k, got)
		}
	}
}

// TestSidebarHeaderDrawsBrandIconOrDot renders a branded section header and an
// unbranded one, asserting the branded header paints its logo (non-background
// pixels in the icon slot recoloured to the brand tint) while the unbranded one
// keeps the dot.
func TestSidebarHeaderDrawsBrandIconOrDot(t *testing.T) {
	s := New(760, 380, ThemeFor(OSMac, false))
	s.SetSubs([]Subscription{
		{Source: source.Reddit, Channel: "r0"},
		{Source: source.Usenet, Channel: "grp0"},
	})
	buf := make([]byte, s.W*s.H*4)
	s.Draw(buf)
	// Both headers are collapsed rows near the top of the band; each paints
	// something (a glyph or a dot) in its leading icon slot, so the slot is not
	// pure sidebar SurfaceAlt.
	th := s.theme
	slotPainted := func(rowTop int) bool {
		for y := rowTop; y < rowTop+s.m.sideItemH; y++ {
			for x := 0; x < rpxOf(s, 16); x++ {
				c := px(buf, s.W, x, y)
				if c.R != th.SurfaceAlt.R || c.G != th.SurfaceAlt.G || c.B != th.SurfaceAlt.B {
					return true
				}
			}
		}
		return false
	}
	// Row 0 is "All Sources"; the two source headers follow it.
	if !slotPainted(s.sideBandTop + s.m.sideItemH) {
		t.Fatal("the Reddit header painted nothing in its icon slot")
	}
	if !slotPainted(s.sideBandTop + 2*s.m.sideItemH) {
		t.Fatal("the Usenet header painted nothing in its icon slot")
	}
}

// TestSidebarSectionBand checks a source header is materialised as a band: its
// row carries the Surface fill (distinct from the SurfaceAlt sidebar ground) and
// Border-coloured top/bottom hairlines.
func TestSidebarSectionBand(t *testing.T) {
	s := New(760, 380, ThemeFor(OSMac, false))
	s.SetSubs([]Subscription{{Source: source.Reddit, Channel: "r0"}})
	buf := make([]byte, s.W*s.H*4)
	s.Draw(buf)
	th := s.theme
	band := sectionBandFill(th)
	// The band fill must be visibly distinct from the sidebar ground.
	if band == th.SurfaceAlt {
		t.Fatal("the section band fill is identical to the ground")
	}
	// Row 0 is "All Sources"; row 1 is the Reddit header band.
	top := s.sideBandTop + s.m.sideItemH
	sum := func(r, g, b uint8) int { return int(r) + int(g) + int(b) }
	var sawBand bool
	for y := top; y < top+s.m.sideItemH; y++ {
		for x := 0; x < s.m.sidebarW; x++ {
			if c := px(buf, s.W, x, y); c.R == band.R && c.G == band.G && c.B == band.B {
				sawBand = true
			}
		}
	}
	if !sawBand {
		t.Error("the source header row has no band fill")
	}
	// The top edge of the header row is a border hairline: darker than the band
	// (the alpha Border ink blended over the fill), away from the centred chevron.
	edge := px(buf, s.W, s.m.sidebarW/2, top)
	if sum(edge.R, edge.G, edge.B) >= sum(band.R, band.G, band.B) {
		t.Errorf("no top border hairline: edge %v not darker than band %v", edge, band)
	}
}

// TestSidebarAccordionKeepsHeadersVisible is the accordion's contract: opening a
// section with far more accounts than the band can hold must not push the other
// section headers (or the discovery rows) out of view. The open section's
// accounts are windowed instead, so every non-account row stays inside the band
// and the second source header and "Search Reddit" remain hit-testable.
func TestSidebarAccordionKeepsHeadersVisible(t *testing.T) {
	s := New(760, 380, ThemeFor(OSMac, false))
	var subs []Subscription
	for i := 0; i < 300; i++ { // a huge Instagram section…
		subs = append(subs, Subscription{Source: source.Instagram, Channel: "ig" + itoa(i)})
	}
	for i := 0; i < 3; i++ { // …then a small Reddit one, whose header sits AFTER it.
		subs = append(subs, Subscription{Source: source.Reddit, Channel: "r" + itoa(i)})
	}
	s.SetSubs(subs)
	s.ToggleSidebarSource(source.Instagram) // open the huge one
	s.layout()

	// The section is windowed, not laid out whole: all 300 accounts are in the
	// ListBox model, but its window (the section rect) shows fewer than that.
	if got := len(s.sideAccountList.Items); got != 300 {
		t.Fatalf("open section should hold all 300 accounts, got %d", got)
	}
	if !s.sectionListOverflows() {
		t.Fatal("open section not windowed: 300 accounts should overflow the window")
	}
	// Every visible row fits the band: the tree never overflows, so no header is
	// pushed off. (flattenSide is the exact visible-row set the TreeView paints.)
	rows := len(s.flattenSide())
	if fit := (s.sideBandBot - s.sideBandTop) / s.m.sideItemH; rows > fit {
		t.Fatalf("visible rows %d exceed the band's %d — a header would be hidden", rows, fit)
	}
	// The Reddit header, which follows the huge open section, is still a visible row.
	var sawReddit, sawSearch bool
	for _, n := range s.flattenSide() {
		switch d := sideData(n); d.Kind {
		case sideSource:
			if d.Source == source.Reddit {
				sawReddit = true
			}
		case sideSearchReddit:
			sawSearch = true
		}
	}
	if !sawReddit {
		t.Fatal("the Reddit header after the open section was pushed out of the band")
	}
	if !sawSearch {
		t.Fatal("the Search Reddit discovery row was pushed out of the band")
	}
}

// TestSidebarFolderOverflowScrollsTree covers the non-accordion overflow path: an
// expanded folder (folders are not windowed) can make the whole tree taller than
// the band, and with no source section open the wheel scrolls the TreeView itself.
func TestSidebarFolderOverflowScrollsTree(t *testing.T) {
	s := New(700, 320, ThemeFor(OSLinux, false))
	var subs []Subscription
	var keys []string
	for i := 0; i < 40; i++ {
		ch := "r/" + itoa(i)
		subs = append(subs, Subscription{Source: source.Reddit, Channel: ch})
		keys = append(keys, sideKeyOf(source.Reddit, ch))
	}
	s.SetSubs(subs)
	s.SetSidebarFolders([]settings.Folder{{Name: "All", Subs: keys}}) // one expanded folder holds them all
	s.layout()
	if s.sourceOpen != "" {
		t.Fatal("no source section should be open in this case")
	}
	if !s.sidebarListOverflows() {
		t.Fatal("an expanded 40-account folder should overflow the tree")
	}
	s.MouseMove(10, 150)
	before := s.sideTree.ScrollRow().Get()
	s.Scroll(200)
	if s.sideTree.ScrollRow().Get() <= before {
		t.Fatalf("wheel did not scroll the tree: %d -> %d", before, s.sideTree.ScrollRow().Get())
	}
}

// TestWindowOpenSectionGuards covers layoutOpenSection's defensive branches: an
// open source no longer present in the profile, a non-positive row height, a band
// too small for even one account row, and the ListBox clamping an over-scroll.
func TestWindowOpenSectionGuards(t *testing.T) {
	spacers := func(s *Scene) int {
		n := 0
		for _, nd := range s.flattenSide() {
			if sideData(nd).Kind == sideSpacer {
				n++
			}
		}
		return n
	}

	// Open a source that has no subscriptions in this profile → no header node →
	// layoutOpenSection returns without handing anything to the ListBox.
	s := New(760, 380, ThemeFor(OSMac, false))
	s.SetSubs([]Subscription{{Source: source.Reddit, Channel: "r0"}})
	s.ToggleSidebarSource(source.Twitter) // Twitter has no subs here
	s.layout()
	if got := len(s.sideAccountList.Items); got != 0 {
		t.Fatalf("an absent open source must not populate the ListBox, got %d items", got)
	}

	// A real, overflowing open section, then the edge branches.
	var subs []Subscription
	for i := 0; i < 40; i++ {
		subs = append(subs, Subscription{Source: source.Instagram, Channel: "ig" + itoa(i)})
	}
	s.SetSubs(subs)
	s.ToggleSidebarSource(source.Twitter) // clear the previous open source
	s.ToggleSidebarSource(source.Instagram)
	s.layout()

	// Non-positive row height: the window falls back to the full section (a spacer
	// per account), so nothing is silently dropped when the band can't be measured.
	s.m.sideItemH = 0
	s.buildSideTree()
	if got := spacers(s); got != 40 {
		t.Fatalf("rh<=0 should window the whole section, got %d spacers", got)
	}
	s.layout() // restore a real row height

	// The ListBox clamps an over-scroll: driving it far past the end and reading back
	// through Draw never scrolls beyond the last full window (a wheel up from the top
	// stays at 0).
	s.MouseMove(10, 150)
	s.Scroll(-1000) // wheel up from the top
	if got := s.sideAccountList.ScrollRow().Get(); got != 0 {
		t.Fatalf("a wheel up from the top must clamp to 0, got %d", got)
	}

	// A band too short for even one account row still shows one (the list scrolls).
	tiny := New(760, 380, ThemeFor(OSMac, false))
	tiny.SetSubs(subs)
	tiny.ToggleSidebarSource(source.Instagram)
	tiny.layout()
	tiny.sideBandBot = tiny.sideBandTop + tiny.m.sideItemH // room for a single row
	tiny.buildSideTree()
	if got := spacers(tiny); got != 1 {
		t.Fatalf("a one-row band should window a single account, got %d spacers", got)
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
		{&toolkit.TreeNode{Data: sideNode{Kind: sideSource, Source: source.Reddit}}, HitToggleSource},
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
	s.ToggleSidebarFolder("Langs")       // collapse it so the a11y value reads "collapsed"
	s.ToggleSidebarSource(source.Reddit) // expand the Reddit group so its rust row shows
	tree := s.A11yTree()
	var haveFolder, haveSub, haveBrowse, haveSearch, haveSource bool
	for _, n := range tree {
		switch {
		case n.Name == "Langs":
			haveFolder = true
			if n.Value != "collapsed" {
				t.Fatalf("collapsed folder a11y value = %q", n.Value)
			}
		case n.Name == "rust":
			haveSub = true
			if n.Value == "" {
				t.Fatal("a subscription a11y node should carry its unseen/total count")
			}
		case strings.HasPrefix(n.Name, "Reddit, "):
			haveSource = true
			if n.Value != "expanded" {
				t.Fatalf("expanded source-group a11y value = %q, want expanded", n.Value)
			}
		case n.Name == "Browse newsgroups":
			haveBrowse = true
		case n.Name == "Search Reddit":
			haveSearch = true
		}
	}
	if !haveFolder || !haveSub || !haveSource || !haveBrowse || !haveSearch {
		t.Fatalf("a11y tree missing rows: folder=%v sub=%v source=%v browse=%v search=%v", haveFolder, haveSub, haveSource, haveBrowse, haveSearch)
	}
}

// TestSidebarSourceAccordion covers the single-open accordion state machine:
// start collapsed, open one, switch to another (closing the first), and toggle
// the open one shut.
func TestSidebarSourceAccordion(t *testing.T) {
	s := New(720, 420, ThemeFor(OSLinux, false))
	s.SetSubs([]Subscription{
		{Source: source.Reddit, Channel: "r/a"},
		{Source: source.Twitter, Channel: "nasa"},
	})
	if s.SidebarSourceOpen() != "" {
		t.Fatalf("accordion should start all-collapsed, got %q", s.SidebarSourceOpen())
	}
	s.ToggleSidebarSource(source.Reddit)
	if s.SidebarSourceOpen() != source.Reddit {
		t.Fatalf("open = %q, want reddit", s.SidebarSourceOpen())
	}
	// Opening a second source closes the first (single-open accordion).
	s.ToggleSidebarSource(source.Twitter)
	if s.SidebarSourceOpen() != source.Twitter {
		t.Fatalf("open = %q, want twitter", s.SidebarSourceOpen())
	}
	// Toggling the open source collapses it.
	s.ToggleSidebarSource(source.Twitter)
	if s.SidebarSourceOpen() != "" {
		t.Fatalf("open = %q, want collapsed", s.SidebarSourceOpen())
	}
}

// TestSourceGroupName covers the accordion header names, including the fuller
// forms and the sourceLabel fallback.
func TestSourceGroupName(t *testing.T) {
	cases := map[source.Kind]string{
		source.HackerNews:  "Hacker News",
		source.Twitter:     "X",
		source.Instagram:   "Instagram",
		source.Syndication: "RSS",
		source.Reddit:      "Reddit", // default → sourceLabel
	}
	for k, want := range cases {
		if got := sourceGroupName(k); got != want {
			t.Errorf("sourceGroupName(%q) = %q, want %q", k, got, want)
		}
	}
}

// TestFlattenSideNil covers flattenSide's nil-tree guard.
func TestFlattenSideNil(t *testing.T) {
	z := &Scene{}
	if got := z.flattenSide(); got != nil {
		t.Fatalf("flattenSide on a nil tree = %v, want nil", got)
	}
}
