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
	thumbH, badgeH            int
	sideItemH, searchH        int
	profileTabH, tabPad, btnH int
	bannerH, navIcon          int
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
	return m
}

// profTabHit maps a sidebar profile tab rect to its profile index.
type profTabHit struct {
	index int
	rect  toolkit.Rect
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

	// Sidebar middle list: the "All Sources" root, the virtual folders, the
	// subscriptions and the "Browse newsgroups" (Usenet only) / "Search Reddit"
	// discovery entries, all rendered by a toolkit.TreeView inside the band
	// between the tab strip and the pinned footer. The TreeView owns the scroll
	// window, the scrollbar gutter and hit-testing; buildSideTree derives its
	// nodes (folder grouping + selection) from the current model.
	sideTop := m.topbarH
	if len(s.Profiles) > 0 {
		sideTop += m.profileTabH
	}
	s.sideBandTop = sideTop
	s.sideBandBot = s.accountsR.Y // the pinned footer begins here
	band := s.sideBandBot - s.sideBandTop
	if band < 0 {
		band = 0
	}
	s.buildSideTree()
	s.sideTree.RowHeight = m.sideItemH
	// Bounds are sprite-local (the sidebar is blitted at (0, topbarH)): Draw uses
	// them, while NodeAt/OnEvent take band-local coordinates. H drives the scroll
	// window + gutter.
	s.sideTree.SetBounds(toolkit.Rect{X: 0, Y: s.sideBandTop - m.topbarH, W: m.sidebarW, H: band})

	// Burger button (left of the topbar) toggles the sidebar. Always present in
	// the feed view so a collapsed sidebar can be reopened.
	s.burgerR = toolkit.Rect{X: 0, Y: 0, W: m.topbarH, H: m.topbarH}

	// Refresh button (right of the topbar) re-fetches the feed — the visible,
	// discoverable twin of the pull-to-refresh overscroll gesture. A topbarH square
	// pinned to the right edge, matching the burger's cell on the left.
	s.refreshR = toolkit.Rect{X: s.W - m.topbarH, Y: 0, W: m.topbarH, H: m.topbarH}

	// Search field in the topbar, between the burger+title header and the refresh
	// button. The header reserves at least the sidebar width, so nothing overlaps
	// when collapsed.
	headerW := s.burgerR.W + m.pad + m.title.width("News") + m.pad
	if headerW < m.sidebarW {
		headerW = m.sidebarW
	}
	s.searchR = toolkit.Rect{X: headerW + m.pad, Y: (m.topbarH - m.searchH) / 2, W: s.refreshR.X - headerW - 2*m.pad, H: m.searchH}

	// Per-newsgroup post counts for the bottom status bar (computed here so the
	// feed geometry, which subtracts the bar, is consistent this frame).
	s.statusSegs = groupCountSegs(s.filtered())

	// Drive the feed: the virtual.CardList model + geometry are reconciled to the
	// current filter here (the feed's per-layout refresh point, which also places
	// the list rect below the pinned banners and opens at the bottom on a filter
	// change). The pinned sign-in banners are laid out in Draw / HitTest directly
	// from authPrompts.
	s.syncFeed()
}

// Draw paints the whole scene into buf (s.W*s.H*4 RGBA bytes).
func (s *Scene) Draw(buf []byte) {
	// A popped-up context menu is an overlay above every mode's own drawing, so
	// paint it last on whatever the mode below produced. Deferred so it runs on
	// each mode's early return, not just the feed path.
	defer s.drawContextMenuOverlay(buf)
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
	case ModeSearch:
		s.drawSearch(buf)
		return
	}
	s.layout()
	m := s.m
	p := painter.NewPixelPainter(buf, s.W, s.H)
	img := &image.RGBA{Pix: buf, Stride: s.W * 4, Rect: image.Rect(0, 0, s.W, s.H)}
	th := s.theme
	onAccent := themeOnAccent(th)
	muteS := mute(th.OnSurface, th.Surface)

	// Window ground: a solid toolkit.Backdrop, not a hand-drawn FillRect.
	ground := &toolkit.Backdrop{Fill: th.Background}
	ground.SetBounds(toolkit.Rect{X: 0, Y: 0, W: s.W, H: s.H})
	ground.Draw(p, th)

	// Begin this frame's cross-surface selectable-run accumulation. The sidebar,
	// the feed cards and the preview text each append their on-screen runs below;
	// commitSelectableRuns feeds the flat set to the selection once the whole feed
	// is painted, and the translucent over-highlight is drawn last.
	s.beginSelectableFrame()

	// Lay out the right-hand preview pane so the feed geometry (which subtracts the
	// pane width) and drawPreview agree this frame.
	s.layoutPreview()

	// --- feed (drawn first; chrome overpaints scroll overflow) ---
	feedTop := m.topbarH
	feedX, feedW := s.feedGeom()
	cardW := s.feedCardW(feedW) // narrower than feedW when the scrollbar shows
	// "Needs sign-in" banners, pinned above the feed list (they do not scroll with
	// the cards).
	for i, ap := range s.authPrompts {
		by := feedTop + i*(m.bannerH+m.cardGap)
		s.drawAuthBanner(p, img, ap, feedX, by, cardW, onAccent)
	}
	// The unified feed itself: the virtual.CardList, laid out (in syncFeed) in the
	// column below the banners. It realises only the visible cards, paints its own
	// selection ring + read veil, and clips to its bounds.
	lr := s.feed.list.Bounds()
	s.feed.list.Draw(p, th)
	// Scrollbar down the feed's right edge when the CardList content overflows its
	// viewport. When the preview pane is open, its divider carries a resize grip,
	// so position the bar the standard gap from that grip (else flush right edge).
	feedGrip := 0
	if s.previewR.W > 0 {
		feedGrip = s.previewR.X
	}
	s.drawVScrollbar(p, toolkit.Rect{X: feedX, Y: lr.Y, W: feedW, H: lr.H}, feedGrip, s.feedContentH(), s.FeedScrollOffset())
	if s.feed.model.Len() == 0 {
		if s.loading {
			// A refresh is running but nothing has arrived yet: show the animated
			// placeholder rather than a bare "No items." that looks broken.
			s.drawLoadingPlaceholder(p, img, feedX, cardW, muteS)
		} else {
			// Centred in the FEED pane, not in everything right of the sidebar.
			// The preview pane takes the right-hand share of that space, so
			// centring across the window put the label under the divider and the
			// chrome drawn afterwards cut it mid-word ("No it"). feedX/feedW is
			// the same geometry the cards and the scrollbar use.
			msg := "No items."
			mw := m.title.width(msg)
			lbl := toolkit.NewLabel(msg)
			lbl.Font, lbl.Ink = m.title.font, muteS
			lbl.SetBounds(toolkit.Rect{X: feedX + (feedW-mw)/2, Y: s.H / 2, W: mw, H: m.title.height})
			lbl.Draw(p, th)
		}
	}

	// --- chrome (cached sprites; static across scroll like Evas smart objects) ---
	if m.sidebarW > 0 {
		blitAt(img, s.sidebarSprite(), 0, m.topbarH)
		// A 1px divider at the sidebar's right edge plus a centred grab handle so
		// the resize affordance is visible and easy to hit. The divider is a
		// 1px-wide toolkit.Backdrop rather than a hand-drawn FillRect.
		div := &toolkit.Backdrop{Fill: th.Border}
		div.SetBounds(toolkit.Rect{X: m.sidebarW - 1, Y: m.topbarH, W: 1, H: s.H - m.topbarH})
		div.Draw(p, th)
		s.drawGripHandle(p, m.sidebarW)
		// The sidebar's scrollbar is drawn by the scene (HideScrollbar on the
		// TreeView) so it is the SAME slim rounded muted bar as the card list,
		// not the TreeView's built-in square/accent one. Rows → pixels via the
		// row height so drawVScrollbar's proportion math matches the feed's.
		if s.sourceOpen != "" && s.sourceSubTotal > s.sourceSubWindow {
			// The open section's accounts scroll within a bounded window; the bar
			// spans just that window and reflects the inner offset, so the pinned
			// headers around it carry no bar of their own.
			rh := m.sideItemH
			area := toolkit.Rect{X: 0, Y: s.sideBandTop + (s.sourceSubHeaderRow+1)*rh, W: m.sidebarW, H: s.sourceSubWindow * rh}
			s.drawVScrollbar(p, area, m.sidebarW, s.sourceSubTotal*rh, s.sourceSubScroll*rh)
		} else if off, _, total, shown := s.sideTree.ScrollExtent(); shown {
			rh := m.sideItemH
			area := toolkit.Rect{X: 0, Y: s.sideBandTop, W: m.sidebarW, H: s.sideBandBot - s.sideBandTop}
			s.drawVScrollbar(p, area, m.sidebarW, total*rh, off*rh)
		}
	}
	blitAt(img, s.topbarSprite(onAccent), 0, 0)

	// --- status footer text (optional) ---
	if s.Status != "" {
		status := toolkit.NewLabel(s.Status)
		status.Font, status.Ink = m.meta.font, muteS
		status.SetBounds(toolkit.Rect{X: m.sidebarW + m.pad, Y: s.H - m.meta.height - rpxOf(s, 4), W: m.meta.width(s.Status), H: m.meta.height})
		status.Draw(p, th)
	}

	// --- right preview/details pane (over the feed, docked right) ---
	s.drawPreview(p)

	// --- download-manager panel (docked bottom, over the feed/preview) ---
	s.drawDownloadPanel(p, img)

	// --- per-group post-count status bar (very bottom) ---
	s.drawStatusBar(p, img)

	// Commit the accumulated sidebar/card/preview runs as the selection's run set,
	// then paint ONE translucent over-highlight above the already-painted feed
	// (the cards and sidebar are pre-rasterised sprites, so the highlight must
	// blend over them rather than behind the glyphs like the detail view does).
	s.commitSelectableRuns()
	s.drawSelectionOverHighlight(p)
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
	sideScroll int // TreeView top-row scroll index
	secScroll  int // open section's inner account-window offset
	fold       int // folder/collapse revision (folder set + expand state)
}
type topbarKey struct {
	w        int
	sidebarW int
	scale    float64
	theme    *toolkit.Theme
	search   string
	focused  bool
	copied   bool // a select-all highlight over the search text after a copy
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
	k := sidebarKey{h: h, sub: m.sidebarW, scale: s.Scale, theme: th, active: s.Active, activeP: s.activeProf, subsRev: s.subsRev, profRev: s.profRev, pendRev: s.pendRev, anim: anim, sideScroll: s.sideTree.ScrollRow().Get(), secScroll: s.sourceSubScroll, fold: s.foldRev}
	if s.sidebarSpr != nil && s.sidebarKey == k {
		return s.sidebarSpr
	}
	onAccent := themeOnAccent(th)
	buf := make([]byte, m.sidebarW*h*4)
	p := painter.NewPixelPainter(buf, m.sidebarW, h)
	img := &image.RGBA{Pix: buf, Stride: m.sidebarW * 4, Rect: image.Rect(0, 0, m.sidebarW, h)}
	// Sidebar ground: a solid SurfaceAlt toolkit.Backdrop.
	ground := &toolkit.Backdrop{Fill: th.SurfaceAlt}
	ground.SetBounds(toolkit.Rect{X: 0, Y: 0, W: m.sidebarW, H: h})
	ground.Draw(p, th)

	// Profile tabs (sidebar-local coords): the active tab's pill is a rounded
	// toolkit.Backdrop; each tab name is a toolkit.Label.
	for _, t := range s.profTabs {
		ly := t.rect.Y - m.topbarH
		col := th.OnSurface
		if t.index == s.activeProf {
			pill := &toolkit.Backdrop{Fill: th.Accent, Radius: rpxOf(s, 5)}
			pill.SetBounds(toolkit.Rect{X: t.rect.X, Y: ly, W: t.rect.W, H: t.rect.H})
			pill.Draw(p, th)
			col = onAccent
		}
		name := s.Profiles[t.index].Name
		nameLbl := toolkit.NewLabel(name)
		nameLbl.Font, nameLbl.Ink = m.tab.font, col
		nameLbl.SetBounds(toolkit.Rect{X: t.rect.X + m.tabPad, Y: ly + (t.rect.H-m.tab.height)/2, W: m.tab.width(name), H: m.tab.height})
		nameLbl.Draw(p, th)
	}

	// The scrollable middle list is a toolkit.TreeView (its bounds were set in
	// layout to sprite-local coordinates): it paints the "All Sources" root, the
	// folders, the subscriptions and the discovery entries through drawSideRow,
	// clips them to the band, and draws its own scrollbar gutter when the list
	// overflows. Its selection background (theme.Accent) marks the active filter.
	// The TreeView fills unselected rows with theme.Surface; the reader's sidebar
	// is SurfaceAlt, so it draws with a theme copy whose Surface is SurfaceAlt to
	// keep the list flush with the surrounding sidebar (the accent selection fill
	// and the Background-on-accent ink are unaffected).
	sideTheme := *th
	sideTheme.Surface = th.SurfaceAlt
	s.sideTree.Draw(p, &sideTheme)

	// Pinned entries at the bottom: Accounts, Network log, Settings. Each icon is
	// drawn (not a font glyph) so nothing renders as a tofu box.
	navCol := mute(th.OnSurface, th.SurfaceAlt)
	navTextX := m.pad + m.navIcon + rpxOf(s, 6)
	drawNavRow := func(localY int, icon func(painter.Painter, toolkit.Rect, toolkit.RGBA, int), text string) {
		// 1px top divider as a toolkit.Backdrop; the icon stays a painter glyph
		// (the sanctioned icon layer); the label is a toolkit.Label.
		rowDiv := &toolkit.Backdrop{Fill: th.Border}
		rowDiv.SetBounds(toolkit.Rect{X: 0, Y: localY - 1, W: m.sidebarW, H: 1})
		rowDiv.Draw(p, th)
		ir := toolkit.Rect{X: m.pad, Y: localY + (m.sideItemH-m.navIcon)/2, W: m.navIcon, H: m.navIcon}
		icon(p, ir, navCol, s.iconStroke())
		navLbl := toolkit.NewLabel(text)
		navLbl.Font, navLbl.Ink = m.side.font, navCol
		navLbl.SetBounds(toolkit.Rect{X: navTextX, Y: localY + (m.sideItemH-m.side.height)/2, W: m.side.width(text), H: m.side.height})
		navLbl.Draw(p, th)
	}
	drawNavRow(s.accountsR.Y-m.topbarH, drawUserIcon, "Accounts")
	drawNavRow(s.logR.Y-m.topbarH, drawListIcon, "Network log")
	drawNavRow(s.settingsR.Y-m.topbarH, drawSlidersIcon, "Settings")

	s.sidebarKey, s.sidebarSpr = k, img
	return img
}

// topbarSprite renders (or reuses) the topbar (accent fill + title + search).
func (s *Scene) topbarSprite(onAccent toolkit.RGBA) *image.RGBA {
	m := s.m
	th := s.theme
	k := topbarKey{w: s.W, sidebarW: m.sidebarW, scale: s.Scale, theme: th, search: s.searchEntry.Text().Get(), focused: s.searchFocused, copied: s.searchCopied}
	if s.topbarSpr != nil && s.topbarKey == k {
		return s.topbarSpr
	}
	buf := make([]byte, s.W*m.topbarH*4)
	p := painter.NewPixelPainter(buf, s.W, m.topbarH)
	img := &image.RGBA{Pix: buf, Stride: s.W * 4, Rect: image.Rect(0, 0, s.W, m.topbarH)}
	// Accent band as a toolkit.Backdrop.
	band := &toolkit.Backdrop{Fill: th.Accent}
	band.SetBounds(toolkit.Rect{X: 0, Y: 0, W: s.W, H: m.topbarH})
	band.Draw(p, th)
	// Burger button at the left, then the title. The burger is a drawn menu icon
	// (three bars) rather than a font glyph, and is always drawn so a collapsed
	// sidebar can be reopened. The "News" title is a toolkit.Label (on-accent).
	ic := m.navIcon
	drawMenuIcon(p, toolkit.Rect{X: s.burgerR.X + (s.burgerR.W-ic)/2, Y: (m.topbarH - ic) / 2, W: ic, H: ic}, onAccent, s.iconStroke())
	titleLbl := toolkit.NewLabel("News")
	titleLbl.Font, titleLbl.Ink = m.title.font, onAccent
	titleLbl.SetBounds(toolkit.Rect{X: s.burgerR.W + m.pad, Y: (m.topbarH - m.title.height) / 2, W: m.title.width("News"), H: m.title.height})
	titleLbl.Draw(p, th)
	// Search box: render the topbar's toolkit.SearchEntry widget itself (its own
	// AA font via ttFont), so what the user sees is the bound widget's text. The
	// widget draws its own aligned caret and — via FocusRingRadius/Width — its own
	// rounded accent focus ring. (topbar is full-width at y=0, so local == absolute
	// coords.)
	se := s.searchEntry
	se.SetBounds(toolkit.Rect(s.searchR))
	se.Font = ttFont(false, rpxOf(s, 13))
	se.FocusRingRadius = rpxOf(s, 6)
	se.FocusRingWidth = rpxOf(s, 2)
	se.SetFocused(s.searchFocused)
	se.Draw(p, th)
	// After a copy, paint a translucent select-all highlight OVER the query text
	// (visual feedback of what went to the clipboard). SearchEntry.Draw fills its
	// own background, so the highlight must go on top — a translucent accent lets
	// the glyphs show through (a real selection look). The text origin mirrors
	// SearchEntry.Draw exactly (PadX + the icon slot) so it lines up.
	if s.searchCopied && s.searchEntry.Text().Get() != "" {
		f := se.Font
		tx := s.searchR.X + toolkit.SearchEntryPadX + toolkit.SearchEntryIconW
		hl := th.Accent
		hl.A = 0x55
		over := &toolkit.Backdrop{Fill: hl}
		over.SetBounds(toolkit.Rect{X: tx, Y: s.searchR.Y + (s.searchR.H-f.Height())/2, W: f.Measure(s.searchEntry.Text().Get()), H: f.Height()})
		over.Draw(p, th)
	}
	// Refresh button at the right, drawn as the Iconoir "refresh-double" glyph at
	// the same visual weight as the burger, so the topbar reads as burger | search |
	// refresh.
	drawRefreshIcon(p, toolkit.Rect{X: s.refreshR.X + (s.refreshR.W-ic)/2, Y: (m.topbarH - ic) / 2, W: ic, H: ic}, onAccent, s.iconStroke())
	s.topbarKey, s.topbarSpr = k, img
	return img
}

// cardText is the headline a feed card renders. Most sources title every post,
// but the social ones — Twitter/X, Mastodon, Bluesky — have no title at all and
// carry the entire post in Body, so a Title-only card rendered *blank* for them.
// Falling back to the body makes those posts legible. The body is HTML-stripped
// and collapsed to one flowing paragraph, because a card is a summary, not the
// reading view: the blank lines a post embeds would otherwise burn the card's
// few lines on whitespace.
func cardText(it source.Item) string {
	if t := strings.TrimSpace(it.Title); t != "" {
		return t
	}
	return collapseSpace(stripHTML(it.Body))
}

// collapseSpace flattens every run of whitespace — including the newlines a
// tweet or toot embeds — into single spaces.
func collapseSpace(s string) string { return strings.Join(strings.Fields(s), " ") }

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
// it ("<unseen>/<total>"). Both are toolkit Labels (via ttFont). On the selected
// TreeView row (accent fill) both use the row ink instead, so the numbers stay
// legible against the accent background.
type countChip struct {
	toolkit.Base
	s             *Scene
	unseen, total int
	selected      bool
	ink           toolkit.RGBA
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
	totalCol := mute(th.OnSurface, th.SurfaceAlt)
	unseenCol := th.Accent
	if c.selected {
		totalCol, unseenCol = c.ink, c.ink
	}
	totalStr := strconv.Itoa(c.total)
	tw := f.Measure(totalStr)
	tot := toolkit.NewLabel(totalStr)
	tot.Font, tot.Ink = f, totalCol
	tot.SetBounds(toolkit.Rect{X: b.X + b.W - tw, Y: b.Y, W: tw, H: b.H})
	tot.Draw(p, th)
	if c.unseen > 0 {
		us := strconv.Itoa(c.unseen) + "/"
		uw := f.Measure(us)
		un := toolkit.NewLabel(us)
		un.Font, un.Ink = f, unseenCol
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
		Icon: func(ip painter.Painter, r toolkit.Rect, ink toolkit.RGBA) {
			drawLockIcon(ip, r, ink, s.iconStroke())
		},
	}
	bn.Revealed().Set(true)
	bn.SetBounds(toolkit.Rect{X: x, Y: y, W: w, H: m.bannerH})
	bn.Draw(p, s.theme)

	// Mirror the Banner's leading-icon geometry to place the label after it.
	iconD := m.bannerH - 2*toolkit.BannerPadY
	tx := x + toolkit.BannerPadX + iconD + toolkit.BannerPadX/2
	ty := y + (m.bannerH-m.side.height)/2
	lbl := sourceLabel(ap.Kind) + " needs sign-in — Open Accounts"
	text := truncate(m.side, lbl, x+w-tx-m.pad)
	msg := toolkit.NewLabel(text)
	msg.Font, msg.Ink = m.side.font, onAccent
	msg.SetBounds(toolkit.Rect{X: tx, Y: ty, W: m.side.width(text), H: m.side.height})
	msg.Draw(p, s.theme)
}

// sameItem reports whether two items are the same post. it.ID is only unique
// within a Source (and empty for some feeds), so identity is Source+ID+headline.
// The headline is cardText, not Title, so untitled posts (tweets, toots) stay
// distinguishable.
func sameItem(a, b source.Item) bool {
	return a.Source == b.Source && a.ID == b.ID && cardText(a) == cardText(b)
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

// drawGripHandle paints a short, centred vertical pill on a resizable divider at
// column cx, so the drag affordance is visible and easy to grab.
func (s *Scene) drawGripHandle(p *painter.PixelPainter, cx int) {
	m := s.m
	gw := rpxOf(s, 4)
	gh := rpxOf(s, 36)
	gy := m.topbarH + (s.H-m.topbarH-gh)/2
	grip := &toolkit.Backdrop{Fill: mute(s.theme.OnSurface, s.theme.Surface), Radius: gw / 2}
	grip.SetBounds(toolkit.Rect{X: cx - gw/2, Y: gy, W: gw, H: gh})
	grip.Draw(p, s.theme)
}

// drawDot paints a small filled circle-ish marker (a rounded square) for a
// source colour in the sidebar.
func (s *Scene) drawDot(p *painter.PixelPainter, x, cy int, col toolkit.RGBA) {
	d := rpxOf(s, 8)
	dot := &toolkit.Backdrop{Fill: col, Radius: d / 2}
	dot.SetBounds(toolkit.Rect{X: x, Y: cy - d/2, W: d, H: d})
	dot.Draw(p, s.theme)
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
	case ModeSearch:
		return s.searchHitTest(x, y)
	}
	s.layout()
	s.layoutPreview()
	m := s.m
	if y < m.topbarH {
		if inRect(s.burgerR, x, y) {
			return Hit{Kind: HitBurger}
		}
		if inRect(s.refreshR, x, y) {
			return Hit{Kind: HitRefresh}
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
		// The scrollable middle list is the TreeView: map the click to the node
		// under it (band-local coordinates). A row scrolled out of the band, or a
		// click on empty space, resolves to no node.
		if y >= s.sideBandTop && y < s.sideBandBot {
			if node := s.sideTree.NodeAt(x, y-s.sideBandTop); node != nil {
				return s.sideNodeHit(node)
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
	// Feed. The pinned "needs sign-in" banners sit above the CardList, one per
	// prompt; a click on one opens the Accounts editor for that provider.
	feedX, feedW := s.feedGeom()
	cardW := s.feedCardW(feedW)
	for i := range s.authPrompts {
		by := m.topbarH + i*(m.bannerH+m.cardGap)
		if inRect(toolkit.Rect{X: feedX, Y: by, W: cardW, H: m.bannerH}, x, y) {
			return Hit{Kind: HitFixAuth, Value: string(s.authPrompts[i].Kind)}
		}
	}
	// A Usenet group card's affordances resolve first: the disclosure chevron
	// (toggle expand/collapse) and — when the post is complete — the download
	// checkbox and the "Reconstruct" pill. These act immediately (they are not
	// text-selectable), so they never defer to the CardList selection.
	if h, ok := s.feedGroupHit(x, y); ok {
		return h
	}
	// A card under the pointer resolves to HitItem (open it in the preview pane).
	// The selection itself is driven through the CardList (FeedClickAt), so a
	// standalone card — or a group card's header/body — previews like any other;
	// a click on an expanded group's member row previews that part (FeedClickAt).
	if it, ok := s.feedItemAtPoint(x, y); ok {
		return Hit{Kind: HitItem, Item: it}
	}
	return Hit{Kind: HitNone}
}

// sideNodeHit maps a clicked sidebar TreeView node to a scene Hit: the "All
// Sources" root and the subscriptions filter the feed (HitSub), the discovery
// entries open their views, and a folder row toggles its collapse state.
func (s *Scene) sideNodeHit(node *toolkit.TreeNode) Hit {
	switch d := sideData(node); d.Kind {
	case sideAll:
		return Hit{Kind: HitSub, Sub: AllFilter}
	case sideSub:
		return Hit{Kind: HitSub, Sub: d.Sub}
	case sideBrowse:
		return Hit{Kind: HitBrowse}
	case sideSearchReddit:
		return Hit{Kind: HitSearchReddit}
	case sideFolder:
		return Hit{Kind: HitToggleFolder, Value: d.Folder}
	case sideSource:
		return Hit{Kind: HitToggleSource, Value: string(d.Source)}
	default:
		return Hit{Kind: HitNone}
	}
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
	case source.Redgifs:
		return "RedGIFs"
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
	case source.Redgifs:
		return rgb(0xFF2E63)
	default:
		return rgb(0x888888)
	}
}
