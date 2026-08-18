package ui

// The in-canvas newsgroup browser / subscribe view (ModeBrowse). It shows the
// configured Usenet server's full carried-group list as a collapsible
// hierarchical tree (built from the dotted group names by browse_tree.go), a
// regexp filter field that narrows the tree live as the user types, a Refresh
// control that re-fetches the list, and a per-leaf Subscribe affordance that
// adds a usenet:<group> subscription to the active profile.
//
// Every element is a composed go-widgets/toolkit widget: the ground + accent
// topbar are toolkit.Backdrop, the Back/Refresh controls toolkit.Button, the
// title/server/count lines toolkit.Label, the filter a toolkit.SearchEntry, and
// the group hierarchy a toolkit.TreeView (chevrons, row window, scrollbar and
// virtualization owned by the widget) whose RowRenderer paints each row's label,
// post count and Subscribe marker. Nothing in the view is hand-drawn.

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"

	"github.com/go-news-reader/reader/source"
)

// browseRowLayout is one visible tree row: its group-model node, the TreeView
// node that renders it, and its indent depth (0 for a top-level hierarchy). It
// mirrors the TreeView's flattened, expand-aware order so hit-testing and
// keyboard navigation share one definition of "visible row" with the widget.
type browseRowLayout struct {
	node  *groupNode
	tnode *toolkit.TreeNode
	depth int
}

// browseViewKey memoises the currently-materialised tree view: the group-list
// revision it was built from and the filter text applied to it.
type browseViewKey struct {
	rev    int
	filter string
}

// SetBrowseGroups stores the server's full active group list (name + estimated
// post count — the browse view's data source) and invalidates the cached tree so
// the next layout rebuilds it.
func (s *Scene) SetBrowseGroups(groups []source.GroupInfo) {
	s.browseGroups = groups
	s.browseGroupsRev++
	s.resetBrowseScroll()
	s.browseSel = 0
	s.touch()
}

// BrowseGroups returns the loaded group list.
func (s *Scene) BrowseGroups() []source.GroupInfo { return s.browseGroups }

// groupStatsLabel formats a sampled estimate as "~B% bin · ~I% img", or "" when
// nothing was sampled. The percentages are of the sampled window, which the row
// presents (with the leading ~) as an estimate of the whole group.
func groupStatsLabel(st source.GroupStats) string {
	if st.Sampled == 0 {
		return ""
	}
	bin := st.Binaries * 100 / st.Sampled
	img := st.Images * 100 / st.Sampled
	return fmt.Sprintf("~%d%% bin · ~%d%% img", bin, img)
}

// SetGroupStats stores the sampled content-mix estimate for a group and repaints
// so its browser row picks up the "~B% bin · ~I% img" suffix.
func (s *Scene) SetGroupStats(name string, st source.GroupStats) {
	if s.browseStats == nil {
		s.browseStats = map[string]source.GroupStats{}
	}
	s.browseStats[name] = st
	s.touch()
}

// HasGroupStats reports whether a sampled estimate is already cached for name
// (the app checks it before kicking off a scan, avoiding a re-scan).
func (s *Scene) HasGroupStats(name string) bool {
	_, ok := s.browseStats[name]
	return ok
}

// groupStats returns the cached estimate for name and whether one exists.
func (s *Scene) groupStats(name string) (source.GroupStats, bool) {
	st, ok := s.browseStats[name]
	return st, ok
}

// SelectedBrowseGroup returns the name of the group under the browser's keyboard
// selection, and whether the selected row is a group (not a hierarchy node). The
// app uses it to scan the focused group's content mix.
func (s *Scene) SelectedBrowseGroup() (string, bool) {
	if s.browseSel < 0 || s.browseSel >= len(s.browseRows) {
		return "", false
	}
	n := s.browseRows[s.browseSel].node
	if n == nil || !n.IsGroup {
		return "", false
	}
	return n.Name, true
}

// SetUsenetServer records the configured Usenet server address. A non-empty
// value gates the sidebar "Browse newsgroups" entry and titles the browse view.
func (s *Scene) SetUsenetServer(addr string) {
	if addr == s.usenetAddr {
		return
	}
	s.usenetAddr = addr
	s.browseServer = addr
	s.subsRev++ // the sidebar entry appears/disappears with the server
	s.touch()
}

// UsenetServer returns the configured Usenet server address ("" when none).
func (s *Scene) UsenetServer() string { return s.usenetAddr }

// BrowseEntry exposes the filter widget so the app can two-way bind it to the
// view-model like the topbar search (mvvm.BindField).
func (s *Scene) BrowseEntry() *toolkit.SearchEntry { return s.browseEntry }

// InvalidateBrowse resets the scroll after the filter binder writes
// BrowseEntry.Text directly; passed as mvvm.BindField's invalidate hook.
func (s *Scene) InvalidateBrowse() { s.resetBrowseScroll(); s.browseSel = 0; s.touch() }

// resetBrowseScroll returns the tree to the top (its TreeView is created lazily,
// so it tolerates being called before the first layout).
func (s *Scene) resetBrowseScroll() {
	if s.browseTreeView != nil {
		s.browseTreeView.ScrollRow().Set(0)
	}
}

// BrowseFocused reports whether the filter field holds keyboard focus.
func (s *Scene) BrowseFocused() bool { return s.browseFocused }

// FocusBrowseFilter gives (or removes) keyboard focus to the filter field.
func (s *Scene) FocusBrowseFilter(v bool) { s.browseFocused = v; s.touch() }

// OpenBrowse enters the newsgroup browser view.
func (s *Scene) OpenBrowse() {
	s.mode = ModeBrowse
	s.resetBrowseScroll()
	s.browseSel = 0
	s.browseFocused = false
	s.touch()
}

// browseWindowRows is how many whole tree rows fit in the tree viewport (below
// the count line). 0 before the first layout / when there is no room.
func (s *Scene) browseWindowRows() int {
	rh := s.m.sideItemH
	h := s.H - s.browseTreeTop
	if rh <= 0 || h <= 0 {
		return 0
	}
	return h / rh
}

// NavBrowse moves the keyboard selection dir rows through the visible tree
// (dir<0 up, dir>0 down), clamped to the current rows, and scrolls it into view.
func (s *Scene) NavBrowse(dir int) {
	s.layoutBrowse()
	n := len(s.browseRows)
	if n == 0 {
		return
	}
	s.browseSel += dir
	if s.browseSel < 0 {
		s.browseSel = 0
	}
	if s.browseSel >= n {
		s.browseSel = n - 1
	}
	// Nudge the TreeView's row window by the minimum needed to keep the selected
	// row visible, then re-clamp + re-sync through a layout.
	tv := s.browseTreeView
	wr := s.browseWindowRows()
	sr := tv.ScrollRow()
	switch {
	case s.browseSel < sr.Get():
		sr.Set(s.browseSel)
	case wr > 0 && s.browseSel >= sr.Get()+wr:
		sr.Set(s.browseSel - wr + 1)
	}
	s.layoutBrowse()
	s.touch()
}

// BrowseSelectedNode reports the keyboard-selected tree node: its full name,
// whether it has children (an expandable hierarchy), whether a real group exists
// at it, and whether that group is already subscribed. ok is false when there is
// no selectable row.
func (s *Scene) BrowseSelectedNode() (name string, hasChildren, isGroup, subscribed, ok bool) {
	s.layoutBrowse()
	if s.browseSel < 0 || s.browseSel >= len(s.browseRows) {
		return "", false, false, false, false
	}
	n := s.browseRows[s.browseSel].node
	return n.Name, len(n.Children) > 0, n.IsGroup, s.IsSubscribed(source.Usenet, n.Name), true
}

// CloseBrowse returns to the feed view.
func (s *Scene) CloseBrowse() { s.mode = ModeFeed; s.touch() }

// ToggleBrowseNode expands or collapses the tree node with the given full name.
func (s *Scene) ToggleBrowseNode(name string) {
	if s.browseExpanded == nil {
		s.browseExpanded = map[string]bool{}
	}
	s.browseExpanded[name] = !s.browseExpanded[name]
	s.touch()
}

// BrowseNodeExpanded reports whether the node with the given name is expanded.
func (s *Scene) BrowseNodeExpanded(name string) bool { return s.browseExpanded[name] }

// IsSubscribed reports whether the active profile already subscribes to the
// given source+channel (case-insensitive on the channel).
func (s *Scene) IsSubscribed(k source.Kind, channel string) bool {
	for _, su := range s.ActiveProfile().Subs {
		if su.Source == k && strings.EqualFold(su.Channel, channel) {
			return true
		}
	}
	return false
}

// SubscribeActive adds a source+channel subscription to the active profile (with
// the standard 25-item limit) and rebuilds the sidebar, reporting whether it was
// added (false when it was already present or there is no active profile). The
// app persists + re-aggregates after a true return.
func (s *Scene) SubscribeActive(k source.Kind, channel string) bool {
	if s.activeProf < 0 || s.activeProf >= len(s.Profiles) {
		return false
	}
	channel = normalizeChannel(k, channel)
	p := &s.Profiles[s.activeProf]
	for _, su := range p.Subs {
		if su.Source == k && strings.EqualFold(su.Channel, channel) {
			return false
		}
	}
	p.Subs = append(p.Subs, source.Subscription{Source: k, Channel: channel, Limit: 25})
	s.rebuildSubs()
	s.touchProfiles()
	return true
}

// UnsubscribeActive removes the source+channel subscription from the active
// profile (case-insensitive on the channel) and rebuilds the sidebar, reporting
// whether one was removed (false when absent or there is no active profile). The
// app persists + re-aggregates after a true return.
func (s *Scene) UnsubscribeActive(k source.Kind, channel string) bool {
	if s.activeProf < 0 || s.activeProf >= len(s.Profiles) {
		return false
	}
	p := &s.Profiles[s.activeProf]
	for i, su := range p.Subs {
		if su.Source == k && strings.EqualFold(su.Channel, channel) {
			p.Subs = append(p.Subs[:i], p.Subs[i+1:]...)
			s.rebuildSubs()
			s.touchProfiles()
			return true
		}
	}
	return false
}

// ensureBrowseView (re)builds the cached full tree from the group list and the
// filtered view for the current filter text, recording the match count and any
// regexp-compile error. It is a no-op when neither the list nor the filter has
// changed since the last call.
func (s *Scene) ensureBrowseView() {
	if s.browseTree == nil || s.browseTreeRev != s.browseGroupsRev {
		s.browseTree = buildGroupTree(s.browseGroups)
		s.browseTreeRev = s.browseGroupsRev
		s.browseView = nil
	}
	filter := strings.TrimSpace(s.browseEntry.Text().Get())
	key := browseViewKey{rev: s.browseGroupsRev, filter: filter}
	if s.browseView != nil && s.browseViewKey == key {
		return
	}
	s.browseFilterErr = ""
	switch {
	case filter == "":
		s.browseView = s.browseTree
		s.browseFiltered = false
	default:
		re, err := regexp.Compile("(?i)" + filter)
		if err != nil {
			// An invalid pattern shows an inline hint and an empty tree rather than
			// crashing; typing a valid continuation recovers.
			s.browseFilterErr = "bad regexp: " + err.Error()
			s.browseView = &groupNode{}
		} else {
			s.browseView = filterGroupTree(s.browseTree, re)
		}
		s.browseFiltered = true
	}
	s.browseMatchCount = s.browseView.Leaves
	s.browseViewKey = key
}

// ensureBrowseTree lazily builds the newsgroup TreeView (RowRenderer wired to the
// row painter; the synthetic Root hidden so the top-level hierarchies sit flush
// at depth 0, exactly like the sidebar's list).
func (s *Scene) ensureBrowseTree() {
	if s.browseTreeView != nil {
		return
	}
	s.browseTreeView = toolkit.NewTreeView(nil)
	s.browseTreeView.RowRenderer = s.drawBrowseRow
	s.browseTreeView.HideRoot = true
}

// buildBrowseTree materialises the TreeView's node set from the current filtered
// view, mirroring it into browseRows (flattened, expand-aware) for hit-testing +
// keyboard navigation, and points the TreeView's Selected at the keyboard row.
// Each TreeNode carries its *groupNode in Data; Expanded comes from the expand
// map (or is forced open in the filtered view, which auto-expands to its
// matches). Order matches flattenBrowse so the widget's own flatten agrees.
func (s *Scene) buildBrowseTree() {
	s.ensureBrowseTree()
	expanded := func(name string) bool { return true }
	if !s.browseFiltered {
		expanded = func(name string) bool { return s.browseExpanded[name] }
	}
	root := &toolkit.TreeNode{}
	s.browseRows = s.browseRows[:0]
	var conv func(gn *groupNode, depth int) *toolkit.TreeNode
	conv = func(gn *groupNode, depth int) *toolkit.TreeNode {
		tn := &toolkit.TreeNode{Data: gn, Expanded: len(gn.Children) > 0 && expanded(gn.Name)}
		s.browseRows = append(s.browseRows, browseRowLayout{node: gn, tnode: tn, depth: depth})
		if tn.Expanded {
			for _, c := range gn.Children {
				tn.Children = append(tn.Children, conv(c, depth+1))
			}
		}
		return tn
	}
	if s.browseView != nil {
		for _, c := range s.browseView.Children {
			root.Children = append(root.Children, conv(c, 0))
		}
	}
	s.browseTreeView.Root = root
	s.browseTreeView.Selected().Set(nil)
	if s.browseSel >= 0 && s.browseSel < len(s.browseRows) {
		s.browseTreeView.Selected().Set(s.browseRows[s.browseSel].tnode)
	}
}

// browseTreeTheme is the TreeView's theme: unselected rows fill with the browse
// ground (Background) so they blend into the Backdrop rather than showing a
// Surface band, while the selection (Accent) and scrollbar are the reader's
// standard tree look, matching the sidebar list.
func (s *Scene) browseTreeTheme() *toolkit.Theme {
	tt := *s.theme
	tt.Surface = s.theme.Background
	return &tt
}

// browseRightEdge is the right edge (device px) of a tree row's content — the
// TreeView's inner width, gutter-inset when the tree overflows — so the Subscribe
// marker sits clear of the scrollbar. Derived from the widget's own exported
// geometry so it tracks the TreeView's windowing decision exactly.
func (s *Scene) browseRightEdge() int {
	tv := s.browseTreeView
	return tv.Bounds().X + toolkit.Scaled(toolkit.TreeChevronW) + tv.RowContentWidth(0)
}

// browseRowScreenY returns the on-screen top of visible tree row i, and whether
// it currently falls inside the scrolled window.
func (s *Scene) browseRowScreenY(i int) (int, bool) {
	tv := s.browseTreeView
	wr := s.browseWindowRows()
	sr := tv.ScrollRow().Get()
	if i < sr {
		return 0, false
	}
	if wr > 0 && i >= sr+wr {
		return 0, false
	}
	return s.browseTreeTop + (i-sr)*s.m.sideItemH, true
}

// browseSubscribeRect is the right-aligned Subscribe affordance for visible tree
// row i (matching the marker the RowRenderer paints). The zero Rect is returned
// for a row that is off-screen or not a real group.
func (s *Scene) browseSubscribeRect(i int) toolkit.Rect {
	if i < 0 || i >= len(s.browseRows) || !s.browseRows[i].node.IsGroup {
		return toolkit.Rect{}
	}
	y, ok := s.browseRowScreenY(i)
	if !ok {
		return toolkit.Rect{}
	}
	return s.browseMarkerRect(s.browseRightEdge(), y)
}

// browseMarkerRect is the Subscribe marker box, right-aligned at rightEdge for a
// row whose top is at screen y. Shared by the RowRenderer (which computes
// rightEdge from its content rect) and the hit test (browseRightEdge) so drawn
// and clickable geometry never disagree.
func (s *Scene) browseMarkerRect(rightEdge, y int) toolkit.Rect {
	m := s.m
	d := m.side.height + rpxOf(s, 6)
	return toolkit.Rect{X: rightEdge - m.pad - d, Y: y + (m.sideItemH-d)/2, W: d, H: d}
}

// layoutBrowse computes the topbar buttons, the filter field, the count line and
// the scrolling tree geometry (TreeView bounds + clamped row window).
func (s *Scene) layoutBrowse() {
	s.m = s.computeMetrics()
	m := s.m
	s.ensureBrowseView()
	pad := m.pad
	btnH := m.btnH

	// Topbar band: "‹ Back" (left), Refresh (right, an icon pill).
	bw := m.tab.width("‹ Back") + rpxOf(s, 20)
	s.browseBackR = toolkit.Rect{X: pad, Y: (m.topbarH - btnH) / 2, W: bw, H: btnH}
	rw := m.navIcon + rpxOf(s, 20)
	s.browseRefreshR = toolkit.Rect{X: s.W - pad - rw, Y: (m.topbarH - btnH) / 2, W: rw, H: btnH}

	// Filter field, then the count / hint line, both fixed below the topbar.
	fy := m.topbarH + pad
	s.browseFilterR = toolkit.Rect{X: pad, Y: fy, W: s.W - 2*pad, H: btnH}
	s.browseCountY = fy + btnH + rpxOf(s, 6)

	// The tree viewport starts below the count line and scrolls.
	s.browseTreeTop = s.browseCountY + m.side.height + rpxOf(s, 6)

	// Build the TreeView nodes + the mirrored flat row list, then size the widget
	// to the viewport and clamp its row window. The clamp (ScrollTo) is the
	// refresh-time clamp: a resize or filter change can't leave a stale ScrollRow.
	s.buildBrowseTree()
	s.browseTreeView.RowHeight = m.sideItemH
	treeH := s.H - s.browseTreeTop
	if treeH < 0 {
		treeH = 0
	}
	s.browseTreeView.SetBounds(toolkit.Rect{X: 0, Y: s.browseTreeTop, W: s.W, H: treeH})
	s.browseTreeView.ScrollTo(s.browseTreeView.ScrollRow().Get())
}

// drawBrowse paints the newsgroup browser: a Backdrop ground, the scrolling
// TreeView (with a no-server prompt in its place when unconfigured), then the
// accent topbar chrome — all composed widgets, nothing hand-drawn.
func (s *Scene) drawBrowse(buf []byte) {
	s.layoutBrowse()
	m := s.m
	p := painter.NewPixelPainter(buf, s.W, s.H)
	th := s.theme
	onAccent := themeOnAccent(th)
	muteS := mute(th.OnSurface, th.Surface)

	// Full-surface ground — a solid Backdrop, not a hand-drawn FillRect.
	ground := &toolkit.Backdrop{Fill: th.Background}
	ground.SetBounds(toolkit.Rect{X: 0, Y: 0, W: s.W, H: s.H})
	ground.Draw(p, th)

	if s.usenetAddr == "" {
		s.drawBrowseNoServer(p, muteS)
	} else {
		s.drawBrowseBody(p, muteS)
	}

	// Topbar (accent) with Back, title + server and the Refresh control, over any
	// tree overflow: an accent Backdrop band, inverted Button pills and Labels.
	band := &toolkit.Backdrop{Fill: th.Accent}
	band.SetBounds(toolkit.Rect{X: 0, Y: 0, W: s.W, H: m.topbarH})
	band.Draw(p, th)

	invTheme := invertedTopbarTheme(th, onAccent)
	back := &toolkit.Button{Label: "‹ Back", Style: toolkit.ButtonProminent}
	back.Font = m.tab.font
	back.SetBounds(s.browseBackR)
	back.Draw(p, invTheme)

	title := "Browse newsgroups"
	tx := s.browseBackR.X + s.browseBackR.W + m.pad
	titleLbl := toolkit.NewLabel(title)
	titleLbl.Font, titleLbl.Ink = m.title.font, onAccent
	titleLbl.SetBounds(toolkit.Rect{X: tx, Y: (m.topbarH - m.title.height) / 2, W: s.W, H: m.title.height})
	titleLbl.Draw(p, th)
	if s.browseServer != "" {
		sx := tx + m.title.width(title) + m.pad
		srv := toolkit.NewLabel(s.browseServer)
		srv.Font, srv.Ink = m.meta.font, onAccent
		srv.SetBounds(toolkit.Rect{X: sx, Y: (m.topbarH - m.meta.height) / 2, W: s.W, H: m.meta.height})
		srv.Draw(p, th)
	}
	// Refresh: an inverted icon Button (onAccent pill with an accent glyph).
	refresh := &toolkit.Button{Style: toolkit.ButtonProminent, Icon: s.browseRefreshIcon}
	refresh.SetBounds(s.browseRefreshR)
	refresh.Draw(p, invTheme)
}

// browseRefreshIcon centres the reader's refresh glyph inside the Refresh
// button's face, in the button's current ink (the accent-foreground of the
// inverted topbar theme).
func (s *Scene) browseRefreshIcon(p painter.Painter, r toolkit.Rect, ink toolkit.RGBA) {
	d := s.m.navIcon
	ir := toolkit.Rect{X: r.X + (r.W-d)/2, Y: r.Y + (r.H-d)/2, W: d, H: d}
	drawRefreshIcon(p, ir, ink, s.iconStroke())
}

// drawBrowseNoServer paints the "configure a server first" prompt as a centred
// Label.
func (s *Scene) drawBrowseNoServer(p painter.Painter, muteS toolkit.RGBA) {
	m := s.m
	msg := "No Usenet server configured — add one in Accounts."
	cx := (s.W - m.title.width(msg)) / 2
	lbl := toolkit.NewLabel(msg)
	lbl.Font, lbl.Ink = m.title.font, muteS
	lbl.SetBounds(toolkit.Rect{X: cx, Y: s.H / 2, W: s.W, H: m.title.height})
	lbl.Draw(p, s.theme)
}

// drawBrowseBody paints the filter field, the count/hint line and the scrolling
// TreeView of newsgroups.
func (s *Scene) drawBrowseBody(p painter.Painter, muteS toolkit.RGBA) {
	m := s.m
	th := s.theme

	// Filter field: the browse SearchEntry widget draws its own body, aligned caret
	// (when focused) and focus ring.
	se := s.browseEntry
	se.SetBounds(s.browseFilterR)
	se.Font = ttFont(false, rpxOf(s, 13))
	se.SetFocused(s.browseFocused)
	se.Draw(p, th)

	// Count / hint line as a Label.
	count := fmt.Sprintf("%d groups", s.browseMatchCount)
	col := muteS
	if s.browseFilterErr != "" {
		count = s.browseFilterErr
		col = errorColor(th)
	} else if len(s.browseGroups) == 0 {
		if s.loading {
			count = "Loading group list…"
		} else {
			count = "No groups — press Refresh."
		}
	}
	countLbl := toolkit.NewLabel(count)
	countLbl.Font, countLbl.Ink = m.side.font, col
	countLbl.SetBounds(toolkit.Rect{X: m.pad, Y: s.browseCountY, W: s.W - 2*m.pad, H: m.side.height})
	countLbl.Draw(p, th)

	// The scrolling group hierarchy: the TreeView owns the chevrons, the row
	// window, the scrollbar gutter and virtualization; drawBrowseRow paints each
	// row's rich content.
	s.browseTreeView.Draw(p, s.browseTreeTheme())
}

// drawBrowseRow is the TreeView RowRenderer: it paints one tree row's content in
// the rect the widget hands it (already past the chevron + indent and inset for
// the scrollbar gutter). It draws the node's label (with a "(N)" leaf count on a
// hierarchy node), a real group's estimated post count as a right-aligned column,
// and a Subscribe (＋) / subscribed (✓) marker. The label + count use the
// resolved ink so they stay legible on the accent-filled selected row.
func (s *Scene) drawBrowseRow(p painter.Painter, _ *toolkit.Theme, cr toolkit.Rect, node *toolkit.TreeNode, selected bool, ink toolkit.RGBA) {
	m := s.m
	th := s.theme
	// Every node the TreeView renders was built by buildBrowseTree with a *groupNode
	// in Data, so the assertion always holds.
	n := node.Data.(*groupNode)

	label := n.Segment
	if len(n.Children) > 0 {
		label = fmt.Sprintf("%s  (%d)", n.Segment, n.Leaves)
	}

	// A real group's Subscribe marker, right-aligned at the content edge. On the
	// selected (accent) row a bare ＋ in the row ink stays visible; elsewhere it is
	// the accent pill with an on-accent glyph. The row body ends at the marker.
	right := cr.X + cr.W
	if n.IsGroup {
		sr := s.browseMarkerRect(cr.X+cr.W, cr.Y)
		right = sr.X
		switch {
		case s.IsSubscribed(source.Usenet, n.Name):
			drawCheckIcon(p, sr, successColor(th), s.iconStroke())
		case selected:
			drawPlusIcon(p, sr, ink, s.iconStroke())
		default:
			// The accent marker ground is a Backdrop widget (rounded fill), with the
			// on-accent ＋ icon composited over it — no hand-drawn shape.
			bg := &toolkit.Backdrop{Fill: th.Accent, Radius: rpxOf(s, 4)}
			bg.SetBounds(sr)
			bg.Draw(p, th)
			drawPlusIcon(p, sr, themeOnAccent(th), s.iconStroke())
		}
	}

	// Label + optional post count, composed as an HBox between the content origin
	// and the Subscribe marker: the label flexes, and a real group's estimated
	// count is a fixed right-aligned column (so the browser conveys how busy each
	// group is).
	content := toolkit.NewHBox()
	content.Spacing = rpxOf(s, 6)
	var cnt string
	var cw int
	if n.IsGroup && n.Count > 0 {
		cnt = strconv.Itoa(n.Count)
		if st, ok := s.groupStats(n.Name); ok {
			if lbl := groupStatsLabel(st); lbl != "" {
				cnt += " · " + lbl
			}
		}
		cw = m.side.width(cnt)
	}
	sideFont := ttFont(false, rpxOf(s, 13))
	nameLbl := toolkit.NewLabel(label)
	nameLbl.Font, nameLbl.Ink, nameLbl.Ellipsis = sideFont, ink, true
	content.AddFlex(nameLbl, 1)
	if cnt != "" {
		// Muted on a normal row; the row ink on the selected row (where muteS would
		// be unreadable against the accent fill).
		cntCol := mute(th.OnSurface, th.Surface)
		if selected {
			cntCol = ink
		}
		cntLbl := toolkit.NewLabel(cnt)
		cntLbl.Font, cntLbl.Ink, cntLbl.Align = sideFont, cntCol, toolkit.AlignRight
		content.AddFixed(cntLbl, cw)
	}
	content.SetBounds(toolkit.Rect{X: cr.X, Y: cr.Y, W: right - cr.X - m.pad, H: m.sideItemH})
	content.Draw(p, th)
}

// browseHitTest maps a click in the newsgroup browser to an action.
func (s *Scene) browseHitTest(x, y int) Hit {
	s.layoutBrowse()
	if inRect(s.browseBackR, x, y) {
		return Hit{Kind: HitCloseBrowse}
	}
	if inRect(s.browseRefreshR, x, y) {
		return Hit{Kind: HitBrowseRefresh}
	}
	if s.usenetAddr == "" {
		return Hit{Kind: HitAccounts} // the no-server view routes taps to Accounts
	}
	if inRect(s.browseFilterR, x, y) {
		return Hit{Kind: HitBrowseFilter}
	}
	// A click above the tree viewport (in the filter/count chrome) hits no row:
	// without this, a row scrolled up under the count line was still selectable
	// through it, silently subscribing to or toggling an invisible group.
	if y < s.browseTreeTop {
		return Hit{Kind: HitNone}
	}
	// The TreeView maps the (band-local) point to the node under it; empty space
	// below the last row resolves to no node.
	node := s.browseTreeView.NodeAt(x, y-s.browseTreeTop)
	if node == nil {
		return Hit{Kind: HitNone}
	}
	// Every visible node carries a *groupNode (buildBrowseTree), so the assertion
	// always holds; the hidden synthetic root is never returned by NodeAt.
	n := node.Data.(*groupNode)
	// A real group's ✓/＋ marker toggles the subscription: ＋ subscribes, ✓
	// unsubscribes. The chevron/label area of a node with children toggles expand;
	// a childless leaf row toggles the subscription too.
	subHit := func() Hit {
		if s.IsSubscribed(source.Usenet, n.Name) {
			return Hit{Kind: HitUnsubscribeGroup, Value: n.Name}
		}
		return Hit{Kind: HitSubscribeGroup, Value: n.Name}
	}
	if n.IsGroup {
		// The clicked row's on-screen top, from the same row-window math the TreeView
		// draws with, gives the Subscribe marker its screen rect.
		rowTop := s.browseTreeTop + ((y-s.browseTreeTop)/s.m.sideItemH)*s.m.sideItemH
		if inRect(s.browseMarkerRect(s.browseRightEdge(), rowTop), x, y) {
			return subHit()
		}
	}
	if len(n.Children) > 0 {
		return Hit{Kind: HitToggleBrowseNode, Value: n.Name}
	}
	return subHit()
}
