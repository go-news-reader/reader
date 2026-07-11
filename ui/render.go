package ui

import (
	"fmt"
	"image"
	"strings"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"

	"github.com/go-news-reader/reader/source"
)

// metrics holds the scaled pixel geometry + fonts for the current frame.
type metrics struct {
	sidebarW, topbarH, pad     int
	rowH, cardGap              int
	thumbW, thumbH, badgeH     int
	sideItemH, searchH         int
	title, meta, badge, side   textFace
	search                     textFace
}

func (s *Scene) computeMetrics() metrics {
	rpx := func(n int) int { return int(float64(n)*s.Scale + 0.5) }
	return metrics{
		sidebarW:  rpx(200),
		topbarH:   rpx(48),
		pad:       rpx(12),
		rowH:      rpx(84),
		cardGap:   rpx(8),
		thumbW:    rpx(104),
		thumbH:    rpx(60),
		badgeH:    rpx(18),
		sideItemH: rpx(34),
		searchH:   rpx(28),
		title:     getFace(rpx(15), true),
		meta:      getFace(rpx(12), false),
		badge:     getFace(rpx(10), true),
		side:      getFace(rpx(13), false),
		search:    getFace(rpx(13), false),
	}
}

// subHit maps a sidebar entry rect to its subscription index (AllFilter = All).
type subHit struct {
	index int
	rect  toolkit.Rect
}

// rowLayout positions one feed card by its top offset within the content.
type rowLayout struct {
	item source.Item
	top  int
}

// layout recomputes metrics, sidebar entries, the search rect and feed rows.
func (s *Scene) layout() {
	s.clampSize()
	s.m = s.computeMetrics()
	m := s.m

	// Sidebar entries: "All" then one per subscription.
	s.subs = s.subs[:0]
	y := m.topbarH
	s.subs = append(s.subs, subHit{index: AllFilter, rect: toolkit.Rect{X: 0, Y: y, W: m.sidebarW, H: m.sideItemH}})
	y += m.sideItemH
	for i := range s.Subs {
		s.subs = append(s.subs, subHit{index: i, rect: toolkit.Rect{X: 0, Y: y, W: m.sidebarW, H: m.sideItemH}})
		y += m.sideItemH
	}

	// Search field in the topbar (right of the title).
	s.searchR = toolkit.Rect{X: m.sidebarW + m.pad, Y: (m.topbarH - m.searchH) / 2, W: s.W - m.sidebarW - 2*m.pad, H: m.searchH}

	// Feed rows.
	s.rows = s.rows[:0]
	top := m.pad
	for _, it := range s.filtered() {
		s.rows = append(s.rows, rowLayout{item: it, top: top})
		top += m.rowH + m.cardGap
	}
	s.contentH = top
}

// Draw paints the whole scene into buf (s.W*s.H*4 RGBA bytes).
func (s *Scene) Draw(buf []byte) {
	s.layout()
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

	// --- feed (drawn first; chrome overpaints scroll overflow) ---
	feedTop := m.topbarH
	feedX := m.sidebarW + m.pad
	feedW := s.W - m.sidebarW - 2*m.pad
	for _, r := range s.rows {
		y := feedTop + r.top - s.ScrollY
		if y+m.rowH < feedTop || y >= s.H {
			continue
		}
		blitAt(img, s.cardSprite(r.item, feedW, onAccent, muteS), feedX, y)
	}
	if len(s.rows) == 0 {
		msg := "No items."
		cx := m.sidebarW + (s.W-m.sidebarW-m.title.width(msg))/2
		m.title.draw(img, cx, s.H/2, msg, muteS)
	}

	// --- sidebar ---
	p.FillRect(painter.Rect{X: 0, Y: m.topbarH, W: m.sidebarW, H: s.H - m.topbarH}, th.SurfaceAlt)
	for _, e := range s.subs {
		label := "All Sources"
		if e.index >= 0 {
			label = s.Subs[e.index].name()
		}
		col := th.OnSurface
		if e.index == s.Active {
			p.FillRect(painter.Rect{X: e.rect.X, Y: e.rect.Y, W: e.rect.W, H: e.rect.H}, th.Surface)
			p.FillRect(painter.Rect{X: 0, Y: e.rect.Y, W: rpxOf(s, 3), H: e.rect.H}, th.Accent)
			col = th.Accent
		}
		ty := e.rect.Y + (m.sideItemH-m.side.height)/2
		if e.index >= 0 {
			s.drawDot(p, m.pad, e.rect.Y+m.sideItemH/2, sourceColor(s.Subs[e.index].Source))
			m.side.draw(img, m.pad+rpxOf(s, 14), ty, label, col)
		} else {
			m.side.draw(img, m.pad, ty, label, col)
		}
	}

	// --- topbar ---
	p.FillRect(painter.Rect{X: 0, Y: 0, W: s.W, H: m.topbarH}, th.Accent)
	m.title.draw(img, m.pad, (m.topbarH-m.title.height)/2, "News", onAccent)
	s.drawSearch(p, img, onAccent)

	// --- status footer text (optional) ---
	if s.Status != "" {
		m.meta.draw(img, m.sidebarW+m.pad, s.H-m.meta.height-rpxOf(s, 4), s.Status, muteS)
	}
}

func (s *Scene) drawSearch(p *painter.PixelPainter, img *image.RGBA, onAccent toolkit.RGBA) {
	m := s.m
	th := s.theme
	p.FillRoundRect(painter.Rect(s.searchR), rpxOf(s, 6), th.Surface)
	if s.searchFocused {
		p.StrokeRoundRect(painter.Rect(s.searchR), rpxOf(s, 6), th.Accent, rpxOf(s, 2))
	}
	tx := s.searchR.X + m.pad/2
	ty := s.searchR.Y + (s.searchR.H-m.search.height)/2
	if s.search == "" && !s.searchFocused {
		m.search.draw(img, tx, ty, "Search…", mute(th.OnSurface, th.Surface))
	} else {
		caret := ""
		if s.searchFocused {
			caret = "|"
		}
		m.search.draw(img, tx, ty, s.search+caret, th.OnSurface)
	}
	_ = onAccent
}

func (s *Scene) drawCard(p *painter.PixelPainter, img *image.RGBA, it source.Item, x, y, w int, onAccent, muteS toolkit.RGBA) {
	m := s.m
	th := s.theme
	p.FillRoundRect(painter.Rect{X: x, Y: y, W: w, H: m.rowH}, rpxOf(s, 6), th.Surface)
	p.StrokeRoundRect(painter.Rect{X: x, Y: y, W: w, H: m.rowH}, rpxOf(s, 6), th.Border, 1)

	pad := m.pad
	textW := w - 2*pad
	hasThumb := len(it.Media) > 0
	if hasThumb {
		textW -= m.thumbW + pad
	}

	// Source badge pill.
	label := sourceLabel(it.Source)
	bw := m.badge.width(label) + pad
	p.FillRoundRect(painter.Rect{X: x + pad, Y: y + pad, W: bw, H: m.badgeH}, m.badgeH/2, sourceColor(it.Source))
	m.badge.draw(img, x+pad+pad/2, y+pad+(m.badgeH-m.badge.height)/2, label, rgb(0xFFFFFF))
	// Channel next to the badge.
	if it.Channel != "" {
		m.meta.draw(img, x+pad+bw+pad/2, y+pad+(m.badgeH-m.meta.height)/2, it.Channel, muteS)
	}

	// Title.
	titleY := y + pad + m.badgeH + rpxOf(s, 4)
	m.title.draw(img, x+pad, titleY, truncate(m.title, it.Title, textW), th.OnSurface)

	// Meta line.
	m.meta.draw(img, x+pad, y+m.rowH-m.meta.height-rpxOf(s, 8), truncate(m.meta, metaLine(it), textW), muteS)

	// Thumbnail.
	if hasThumb {
		r := toolkit.Rect{X: x + w - pad - m.thumbW, Y: y + (m.rowH-m.thumbH)/2, W: m.thumbW, H: m.thumbH}
		s.drawThumb(p, img, it, r, muteS)
	}
	_ = onAccent
}

// cardKey identifies a cached card sprite. A card only re-renders when its
// content, width, scale, theme or thumbnail changes.
type cardKey struct {
	id    string
	w     int
	scale float64
	theme *toolkit.Theme
	thumb *image.RGBA
}

// cardSprite returns a cached bitmap of the card for it at width w, rendering it
// once on a cache miss. Scrolling then reuses the sprite via a memcpy blit
// instead of re-rasterising every glyph each frame.
func (s *Scene) cardSprite(it source.Item, w int, onAccent, muteS toolkit.RGBA) *image.RGBA {
	var thumb *image.RGBA
	if s.Thumbs != nil {
		thumb = s.Thumbs[it.ID]
	}
	k := cardKey{id: it.ID, w: w, scale: s.Scale, theme: s.theme, thumb: thumb}
	if s.cardCache == nil {
		s.cardCache = map[cardKey]*image.RGBA{}
	}
	if sp, ok := s.cardCache[k]; ok {
		return sp
	}
	h := s.m.rowH
	buf := make([]byte, w*h*4)
	p := painter.NewPixelPainter(buf, w, h)
	img := &image.RGBA{Pix: buf, Stride: w * 4, Rect: image.Rect(0, 0, w, h)}
	// Fill with the feed background so the card's rounded corners composite
	// correctly when the opaque sprite is blitted onto the scene.
	p.FillRect(painter.Rect{X: 0, Y: 0, W: w, H: h}, s.theme.Background)
	s.drawCard(p, img, it, 0, 0, w, onAccent, muteS)
	s.cardCache[k] = img
	return img
}

// blitAt copies src into dst at (x, y) with a per-row memcpy, clamped to dst's
// bounds. Used for the fast scroll path.
func blitAt(dst, src *image.RGBA, x, y int) {
	sb := src.Bounds()
	for sy := 0; sy < sb.Dy(); sy++ {
		dy := y + sy
		if dy < dst.Rect.Min.Y || dy >= dst.Rect.Max.Y {
			continue
		}
		dx0, sx0 := x, 0
		if dx0 < dst.Rect.Min.X {
			sx0 = dst.Rect.Min.X - dx0
			dx0 = dst.Rect.Min.X
		}
		wpix := sb.Dx() - sx0
		if dx0+wpix > dst.Rect.Max.X {
			wpix = dst.Rect.Max.X - dx0
		}
		if wpix <= 0 {
			continue
		}
		di := dst.PixOffset(dx0, dy)
		si := src.PixOffset(sb.Min.X+sx0, sb.Min.Y+sy)
		copy(dst.Pix[di:di+wpix*4], src.Pix[si:si+wpix*4])
	}
}

func (s *Scene) drawThumb(p *painter.PixelPainter, img *image.RGBA, it source.Item, r toolkit.Rect, muteS toolkit.RGBA) {
	p.FillRect(painter.Rect(r), s.theme.SurfaceAlt)
	if s.Thumbs != nil {
		if t, ok := s.Thumbs[it.ID]; ok && t != nil {
			blit(img, t, r.X, r.Y, r.W, r.H)
			return
		}
	}
	lbl := string(it.Media[0].Kind)
	s.m.meta.draw(img, r.X+(r.W-s.m.meta.width(lbl))/2, r.Y+(r.H-s.m.meta.height)/2, lbl, muteS)
}

// drawDot paints a small filled circle-ish marker (a rounded square) for a
// source colour in the sidebar.
func (s *Scene) drawDot(p *painter.PixelPainter, x, cy int, col toolkit.RGBA) {
	d := rpxOf(s, 8)
	p.FillRoundRect(painter.Rect{X: x, Y: cy - d/2, W: d, H: d}, d/2, col)
}

// HitTest maps a click at (x, y) to an action.
func (s *Scene) HitTest(x, y int) Hit {
	s.layout()
	m := s.m
	if y < m.topbarH {
		if inRect(s.searchR, x, y) {
			return Hit{Kind: HitSearch}
		}
		return Hit{Kind: HitNone}
	}
	if x < m.sidebarW {
		for _, e := range s.subs {
			if inRect(e.rect, x, y) {
				return Hit{Kind: HitSub, Sub: e.index}
			}
		}
		return Hit{Kind: HitNone}
	}
	// Feed.
	contentY := y - m.topbarH + s.ScrollY
	for _, r := range s.rows {
		if contentY >= r.top && contentY < r.top+m.rowH {
			return Hit{Kind: HitItem, Item: r.item}
		}
	}
	return Hit{Kind: HitNone}
}

// --- helpers ---

func rpxOf(s *Scene, n int) int {
	v := int(float64(n)*s.Scale + 0.5)
	if v < 1 {
		v = 1
	}
	return v
}

func inRect(r toolkit.Rect, x, y int) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

// metaLine builds the "author · channel · N pts · N comments" line.
func metaLine(it source.Item) string {
	parts := []string{}
	if it.Author != "" {
		parts = append(parts, it.Author)
	}
	if it.Score >= 0 {
		parts = append(parts, fmt.Sprintf("%d pts", it.Score))
	}
	if it.Comments >= 0 {
		parts = append(parts, fmt.Sprintf("%d comments", it.Comments))
	}
	return strings.Join(parts, " · ")
}

// truncate shortens s with an ellipsis so it fits maxW pixels in face.
func truncate(face textFace, s string, maxW int) string {
	if maxW <= 0 || face.width(s) <= maxW {
		return s
	}
	r := []rune(s)
	for len(r) > 0 {
		r = r[:len(r)-1]
		if face.width(string(r)+"…") <= maxW {
			return string(r) + "…"
		}
	}
	return "…"
}

// blit copies src into dst at (x,y), clipped to maxW×maxH and the dst bounds.
func blit(dst, src *image.RGBA, x, y, maxW, maxH int) {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w > maxW {
		w = maxW
	}
	if h > maxH {
		h = maxH
	}
	for yy := 0; yy < h; yy++ {
		for xx := 0; xx < w; xx++ {
			dst.Set(x+xx, y+yy, src.At(b.Min.X+xx, b.Min.Y+yy))
		}
	}
}

// mute blends fg toward bg (~55%) for secondary text.
func mute(fg, bg toolkit.RGBA) toolkit.RGBA {
	mix := func(a, b uint8) uint8 { return uint8((int(a)*55 + int(b)*45) / 100) }
	return toolkit.RGBA{R: mix(fg.R, bg.R), G: mix(fg.G, bg.G), B: mix(fg.B, bg.B), A: 0xFF}
}

// sourceLabel is the short badge text for a source kind.
func sourceLabel(k source.Kind) string {
	switch k {
	case source.Reddit:
		return "Reddit"
	case source.HackerNews:
		return "HN"
	case source.Syndication:
		return "RSS"
	case source.Usenet:
		return "Usenet"
	case source.Mastodon:
		return "Mastodon"
	case source.Lemmy:
		return "Lemmy"
	case source.Bluesky:
		return "Bluesky"
	case source.Twitter:
		return "X"
	case source.Instagram:
		return "IG"
	case source.TikTok:
		return "TikTok"
	default:
		return string(k)
	}
}

// sourceColor is the brand-ish accent for a source badge.
func sourceColor(k source.Kind) toolkit.RGBA {
	switch k {
	case source.Reddit:
		return rgb(0xFF4500)
	case source.HackerNews:
		return rgb(0xFF6600)
	case source.Syndication:
		return rgb(0xEE802F)
	case source.Usenet:
		return rgb(0x6A5ACD)
	case source.Mastodon:
		return rgb(0x6364FF)
	case source.Lemmy:
		return rgb(0x00BC8C)
	case source.Bluesky:
		return rgb(0x0085FF)
	case source.Twitter:
		return rgb(0x1DA1F2)
	case source.Instagram:
		return rgb(0xE1306C)
	case source.TikTok:
		return rgb(0x25F4EE)
	default:
		return rgb(0x888888)
	}
}
