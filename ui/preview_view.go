package ui

// The right-hand preview/details pane (feed view). It is a docked, always-visible
// column on the right that reflects the item last clicked in the feed: source
// badge, title, meta, an image (whatever has been decoded into Thumbs — e.g. a
// Usenet binary the app reconstructed) and the body text, with an "Open" button
// for the full-screen reading view. The feed list narrows to make room; on a
// window too narrow to keep a usable feed the pane hides itself (previewWidth 0).

import (
	"image"
	"strings"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"

	"github.com/go-news-reader/reader/source"
)

// PrefetchRequest names a shown Usenet image post to prefetch: an id (the feed
// item id or a group's release base) and the member articles to fetch.
type PrefetchRequest struct {
	ID    string
	Parts []ReconstructPart
}

// ImagePrefetch returns a request for every shown Usenet post likely to carry an
// image — a multipart group (binary post) and a standalone article whose subject
// names an image file — so the app can download their thumbnails in parallel.
func (s *Scene) ImagePrefetch() []PrefetchRequest {
	var out []PrefetchRequest
	for _, e := range groupItems(s.filtered()) {
		switch {
		case e.group != nil:
			parts := make([]ReconstructPart, 0, len(e.group.Members))
			for _, mem := range e.group.Members {
				if id := bareMessageID(mem.Item.Permalink); id != "" {
					parts = append(parts, ReconstructPart{MessageID: id, Filename: mem.Info.Filename})
				}
			}
			if len(parts) > 0 {
				out = append(out, PrefetchRequest{ID: e.group.Base, Parts: parts})
			}
		case e.item.Source == source.Usenet && isImageSubject(e.item.Title):
			if id := bareMessageID(e.item.Permalink); id != "" {
				out = append(out, PrefetchRequest{ID: e.item.ID, Parts: []ReconstructPart{{MessageID: id, Filename: e.item.Title}}})
			}
		}
	}
	return out
}

// bareMessageID strips the "news:" scheme and angle brackets off a permalink.
func bareMessageID(permalink string) string {
	return strings.Trim(strings.TrimPrefix(permalink, "news:"), "<>")
}

// isImageSubject reports whether a subject/filename names a common image format.
func isImageSubject(s string) bool {
	s = strings.ToLower(s)
	for _, ext := range []string{".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp"} {
		if strings.Contains(s, ext) {
			return true
		}
	}
	return false
}

// Preview-pane sizing, in logical (unscaled) pixels.
const (
	previewPaneW = 360 // preferred pane width
	previewMinW  = 260 // below this the pane is not worth showing
	feedKeepW    = 300 // feed width to preserve before the pane may claim space
)

// previewWidth is the pane's device-pixel width for the current window, or 0 when
// it is hidden (not the feed view, or the window is too narrow to keep both a
// usable feed and a usable pane).
func (s *Scene) previewWidth() int {
	if s.mode != ModeFeed {
		return 0
	}
	lo := rpxOf(s, previewMinW)
	avail := s.W - s.m.sidebarW - rpxOf(s, feedKeepW)
	if avail < lo {
		return 0 // window too narrow to keep both a usable feed and pane
	}
	w := rpxOf(s, previewPaneW)
	if s.previewUserW > 0 {
		w = s.previewUserW // user-dragged width pins the pane, clamped below
	} else if grow := avail * 2 / 5; grow > w {
		// With no explicit drag, the default pane tracks the window: it claims up
		// to ~2/5 of the space a pane may use, so the preview (image included)
		// grows when the window grows instead of only shrinking. It never drops
		// below the preferred width.
		w = grow
	}
	if w > avail {
		w = avail
	}
	if w < lo {
		w = lo
	}
	return w
}

// BeginPreviewResize / EndPreviewResize / DraggingPreview drive the preview
// pane's left-edge divider drag, mirroring the sidebar divider.
func (s *Scene) BeginPreviewResize()   { s.draggingPreview = true }
func (s *Scene) EndPreviewResize()     { s.draggingPreview = false }
func (s *Scene) DraggingPreview() bool { return s.draggingPreview }

// feedGeom returns the feed list's left origin and width, accounting for the
// sidebar on the left and the preview pane on the right. Every feed layout /
// draw / hit-test path goes through it so the three stay consistent.
func (s *Scene) feedGeom() (x, w int) {
	m := s.m
	x = m.sidebarW + m.pad
	// previewWidth reserves space only while the feed keeps at least feedKeepW, and
	// sidebarWidthPx is capped at half the window, so w stays positive.
	w = s.W - m.sidebarW - s.previewWidth() - 2*m.pad
	return x, w
}

// feedScrollbarShown reports whether the feed's vertical scrollbar is visible
// (its content overflows the viewport between the topbar and the download panel).
func (s *Scene) feedScrollbarShown() bool {
	return s.feedScroll.contentH > s.feedBottom()-s.m.topbarH
}

// feedCardW is the feed content width for cards/banners: when the scrollbar is
// shown it reserves a gutter so cards stop before the bar's left edge instead of
// sitting under it. It derives the bar's left edge from the same placement rule
// drawVScrollbar uses (scrollbarRightX with the same grip), so cards and bar stay
// aligned however the bar is positioned. Draw and hit-test both go through it.
func (s *Scene) feedCardW(feedW int) int {
	// gripX in feed-relative coords: when the preview pane is open its divider grip
	// sits at the feed's right edge (feedW); otherwise there is no grip (0). The
	// gutter comes from the shared scrollClampRight so it matches every other panel.
	gripX := 0
	if s.previewR.W > 0 {
		gripX = feedW
	}
	return s.scrollClampRight(feedW, feedW, gripX, s.feedScrollbarShown())
}

// SelectPreview loads it into the preview pane (clicking a feed item), resetting
// the pane scroll. The app calls SetPreviewLoading before an async image fetch.
func (s *Scene) SelectPreview(it source.Item) {
	s.previewItem = it
	s.previewHas = true
	s.previewScroll.offset = 0
	s.previewImgPending = false
	s.browserFocused = false // a new selection drops browser keyboard focus
	s.touch()
}

// PreviewItem returns the previewed item and whether one is selected.
func (s *Scene) PreviewItem() (source.Item, bool) { return s.previewItem, s.previewHas }

// PreviewOpenButton returns the preview pane's "Open" button rect and whether it
// is currently shown (an item is selected and the pane is visible). It reflects
// the last layout; front-ends/tests call it after a Draw/HitTest.
func (s *Scene) PreviewOpenButton() (toolkit.Rect, bool) {
	return s.previewOpenR, s.previewOpenR.W > 0
}

// GroupPreviewItem synthesises a source.Item to preview a Usenet multipart post
// (a group card, which is not itself a single Item): its release base as title,
// a declared image attachment so the pane reserves the image box, and the id set
// to the base so the app can key the reconstructed thumbnail by it. Reports false
// when no shown group has that base.
func (s *Scene) GroupPreviewItem(base string) (source.Item, bool) {
	for _, e := range groupItems(s.filtered()) {
		if e.group == nil || e.group.Base != base {
			continue
		}
		g := e.group
		it := source.Item{
			ID: base, Source: source.Usenet, Title: base,
			Body:  groupMeta(g),
			Media: []source.Media{{Kind: source.MediaImage}},
		}
		if len(g.Members) > 0 {
			it.Channel = g.Members[0].Item.Channel
		}
		return it, true
	}
	return source.Item{}, false
}

// SetPreviewLoading marks (or clears) that an image fetch is in flight for the
// previewed item, so the pane shows a spinner until SetThumb lands.
func (s *Scene) SetPreviewLoading(v bool) { s.previewImgPending = v; s.touch() }

// webPreviewItem reports whether the current selection is a web-linked item
// whose target renders in the embedded browser (rather than as a Usenet image
// or a plain-text summary). The preview pane shows the mini-browser for these.
func (s *Scene) webPreviewItem() bool {
	return s.previewHas && webPreviewURL(s.previewItem) != ""
}

// WebPreviewActive reports whether the embedded browser is the active preview
// content — a web-linked item is selected. Front-ends check it before routing a
// raw mouse/scroll/key event into the browser widget.
func (s *Scene) WebPreviewActive() bool { return s.webPreviewItem() }

// ForwardBrowserClick forwards a click at screen coords (x, y) into the embedded
// browser when the web preview is active and the point lands inside the widget,
// translating to widget-local coordinates. It returns true when the browser
// consumed the click (so the caller does no further routing). A click outside
// the widget (or when the browser is not shown) blurs the browser's keyboard
// focus and returns false so the caller falls through to its own hit-testing.
func (s *Scene) ForwardBrowserClick(x, y int) bool {
	if !s.webPreviewItem() {
		return false
	}
	b := s.browser.Bounds()
	if b.W <= 0 || !inRect(b, x, y) {
		s.setBrowserFocused(false)
		return false
	}
	s.browser.OnEvent(toolkit.Event{Kind: toolkit.EventClick, X: x - b.X, Y: y - b.Y})
	s.setBrowserFocused(true)
	s.touch()
	return true
}

// browserWheelStep is how many device pixels of wheel travel map to one browser
// content row (the widget scrolls BrowserScrollStep px per row).
const browserWheelStep = 20

// ForwardBrowserScroll forwards a wheel delta (device px) into the embedded
// browser when the web preview is active and the pointer is over the widget,
// converting pixels to the widget's row-based scroll intent. Returns true when
// the browser consumed the wheel.
func (s *Scene) ForwardBrowserScroll(dy int) bool {
	if !s.webPreviewItem() {
		return false
	}
	b := s.browser.Bounds()
	if b.W <= 0 || !inRect(b, s.lastMouseX, s.lastMouseY) {
		return false
	}
	rows := dy / browserWheelStep
	if rows == 0 {
		switch {
		case dy > 0:
			rows = 1
		case dy < 0:
			rows = -1
		default:
			return false
		}
	}
	s.browser.OnEvent(toolkit.Event{Kind: toolkit.EventScroll, X: s.lastMouseX - b.X, Y: s.lastMouseY - b.Y, Delta: rows})
	s.touch()
	return true
}

// ForwardBrowserKey forwards an editing key or printable rune into the embedded
// browser's address field when the web preview is active and the browser holds
// keyboard focus (a prior click landed inside it). Enter commits/navigates the
// address, Backspace edits it, and a printable rune types into it. Returns true
// when the key was routed to the browser.
func (s *Scene) ForwardBrowserKey(name string, r rune) bool {
	if !s.webPreviewItem() || !s.browserFocused {
		return false
	}
	switch name {
	case "Backspace":
		s.browser.OnEvent(toolkit.Event{Kind: toolkit.EventKeyDown, Code: "Backspace"})
	case "Enter":
		s.browser.OnEvent(toolkit.Event{Kind: toolkit.EventKeyDown, Code: "Enter"})
	default:
		if r == 0 {
			return false
		}
		s.browser.OnEvent(toolkit.Event{Kind: toolkit.EventChar, Code: string(r)})
	}
	s.touch()
	return true
}

// setBrowserFocused records (and repaints on change) whether the embedded
// browser holds keyboard focus.
func (s *Scene) setBrowserFocused(v bool) {
	if s.browserFocused != v {
		s.browserFocused = v
		s.touch()
	}
}

// BrowserFocused reports whether the embedded browser holds keyboard focus.
func (s *Scene) BrowserFocused() bool { return s.browserFocused }

// WebPreviewURL returns the external http(s) target page to render for it, or ""
// when there is none or the source isn't web-renderable. Usenet posts preview as
// reconstructed images, not web pages, so they never return a URL here.
func (s *Scene) WebPreviewURL(it source.Item) string { return webPreviewURL(it) }

func webPreviewURL(it source.Item) string {
	if it.Source == source.Usenet {
		return ""
	}
	u := it.Link
	if u == "" {
		u = it.Permalink
	}
	if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
		return u
	}
	return ""
}

// HasThumb reports whether a decoded image is cached for the item id.
func (s *Scene) HasThumb(id string) bool { return s.hasThumb(id) }

// FinishPreviewImage stores the reconstructed image (when non-nil) and clears the
// pane's loading state if the fetch was for the item still being previewed. The
// image is keyed by id, so a late result for a since-deselected item is cached
// (for its card thumbnail) without disturbing the current selection.
func (s *Scene) FinishPreviewImage(id string, img *image.RGBA) {
	if img != nil {
		s.SetThumb(id, img)
	}
	if s.previewHas && s.previewItem.ID == id {
		s.previewImgPending = false
	}
	s.touch()
}

// previewBody is the laid-out (wrapped/measured) preview content.
type previewBody struct {
	innerX, innerW      int
	titleFace, bodyFace textFace
	titleLines          []string
	bodyLines           []string
	meta                string
	imgH                int // height reserved for the image box (0 when none)
	height              int
}

// previewInner is the content column inside the pane (pane minus padding).
func (s *Scene) previewInner() (x, w int) {
	m := s.m
	return s.previewR.X + m.pad, s.previewR.W - 2*m.pad
}

// previewContent wraps and measures the pane body and reserves the image box.
func (s *Scene) previewContent() previewBody {
	m := s.m
	x, w := s.previewInner()
	titleFace := getFace(rpxOf(s, 17), true)
	bodyFace := getFace(rpxOf(s, 14), false)
	it := s.previewItem
	d := previewBody{
		innerX: x, innerW: w, titleFace: titleFace, bodyFace: bodyFace,
		titleLines: wrapText(titleFace, it.Title, w),
		bodyLines:  wrapText(bodyFace, stripHTML(it.Body), w),
		meta:       metaLine(it),
	}
	gap := rpxOf(s, 8)
	// Height consumed by the header above the image (badge + title + meta).
	headerH := m.pad + m.badgeH + gap
	headerH += len(d.titleLines) * (titleFace.height + rpxOf(s, 2))
	headerH += gap + m.meta.height + gap
	// Reserve the image box whenever the item declares media (or one is already
	// decoded). Once decoded, the image grows to fill the LARGEST space available
	// in the pane — bound by the full remaining height OR the pane width, whichever
	// the aspect ratio reaches first (toolkit.FitBounds) — so a portrait image
	// fills the height and a landscape fills the width, each as big as it fits.
	// Before it loads, a placeholder box hosts the spinner / label.
	switch {
	case len(it.Media) > 0 || s.hasThumb(it.ID):
		if t := s.thumb(it.ID); t != nil {
			availH := s.previewR.H - headerH - gap - m.pad
			if lo := rpxOf(s, 160); availH < lo {
				availH = lo // keep a usable box in a very short pane
			}
			b := t.Bounds()
			fit := toolkit.FitBounds(b.Dx(), b.Dy(), toolkit.Rect{W: w, H: availH})
			d.imgH = fit.H
		} else {
			d.imgH = rpxOf(s, 160)
		}
	}
	h := headerH
	if d.imgH > 0 {
		h += d.imgH + gap
	}
	h += len(d.bodyLines) * (bodyFace.height + rpxOf(s, 3))
	h += m.pad
	d.height = h
	return d
}

// layoutPreview computes the pane rect, its Open button, image rect and the
// scrollable content height. It is a no-op (empty previewR) when the pane hides.
func (s *Scene) layoutPreview() {
	m := s.m
	pw := s.previewWidth()
	if pw == 0 {
		s.previewR = toolkit.Rect{}
		s.previewOpenR = toolkit.Rect{}
		s.previewImgR = toolkit.Rect{}
		s.browser.SetBounds(toolkit.Rect{})
		s.previewScroll.contentH = 0
		return
	}
	s.previewR = toolkit.Rect{X: s.W - pw, Y: m.topbarH, W: pw, H: s.feedBottom() - m.topbarH}
	// "Open" pill, top-right of the pane (fixed, over the content), shown only when
	// an item is selected and it has a full-view target.
	if s.previewHas {
		ow := m.pad*2 + m.side.width("Open")
		s.previewOpenR = toolkit.Rect{X: s.previewR.X + s.previewR.W - m.pad - ow, Y: m.topbarH + m.pad/2, W: ow, H: m.badgeH + rpxOf(s, 4)}
	} else {
		s.previewOpenR = toolkit.Rect{}
	}
	if !s.previewHas {
		s.previewScroll.contentH = 0
		s.previewImgR = toolkit.Rect{}
		s.browser.SetBounds(toolkit.Rect{})
		return
	}
	if s.webPreviewItem() {
		// A web-linked item: a fixed header (badge/title/meta) sits at the top and
		// the embedded browser fills the remaining pane. The browser owns its own
		// chrome, scroll and progress bar, so the scrolling column is not used.
		s.browser.SetFont(ttFont(false, rpxOf(s, 13)))
		hh := s.previewHeaderH()
		// Inset the browser from the pane's left edge by a gutter so the resize
		// grip stays visible on the pane surface — otherwise the browser's own
		// (often white) page background paints right up to the divider and hides it.
		gut := rpxOf(s, 8)
		// max guards the rare case where a tall download panel shrinks the pane
		// below the header, which would otherwise hand SetBounds a negative height.
		br := toolkit.Rect{X: s.previewR.X + gut, Y: s.previewR.Y + hh, W: s.previewR.W - gut, H: max(0, s.previewR.H-hh)}
		s.browser.SetBounds(br)
		s.previewImgR = toolkit.Rect{}
		s.previewScroll.contentH = 0
		return
	}
	s.browser.SetBounds(toolkit.Rect{})
	d := s.previewContent()
	s.previewScroll.refresh(d.height, s.previewR.H)
}

// previewHeaderLines are the (wrapped, at most two) title lines shown in the
// fixed web-preview header.
func (s *Scene) previewHeaderLines() []string {
	_, w := s.previewInner()
	lines := wrapText(getFace(rpxOf(s, 17), true), s.previewItem.Title, w)
	if len(lines) > 2 {
		lines = lines[:2]
	}
	return lines
}

// previewHeaderH is the height of the fixed web-preview header (pad + badge +
// title lines + meta), above which the embedded browser is placed.
func (s *Scene) previewHeaderH() int {
	m := s.m
	gap := rpxOf(s, 8)
	titleFace := getFace(rpxOf(s, 17), true)
	h := m.pad + m.badgeH + gap
	h += len(s.previewHeaderLines()) * (titleFace.height + rpxOf(s, 2))
	h += gap + m.meta.height + gap
	return h
}

// drawPreview paints the pane: its surface, left divider, and either the empty
// prompt or the selected item's badge/title/meta/image/body plus the Open button.
// previewImage is the preview pane's image cell as a toolkit widget so the box
// layout positions it: it records its rect (for the click-to-open hit-test) and
// draws the decoded thumbnail, a spinner while it loads, or a kind placeholder.
type previewImage struct {
	toolkit.Base
	s   *Scene
	it  source.Item
	p   *painter.PixelPainter
	img *image.RGBA
}

func (w *previewImage) Draw(_ painter.Painter, th *toolkit.Theme) {
	s, b := w.s, w.Bounds()
	s.previewImgR = b
	w.p.FillRect(painter.Rect(b), th.SurfaceAlt)
	switch {
	case s.thumb(w.it.ID) != nil:
		s.drawPreviewImage(w.p, th, s.thumb(w.it.ID), b)
	case s.previewImgPending:
		// Clear, centred loading placeholder: a label above a sizable spinner, so a
		// slow reconstruct reads as "loading", not a frozen blank pane.
		msg := "Loading preview…"
		lw := s.m.meta.width(msg)
		d := rpxOf(s, 44)
		gap := rpxOf(s, 12)
		blockH := s.m.meta.height + gap + d
		top := b.Y + (b.H-blockH)/2
		s.m.meta.draw(w.img, b.X+(b.W-lw)/2, top, msg, mute(th.OnSurface, th.SurfaceAlt))
		s.spinnerAt(toolkit.Rect{X: b.X + (b.W-d)/2, Y: top + s.m.meta.height + gap, W: d, H: d}, toolkit.SpinnerDots).Draw(w.p, th)
	default:
		lbl := "image"
		if len(w.it.Media) > 0 {
			lbl = string(w.it.Media[0].Kind)
		}
		s.m.meta.draw(w.img, b.X+(b.W-s.m.meta.width(lbl))/2, b.Y+(b.H-s.m.meta.height)/2, lbl, mute(th.OnSurface, th.Surface))
	}
}

func (s *Scene) drawPreview(p *painter.PixelPainter, img *image.RGBA) {
	if s.previewR.W == 0 {
		return
	}
	m := s.m
	th := s.theme
	muteS := mute(th.OnSurface, th.Surface)
	r := s.previewR
	p.FillRect(painter.Rect(r), th.Surface)
	p.FillRect(painter.Rect{X: r.X, Y: r.Y, W: 1, H: r.H}, th.Border) // left divider
	s.drawGripHandle(p, r.X)                                          // resize grab handle

	if !s.previewHas {
		msg := "Select an item to preview"
		m.side.draw(img, r.X+(r.W-m.side.width(msg))/2, r.Y+r.H/2-m.side.height/2, msg, muteS)
		return
	}

	// A web-linked item: a fixed header over the embedded browser (its own chrome,
	// page and progress bar), not the scrolling image/text column.
	if s.webPreviewItem() {
		s.drawWebPreview(p, img)
		return
	}

	d := s.previewContent()
	it := s.previewItem
	gap := rpxOf(s, 8)
	x := d.innerX

	// Content, composed with the toolkit box model: a VBox stacks the badge row,
	// title lines, meta, image and body lines; each is a widget the box positions.
	// The text widgets reuse the getFace line renderer (textLine) so wrapping and
	// CJK stay identical — only the layout is now box-driven.
	// Content text is stock toolkit.Label carrying the reader's fallback fonts.
	titleFont, bodyFont, metaFont := ttFont(true, rpxOf(s, 17)), ttFont(false, rpxOf(s, 14)), ttFont(false, rpxOf(s, 12))
	mkLabel := func(text string, font toolkit.Font, ink toolkit.RGBA) *toolkit.Label {
		l := toolkit.NewLabel(text)
		l.Font, l.Ink = font, ink
		return l
	}
	label := sourceLabel(it.Source)
	badge := &toolkit.Badge{Text: label, Fill: sourceColor(it.Source), Ink: onAccentFor(sourceColor(it.Source))}
	badge.Font = ttFont(true, rpxOf(s, 10))
	bw := m.badge.width(label) + m.pad
	badgeRow := toolkit.NewHBox()
	badgeRow.Spacing = rpxOf(s, 6)
	badgeRow.AddFixed(badge, bw)
	badgeRow.AddFlex(mkLabel(truncate(m.meta, it.Channel, d.innerW-bw-rpxOf(s, 6)), metaFont, muteS), 1)

	col := toolkit.NewVBox()
	col.Spacing = -1
	col.AddFixed(badgeRow, m.badgeH)
	col.AddFixed(toolkit.NewLabel(""), gap)
	for _, ln := range d.titleLines {
		col.AddFixed(mkLabel(ln, titleFont, th.OnSurface), d.titleFace.height+rpxOf(s, 2))
	}
	col.AddFixed(toolkit.NewLabel(""), gap)
	col.AddFixed(mkLabel(truncate(m.meta, d.meta, d.innerW), metaFont, muteS), m.meta.height)
	col.AddFixed(toolkit.NewLabel(""), gap)
	if d.imgH > 0 {
		col.AddFixed(&previewImage{s: s, it: it, p: p, img: img}, d.imgH)
		col.AddFixed(toolkit.NewLabel(""), gap)
	}
	for _, ln := range d.bodyLines {
		col.AddFixed(mkLabel(ln, bodyFont, th.OnSurface), d.bodyFace.height+rpxOf(s, 3))
	}
	col.SetBounds(toolkit.Rect{X: x, Y: r.Y + m.pad - s.previewScroll.offset, W: d.innerW, H: d.height})
	// Clip the scrolling content to the pane so overflow (notably a tall
	// rendered web page, but also scrolled-up header/body) never paints over the
	// topbar above or the download panel below.
	p.PushClip(painter.Rect(r))
	col.Draw(p, th)
	p.PopClip()

	// Scrollbar down the pane's right edge when the preview overflows.
	s.drawVScrollbar(p, r, 0, s.previewScroll.contentH, s.previewScroll.offset)

	// "Open" pill (fixed, over the content).
	if s.previewOpenR.W > 0 {
		p.FillRoundRect(painter.Rect(s.previewOpenR), rpxOf(s, 6), th.Accent)
		m.side.draw(img, s.previewOpenR.X+m.pad, s.previewOpenR.Y+(s.previewOpenR.H-m.side.height)/2, "Open", themeOnAccent(th))
	}
}

// drawWebPreview paints a web-linked item: a fixed header (badge / title / meta)
// across the top, then the embedded toolkit.Browser (its own tab strip, toolbar,
// address field, scrollable page and loading bar) filling the rest, with the
// "Open" pill floated on top. The browser was positioned by layoutPreview.
func (s *Scene) drawWebPreview(p *painter.PixelPainter, img *image.RGBA) {
	m := s.m
	th := s.theme
	muteS := mute(th.OnSurface, th.Surface)
	r := s.previewR
	it := s.previewItem
	gap := rpxOf(s, 8)
	x, innerW := s.previewInner()

	metaFont := ttFont(false, rpxOf(s, 12))
	titleFont := ttFont(true, rpxOf(s, 17))
	mkLabel := func(text string, font toolkit.Font, ink toolkit.RGBA) *toolkit.Label {
		l := toolkit.NewLabel(text)
		l.Font, l.Ink = font, ink
		return l
	}
	label := sourceLabel(it.Source)
	badge := &toolkit.Badge{Text: label, Fill: sourceColor(it.Source), Ink: onAccentFor(sourceColor(it.Source))}
	badge.Font = ttFont(true, rpxOf(s, 10))
	bw := m.badge.width(label) + m.pad
	badgeRow := toolkit.NewHBox()
	badgeRow.Spacing = rpxOf(s, 6)
	badgeRow.AddFixed(badge, bw)
	badgeRow.AddFlex(mkLabel(truncate(m.meta, it.Channel, innerW-bw-rpxOf(s, 6)), metaFont, muteS), 1)

	titleFace := getFace(rpxOf(s, 17), true)
	col := toolkit.NewVBox()
	col.Spacing = -1
	col.AddFixed(badgeRow, m.badgeH)
	col.AddFixed(toolkit.NewLabel(""), gap)
	for _, ln := range s.previewHeaderLines() {
		col.AddFixed(mkLabel(ln, titleFont, th.OnSurface), titleFace.height+rpxOf(s, 2))
	}
	col.AddFixed(toolkit.NewLabel(""), gap)
	col.AddFixed(mkLabel(truncate(m.meta, metaLine(it), innerW), metaFont, muteS), m.meta.height)
	hh := s.previewHeaderH()
	col.SetBounds(toolkit.Rect{X: x, Y: r.Y + m.pad, W: innerW, H: hh - m.pad})
	col.Draw(p, th)

	// The embedded browser fills the pane below the header.
	s.browser.Draw(p, th)

	// "Open" pill floated over the header, top-right.
	if s.previewOpenR.W > 0 {
		p.FillRoundRect(painter.Rect(s.previewOpenR), rpxOf(s, 6), th.Accent)
		m.side.draw(img, s.previewOpenR.X+m.pad, s.previewOpenR.Y+(s.previewOpenR.H-m.side.height)/2, "Open", themeOnAccent(th))
	}
}

// previewHitTest resolves a click inside the pane: the Open button (full detail)
// or nothing (the pane is otherwise passive). Returns HitNone with handled=false
// when the click is not in the pane, so the caller falls through to the feed.
func (s *Scene) previewHitTest(x, y int) (Hit, bool) {
	if s.previewR.W == 0 || !inRect(s.previewR, x, y) {
		return Hit{}, false
	}
	if s.previewHas && s.previewOpenR.W > 0 && inRect(s.previewOpenR, x, y) {
		return Hit{Kind: HitOpenPreview, Item: s.previewItem}, true
	}
	// The embedded browser (web preview) handles its own clicks — the front-end
	// forwards raw events to it (see ForwardBrowserClick), so a click inside the
	// pane that is not the Open button resolves to nothing here.
	return Hit{Kind: HitNone}, true
}

// hasThumb / thumb read the decoded-image cache for an item id.
func (s *Scene) hasThumb(id string) bool { return s.thumb(id) != nil }
func (s *Scene) thumb(id string) *image.RGBA {
	if s.Thumbs == nil {
		return nil
	}
	return s.Thumbs[id]
}

// drawPreviewImage renders the decoded thumbnail through the toolkit Image widget
// in aspect-fit mode (centred within r, no distortion) — the toolkit owns the
// scaling/letterboxing rather than the reader hand-blitting pixels.
func (s *Scene) drawPreviewImage(p *painter.PixelPainter, th *toolkit.Theme, t *image.RGBA, r toolkit.Rect) {
	b := t.Bounds()
	iw := toolkit.NewImageFit(tightPix(t), b.Dx(), b.Dy())
	iw.SetBounds(r)
	iw.Draw(p, th)
}

// tightPix returns t's pixels as a row-major W*H*4 buffer the toolkit Image
// expects: t.Pix directly when the image is origin-0 and tightly strided, else a
// packed copy.
func tightPix(t *image.RGBA) []byte {
	b := t.Bounds()
	w, h := b.Dx(), b.Dy()
	if b.Min.X == 0 && b.Min.Y == 0 && t.Stride == w*4 {
		return t.Pix
	}
	out := make([]byte, w*h*4)
	for y := 0; y < h; y++ {
		src := t.PixOffset(b.Min.X, b.Min.Y+y)
		copy(out[y*w*4:(y+1)*w*4], t.Pix[src:src+w*4])
	}
	return out
}
