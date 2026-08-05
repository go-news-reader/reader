package ui

// Browser-style tabs for the web preview. Each opened target page is a tab; the
// page render, link map and back/forward history already live in the per-item
// maps (previewWeb*/previewWebHist keyed by item id), so a tab is just the
// source.Item plus its position in the strip. The strip is drawn above the
// address-bar toolbar and lets the reader flip between opened articles without
// losing each one's scroll position or history.

import (
	"image"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"

	"github.com/go-news-reader/reader/source"
)

// maxWebTabs caps how many pages stay open; opening more evicts the oldest
// (front) tab and its cached render, like a browser trimming its tab bar.
const maxWebTabs = 6

// webTabHit records a drawn tab's clickable regions for hit-testing.
type webTabHit struct {
	id     string
	body   toolkit.Rect // the tab body (switch to it)
	closeR toolkit.Rect // the tab's close box
}

// ensureWebTab records that a target page has rendered for it, adding a tab when
// the item is not already open. Re-viewing an open item keeps its position (the
// strip does not reshuffle). When the cap is exceeded the oldest tab that is not
// the one just added is closed, dropping its cached web state.
func (s *Scene) ensureWebTab(it source.Item) {
	for _, t := range s.webTabs {
		if t.ID == it.ID {
			return // already open
		}
	}
	s.webTabs = append(s.webTabs, it)
	for len(s.webTabs) > maxWebTabs {
		s.evictWebState(s.webTabs[0].ID)
		s.webTabs = s.webTabs[1:]
	}
}

// evictWebState drops every cached render/link/history entry for an item id (a
// closed or trimmed tab). Safe on nil maps and unknown ids.
func (s *Scene) evictWebState(id string) {
	delete(s.previewWeb, id)
	delete(s.previewWebLinks, id)
	delete(s.previewWebRenderW, id)
	delete(s.previewWebHist, id)
}

// CloseWebTab removes the tab for id (dropping its cached web state) and returns
// the item to switch to (the neighbour that takes its place) with ok=true when
// the closed tab was the active one and another remains — the app re-selects it.
// ok is false when the closed tab was not active, or none remain.
func (s *Scene) CloseWebTab(id string) (source.Item, bool) {
	idx := -1
	for i, t := range s.webTabs {
		if t.ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return source.Item{}, false
	}
	wasActive := s.previewHas && s.previewItem.ID == id
	s.webTabs = append(s.webTabs[:idx], s.webTabs[idx+1:]...)
	s.evictWebState(id)
	if !wasActive || len(s.webTabs) == 0 {
		s.touch()
		return source.Item{}, false
	}
	// The neighbour that now occupies this slot (or the new last tab) becomes active.
	next := idx
	if next >= len(s.webTabs) {
		next = len(s.webTabs) - 1
	}
	s.touch()
	return s.webTabs[next], true
}

// WebTabItem returns the open tab's item for id (so the app can re-select it).
func (s *Scene) WebTabItem(id string) (source.Item, bool) {
	for _, t := range s.webTabs {
		if t.ID == id {
			return t, true
		}
	}
	return source.Item{}, false
}

// webTabsShown reports whether the tab strip is drawn (at least two pages open).
func (s *Scene) webTabsShown() bool { return len(s.webTabs) >= 2 }

// tabStripH is the height of the tab strip when shown, else 0.
func (s *Scene) tabStripH() int {
	if !s.webTabsShown() {
		return 0
	}
	return s.m.badgeH + rpxOf(s, 6)
}

// drawWebTabs paints the tab strip across the top of the pane (row r, at y) and
// records each tab's hit regions. Tabs share the row width equally, each showing
// a truncated title and a close box; the active tab (the current preview) is
// tinted with the surface colour, the rest sit on the alt surface.
func (s *Scene) drawWebTabs(p *painter.PixelPainter, img *image.RGBA, m metrics, th *toolkit.Theme, r toolkit.Rect, y, right int) {
	s.webTabHits = s.webTabHits[:0]
	n := len(s.webTabs)
	gap := rpxOf(s, 4)
	avail := right - (r.X + m.pad)
	tw := (avail - gap*(n-1)) / n
	if tw < rpxOf(s, 40) {
		tw = rpxOf(s, 40) // keep tabs tappable even when many are open
	}
	h := s.tabStripH()
	closeW := m.side.width("×") + rpxOf(s, 6)
	for i, t := range s.webTabs {
		tabX := r.X + m.pad + i*(tw+gap)
		if tabX+tw > right {
			break // ran out of room (very narrow pane / many tabs)
		}
		body := toolkit.Rect{X: tabX, Y: y, W: tw, H: h}
		active := s.previewHas && s.previewItem.ID == t.ID
		fill := th.SurfaceAlt
		if active {
			fill = th.Surface
		}
		p.FillRoundRect(painter.Rect(body), rpxOf(s, 5), fill)
		if active {
			p.StrokeRoundRect(painter.Rect(body), rpxOf(s, 5), th.Accent, rpxOf(s, 1))
		}
		// Title, clipped to the room left of the close box.
		label := t.Title
		if label == "" {
			label = "(untitled)"
		}
		labelW := tw - rpxOf(s, 6)*2 - closeW
		p.PushClip(painter.Rect(body))
		m.side.draw(img, body.X+rpxOf(s, 6), body.Y+(h-m.side.height)/2, clipTextTail(m.side, label, labelW), th.OnSurface)
		p.PopClip()
		// Close box at the tab's right edge.
		closeR := toolkit.Rect{X: body.X + tw - closeW, Y: y, W: closeW, H: h}
		m.side.draw(img, closeR.X+rpxOf(s, 3), closeR.Y+(h-m.side.height)/2, "×", mute(th.OnSurface, fill))
		s.webTabHits = append(s.webTabHits, webTabHit{id: t.ID, body: body, closeR: closeR})
	}
}

// webTabHitTest resolves a click in the tab strip to a close-box (checked first,
// it sits inside the body) or a tab switch. Returns HitNone/false on a miss.
func (s *Scene) webTabHitTest(x, y int) (Hit, bool) {
	for _, t := range s.webTabHits {
		if inRect(t.closeR, x, y) {
			return Hit{Kind: HitWebTabClose, Value: t.id}, true
		}
		if inRect(t.body, x, y) {
			return Hit{Kind: HitWebTab, Value: t.id}, true
		}
	}
	return Hit{}, false
}

// clipTextTail trims the tail of str so it fits within w px (keeping the head —
// a title reads left-to-right), appending an ellipsis when it had to cut.
func clipTextTail(f textFace, str string, w int) string {
	if w <= 0 || f.width(str) <= w {
		return str
	}
	rs := []rune(str)
	for i := len(rs) - 1; i > 0; i-- {
		if cand := string(rs[:i]) + "…"; f.width(cand) <= w {
			return cand
		}
	}
	return "…"
}
