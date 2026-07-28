package ui

// The in-canvas newsgroup browser / subscribe view (ModeBrowse). It shows the
// configured Usenet server's full carried-group list as a collapsible
// hierarchical tree (built from the dotted group names by browse_tree.go), a
// regexp filter field that narrows the tree live as the user types, a Refresh
// control that re-fetches the list, and a per-leaf Subscribe affordance that
// adds a usenet:<group> subscription to the active profile. It is drawn with the
// same painter + anti-aliased text as the rest of the app.

import (
	"fmt"
	"image"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"

	"github.com/go-news-reader/reader/source"
)

// browseRowLayout positions one visible tree row: its top offset below the tree
// viewport origin, plus the node and indent depth it renders.
type browseRowLayout struct {
	top   int
	node  *groupNode
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
	s.browseScrollY = 0
	s.touch()
}

// BrowseGroups returns the loaded group list.
func (s *Scene) BrowseGroups() []source.GroupInfo { return s.browseGroups }

// SetUsenetServer records the configured Usenet server address. A non-empty
// value gates the sidebar "Browse newsgroups" entry and titles the browse view.
func (s *Scene) SetUsenetServer(addr string) {
	if addr == s.usenetAddr {
		return
	}
	s.usenetAddr = addr
	s.browseServer = addr
	s.subsRev++ // the sidebar entry appears/disappears with the server
	s.invalidateCards()
	s.touch()
}

// UsenetServer returns the configured Usenet server address ("" when none).
func (s *Scene) UsenetServer() string { return s.usenetAddr }

// BrowseEntry exposes the filter widget so the app can two-way bind it to the
// view-model like the topbar search (mvvm.BindField).
func (s *Scene) BrowseEntry() *toolkit.SearchEntry { return s.browseEntry }

// InvalidateBrowse bumps the damage sequence after the filter binder writes
// BrowseEntry.Text directly; passed as mvvm.BindField's invalidate hook.
func (s *Scene) InvalidateBrowse() { s.browseScrollY = 0; s.touch() }

// BrowseFocused reports whether the filter field holds keyboard focus.
func (s *Scene) BrowseFocused() bool { return s.browseFocused }

// FocusBrowseFilter gives (or removes) keyboard focus to the filter field.
func (s *Scene) FocusBrowseFilter(v bool) { s.browseFocused = v; s.touch() }

// OpenBrowse enters the newsgroup browser view.
func (s *Scene) OpenBrowse() {
	s.mode = ModeBrowse
	s.browseScrollY = 0
	s.browseFocused = false
	s.touch()
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
	filter := strings.TrimSpace(s.browseEntry.Text)
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

// browseIndentW is the per-depth indent of a tree row, in device pixels.
func (s *Scene) browseIndentW() int { return rpxOf(s, 16) }

// browseChevronRect is the disclosure-triangle box for a row at screen y and
// indent depth.
func (s *Scene) browseChevronRect(x, y, depth int) toolkit.Rect {
	m := s.m
	d := m.side.height
	return toolkit.Rect{X: x + depth*s.browseIndentW(), Y: y + (m.sideItemH-d)/2, W: d, H: d}
}

// browseSubscribeRect is the right-aligned Subscribe affordance for a leaf row.
func (s *Scene) browseSubscribeRect(x, y, w int) toolkit.Rect {
	m := s.m
	d := m.side.height + rpxOf(s, 6)
	return toolkit.Rect{X: x + w - m.pad - d, Y: y + (m.sideItemH-d)/2, W: d, H: d}
}

// layoutBrowse computes the topbar buttons, the filter field, the count line and
// the flattened tree rows, applying the vertical scroll offset via row tops.
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

	expanded := func(name string) bool { return true }
	if !s.browseFiltered {
		expanded = func(name string) bool { return s.browseExpanded[name] }
	}
	rows := flattenBrowse(s.browseView, expanded)
	s.browseRows = s.browseRows[:0]
	top := 0
	for _, r := range rows {
		s.browseRows = append(s.browseRows, browseRowLayout{top: top, node: r.node, depth: r.depth})
		top += m.sideItemH
	}
	// browseContentH is expressed relative to the topbar so the generic Scroll
	// clamp (contentH - (H - topbarH)) yields the tree viewport correctly.
	s.browseContentH = (s.browseTreeTop - m.topbarH) + top
}

// drawBrowse paints the newsgroup browser.
func (s *Scene) drawBrowse(buf []byte) {
	s.layoutBrowse()
	m := s.m
	p := painter.NewPixelPainter(buf, s.W, s.H)
	img := &image.RGBA{Pix: buf, Stride: s.W * 4, Rect: image.Rect(0, 0, s.W, s.H)}
	th := s.theme
	onAccent := th.Background
	if v, ok := th.Extra["OnAccent"]; ok {
		onAccent = v
	}
	muteS := mute(th.OnSurface, th.Surface)

	p.FillRect(painter.Rect{X: 0, Y: 0, W: s.W, H: s.H}, th.Background)

	if s.usenetAddr == "" {
		// Opened without a configured server: prompt the user to add one.
		s.drawBrowseNoServer(p, img, muteS)
	} else {
		s.drawBrowseTree(p, img, muteS)
	}

	// Scrollbar down the right edge when the tree overflows (a large server carries
	// tens of thousands of groups; expanding hierarchies grows it further).
	s.drawVScrollbar(p, toolkit.Rect{X: 0, Y: m.topbarH, W: s.W, H: s.H - m.topbarH}, s.browseContentH, s.browseScrollY)

	// Topbar (accent) with Back, title + server, and the Refresh control, drawn
	// over any tree overflow.
	p.FillRect(painter.Rect{X: 0, Y: 0, W: s.W, H: m.topbarH}, th.Accent)
	p.FillRoundRect(painter.Rect(s.browseBackR), rpxOf(s, 6), onAccent)
	m.tab.draw(img, s.browseBackR.X+rpxOf(s, 10), s.browseBackR.Y+(s.browseBackR.H-m.tab.height)/2, "‹ Back", th.Accent)
	title := "Browse newsgroups"
	tx := s.browseBackR.X + s.browseBackR.W + m.pad
	m.title.draw(img, tx, (m.topbarH-m.title.height)/2, title, onAccent)
	if s.browseServer != "" {
		sx := tx + m.title.width(title) + m.pad
		m.meta.draw(img, sx, (m.topbarH-m.meta.height)/2, s.browseServer, onAccent)
	}
	// Refresh pill (icon). While loading it is drawn muted, matching the sidebar.
	p.FillRoundRect(painter.Rect(s.browseRefreshR), rpxOf(s, 6), onAccent)
	ir := toolkit.Rect{X: s.browseRefreshR.X + (s.browseRefreshR.W-m.navIcon)/2, Y: s.browseRefreshR.Y + (s.browseRefreshR.H-m.navIcon)/2, W: m.navIcon, H: m.navIcon}
	drawRefreshIcon(p, ir, th.Accent, s.iconStroke())
}

// drawBrowseNoServer paints the "configure a server first" prompt.
func (s *Scene) drawBrowseNoServer(p *painter.PixelPainter, img *image.RGBA, muteS toolkit.RGBA) {
	m := s.m
	msg := "No Usenet server configured — add one in Accounts."
	cx := (s.W - m.title.width(msg)) / 2
	m.title.draw(img, cx, s.H/2, msg, muteS)
}

// drawBrowseTree paints the filter field, the count/hint line and the scrolling
// tree of newsgroups.
func (s *Scene) drawBrowseTree(p *painter.PixelPainter, img *image.RGBA, muteS toolkit.RGBA) {
	m := s.m
	th := s.theme

	// Filter field: the browse SearchEntry widget (it draws its own aligned caret
	// when focused) with a focus ring.
	se := s.browseEntry
	se.SetBounds(toolkit.Rect(s.browseFilterR))
	se.Font = ttFont(false, rpxOf(s, 13))
	se.Focused = s.browseFocused
	p.FillRoundRect(painter.Rect(s.browseFilterR), rpxOf(s, 6), th.Surface)
	se.Draw(p, th)
	if s.browseFocused {
		p.StrokeRoundRect(painter.Rect(s.browseFilterR), rpxOf(s, 6), th.Accent, rpxOf(s, 2))
	}

	// Count / hint line.
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
	m.side.draw(img, m.pad, s.browseCountY, count, col)

	// Tree rows (scrolled), clipped to the tree viewport.
	feedW := s.W - 2*m.pad
	for _, r := range s.browseRows {
		y := s.browseTreeTop + r.top - s.browseScrollY
		if y+m.sideItemH < s.browseTreeTop || y >= s.H {
			continue
		}
		s.drawBrowseRow(p, img, r, m.pad, y, feedW, muteS)
	}
}

// drawBrowseRow paints one tree row at screen y: indent, optional chevron with
// child count, the node segment, and — for a real group — a Subscribe (＋) or
// subscribed (✓) marker.
func (s *Scene) drawBrowseRow(p *painter.PixelPainter, img *image.RGBA, r browseRowLayout, x, y, w int, muteS toolkit.RGBA) {
	m := s.m
	th := s.theme
	n := r.node
	chev := s.browseChevronRect(x, y, r.depth)
	textX := chev.X + chev.W + rpxOf(s, 4)
	if len(n.Children) > 0 {
		expanded := s.browseFiltered || s.browseExpanded[n.Name]
		drawChevron(p, chev, th.OnSurface, expanded)
	}
	label := n.Segment
	if len(n.Children) > 0 {
		label = fmt.Sprintf("%s  (%d)", n.Segment, n.Leaves)
	}
	right := x + w
	if n.IsGroup {
		sr := s.browseSubscribeRect(x, y, w)
		right = sr.X
		if s.IsSubscribed(source.Usenet, n.Name) {
			drawCheckIcon(p, sr, successColor(th), s.iconStroke())
		} else {
			p.FillRoundRect(painter.Rect(sr), rpxOf(s, 4), th.Accent)
			drawPlusIcon(p, sr, themeOnAccent(th), s.iconStroke())
		}
	}
	ty := y + (m.sideItemH-m.side.height)/2
	// A real group shows its estimated post count, right-aligned before the
	// Subscribe marker, so the browser conveys how busy each newsgroup is.
	labelRight := right
	if n.IsGroup && n.Count > 0 {
		cnt := strconv.Itoa(n.Count)
		cw := m.side.width(cnt)
		cx := right - m.pad - cw
		m.side.draw(img, cx, ty, cnt, muteS)
		labelRight = cx - rpxOf(s, 6)
	}
	m.side.draw(img, textX, ty, truncate(m.side, label, labelRight-textX-m.pad), th.OnSurface)
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
	m := s.m
	feedW := s.W - 2*m.pad
	// A click above the tree viewport (in the filter/count chrome) hits no row:
	// without this, a row scrolled up under the count line was still selectable
	// through it, silently subscribing to or toggling an invisible group.
	if y < s.browseTreeTop {
		return Hit{Kind: HitNone}
	}
	for _, r := range s.browseRows {
		top := s.browseTreeTop + r.top - s.browseScrollY
		// Skip rows outside the tree viewport, mirroring the draw-side clip so a
		// drawn-clipped (invisible) row is never hit-testable.
		if top+m.sideItemH < s.browseTreeTop || top >= s.H {
			continue
		}
		if y < top || y >= top+m.sideItemH {
			continue
		}
		n := r.node
		// A real group's ✓/＋ marker toggles the subscription: ＋ subscribes, ✓
		// unsubscribes. The chevron area (a node with children) toggles expand; a
		// childless leaf row toggles the subscription too.
		subHit := func() Hit {
			if s.IsSubscribed(source.Usenet, n.Name) {
				return Hit{Kind: HitUnsubscribeGroup, Value: n.Name}
			}
			return Hit{Kind: HitSubscribeGroup, Value: n.Name}
		}
		if n.IsGroup && inRect(s.browseSubscribeRect(m.pad, top, feedW), x, y) {
			return subHit()
		}
		if len(n.Children) > 0 {
			return Hit{Kind: HitToggleBrowseNode, Value: n.Name}
		}
		// A matched row with no children is a real group leaf; its row toggles the
		// subscription.
		return subHit()
	}
	return Hit{Kind: HitNone}
}
