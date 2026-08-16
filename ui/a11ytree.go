package ui

import (
	"strconv"

	"github.com/go-widgets/toolkit"
)

// A11yNode is one element of the reader's accessibility tree: the toolkit's
// description of it, plus where it sits on screen so a platform bridge can
// hit-test and focus it.
//
// Rect is in screen coordinates. A feed card scrolled out of view still appears
// in the tree, with a rect above or below the viewport: a reader must be able to
// walk the whole list, not only the part that happens to be painted, and it is
// the bridge's job to scroll to what the user selects.
type A11yNode struct {
	toolkit.A11yInfo
	Rect toolkit.Rect
}

// A11yTree describes what the reader is currently showing, as a flat list in
// reading order.
//
// It is built from the scene's own laid-out state rather than from a widget
// tree, because there is no widget tree to walk: every view except the network
// log composes its widgets inside Draw and discards them. The durable structure
// is the layout — the same rects HitTest resolves against — which is also the
// better source: it is what the user can actually perceive and act on.
//
// Tying the two together is deliberate. Anything interactive here carries the
// rect its hit test uses, so "you can click it" and "a reader can find it" are
// the same statement; TestA11yNodesAreReachable asserts exactly that by
// hit-testing every interactive node's centre.
func (s *Scene) A11yTree() []A11yNode {
	// A host's a11y bridge may pull this on every paint frame; each rebuild runs
	// a full layout pass (text measurement included), so serve a cached tree
	// while nothing has changed. rev advances on any state change, so a matching
	// a11yRev proves the cache still describes what is on screen.
	if s.a11yCache != nil && s.a11yRev == s.rev {
		return s.a11yCache
	}
	tree := s.buildA11yTree()
	s.a11yCache = tree
	s.a11yRev = s.rev
	return tree
}

func (s *Scene) buildA11yTree() []A11yNode {
	// Lay out the mode being described, exactly as its own hit test does. The
	// rects a view exposes are computed by ITS layout pass, so reading them after
	// only the feed's leaves every other view's controls at the zero rect —
	// present in the tree and unreachable, which is worse than absent.
	switch s.mode {
	case ModeDetail:
		s.layoutDetail()
		return s.a11yDetail()
	case ModeLog:
		s.layoutLog()
		return s.a11yLog()
	case ModeSettings:
		s.layoutSettings()
		return s.a11ySimple("Settings", s.sDoneR, "Done")
	case ModeAccounts:
		s.layoutAccounts()
		return s.a11ySimple("Accounts", s.accDoneR, "Done")
	case ModeBrowse:
		s.layoutBrowse()
		return s.a11ySimple("Browse newsgroups", s.browseBackR, "Back")
	}
	s.layout()
	s.layoutPreview()
	return s.a11yFeed()
}

// node is a small constructor keeping the call sites readable.
func node(role toolkit.Role, name, value string, r toolkit.Rect) A11yNode {
	return A11yNode{A11yInfo: toolkit.A11yInfo{Role: role, Name: name, Value: value}, Rect: r}
}

// a11yFeed describes the main view: topbar, sidebar, the feed itself, and the
// pinned footer entries.
func (s *Scene) a11yFeed() []A11yNode {
	m := s.m
	out := []A11yNode{
		node(toolkit.RoleDocument, "News", "", toolkit.Rect{W: s.W, H: s.H}),
		node(toolkit.RoleButton, "Toggle sidebar", "", s.burgerR),
		node(toolkit.RoleSearchbox, "Search", s.Search(), s.searchR),
		node(toolkit.RoleButton, "Refresh", "", s.refreshR),
	}

	// profTabs is rebuilt from Profiles by the layout pass above, so every index
	// here is in range by construction.
	for _, t := range s.profTabs {
		v := ""
		if t.index == s.activeProf {
			v = "active"
		}
		out = append(out, node(toolkit.RoleButton, s.Profiles[t.index].Name, v, t.rect))
	}

	// Sidebar: the filter list (a TreeView). Each row carries its unseen/total
	// count, which is otherwise only conveyed by colour; a folder row carries its
	// collapsed/expanded state.
	if m.sidebarW > 0 {
		out = append(out, s.sidebarA11yNodes()...)
	}

	// Pinned in-feed sign-in banners are alerts: they are the reason a source is
	// empty. They sit above the feed list, one per prompt, and do not scroll.
	feedX, feedW := s.feedGeom()
	cardW := s.feedCardW(feedW)
	for i, p := range s.authPrompts {
		by := m.topbarH + i*(m.bannerH+m.cardGap)
		out = append(out, node(toolkit.RoleAlert, string(p.Kind)+" needs sign-in", p.Reason,
			toolkit.Rect{X: feedX, Y: by, W: cardW, H: m.bannerH}))
	}

	// The feed, described from the CardList's display entries in reading order
	// (oldest→newest, newest at the bottom). A card is a group rather than a
	// button: it is an article you can open, and the toolkit's role set has
	// nothing closer. Name is the headline the card actually renders (title, or
	// the body for a post with none). A scrolled-off card still appears, with a
	// rect above or below the viewport — a reader walks the whole list, not only
	// the painted part.
	disp := s.feed.display
	out = append(out, node(toolkit.RoleList, "Feed", strconv.Itoa(len(disp))+" posts", toolkit.Rect{}))
	lr := s.feed.list.Bounds()
	off := s.FeedScrollOffset()
	acc := 0
	for i, e := range disp {
		h := s.feedRowHeight(i)
		rect := toolkit.Rect{X: lr.X, Y: lr.Y - off + acc, W: lr.W, H: h}
		acc += h
		if e.group != nil {
			// The group card announces its part count and its collapsed/expanded
			// state (the chevron's meaning is otherwise conveyed only by the glyph).
			// When expanded, each listed member part follows as its own node so a
			// reader can walk the parts the sighted user sees — the same rows
			// feedGroupMemberAt hit-tests, at their on-screen rects.
			state := "collapsed"
			if s.GroupExpanded(e.group.Base) {
				state = "expanded"
			}
			out = append(out, node(toolkit.RoleGroup, e.group.Base,
				strconv.Itoa(len(e.group.Members))+" parts, "+state, rect))
			if s.GroupExpanded(e.group.Base) {
				gc := s.feedGroupCard(e.group)
				gc.SetBounds(rect)
				for mi, mem := range e.group.Members {
					out = append(out, node(toolkit.RoleText, memberLine(mem), "", gc.MemberRect(mi)))
				}
			}
			continue
		}
		out = append(out, node(toolkit.RoleGroup, cardText(e.item), metaLine(e.item), rect))
	}

	out = append(out,
		node(toolkit.RoleButton, "Accounts", "", s.accountsR),
		node(toolkit.RoleButton, "Network log", "", s.logR),
		node(toolkit.RoleButton, "Settings", "", s.settingsR),
	)
	return out
}

// a11yDetail describes the reading view: its controls, then the article.
func (s *Scene) a11yDetail() []A11yNode {
	d := s.detailContent()
	out := []A11yNode{
		node(toolkit.RoleDocument, "Reading", "", toolkit.Rect{W: s.W, H: s.H}),
		node(toolkit.RoleButton, "Back", "", s.backR),
	}
	if s.openR.W > 0 {
		out = append(out, node(toolkit.RoleButton, "Open original", s.detail.Link, s.openR))
	}
	body := ""
	for _, ln := range d.bodyLines {
		if ln == "" {
			continue
		}
		if body != "" {
			body += " "
		}
		body += ln
	}
	out = append(out, node(toolkit.RoleText, cardText(s.detail), d.meta, toolkit.Rect{}))
	if body != "" {
		out = append(out, node(toolkit.RoleText, body, "", toolkit.Rect{}))
	}
	if alt := mediaAlt(s.detail); alt != "" {
		out = append(out, node(toolkit.RoleImg, alt, "", toolkit.Rect{}))
	}
	return out
}

// a11yLog describes the network log. Its rows ARE a retained toolkit Container,
// the one part of the reader that has a real widget tree, so this is the one
// place CollectA11y can do the work — the two models composing rather than
// competing.
func (s *Scene) a11yLog() []A11yNode {
	out := []A11yNode{
		node(toolkit.RoleDocument, "Network log", "", toolkit.Rect{W: s.W, H: s.H}),
		node(toolkit.RoleButton, "Back", "", s.logBackR),
	}
	// ensureLogBinding always leaves a container behind.
	s.ensureLogBinding()
	for _, info := range toolkit.CollectA11y(s.logContainer.Children()) {
		out = append(out, A11yNode{A11yInfo: info})
	}
	return out
}

// a11ySimple describes a full-screen editor by its title and its way out. These
// views are dense forms of stock toolkit widgets whose own A11y() the eventual
// platform bridge can collect directly; what they lack without this is a name
// and an exit.
func (s *Scene) a11ySimple(title string, exit toolkit.Rect, exitName string) []A11yNode {
	return []A11yNode{
		node(toolkit.RoleDocument, title, "", toolkit.Rect{W: s.W, H: s.H}),
		node(toolkit.RoleButton, exitName, "", exit),
	}
}
