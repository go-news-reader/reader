package ui

// Rendering and geometry for the collapsible Usenet group card. The header
// (chevron + base name + "Usenet · N parts · SIZE" meta + a Reconstruct pill)
// is always shown; expanding lists the member parts beneath, indented. The
// chevron is a self-drawn filled triangle (the painter has no arc/line
// primitive) — right-pointing when collapsed, down-pointing when expanded — in
// the same weight as the Iconoir nav icons.

import (
	"fmt"
	"image"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"

	"github.com/go-news-reader/reader/source"
)

// rowHeight returns the laid-out height of a display entry: a standard card, a
// collapsed group header, or an expanded group header plus its member rows.
func (s *Scene) rowHeight(e feedEntry) int {
	if e.group == nil {
		return s.m.rowH
	}
	h := s.m.groupHeadH
	if s.GroupExpanded(e.group.Base) {
		h += len(e.group.Members) * s.m.memberH
	}
	return h
}

// chevronRect is the square that holds the expand/collapse triangle, at the
// header's left. x/yTop are the row's top-left in the caller's coordinate space.
func (s *Scene) chevronRect(x, yTop int) toolkit.Rect {
	m := s.m
	d := m.side.height
	return toolkit.Rect{X: x + m.pad, Y: yTop + (m.groupHeadH-d)/2, W: d, H: d}
}

// reconstructRect is the "Reconstruct" pill, right-aligned in the header.
func (s *Scene) reconstructRect(x, yTop, w int) toolkit.Rect {
	m := s.m
	bw := m.badge.width("Reconstruct") + m.pad
	bh := m.badgeH + rpxOf(s, 6)
	return toolkit.Rect{X: x + w - m.pad - bw, Y: yTop + (m.groupHeadH-bh)/2, W: bw, H: bh}
}

// memberRect is the i-th member part row within an expanded group.
func (s *Scene) memberRect(x, yTop, w, i int) toolkit.Rect {
	m := s.m
	return toolkit.Rect{X: x + m.pad, Y: yTop + m.groupHeadH + i*m.memberH, W: w - 2*m.pad, H: m.memberH}
}

// drawGroup paints a group card at (x, y) width w. It draws the enclosing panel,
// the header (chevron, base name, source badge, meta, Reconstruct pill) and,
// when expanded, the member part rows.
func (s *Scene) drawGroup(p *painter.PixelPainter, img *image.RGBA, g *itemGroup, x, y, w int, onAccent, muteS toolkit.RGBA) {
	m := s.m
	th := s.theme
	expanded := s.GroupExpanded(g.Base)
	totalH := m.groupHeadH
	if expanded {
		totalH += len(g.Members) * m.memberH
	}
	p.FillRoundRect(painter.Rect{X: x, Y: y, W: w, H: totalH}, rpxOf(s, 6), th.Surface)
	p.StrokeRoundRect(painter.Rect{X: x, Y: y, W: w, H: totalH}, rpxOf(s, 6), th.Border, 1)

	// Chevron (collapsed ▸ / expanded ▾).
	chev := s.chevronRect(x, y)
	drawChevron(p, chev, th.OnSurface, expanded)

	// Source badge, then the base name, on the top line.
	nameX := chev.X + chev.W + m.pad/2
	label := sourceLabel(source.Usenet)
	bw := m.badge.width(label) + m.pad
	badgeY := y + m.pad
	p.FillRoundRect(painter.Rect{X: nameX, Y: badgeY, W: bw, H: m.badgeH}, m.badgeH/2, sourceColor(source.Usenet))
	m.badge.draw(img, nameX+m.pad/2, badgeY+(m.badgeH-m.badge.height)/2, label, rgb(0xFFFFFF))

	// Reconstruct pill (right-aligned).
	rr := s.reconstructRect(x, y, w)
	p.FillRoundRect(painter.Rect(rr), rr.H/2, th.Accent)
	m.badge.draw(img, rr.X+(rr.W-m.badge.width("Reconstruct"))/2, rr.Y+(rr.H-m.badge.height)/2, "Reconstruct", onAccent)

	// Base name (title) below the badge.
	titleY := badgeY + m.badgeH + rpxOf(s, 4)
	nameW := rr.X - nameX - m.pad
	m.title.draw(img, nameX, titleY, truncate(m.title, g.Base, nameW), th.OnSurface)

	// Meta line "N parts · M files · SIZE".
	m.meta.draw(img, nameX, titleY+m.title.height+rpxOf(s, 2), truncate(m.meta, groupMeta(g), w-nameX-m.pad), muteS)

	if !expanded {
		return
	}
	// Member part rows: "filename (p/P) size", indented, divider-separated.
	for i, mem := range g.Members {
		mr := s.memberRect(x, y, w, i)
		p.FillRect(painter.Rect{X: mr.X, Y: mr.Y, W: mr.W, H: 1}, th.Border)
		txt := memberLine(mem)
		m.meta.draw(img, mr.X+m.pad, mr.Y+(mr.H-m.meta.height)/2, truncate(m.meta, txt, mr.W-2*m.pad), th.OnSurface)
	}
}

// memberLine renders one expanded member row's text.
func memberLine(mem groupMember) string {
	name := mem.Info.Filename
	if mem.Info.PartTotal > 0 {
		name += fmt.Sprintf("  (%d/%d)", mem.Info.Part, mem.Info.PartTotal)
	}
	if mem.Info.Size > 0 {
		name += "  " + humanSize(mem.Info.Size)
	}
	return name
}

// drawChevron paints a filled disclosure triangle inside r: down-pointing (▾)
// when expanded, right-pointing (▸) when collapsed. It is built from 1px
// FillRects since the painter offers no polygon primitive.
func drawChevron(p *painter.PixelPainter, r toolkit.Rect, col toolkit.RGBA, expanded bool) {
	// iconInset returns a square d×d box, so each successive row/column width
	// (b.W*(b.H-i)/b.H) stays ≥1 down to the apex — no degenerate-size guard is
	// needed (a zero box simply skips both loops).
	b := iconInset(r, 66)
	if expanded {
		// Base at the top, apex at the bottom: width shrinks top→bottom.
		for i := 0; i < b.H; i++ {
			rowW := b.W * (b.H - i) / b.H
			p.FillRect(toolkit.Rect{X: b.X + (b.W-rowW)/2, Y: b.Y + i, W: rowW, H: 1}, col)
		}
		return
	}
	// Base at the left, apex at the right: height shrinks left→right.
	for i := 0; i < b.W; i++ {
		colH := b.H * (b.W - i) / b.W
		p.FillRect(toolkit.Rect{X: b.X + i, Y: b.Y + (b.H-colH)/2, W: 1, H: colH}, col)
	}
}
