package ui

// The sidebar's middle list — "All Sources", the virtual folders, the
// subscriptions and the "Browse newsgroups" / "Search Reddit" discovery entries
// — is a toolkit.TreeView. The reader keeps the profile band (above) and the
// pinned Accounts/Log/Settings footer (below) hand-drawn; only the scrollable
// list in between is the widget, so the TreeView owns row layout, the scroll
// window, the scrollbar gutter and hit-testing while the reader supplies each
// row's rich content (source dot, elided label, count chip, pending spinner)
// through its RowRenderer.
//
// TreeView renders its children indented one level under the Root. To keep
// "All Sources", the folders and the unclassified subscriptions flush at the
// same indent (rather than one level deep under a visible root), the Root is a
// synthetic hidden container (HideRoot) and its children ARE the top-level rows:
// an "All Sources" row first, then the folders, the unclassified subscriptions
// and the discovery entries; a folder's own subscriptions nest one level under
// it. Selecting a row maps its TreeNode.Data (a sideNode identity) back to the
// existing filter/navigation actions.

import (
	"strconv"
	"strings"

	simpleicons "github.com/go-icons/simple-icons"
	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"

	"github.com/go-news-reader/reader/source"
)

// sideKind classifies a sidebar TreeView node.
type sideKind int

const (
	sideAll          sideKind = iota // the "All Sources" filter (the tree root)
	sideSub                          // one subscription; Sub indexes s.Subs
	sideFolder                       // a virtual folder; Folder is its name
	sideSource                       // an auto-group header for one source; Source is its kind
	sideBrowse                       // the "Browse newsgroups" discovery entry
	sideSearchReddit                 // the "Search Reddit" discovery entry
	sideSpacer                       // a blank placeholder row the open section's account ListBox paints over
)

// sideNode is the identity a sidebar TreeNode carries in its Data, mapping a
// selected/clicked row back to an action.
type sideNode struct {
	Kind   sideKind
	Sub    int         // sideSub: index into s.Subs
	Folder string      // sideFolder: folder name
	Source source.Kind // sideSource: the grouped source
	Count  int         // sideSource: how many subscriptions the group holds
}

// ensureSideTree lazily builds the sidebar TreeView (RowRenderer wired to the
// scene's rich-row painter).
func (s *Scene) ensureSideTree() {
	if s.sideTree != nil {
		return
	}
	s.sideTree = toolkit.NewTreeView(nil)
	s.sideTree.RowRenderer = s.drawSideRow
	// The Root is a synthetic hidden container: its children (the "All Sources"
	// row, the folders, the unclassified subscriptions and the discovery entries)
	// are the top-level rows, flush at depth 0 with no root indentation.
	s.sideTree.HideRoot = true
	// The scene draws every panel's scrollbar itself (drawVScrollbar → a slim
	// rounded muted toolkit.Scrollbar) so the sidebar and the card list share one
	// bar style; suppress the TreeView's own square/accent bar and read its
	// ScrollExtent instead.
	s.sideTree.HideScrollbar = true

	// The open accordion section's account rows are a real toolkit.ListBox that
	// OWNS its own scroll window, scrollbar and reserved gutter — the widget paints
	// the accounts over the spacer rows the TreeView leaves for it (see
	// layoutOpenSection). Unlike the TreeView, its scrollbar is left VISIBLE: the
	// component drawing its own bar (and insetting its content past the gutter so no
	// count chip sits under the thumb) is the whole point of routing the section
	// through it. Its ItemRenderer decodes each item — a subscription index as a
	// decimal string — back to the reader's rich account-row painter.
	s.sideAccountList = toolkit.NewListBox(nil)
	s.sideAccountList.ItemRenderer = s.drawAccountRow
	s.sideAccountList.OnActivate = func(row int) {
		if sub, ok := s.accountRowSub(row); ok {
			s.SetActive(sub)
		}
	}
}

// accountRowSub decodes the open section ListBox's row (an Items index) to the
// subscription index it stands for, or ok=false for an out-of-range row or an
// unparsable item.
func (s *Scene) accountRowSub(row int) (int, bool) {
	if s.sideAccountList == nil || row < 0 || row >= len(s.sideAccountList.Items) {
		return 0, false
	}
	sub, err := strconv.Atoi(s.sideAccountList.Items[row])
	if err != nil {
		return 0, false
	}
	return sub, true
}

// drawAccountRow is the open section ListBox's ItemRenderer: it decodes the row's
// item (a subscription index) and paints it with the reader's rich account-row
// painter into the content rect rc the ListBox hands it — rc is already inset for
// the widget's own scrollbar gutter, so the count chip never sits under the thumb.
func (s *Scene) drawAccountRow(p painter.Painter, _ *toolkit.Theme, rc toolkit.Rect, _ int, item string, selected bool, ink toolkit.RGBA) {
	sub, err := strconv.Atoi(item)
	if err != nil || sub < 0 || sub >= len(s.Subs) {
		return
	}
	s.drawSideSubRow(p, rc, sub, ttFont(false, rpxOf(s, 13)), ink, selected)
}

// buildSideTree (re)builds the TreeView's node set from the current
// subscriptions + folders and sets Selected to the node matching the active
// filter. It runs each layout: cheap (a few dozen nodes) and keeps the folder
// grouping, unseen counts and selection in lock-step with the model.
func (s *Scene) buildSideTree() {
	s.ensureSideTree()
	// The Root is hidden (HideRoot); its children are the visible top-level rows.
	root := &toolkit.TreeNode{}
	// "All Sources" is the first child (depth 0), so clicking it still selects the
	// AllFilter. It — not the hidden root — is the Selected node for AllFilter,
	// overridden below when a real subscription is the active filter.
	allNode := &toolkit.TreeNode{Label: "All Sources", Data: sideNode{Kind: sideAll}}
	root.Children = append(root.Children, allNode)
	s.sideTree.Selected().Set(allNode)

	// Index the active profile's subscriptions by their stable key so a folder can
	// claim the ones it lists that are actually present in this profile.
	present := make(map[string]int, len(s.Subs))
	for i, sub := range s.Subs {
		present[subKey(sub.Source, sub.Channel)] = i
	}
	foldered := make(map[string]bool, len(s.Subs))

	// Folders first (a folder empty in this profile is hidden but still persisted,
	// so a subscription can be moved into it from any profile).
	for _, f := range s.folders {
		var kids []*toolkit.TreeNode
		for _, key := range f.Subs {
			if foldered[key] {
				continue // a subscription lives in the first folder that lists it
			}
			if idx, ok := present[key]; ok {
				foldered[key] = true
				kids = append(kids, s.sideSubNode(idx))
			}
		}
		if len(kids) == 0 {
			continue
		}
		root.Children = append(root.Children, &toolkit.TreeNode{
			Label:    f.Name,
			Expanded: !s.folderCollapsed[f.Name],
			Data:     sideNode{Kind: sideFolder, Folder: f.Name},
			Children: kids,
		})
	}

	// Every subscription not in a folder is grouped under a collapsible header for
	// its source, so a profile with hundreds of subscriptions collapses to a short
	// list of source headers. The headers behave as an accordion: only the one
	// source in sourceOpen is expanded (sourceOpen "" means all collapsed). Sources
	// appear in the order they first occur in the profile, which is stable.
	var order []source.Kind
	bySource := map[source.Kind][]*toolkit.TreeNode{}
	for i, sub := range s.Subs {
		if foldered[subKey(sub.Source, sub.Channel)] {
			continue
		}
		if _, seen := bySource[sub.Source]; !seen {
			order = append(order, sub.Source)
		}
		bySource[sub.Source] = append(bySource[sub.Source], s.sideSubNode(i))
	}
	for _, src := range order {
		kids := bySource[src]
		root.Children = append(root.Children, &toolkit.TreeNode{
			Expanded: src == s.sourceOpen,
			Data:     sideNode{Kind: sideSource, Source: src, Count: len(kids)},
			Children: kids,
		})
	}

	// Discovery entries last: Browse (only with a Usenet server) then Search Reddit.
	if s.usenetAddr != "" {
		root.Children = append(root.Children, &toolkit.TreeNode{Data: sideNode{Kind: sideBrowse}})
	}
	root.Children = append(root.Children, &toolkit.TreeNode{Data: sideNode{Kind: sideSearchReddit}})

	s.sideTree.Root = root
	s.layoutOpenSection()
}

// layoutOpenSection replaces the open source group's account children with N
// blank spacer rows and hands the accounts themselves to s.sideAccountList — a
// real toolkit.ListBox that owns the section's scroll window, scrollbar and
// gutter. N is the number of account rows that fit under every other (always
// visible) row, so the accordion never pushes a header or the discovery entries
// out of the band; the ListBox scrolls its full account list within that region.
// It fills the ListBox's Items (subscription indices), points its selection at
// the active account's row, sets its bounds and records the section's screen rect
// for the render / hit-test / wheel routers. With nothing open it clears the list.
func (s *Scene) layoutOpenSection() {
	s.sideAccountRect = toolkit.Rect{}
	s.sideAccountList.Items = nil
	if s.sourceOpen == "" {
		return
	}
	var open *toolkit.TreeNode
	for _, n := range s.sideTree.Root.Children {
		if d := sideData(n); d.Kind == sideSource && d.Source == s.sourceOpen {
			open = n
			break
		}
	}
	if open == nil || len(open.Children) == 0 {
		return
	}
	// The account nodes' subscription indices become the ListBox's model; the
	// header keeps only spacer children (blank rows the ListBox paints over).
	full := open.Children
	subs := make([]string, len(full))
	for i, n := range full {
		subs[i] = strconv.Itoa(sideData(n).Sub)
	}
	// Count every other visible row (the open header contributes one row while its
	// accounts are withheld): that is the space the accounts must fit under.
	open.Children = nil
	fixed := len(s.flattenSide())
	rh, band := s.m.sideItemH, s.sideBandBot-s.sideBandTop
	window := len(full)
	if rh > 0 {
		if window = band/rh - fixed; window < 1 {
			window = 1 // headers alone already fill the band; let the list scroll
		}
	}
	if window > len(full) {
		window = len(full)
	}
	open.Children = make([]*toolkit.TreeNode, window)
	for i := range open.Children {
		open.Children[i] = &toolkit.TreeNode{Data: sideNode{Kind: sideSpacer}}
	}
	// The open header's flattened index positions the account region.
	headerRow := 0
	for i, n := range s.flattenSide() {
		if d := sideData(n); d.Kind == sideSource && d.Source == s.sourceOpen {
			headerRow = i
			break
		}
	}
	s.sideAccountList.Items = subs
	s.sideAccountList.RowHeight = rh
	// Point the highlight at the active subscription's row (or none).
	sel := -1
	for i, n := range full {
		if sideData(n).Sub == s.Active {
			sel = i
			break
		}
	}
	s.sideAccountList.Selected().Set(sel)
	// The section's on-screen rect (top just under the open header, N rows tall).
	top := s.sideBandTop + (headerRow+1)*rh
	s.sideAccountRect = toolkit.Rect{X: 0, Y: top, W: s.m.sidebarW, H: window * rh}
	// Bounds are sprite-local (the sidebar is blitted at (0, topbarH)); Draw and the
	// hit/scroll math read them for the scroll window + gutter.
	s.sideAccountList.SetBounds(toolkit.Rect{X: 0, Y: top - s.m.topbarH, W: s.m.sidebarW, H: window * rh})
}

// sectionListShown reports whether the open accordion section currently hands its
// accounts to the ListBox (a source is open with at least one account and a band
// to draw it in). The render, hit-test and wheel routers gate on it.
func (s *Scene) sectionListShown() bool {
	return s.sourceOpen != "" && s.sideAccountList != nil &&
		len(s.sideAccountList.Items) > 0 && s.sideAccountRect.H > 0
}

// sectionListOverflows reports whether the open section holds more accounts than
// its window shows, so the ListBox paints a scrollbar and a wheel scrolls it.
func (s *Scene) sectionListOverflows() bool {
	if !s.sectionListShown() || s.m.sideItemH <= 0 {
		return false
	}
	return len(s.sideAccountList.Items) > s.sideAccountRect.H/s.m.sideItemH
}

// flattenSide walks the sidebar tree in visible (expand-aware) order, returning
// each node so the reader can build the accessibility tree with per-row rects.
func (s *Scene) flattenSide() []*toolkit.TreeNode {
	var out []*toolkit.TreeNode
	if s.sideTree == nil || s.sideTree.Root == nil {
		return out
	}
	var walk func(n *toolkit.TreeNode)
	walk = func(n *toolkit.TreeNode) {
		out = append(out, n)
		if !n.Expanded {
			return
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	// The root is hidden (HideRoot): it is never a visible row, so its children
	// (always shown, regardless of the root's Expanded flag) are the top-level
	// rows, matching the TreeView's own flattening.
	for _, c := range s.sideTree.Root.Children {
		walk(c)
	}
	return out
}

// sidebarA11yNodes describes the sidebar list for the accessibility tree: a
// "Sources" list header, then one node per visible row with the same rect its
// TreeView row occupies on screen (so "you can click it" and "a reader can find
// it" stay the same statement). Rows scrolled out of the band carry a rect above
// or below it, exactly like the feed's off-screen cards.
func (s *Scene) sidebarA11yNodes() []A11yNode {
	m := s.m
	out := []A11yNode{node(toolkit.RoleList, "Sources", strconv.Itoa(len(s.Subs))+" subscriptions", toolkit.Rect{})}
	rows := s.flattenSide()
	for i, n := range rows {
		// The open section's account rows are blank spacers the ListBox paints over;
		// they emit no header-style node here. The accounts themselves are appended
		// below, straight from the ListBox, so "you can click it" and "a reader can
		// find it" stay the same statement for the visible account rows.
		if sideData(n).Kind == sideSpacer {
			continue
		}
		r := toolkit.Rect{X: 0, Y: s.sideBandTop + (i-s.sideTree.ScrollRow().Get())*m.sideItemH, W: m.sidebarW, H: m.sideItemH}
		name, value := "", ""
		switch d := sideData(n); d.Kind {
		case sideAll:
			name = "All Sources"
		case sideSub:
			sub := s.Subs[d.Sub]
			name = sub.name()
			total, unseen := s.subCounts(sub)
			value = strconv.Itoa(unseen) + "/" + strconv.Itoa(total)
		case sideFolder:
			name = d.Folder
			if s.folderCollapsed[d.Folder] {
				value = "collapsed"
			} else {
				value = "expanded"
			}
		case sideSource:
			name = sourceGroupName(d.Source) + ", " + strconv.Itoa(d.Count) + " subscriptions"
			if d.Source == s.sourceOpen {
				value = "expanded"
			} else {
				value = "collapsed"
			}
		case sideBrowse:
			name = "Browse newsgroups"
		case sideSearchReddit:
			name = "Search Reddit"
		}
		out = append(out, node(toolkit.RoleButton, name, value, r))
	}
	// The open section's accounts come from the ListBox: one node per row currently
	// in its scroll window, at the same screen rect the section paints it (so it
	// hit-tests to the very account it names), carrying the same unseen/total value
	// the old sub rows did.
	if s.sectionListShown() {
		lb := s.sideAccountList
		rh := m.sideItemH
		window := s.sideAccountRect.H / rh
		// Match the row the ListBox actually paints at the top: its ScrollRow clamped
		// to the last full window (Draw clamps the same way).
		start := max(0, min(lb.ScrollRow().Get(), len(lb.Items)-window))
		for vis := 0; vis < window && start+vis < len(lb.Items); vis++ {
			if sub, ok := s.accountRowSub(start + vis); ok {
				su := s.Subs[sub]
				total, unseen := s.subCounts(su)
				r := toolkit.Rect{X: 0, Y: s.sideAccountRect.Y + vis*rh, W: m.sidebarW, H: rh}
				out = append(out, node(toolkit.RoleButton, su.name(), strconv.Itoa(unseen)+"/"+strconv.Itoa(total), r))
			}
		}
	}
	return out
}

// sideSubNode builds a subscription leaf node, recording it as Selected when it
// is the active filter.
func (s *Scene) sideSubNode(i int) *toolkit.TreeNode {
	n := &toolkit.TreeNode{Data: sideNode{Kind: sideSub, Sub: i}}
	if s.Active == i {
		s.sideTree.Selected().Set(n)
	}
	return n
}

// sideData returns a node's sideNode identity.
func sideData(n *toolkit.TreeNode) sideNode { d, _ := n.Data.(sideNode); return d }

// sidebarListOverflows reports whether the sidebar list is taller than its band
// (so the TreeView shows a scrollbar and a wheel scrolls it). It counts the
// visible (expand-aware) rows via flattenSide, which honours HideRoot.
func (s *Scene) sidebarListOverflows() bool {
	if s.sideTree == nil || s.sideTree.Root == nil {
		return false
	}
	rh := s.m.sideItemH
	band := s.sideBandBot - s.sideBandTop
	if rh <= 0 || band <= 0 {
		return false
	}
	return len(s.flattenSide()) > band/rh
}

// sidebarScrollbarShown reports whether the whole sidebar list overflows the band
// and so the scene paints a vertical scrollbar down its right edge, meaning the
// TreeView row renderers must reserve its gutter. The open accordion section's
// scrollbar is NOT counted here: those account rows are drawn by the ListBox,
// which owns and insets past its own gutter, so the TreeView never paints under
// it. It mirrors the single remaining drawVScrollbar branch in the sidebar sprite.
func (s *Scene) sidebarScrollbarShown() bool {
	// Called from drawSideRow (a TreeView RowRenderer), so the tree exists.
	_, _, _, shown := s.sideTree.ScrollExtent()
	return shown
}

// drawSideRow is the TreeView RowRenderer: it paints one sidebar row's content
// in the content rect the widget hands it (already past the chevron + indent and
// inset for the scrollbar gutter), in the resolved ink (theme.Background on the
// selected row's accent fill, else theme.OnSurface). The source dot keeps its
// brand colour on every row.
func (s *Scene) drawSideRow(p painter.Painter, th *toolkit.Theme, cr toolkit.Rect, node *toolkit.TreeNode, selected bool, ink toolkit.RGBA) {
	// The sidebar's scrollbar is drawn down the right edge by the scene (the
	// TreeView's own bar is hidden). When one is shown, reserve its gutter here —
	// through the same scrollClampRight contract every other panel uses — so no
	// row's count chip or label paints under the bar. Without this the open
	// accordion section's account counts sat behind the section scrollbar.
	if s.sidebarScrollbarShown() {
		if r := s.scrollClampRight(cr.X+cr.W, s.m.sidebarW, s.m.sidebarW, true); r < cr.X+cr.W {
			cr.W = r - cr.X
		}
	}
	sideF := ttFont(false, rpxOf(s, 13))
	switch d := sideData(node); d.Kind {
	case sideAll:
		s.drawSideLabel(p, cr, sideF, "All Sources", ink)
	case sideBrowse:
		s.drawSideIconLabel(p, cr, node, "Browse newsgroups")
	case sideSearchReddit:
		s.drawSideIconLabel(p, cr, node, "Search Reddit")
	case sideFolder:
		s.drawSideFolderRow(p, cr, d.Folder, ink, selected)
	case sideSource:
		s.drawSideSourceRow(p, cr, d.Source, d.Count, ink)
	case sideSub:
		s.drawSideSubRow(p, cr, d.Sub, sideF, ink, selected)
	case sideSpacer:
		// A blank placeholder under the open source header: the account ListBox is
		// drawn over this region afterwards, so the row itself paints nothing.
	}
}

// drawSideSourceRow draws an auto-group header in the VS Code section-header
// idiom: the source's brand dot, then its name UPPERCASED in a bold, smaller,
// muted face (so a header reads as a section divider, distinct from the account
// rows beneath it), and — right-aligned and muted — how many accounts the group
// holds. The chevron and indent the TreeView already painted mark it expandable.
func (s *Scene) drawSideSourceRow(p painter.Painter, cr toolkit.Rect, src source.Kind, count int, ink toolkit.RGBA) {
	gap := rpxOf(s, 4)
	dotSlot := rpxOf(s, 14)
	// Right-aligned muted subscription count ("1065").
	countF := ttFont(false, rpxOf(s, 11))
	countStr := strconv.Itoa(count)
	rightW := 0
	if count > 0 {
		rightW = countF.Measure(countStr) + rpxOf(s, 2)
	}
	labelW := cr.W - dotSlot - gap
	if rightW > 0 {
		labelW -= rightW + gap
	}
	// Materialise the section as a band: a fill distinct from the sidebar ground
	// (SurfaceAlt) with top + bottom hairline borders. The band spans the whole row
	// (the RowRenderer may paint left of its content rect, over the chevron column),
	// so the chevron the TreeView drew first is repainted afterwards with the exact
	// same geometry (toolkit.Scaled matches the reader's metric scale).
	rowW := cr.X + cr.W
	band := sectionBandFill(s.theme)
	p.FillRect(toolkit.Rect{X: 0, Y: cr.Y, W: rowW, H: cr.H}, band)
	bw := rpxOf(s, 1)
	p.FillRect(toolkit.Rect{X: 0, Y: cr.Y, W: rowW, H: bw}, s.theme.Border)
	p.FillRect(toolkit.Rect{X: 0, Y: cr.Y + cr.H - bw, W: rowW, H: bw}, s.theme.Border)
	cx, cy := toolkit.Scaled(4), cr.Y+cr.H/2
	if src == s.sourceOpen { // ▼ expanded
		for q := 0; q < 4; q++ {
			p.FillRect(toolkit.Rect{X: cx - q, Y: cy + 2 - q, W: 1 + 2*q, H: 1}, ink)
		}
	} else { // ▶ collapsed
		for q := 0; q < 4; q++ {
			p.FillRect(toolkit.Rect{X: cx + 2 - q, Y: cy - q, W: 1, H: 1 + 2*q}, ink)
		}
	}
	// Brand logo: the source's Simple Icons glyph (go-icons/simple-icons), tinted
	// with its brand colour and rendered through the toolkit's SVG icon drawer.
	// Sources with no brand logo (Usenet, RedGIFs) keep the coloured dot.
	if name := sourceIconName(src); name != "" && simpleicons.Has(name) {
		d := rpxOf(s, 15)
		ir := toolkit.Rect{X: cr.X + (dotSlot-d)/2, Y: cr.Y + (cr.H-d)/2, W: d, H: d}
		toolkit.SVGIcon(simpleicons.Icon(name))(p, ir, sourceColor(src))
	} else {
		dot := &sideDot{s: s, col: sourceColor(src)}
		dot.SetBounds(toolkit.Rect{X: cr.X, Y: cr.Y, W: dotSlot, H: cr.H})
		dot.Draw(p, s.theme)
	}
	// Section title: bold, small and UPPERCASE, in a muted header ink — the VS Code
	// activity-bar section look.
	hdrF := ttFont(true, rpxOf(s, 11))
	hdrInk := mute(ink, band) // muted against the band fill, not the ground
	lbl := toolkit.NewLabel(truncateFont(hdrF, strings.ToUpper(sourceGroupName(src)), labelW))
	lbl.Font, lbl.Ink = hdrF, hdrInk
	lbl.SetBounds(toolkit.Rect{X: cr.X + dotSlot + gap, Y: cr.Y, W: labelW, H: cr.H})
	lbl.Draw(p, s.theme)
	if rightW > 0 {
		c := toolkit.NewLabel(countStr)
		c.Font, c.Ink = countF, mute(s.theme.OnSurface, band)
		c.SetBounds(toolkit.Rect{X: cr.X + cr.W - rightW, Y: cr.Y, W: rightW, H: cr.H})
		c.Draw(p, s.theme)
	}
}

// sourceGroupName is the accordion header name for a source — the fuller form of
// [sourceLabel] (which abbreviates for the compact per-post pill).
func sourceGroupName(k source.Kind) string {
	switch k {
	case source.HackerNews:
		return "Hacker News"
	case source.Twitter:
		return "X"
	case source.Instagram:
		return "Instagram"
	case source.Syndication:
		return "RSS"
	default:
		return sourceLabel(k)
	}
}

// drawSideLabel draws a single elided label filling the content rect.
func (s *Scene) drawSideLabel(p painter.Painter, cr toolkit.Rect, f toolkit.Font, text string, ink toolkit.RGBA) {
	lbl := toolkit.NewLabel(truncateFont(f, text, cr.W))
	lbl.Font, lbl.Ink = f, ink
	lbl.SetBounds(cr)
	lbl.Draw(p, s.theme)
}

// drawSideIconLabel draws a discovery entry (Browse newsgroups / Search Reddit):
// a leading accent icon then its accent label, matching the old hand-drawn
// discovery rows. These entries are never the selected filter, so they always
// paint in the accent colour.
func (s *Scene) drawSideIconLabel(p painter.Painter, cr toolkit.Rect, node *toolkit.TreeNode, text string) {
	m := s.m
	col := s.theme.Accent
	icD := m.navIcon
	ir := toolkit.Rect{X: cr.X, Y: cr.Y + (cr.H-icD)/2, W: icD, H: icD}
	if sideData(node).Kind == sideBrowse {
		drawPlusIcon(p, ir, col, s.iconStroke())
	} else {
		drawSearchIcon(p, ir, col)
	}
	gap := rpxOf(s, 6)
	f := ttFont(false, rpxOf(s, 13))
	tx := cr.X + icD + gap
	labelW := cr.X + cr.W - tx
	lbl := toolkit.NewLabel(truncateFont(f, text, labelW))
	lbl.Font, lbl.Ink = f, col
	lbl.SetBounds(toolkit.Rect{X: tx, Y: cr.Y, W: labelW, H: cr.H})
	lbl.Draw(p, s.theme)
}

// drawSideFolderRow draws a folder row: a folder icon, its name, and an
// aggregate count chip over the subscriptions it holds in this profile.
func (s *Scene) drawSideFolderRow(p painter.Painter, cr toolkit.Rect, name string, ink toolkit.RGBA, selected bool) {
	gap := rpxOf(s, 4)
	icD := rpxOf(s, 14)
	ir := toolkit.Rect{X: cr.X, Y: cr.Y + (cr.H-icD)/2, W: icD, H: icD}
	drawFolderIcon(p, ir, ink)
	// While this folder is being renamed inline, draw the focused text Entry (its
	// buffer) in place of the label + count chip, filling the row past the icon.
	if name == s.renamingFolder && s.renameFolderEntry != nil {
		e := s.renameFolderEntry
		e.Font = ttFont(false, rpxOf(s, 13))
		e.SetBounds(toolkit.Rect{X: cr.X + icD + gap, Y: cr.Y, W: cr.W - icD - gap, H: cr.H})
		e.Draw(p, s.theme)
		return
	}
	total, unseen := s.folderCounts(name)
	var chip *countChip
	rightW := 0
	if total > 0 {
		chip = &countChip{s: s, unseen: unseen, total: total, selected: selected, ink: ink}
		rightW = chip.width()
	}
	labelW := cr.W - icD - gap
	if rightW > 0 {
		labelW -= rightW + gap
	}
	f := ttFont(false, rpxOf(s, 13))
	lbl := toolkit.NewLabel(truncateFont(f, name, labelW))
	lbl.Font, lbl.Ink = f, ink
	lbl.SetBounds(toolkit.Rect{X: cr.X + icD + gap, Y: cr.Y, W: labelW, H: cr.H})
	lbl.Draw(p, s.theme)
	if chip != nil {
		chip.SetBounds(toolkit.Rect{X: cr.X + cr.W - rightW, Y: cr.Y, W: rightW, H: cr.H})
		chip.Draw(p, s.theme)
	}
}

// drawSideSubRow draws a subscription row: the source dot, the elided channel
// label, and either the post-count chip or a pending spinner.
func (s *Scene) drawSideSubRow(p painter.Painter, cr toolkit.Rect, idx int, f toolkit.Font, ink toolkit.RGBA, selected bool) {
	sub := s.Subs[idx]
	gap := rpxOf(s, 4)
	dotSlot := rpxOf(s, 14)
	pending := s.IsPendingSub(sub.Source, sub.Channel)
	var chip *countChip
	rightW := 0
	if pending {
		rightW = rpxOf(s, 14) // reserved for the spinner
	} else if t, u := s.subCounts(sub); t > 0 {
		chip = &countChip{s: s, unseen: u, total: t, selected: selected, ink: ink}
		rightW = chip.width()
	}
	labelW := cr.W - dotSlot - gap
	if rightW > 0 {
		labelW -= rightW + gap
	}
	// Source dot, vertically centred in the row.
	dot := &sideDot{s: s, col: sourceColor(sub.Source)}
	dot.SetBounds(toolkit.Rect{X: cr.X, Y: cr.Y, W: dotSlot, H: cr.H})
	dot.Draw(p, s.theme)
	lbl := toolkit.NewLabel(truncateFont(f, sub.name(), labelW))
	lbl.Font, lbl.Ink = f, ink
	lbl.SetBounds(toolkit.Rect{X: cr.X + dotSlot + gap, Y: cr.Y, W: labelW, H: cr.H})
	lbl.Draw(p, s.theme)
	switch {
	case pending:
		d := rpxOf(s, 14)
		s.spinnerAt(toolkit.Rect{X: cr.X + cr.W - d, Y: cr.Y + (cr.H-d)/2, W: d, H: d}, toolkit.SpinnerDots).Draw(p, s.theme)
	case chip != nil:
		chip.SetBounds(toolkit.Rect{X: cr.X + cr.W - rightW, Y: cr.Y, W: rightW, H: cr.H})
		chip.Draw(p, s.theme)
	}
}

// folderCounts sums the total + unseen post counts over the subscriptions a
// folder holds in the active profile.
func (s *Scene) folderCounts(name string) (total, unseen int) {
	present := make(map[string]Subscription, len(s.Subs))
	for _, sub := range s.Subs {
		present[subKey(sub.Source, sub.Channel)] = sub
	}
	for _, f := range s.folders {
		if f.Name != name {
			continue
		}
		for _, key := range f.Subs {
			sub, ok := present[key]
			if !ok {
				continue
			}
			t, u := s.subCounts(sub)
			total += t
			unseen += u
		}
	}
	return total, unseen
}
