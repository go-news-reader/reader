package ui

import (
	"fmt"
	"image"
	"strconv"
	"strings"
	"time"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"

	"github.com/go-news-reader/reader/source"
)

// metrics holds the scaled pixel geometry + fonts for the current frame.
type metrics struct {
	sidebarW, topbarH, pad    int
	rowH, cardGap             int
	thumbW, thumbH, badgeH    int
	sideItemH, searchH        int
	profileTabH, tabPad, btnH int
	bannerH, navIcon          int
	loadStripH                int
	groupHeadH, memberH       int
	title, meta, badge, side  textFace
	search, tab               textFace
}

func (s *Scene) computeMetrics() metrics {
	rpx := func(n int) int { return int(float64(n)*s.Scale + 0.5) }
	m := metrics{
		sidebarW:  s.sidebarWidthPx(),
		topbarH:   rpx(48),
		pad:       rpx(12),
		rowH:      rpx(84),
		cardGap:   rpx(8),
		thumbW:    rpx(104),
		thumbH:    rpx(60),
		badgeH:    rpx(18),
		sideItemH: rpx(34),
		searchH:   rpx(28),
		tabPad:    rpx(8),
		title:     getFace(rpx(15), true),
		meta:      getFace(rpx(12), false),
		badge:     getFace(rpx(10), true),
		side:      getFace(rpx(13), false),
		search:    getFace(rpx(13), false),
		tab:       getFace(rpx(12), true),
	}
	m.profileTabH = m.tab.height + rpx(12)
	m.btnH = m.tab.height + rpx(8)
	m.bannerH = m.side.height + rpx(16)
	m.navIcon = m.side.height + rpx(4)
	m.loadStripH = m.side.height + rpx(4) + rpx(5) + rpx(6)
	m.groupHeadH = m.badgeH + m.title.height + m.meta.height + rpx(24)
	m.memberH = m.meta.height + rpx(16)
	return m
}

// subHit maps a sidebar entry rect to its subscription index (AllFilter = All).
type subHit struct {
	index int
	rect  toolkit.Rect
}

// profTabHit maps a sidebar profile tab rect to its profile index.
type profTabHit struct {
	index int
	rect  toolkit.Rect
}

// feedRow positions one feed row within the scrollable content: a standalone
// item card (group nil) or a collapsed/expanded Usenet group card. height is the
// row's laid-out height (a group's grows when expanded), so scroll math and the
// offscreen-skip work with variable row heights.
type feedRow struct {
	top    int
	height int
	item   source.Item // valid when group == nil
	group  *itemGroup  // non-nil for a Usenet post group
}

// authRowLayout positions one "needs sign-in" banner row by its top offset
// within the scrollable feed content, carrying the prompt index it renders.
type authRowLayout struct {
	idx int
	top int
}

// layout recomputes metrics, sidebar entries, the search rect and feed rows.
func (s *Scene) layout() {
	s.clampSize()
	s.m = s.computeMetrics()
	m := s.m

	// Profile tabs band at the top of the sidebar.
	s.profTabs = s.profTabs[:0]
	tabTop := m.topbarH
	tx := m.tabPad
	for i, p := range s.Profiles {
		w := m.tab.width(p.Name) + 2*m.tabPad
		s.profTabs = append(s.profTabs, profTabHit{index: i, rect: toolkit.Rect{X: tx, Y: tabTop + rpxOf(s, 4), W: w, H: m.profileTabH - rpxOf(s, 8)}})
		tx += w + rpxOf(s, 4)
	}

	// Pinned entries at the bottom of the sidebar, top-to-bottom: 👤 Accounts,
	// 📡 Network log, ⚙ Settings. Laid out first so the scrollable list above
	// knows where the footer starts.
	s.settingsR = toolkit.Rect{X: 0, Y: s.H - m.sideItemH, W: m.sidebarW, H: m.sideItemH}
	s.logR = toolkit.Rect{X: 0, Y: s.H - 2*m.sideItemH, W: m.sidebarW, H: m.sideItemH}
	s.accountsR = toolkit.Rect{X: 0, Y: s.H - 3*m.sideItemH, W: m.sidebarW, H: m.sideItemH}

	// Sidebar entries: "All", one per subscription, then "＋ Browse newsgroups"
	// (shown only when a Usenet server is configured, else it is a discovery
	// dead-end). This block scrolls as a unit inside the band between the tab
	// strip and the pinned footer; sideScrollY offsets every row and is clamped to
	// the overflow so the rows never invade the footer.
	sideTop := m.topbarH
	if len(s.Profiles) > 0 {
		sideTop += m.profileTabH
	}
	nRows := 1 + len(s.Subs) // "All" + subscriptions
	if s.usenetAddr != "" {
		nRows++ // Browse entry
	}
	s.sideBandTop = sideTop
	s.sideBandBot = s.accountsR.Y // the pinned footer begins here
	band := s.sideBandBot - s.sideBandTop
	if band < 0 {
		band = 0
	}
	s.sideMaxScroll = nRows*m.sideItemH - band
	if s.sideMaxScroll < 0 {
		s.sideMaxScroll = 0
	}
	if s.sideScrollY > s.sideMaxScroll {
		s.sideScrollY = s.sideMaxScroll
	}

	s.subs = s.subs[:0]
	y := sideTop - s.sideScrollY
	s.subs = append(s.subs, subHit{index: AllFilter, rect: toolkit.Rect{X: 0, Y: y, W: m.sidebarW, H: m.sideItemH}})
	y += m.sideItemH
	for i := range s.Subs {
		s.subs = append(s.subs, subHit{index: i, rect: toolkit.Rect{X: 0, Y: y, W: m.sidebarW, H: m.sideItemH}})
		y += m.sideItemH
	}
	if s.usenetAddr != "" {
		s.browseR = toolkit.Rect{X: 0, Y: y, W: m.sidebarW, H: m.sideItemH}
		y += m.sideItemH
	} else {
		s.browseR = toolkit.Rect{}
	}

	// Burger button (left of the topbar) toggles the sidebar. Always present in
	// the feed view so a collapsed sidebar can be reopened.
	s.burgerR = toolkit.Rect{X: 0, Y: 0, W: m.topbarH, H: m.topbarH}

	// Search field in the topbar, right of the burger+title header. The header
	// reserves at least the sidebar width, so nothing overlaps when collapsed.
	headerW := s.burgerR.W + m.pad + m.title.width("News") + m.pad
	if headerW < m.sidebarW {
		headerW = m.sidebarW
	}
	s.searchR = toolkit.Rect{X: headerW + m.pad, Y: (m.topbarH - m.searchH) / 2, W: s.W - headerW - 2*m.pad, H: m.searchH}

	top := m.pad

	// While a refresh streams in and some items are already showing, a slim
	// progress strip sits at the very top of the scrollable feed content (above
	// the banners and cards). With no items yet, the centred placeholder is drawn
	// instead (in Draw), so the strip is reserved only when there is content.
	fil := s.filtered()
	s.showStrip = false
	if s.loading && s.loadTotal > 0 && len(fil) > 0 {
		s.loadStripTop = top
		s.showStrip = true
		top += m.loadStripH + m.cardGap
	}

	// "Needs sign-in" banner rows sit at the top of the scrollable feed content,
	// above the cards, so they scroll with the feed and compose with the card
	// hit-testing through the same offset.
	s.authRows = s.authRows[:0]
	for i := range s.authPrompts {
		s.authRows = append(s.authRows, authRowLayout{idx: i, top: top})
		top += m.bannerH + m.cardGap
	}

	// Feed rows: consecutive Usenet parts of one post collapse into a single
	// group card; everything else is a standalone card. A group's height grows
	// when expanded so scrolling accounts for the listed members.
	s.rows = s.rows[:0]
	for _, e := range groupItems(fil) {
		r := feedRow{top: top, item: e.item, group: e.group}
		r.height = s.rowHeight(e)
		s.rows = append(s.rows, r)
		top += r.height + m.cardGap
	}
	s.contentH = top

	// Per-newsgroup post counts for the bottom status bar (computed here so the
	// feed geometry, which subtracts the bar, is consistent this frame).
	s.statusSegs = groupCountSegs(fil)
}

// Draw paints the whole scene into buf (s.W*s.H*4 RGBA bytes).
func (s *Scene) Draw(buf []byte) {
	switch s.mode {
	case ModeDetail:
		s.drawDetail(buf)
		return
	case ModeSettings:
		s.drawSettings(buf)
		return
	case ModeLog:
		s.drawLog(buf)
		return
	case ModeAccounts:
		s.drawAccounts(buf)
		return
	case ModeBrowse:
		s.drawBrowse(buf)
		return
	}
	s.layout()
	m := s.m
	p := painter.NewPixelPainter(buf, s.W, s.H)
	img := &image.RGBA{Pix: buf, Stride: s.W * 4, Rect: image.Rect(0, 0, s.W, s.H)}
	th := s.theme
	onAccent := themeOnAccent(th)
	muteS := mute(th.OnSurface, th.Surface)

	p.FillRect(painter.Rect{X: 0, Y: 0, W: s.W, H: s.H}, th.Background)

	// Lay out the right-hand preview pane so the feed geometry (which subtracts the
	// pane width) and drawPreview agree this frame.
	s.layoutPreview()

	// --- feed (drawn first; chrome overpaints scroll overflow) ---
	feedTop := m.topbarH
	feedX, feedW := s.feedGeom()
	cardW := s.feedCardW(feedW) // narrower than feedW when the scrollbar shows
	// Progress strip (loading + some items): drawn above the banners, scrolling
	// with the feed content.
	feedBot := s.feedBottom() // content stops above the download panel
	if s.showStrip {
		y := feedTop + s.loadStripTop - s.ScrollY
		if y+m.loadStripH >= feedTop && y < feedBot {
			s.drawLoadStrip(p, img, feedX, y, cardW, muteS)
		}
	}
	// "Needs sign-in" banners (drawn above the cards, scrolling with the feed).
	for _, a := range s.authRows {
		y := feedTop + a.top - s.ScrollY
		if y+m.bannerH < feedTop || y >= feedBot {
			continue
		}
		s.drawAuthBanner(p, img, s.authPrompts[a.idx], feedX, y, cardW, onAccent)
	}
	for _, r := range s.rows {
		y := feedTop + r.top - s.ScrollY
		if y+r.height < feedTop || y >= feedBot {
			continue
		}
		if r.group != nil {
			s.drawGroup(p, img, r.group, feedX, y, cardW, onAccent, muteS)
			continue
		}
		blitAt(img, s.cardSprite(r.item, cardW, onAccent, muteS), feedX, y)
		if s.previewHas && sameItem(r.item, s.previewItem) {
			// Selected card: an accent outline so the current post is visibly
			// picked out, mirroring the sidebar's selected-group affordance.
			p.StrokeRoundRect(painter.Rect{X: feedX, Y: y, W: cardW, H: r.height}, rpxOf(s, 6), th.Accent, rpxOf(s, 2))
		}
	}
	// Scrollbar down the feed's right edge when the content overflows the viewport
	// (the shared indicator; the trees/detail/log reuse the same widget).
	s.drawVScrollbar(p, toolkit.Rect{X: feedX, Y: feedTop, W: feedW, H: feedBot - feedTop}, s.contentH, s.ScrollY)
	if len(s.rows) == 0 {
		if s.loading {
			// A refresh is running but nothing has arrived yet: show the animated
			// placeholder rather than a bare "No items." that looks broken.
			s.drawLoadingPlaceholder(p, img, feedX, cardW, muteS)
		} else {
			msg := "No items."
			cx := m.sidebarW + (s.W-m.sidebarW-m.title.width(msg))/2
			m.title.draw(img, cx, s.H/2, msg, muteS)
		}
	}

	// --- chrome (cached sprites; static across scroll like Evas smart objects) ---
	if m.sidebarW > 0 {
		blitAt(img, s.sidebarSprite(), 0, m.topbarH)
		// A 1px divider at the sidebar's right edge plus a centred grab handle so
		// the resize affordance is visible and easy to hit.
		p.FillRect(painter.Rect{X: m.sidebarW - 1, Y: m.topbarH, W: 1, H: s.H - m.topbarH}, th.Border)
		s.drawGripHandle(p, m.sidebarW)
	}
	blitAt(img, s.topbarSprite(onAccent), 0, 0)

	// --- status footer text (optional) ---
	if s.Status != "" {
		m.meta.draw(img, m.sidebarW+m.pad, s.H-m.meta.height-rpxOf(s, 4), s.Status, muteS)
	}

	// --- right preview/details pane (over the feed, docked right) ---
	s.drawPreview(p, img)

	// --- download-manager panel (docked bottom, over the feed/preview) ---
	s.drawDownloadPanel(p, img)

	// --- per-group post-count status bar (very bottom) ---
	s.drawStatusBar(p, img)
}

// sidebarKey / topbarKey identify the single-slot chrome sprite caches. The
// chrome only re-rasterises when one of its inputs changes — never on scroll.
type sidebarKey struct {
	h, sub     int
	scale      float64
	theme      *toolkit.Theme
	active     int
	activeP    int
	subsRev    int
	profRev    int
	pendRev    int
	anim       int // animation frame, but only while a source is pending
	sideScroll int // subscription-list scroll offset
}
type topbarKey struct {
	w        int
	sidebarW int
	scale    float64
	theme    *toolkit.Theme
	search   string
	focused  bool
}

// sidebarSprite renders (or reuses) the sidebar column at local origin.
func (s *Scene) sidebarSprite() *image.RGBA {
	m := s.m
	th := s.theme
	h := s.H - m.topbarH
	// The pending spinner animates: fold the animation frame into the cache key
	// while any source is pending, so the sprite re-rasterises each tick and the
	// spinner rotates. With nothing pending the frame is pinned to 0, so an idle
	// sidebar keeps its cached sprite.
	anim := 0
	if s.PendingCount() > 0 {
		anim = s.animFrame
	}
	k := sidebarKey{h: h, sub: m.sidebarW, scale: s.Scale, theme: th, active: s.Active, activeP: s.activeProf, subsRev: s.subsRev, profRev: s.profRev, pendRev: s.pendRev, anim: anim, sideScroll: s.sideScrollY}
	if s.sidebarSpr != nil && s.sidebarKey == k {
		return s.sidebarSpr
	}
	onAccent := themeOnAccent(th)
	buf := make([]byte, m.sidebarW*h*4)
	p := painter.NewPixelPainter(buf, m.sidebarW, h)
	img := &image.RGBA{Pix: buf, Stride: m.sidebarW * 4, Rect: image.Rect(0, 0, m.sidebarW, h)}
	p.FillRect(painter.Rect{X: 0, Y: 0, W: m.sidebarW, H: h}, th.SurfaceAlt)

	// Profile tabs (sidebar-local coords).
	for _, t := range s.profTabs {
		ly := t.rect.Y - m.topbarH
		col := th.OnSurface
		if t.index == s.activeProf {
			p.FillRoundRect(painter.Rect{X: t.rect.X, Y: ly, W: t.rect.W, H: t.rect.H}, rpxOf(s, 5), th.Accent)
			col = onAccent
		}
		m.tab.draw(img, t.rect.X+m.tabPad, ly+(t.rect.H-m.tab.height)/2, s.Profiles[t.index].Name, col)
	}

	// Subscription rows. When the list overflows it scrolls; skip any row that is
	// not fully within the band between the tab strip and the pinned footer so a
	// scrolled row never paints over the footer or up into the tabs.
	for _, e := range s.subs {
		if e.rect.Y < s.sideBandTop || e.rect.Y+m.sideItemH > s.sideBandBot {
			continue
		}
		ly := e.rect.Y - m.topbarH // sidebar-local Y
		label := "All Sources"
		if e.index >= 0 {
			label = s.Subs[e.index].name()
		}
		col := th.OnSurface
		if e.index == s.Active {
			p.FillRect(painter.Rect{X: 0, Y: ly, W: m.sidebarW, H: m.sideItemH}, th.Surface)
			p.FillRect(painter.Rect{X: 0, Y: ly, W: rpxOf(s, 3), H: m.sideItemH}, th.Accent)
			col = th.Accent
		}
		sideF := ttFont(false, rpxOf(s, 13))
		if e.index < 0 {
			// "All Sources": a single label filling the row.
			lbl := toolkit.NewLabel(label)
			lbl.Font, lbl.Ink = sideF, col
			lbl.SetBounds(toolkit.Rect{X: m.pad, Y: ly, W: m.sidebarW - 2*m.pad, H: m.sideItemH})
			lbl.Draw(p, th)
			continue
		}
		// A subscription row, composed as an HBox: source dot | channel label |
		// (post count, or a spinner while the source is still fetching). The label
		// renders through ttFont so non-Latin channel names (e.g. CJK) show.
		sub := s.Subs[e.index]
		gap := rpxOf(s, 4)
		dotSlot := rpxOf(s, 14)
		innerW := m.sidebarW - 2*m.pad
		pending := s.IsPendingSub(sub.Source, sub.Channel)
		var chip *countChip
		rightW := 0
		if pending {
			rightW = rpxOf(s, 14) // reserved for the spinner
		} else if total, unseen := s.subCounts(sub); total > 0 {
			chip = &countChip{s: s, unseen: unseen, total: total}
			rightW = chip.width()
		}
		labelW := innerW - dotSlot - gap
		if rightW > 0 {
			labelW -= rightW + gap
		}
		lbl := toolkit.NewLabel(truncateFont(sideF, label, labelW))
		lbl.Font, lbl.Ink = sideF, col
		row := toolkit.NewHBox()
		row.Spacing = gap
		row.AddFixed(&sideDot{s: s, col: sourceColor(sub.Source)}, dotSlot)
		row.AddFlex(lbl, 1)
		switch {
		case chip != nil:
			row.AddFixed(chip, rightW)
		case pending:
			row.AddFixed(toolkit.NewLabel(""), rightW) // reserve the spinner slot
		}
		row.SetBounds(toolkit.Rect{X: m.pad, Y: ly, W: innerW, H: m.sideItemH})
		row.Draw(p, th)
		if pending {
			d := rpxOf(s, 14)
			s.spinnerAt(toolkit.Rect{X: m.sidebarW - m.pad - d, Y: ly + (m.sideItemH-d)/2, W: d, H: d}).Draw(p, th)
		}
	}

	// "＋ Browse newsgroups" entry (sidebar-local coords), shown below the subs
	// when a Usenet server is configured.
	if s.usenetAddr != "" && s.browseR.W > 0 &&
		s.browseR.Y >= s.sideBandTop && s.browseR.Y+m.sideItemH <= s.sideBandBot {
		ly := s.browseR.Y - m.topbarH
		ir := toolkit.Rect{X: m.pad, Y: ly + (m.sideItemH-m.navIcon)/2, W: m.navIcon, H: m.navIcon}
		drawPlusIcon(p, ir, th.Accent, s.iconStroke())
		m.side.draw(img, m.pad+rpxOf(s, 14), ly+(m.sideItemH-m.side.height)/2, "Browse newsgroups", th.Accent)
	}

	// Pinned entries at the bottom: Accounts, Network log, Settings. Each icon is
	// drawn (not a font glyph) so nothing renders as a tofu box.
	navCol := mute(th.OnSurface, th.SurfaceAlt)
	navTextX := m.pad + m.navIcon + rpxOf(s, 6)
	drawNavRow := func(localY int, icon func(painter.Painter, toolkit.Rect, toolkit.RGBA, int), text string) {
		p.FillRect(painter.Rect{X: 0, Y: localY - 1, W: m.sidebarW, H: 1}, th.Border)
		ir := toolkit.Rect{X: m.pad, Y: localY + (m.sideItemH-m.navIcon)/2, W: m.navIcon, H: m.navIcon}
		icon(p, ir, navCol, s.iconStroke())
		m.side.draw(img, navTextX, localY+(m.sideItemH-m.side.height)/2, text, navCol)
	}
	drawNavRow(s.accountsR.Y-m.topbarH, drawUserIcon, "Accounts")
	drawNavRow(s.logR.Y-m.topbarH, drawListIcon, "Network log")
	drawNavRow(s.settingsR.Y-m.topbarH, drawSlidersIcon, "Settings")

	// Scrollbar down the sub-list's right edge when it overflows the band between
	// the profile tabs and the pinned footer (sprite-local coords; the sprite key
	// includes sideScrollY so it re-rasterises as the list scrolls).
	if s.sideMaxScroll > 0 {
		band := s.sideBandBot - s.sideBandTop
		s.drawVScrollbar(p, toolkit.Rect{X: 0, Y: s.sideBandTop - m.topbarH, W: m.sidebarW, H: band}, band+s.sideMaxScroll, s.sideScrollY)
	}

	s.sidebarKey, s.sidebarSpr = k, img
	return img
}

// topbarSprite renders (or reuses) the topbar (accent fill + title + search).
func (s *Scene) topbarSprite(onAccent toolkit.RGBA) *image.RGBA {
	m := s.m
	th := s.theme
	k := topbarKey{w: s.W, sidebarW: m.sidebarW, scale: s.Scale, theme: th, search: s.searchEntry.Text, focused: s.searchFocused}
	if s.topbarSpr != nil && s.topbarKey == k {
		return s.topbarSpr
	}
	buf := make([]byte, s.W*m.topbarH*4)
	p := painter.NewPixelPainter(buf, s.W, m.topbarH)
	img := &image.RGBA{Pix: buf, Stride: s.W * 4, Rect: image.Rect(0, 0, s.W, m.topbarH)}
	p.FillRect(painter.Rect{X: 0, Y: 0, W: s.W, H: m.topbarH}, th.Accent)
	// Burger button at the left, then the title. The burger is a drawn menu icon
	// (three bars) rather than a font glyph, and is always drawn so a collapsed
	// sidebar can be reopened.
	ic := m.navIcon
	drawMenuIcon(p, toolkit.Rect{X: s.burgerR.X + (s.burgerR.W-ic)/2, Y: (m.topbarH - ic) / 2, W: ic, H: ic}, onAccent, s.iconStroke())
	m.title.draw(img, s.burgerR.W+m.pad, (m.topbarH-m.title.height)/2, "News", onAccent)
	// Search box: render the topbar's toolkit.SearchEntry widget itself (its own
	// AA font via ttFont), so what the user sees is the bound widget's text. The
	// scene overlays a rounded focus ring + caret because SearchEntry is
	// focus-agnostic. (topbar is full-width at y=0, so local == absolute coords.)
	se := s.searchEntry
	se.SetBounds(toolkit.Rect(s.searchR))
	se.Font = ttFont(false, rpxOf(s, 13))
	se.Focused = s.searchFocused // the widget draws its own aligned caret
	se.Draw(p, th)
	if s.searchFocused {
		p.StrokeRoundRect(painter.Rect(s.searchR), rpxOf(s, 6), th.Accent, rpxOf(s, 2))
	}
	s.topbarKey, s.topbarSpr = k, img
	return img
}

// cardThumb is a toolkit.Widget wrapping a feed card's thumbnail so the card's
// box layout positions it like any other child; its Draw blits the cached image
// (or a placeholder) into the box-computed rect via drawThumb.
type cardThumb struct {
	toolkit.Base
	s    *Scene
	it   source.Item
	p    *painter.PixelPainter
	img  *image.RGBA
	mute toolkit.RGBA
}

func (c *cardThumb) Draw(_ painter.Painter, _ *toolkit.Theme) {
	c.s.drawThumb(c.p, c.img, c.it, c.Bounds(), c.mute)
}

// drawCard paints one feed card. Its interior is composed with toolkit box
// layouts (Sencha model) rather than hand-positioned draws: an outer HBox splits
// the content column from the thumbnail, and the column is a VBox of a badge row,
// the title, a flexible spacer, and the meta line — each a real widget (Badge /
// Label / cardThumb) the boxes lay out and draw.
func (s *Scene) drawCard(p *painter.PixelPainter, img *image.RGBA, it source.Item, x, y, w int, onAccent, muteS toolkit.RGBA) {
	m := s.m
	th := s.theme
	p.FillRoundRect(painter.Rect{X: x, Y: y, W: w, H: m.rowH}, rpxOf(s, 6), th.Surface)
	p.StrokeRoundRect(painter.Rect{X: x, Y: y, W: w, H: m.rowH}, rpxOf(s, 6), th.Border, 1)

	pad := m.pad
	hasThumb := len(it.Media) > 0
	textW := w - 2*pad
	if hasThumb {
		textW -= m.thumbW + pad // pad = the HBox gap the thumbnail column reserves
	}

	// Badge row: the source pill (fixed to its label width) then the channel.
	label := sourceLabel(it.Source)
	badge := &toolkit.Badge{Text: label, Fill: sourceColor(it.Source), Ink: onAccentFor(sourceColor(it.Source))}
	badge.Font = ttFont(true, rpxOf(s, 10))
	badgeRow := toolkit.NewHBox()
	badgeRow.Spacing = rpxOf(s, 4)
	badgeRow.AddFixed(badge, m.badge.width(label)+pad)
	channel := toolkit.NewLabel(it.Channel)
	channel.Font = ttFont(false, rpxOf(s, 12))
	channel.Ink = muteS
	badgeRow.AddFlex(channel, 1)

	title := toolkit.NewLabel(truncate(m.title, it.Title, textW))
	title.Font = ttFont(true, rpxOf(s, 15))
	title.Ink = th.OnSurface

	meta := toolkit.NewLabel(truncate(m.meta, metaLine(it), textW))
	meta.Font = ttFont(false, rpxOf(s, 12))
	meta.Ink = muteS

	// Content column: badge row, title, flexible gap (pushes meta to the bottom),
	// meta line.
	col := toolkit.NewVBox()
	col.Spacing = -1
	col.AddFixed(badgeRow, m.badgeH)
	col.AddFixed(title, m.title.height+rpxOf(s, 4))
	col.AddFlex(toolkit.NewLabel(""), 1)
	col.AddFixed(meta, m.meta.height+rpxOf(s, 8))

	// Card row: content column (flex) then the optional thumbnail (fixed).
	row := toolkit.NewHBox()
	row.Spacing = pad
	row.AddFlex(col, 1)
	if hasThumb {
		row.AddFixed(&cardThumb{s: s, it: it, p: p, img: img, mute: muteS}, m.thumbW)
	}
	row.SetBounds(toolkit.Rect{X: x + pad, Y: y + pad, W: w - 2*pad, H: m.rowH - 2*pad})
	row.Draw(p, th)
	_ = onAccent
}

// textLine is a toolkit widget that draws one line of anti-aliased getFace text
// (which carries the script-fallback chain, so CJK/Arabic/… render), vertically
// centred in its bounds. It lets box layouts position pre-wrapped text lines
// without changing how they rasterise. It captures the scene's RGBA buffer since
// getFace draws into an *image.RGBA, not through the painter.
type textLine struct {
	toolkit.Base
	face       textFace
	text       string
	ink        toolkit.RGBA
	img        *image.RGBA
	alignRight bool // right-align within the bounds (e.g. a duration column)
}

func (t *textLine) Draw(_ painter.Painter, _ *toolkit.Theme) {
	b := t.Bounds()
	ty := b.Y
	if b.H > t.face.height {
		ty += (b.H - t.face.height) / 2
	}
	tx := b.X
	if t.alignRight {
		tx = b.X + b.W - t.face.width(t.text)
	}
	t.face.draw(t.img, tx, ty, t.text, t.ink)
}

// truncateFont clips s with a trailing ellipsis to fit maxW pixels in the
// toolkit font f — the box-layout analogue of truncate (which measures a
// getFace textFace), so a Label pre-fits its computed slot.
func truncateFont(f toolkit.Font, s string, maxW int) string {
	if maxW <= 0 || f.Measure(s) <= maxW {
		return s
	}
	r := []rune(s)
	for len(r) > 0 {
		r = r[:len(r)-1]
		if f.Measure(string(r)+"…") <= maxW {
			return string(r) + "…"
		}
	}
	return "…"
}

// sideDot is a toolkit widget painting a subscription's source-colour dot, so a
// sidebar row lays out as a box (dot | label | count) instead of hand-placed x/y.
type sideDot struct {
	toolkit.Base
	s   *Scene
	col toolkit.RGBA
}

func (d *sideDot) Draw(p painter.Painter, _ *toolkit.Theme) {
	if pix, ok := p.(*painter.PixelPainter); ok {
		b := d.Bounds()
		d.s.drawDot(pix, b.X, b.Y+b.H/2, d.col)
	}
}

// countChip renders a sidebar row's post count right-aligned in its bounds: the
// group total muted, and — when non-zero — the unseen/new count in accent before
// it ("<unseen>/<total>"). Both are toolkit Labels (via ttFont).
type countChip struct {
	toolkit.Base
	s             *Scene
	unseen, total int
}

// width is the chip's rendered pixel width, so the row's HBox can reserve it.
func (c *countChip) width() int {
	f := ttFont(false, rpxOf(c.s, 12))
	w := f.Measure(strconv.Itoa(c.total))
	if c.unseen > 0 {
		w += f.Measure(strconv.Itoa(c.unseen) + "/")
	}
	return w
}

func (c *countChip) Draw(p painter.Painter, th *toolkit.Theme) {
	b := c.Bounds()
	f := ttFont(false, rpxOf(c.s, 12))
	totalStr := strconv.Itoa(c.total)
	tw := f.Measure(totalStr)
	tot := toolkit.NewLabel(totalStr)
	tot.Font, tot.Ink = f, mute(th.OnSurface, th.SurfaceAlt)
	tot.SetBounds(toolkit.Rect{X: b.X + b.W - tw, Y: b.Y, W: tw, H: b.H})
	tot.Draw(p, th)
	if c.unseen > 0 {
		us := strconv.Itoa(c.unseen) + "/"
		uw := f.Measure(us)
		un := toolkit.NewLabel(us)
		un.Font, un.Ink = f, th.Accent
		un.SetBounds(toolkit.Rect{X: b.X + b.W - tw - uw, Y: b.Y, W: uw, H: b.H})
		un.Draw(p, th)
	}
}

// drawAuthBanner paints one clickable "needs sign-in" row with toolkit.Banner:
// the widget fills the accent strip and draws the leading padlock through its
// Icon slot; the "<Provider> needs sign-in — Open Accounts" message is drawn on
// top in the reader's TrueType face. The whole row hit-tests as HitFixAuth (see
// HitTest), so the visible "Open Accounts" text is the click target. onAccent is
// the readable ink on the accent fill (theme Extra["OnAccent"] when present,
// which is exactly what Banner's accentInk resolves to).
func (s *Scene) drawAuthBanner(p *painter.PixelPainter, img *image.RGBA, ap AuthPrompt, x, y, w int, onAccent toolkit.RGBA) {
	m := s.m
	bn := &toolkit.Banner{
		Revealed: true,
		Icon: func(ip painter.Painter, r toolkit.Rect, ink toolkit.RGBA) {
			drawLockIcon(ip, r, ink, s.iconStroke())
		},
	}
	bn.SetBounds(toolkit.Rect{X: x, Y: y, W: w, H: m.bannerH})
	bn.Draw(p, s.theme)

	// Mirror the Banner's leading-icon geometry to place the label after it.
	iconD := m.bannerH - 2*toolkit.BannerPadY
	tx := x + toolkit.BannerPadX + iconD + toolkit.BannerPadX/2
	ty := y + (m.bannerH-m.side.height)/2
	lbl := sourceLabel(ap.Kind) + " needs sign-in — Open Accounts"
	m.side.draw(img, tx, ty, truncate(m.side, lbl, x+w-tx-m.pad), onAccent)
}

// cardKey identifies a cached card sprite. A card only re-renders when its
// content, width, scale, theme or thumbnail changes.
// cardKey must capture enough of the item's identity that two different items
// never share a sprite. it.ID is only stable *within* a Source (and is "" for
// some feeds), so a HackerNews and a Lemmy post both keyed "1" — or two empty-ID
// RSS items — would collide and blit the wrong card. Including Source + Title
// disambiguates without hashing the whole item.
// sameItem reports whether two items are the same post. it.ID is only unique
// within a Source (and empty for some feeds), so identity is Source+ID+Title —
// exactly what cardKey uses to avoid sprite collisions.
func sameItem(a, b source.Item) bool {
	return a.Source == b.Source && a.ID == b.ID && a.Title == b.Title
}

type cardKey struct {
	id    string
	src   source.Kind
	title string
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
	k := cardKey{id: it.ID, src: it.Source, title: it.Title, w: w, scale: s.Scale, theme: s.theme, thumb: thumb}
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

// drawGripHandle paints a short, centred vertical pill on a resizable divider at
// column cx, so the drag affordance is visible and easy to grab.
func (s *Scene) drawGripHandle(p *painter.PixelPainter, cx int) {
	m := s.m
	gw := rpxOf(s, 4)
	gh := rpxOf(s, 36)
	gy := m.topbarH + (s.H-m.topbarH-gh)/2
	p.FillRoundRect(painter.Rect{X: cx - gw/2, Y: gy, W: gw, H: gh}, gw/2, mute(s.theme.OnSurface, s.theme.Surface))
}

// drawDot paints a small filled circle-ish marker (a rounded square) for a
// source colour in the sidebar.
func (s *Scene) drawDot(p *painter.PixelPainter, x, cy int, col toolkit.RGBA) {
	d := rpxOf(s, 8)
	p.FillRoundRect(painter.Rect{X: x, Y: cy - d/2, W: d, H: d}, d/2, col)
}

// drawSourceBadge paints a source-coloured pill at r via toolkit.Badge (using
// its per-source Fill colour). Text is left empty so the widget renders the
// rounded pill only; the caller draws the label on top in the reader's
// TrueType face. Shared by the item card and the Usenet group header.
func (s *Scene) drawSourceBadge(p *painter.PixelPainter, r toolkit.Rect, k source.Kind) {
	// Ink is derived from the fill's luminance, not hard white: bright source
	// colours (TikTok cyan 0x25F4EE, Lemmy 0x00BC8C) render white text almost
	// illegibly; onAccentFor picks black on those and white on the dark fills.
	b := &toolkit.Badge{Text: sourceLabel(k), Fill: sourceColor(k), Ink: onAccentFor(sourceColor(k))}
	b.Font = ttFont(true, rpxOf(s, 10)) // render its own AA label (no text-on-top)
	b.SetBounds(r)
	b.Draw(p, s.theme)
}

// HitTest maps a click at (x, y) to an action.
func (s *Scene) HitTest(x, y int) Hit {
	switch s.mode {
	case ModeDetail:
		return s.detailHitTest(x, y)
	case ModeSettings:
		return s.hitSettings(x, y)
	case ModeLog:
		return s.logHitTest(x, y)
	case ModeAccounts:
		return s.accountsHitTest(x, y)
	case ModeBrowse:
		return s.browseHitTest(x, y)
	}
	s.layout()
	s.layoutPreview()
	m := s.m
	if y < m.topbarH {
		if inRect(s.burgerR, x, y) {
			return Hit{Kind: HitBurger}
		}
		if inRect(s.searchR, x, y) {
			return Hit{Kind: HitSearch}
		}
		return Hit{Kind: HitNone}
	}
	// A thin grip at the preview pane's left edge starts a pane-resize drag (only
	// within the pane's vertical extent, not down in the download panel).
	if s.previewR.W > 0 && y < s.feedBottom() {
		grip := rpxOf(s, 7)
		if x >= s.previewR.X-grip && x <= s.previewR.X+grip {
			return Hit{Kind: HitPreviewDivider}
		}
	}
	// The right preview pane is drawn over the feed; a click inside it resolves
	// here (its Open button or nothing) and never falls through to a feed card.
	if h, ok := s.previewHitTest(x, y); ok {
		return h
	}
	// A thin grip at the sidebar's right edge starts a divider drag (feed only,
	// and only when the sidebar is shown).
	if m.sidebarW > 0 {
		grip := rpxOf(s, 7)
		if x >= m.sidebarW-grip && x <= m.sidebarW+grip {
			return Hit{Kind: HitSidebarDivider}
		}
	}
	if x < m.sidebarW {
		for _, t := range s.profTabs {
			if inRect(t.rect, x, y) {
				return Hit{Kind: HitProfile, Profile: t.index}
			}
		}
		if inRect(s.settingsR, x, y) {
			return Hit{Kind: HitSettings}
		}
		if inRect(s.logR, x, y) {
			return Hit{Kind: HitLog}
		}
		if inRect(s.accountsR, x, y) {
			return Hit{Kind: HitAccounts}
		}
		// The scrollable sub-list rows (subs + Browse) are only hittable inside the
		// band; a row scrolled under the tabs or the footer must not be clickable
		// through that chrome (which is drawn on top of it).
		inBand := func(r toolkit.Rect) bool {
			return r.Y >= s.sideBandTop && r.Y+m.sideItemH <= s.sideBandBot
		}
		if s.usenetAddr != "" && s.browseR.W > 0 && inBand(s.browseR) && inRect(s.browseR, x, y) {
			return Hit{Kind: HitBrowse}
		}
		for _, e := range s.subs {
			if inBand(e.rect) && inRect(e.rect, x, y) {
				return Hit{Kind: HitSub, Sub: e.index}
			}
		}
		return Hit{Kind: HitNone}
	}
	// Download panel (docked bottom, over the feed): the Clear button, else inert.
	if s.downloadPanelH() > 0 && y >= s.feedBottom() {
		s.layoutDownloadPanel()
		if inRect(s.dlClearR, x, y) {
			return Hit{Kind: HitClearDownloads}
		}
		return Hit{Kind: HitNone}
	}
	// Feed.
	feedX, feedW := s.feedGeom()
	cardW := s.feedCardW(feedW) // match the draw's scrollbar gutter
	contentY := y - m.topbarH + s.ScrollY
	for _, a := range s.authRows {
		if contentY >= a.top && contentY < a.top+m.bannerH {
			return Hit{Kind: HitFixAuth, Value: string(s.authPrompts[a.idx].Kind)}
		}
	}
	for _, r := range s.rows {
		if contentY < r.top || contentY >= r.top+r.height {
			continue
		}
		if r.group == nil {
			return Hit{Kind: HitItem, Item: r.item}
		}
		return s.hitGroup(r, feedX, cardW, x, contentY)
	}
	return Hit{Kind: HitNone}
}

// hitGroup resolves a click landing inside a group row (feed content coords):
// the Reconstruct affordance, the header (toggle expand/collapse), or — when
// expanded — one of the listed member parts (open its detail, like a card).
func (s *Scene) hitGroup(r feedRow, feedX, feedW, x, contentY int) Hit {
	g := r.group
	// A complete post offers a download checkbox (queue) and the Reconstruct pill,
	// both in the header's right slot; the checkbox takes precedence where drawn.
	if g.Complete() {
		if inRect(s.downloadCheckRect(feedX, r.top, feedW), x, contentY) {
			return Hit{Kind: HitToggleDownload, Value: g.Base}
		}
		if inRect(s.reconstructRect(feedX, r.top, feedW), x, contentY) {
			return Hit{Kind: HitReconstruct, Value: g.Base}
		}
	}
	if contentY < r.top+s.m.groupHeadH {
		// The chevron toggles expand/collapse; the rest of the header previews the
		// post (reconstructs its image into the pane), like clicking a card.
		if inRect(s.chevronRect(feedX, r.top), x, contentY) {
			return Hit{Kind: HitToggleGroup, Value: g.Base}
		}
		return Hit{Kind: HitPreviewGroup, Value: g.Base}
	}
	if s.GroupExpanded(g.Base) {
		for i, mem := range g.Members {
			if inRect(s.memberRect(feedX, r.top, feedW, i), x, contentY) {
				return Hit{Kind: HitItem, Item: mem.Item}
			}
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
	if it.Created > 0 {
		parts = append(parts, time.Unix(it.Created, 0).UTC().Format("2 Jan 2006 15:04"))
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
