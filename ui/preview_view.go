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
	s.webURLFocused = false // a new selection drops any in-progress address edit
	s.webURLBuf = ""
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

// webImg / hasWeb read the rendered-page cache for an item id.
func (s *Scene) webImg(id string) *image.RGBA {
	if s.previewWeb == nil {
		return nil
	}
	return s.previewWeb[id]
}
func (s *Scene) hasWeb(id string) bool { return s.webImg(id) != nil }

// HasWeb reports whether a rendered target page is cached for the item id (the
// app checks it before kicking off a render, on the UI thread).
func (s *Scene) HasWeb(id string) bool { return s.hasWeb(id) }

// SetPreviewWebLoading marks (or clears) that a page render is in flight for the
// previewed item, so the pane shows a spinner until SetPreviewWeb lands.
func (s *Scene) SetPreviewWebLoading(v bool) { s.previewWebPending = v; s.touch() }

// WebLoading reports whether a page render is currently in flight for the
// previewed item.
func (s *Scene) WebLoading() bool { return s.previewWebPending }

// SetPreviewWeb stores a rendered target-page image (keyed by item id) plus its
// clickable link map and the width it was rendered at, and — when the fetch was
// for the item still being previewed — clears the pane's web loading state and
// resets the scroll to the top of the new page. A nil image only clears the
// pending flag (a render that failed falls back to the text summary).
func (s *Scene) SetPreviewWeb(id string, img *image.RGBA, links []WebLink, renderW int) {
	if img != nil {
		if s.previewWeb == nil {
			s.previewWeb = map[string]*image.RGBA{}
			s.previewWebLinks = map[string][]WebLink{}
			s.previewWebRenderW = map[string]int{}
		}
		s.previewWeb[id] = img
		s.previewWebLinks[id] = links
		s.previewWebRenderW[id] = renderW
	}
	if s.previewHas && s.previewItem.ID == id {
		s.previewWebPending = false
		s.previewScroll.offset = 0
		if img != nil {
			s.ensureWebTab(s.previewItem) // a rendered page opens (or keeps) its tab
		}
	}
	s.touch()
}

// webHist is an item's in-pane browsing history: the ordered list of visited
// URLs plus a cursor at the currently-shown one. Back/Forward move the cursor;
// a fresh navigation truncates everything after it (standard browser semantics).
type webHist struct {
	urls []string
	cur  int
}

func (s *Scene) histFor(id string) *webHist {
	if s.previewWebHist == nil {
		s.previewWebHist = map[string]*webHist{}
	}
	return s.previewWebHist[id]
}

// InitWebHistory starts (or restarts) the history for an item at its target URL
// — called when the item is first previewed, so Back never leaves the page that
// was opened.
func (s *Scene) InitWebHistory(id, url string) {
	if s.previewWebHist == nil {
		s.previewWebHist = map[string]*webHist{}
	}
	s.previewWebHist[id] = &webHist{urls: []string{url}, cur: 0}
}

// PushWebURL records a navigation to url (a clicked link): it drops any forward
// entries past the cursor, appends url, and advances the cursor to it.
func (s *Scene) PushWebURL(id, url string) {
	h := s.histFor(id)
	if h == nil || len(h.urls) == 0 {
		s.previewWebHist[id] = &webHist{urls: []string{url}, cur: 0}
		return
	}
	h.urls = append(h.urls[:h.cur+1], url)
	h.cur = len(h.urls) - 1
}

// WebBackURL steps the cursor back one and returns the URL now current (the page
// to re-render) and whether a back step was possible.
func (s *Scene) WebBackURL(id string) (string, bool) {
	h := s.histFor(id)
	if h == nil || h.cur < 1 {
		return "", false
	}
	h.cur--
	return h.urls[h.cur], true
}

// WebForwardURL steps the cursor forward one and returns the URL now current and
// whether a forward step was possible.
func (s *Scene) WebForwardURL(id string) (string, bool) {
	h := s.histFor(id)
	if h == nil || h.cur >= len(h.urls)-1 {
		return "", false
	}
	h.cur++
	return h.urls[h.cur], true
}

// WebCanBack reports whether the item's web view can navigate back.
func (s *Scene) WebCanBack(id string) bool {
	h := s.histFor(id)
	return h != nil && h.cur > 0
}

// WebCanForward reports whether the item's web view can navigate forward.
func (s *Scene) WebCanForward(id string) bool {
	h := s.histFor(id)
	return h != nil && h.cur < len(h.urls)-1
}

// CurrentWebURL returns the URL of the page currently shown for the item, or ""
// when the item has no web history.
func (s *Scene) CurrentWebURL(id string) string {
	h := s.histFor(id)
	if h == nil || len(h.urls) == 0 {
		return ""
	}
	return h.urls[h.cur]
}

// WebURLFocused reports whether the preview's address field holds keyboard focus.
func (s *Scene) WebURLFocused() bool { return s.webURLFocused }

// FocusWebURL gives (v=true) or removes keyboard focus from the address field.
// Taking focus seeds the edit buffer with the page currently shown and drops the
// topbar search focus, so the two text fields never both capture keystrokes.
func (s *Scene) FocusWebURL(v bool) {
	if v {
		s.searchFocused = false
		if s.previewHas {
			s.webURLBuf = s.CurrentWebURL(s.previewItem.ID)
		}
	}
	s.webURLFocused = v
	s.touch()
}

// webURLDisplay is the text shown in the address field: the buffer being typed
// while focused, else the page currently shown.
func (s *Scene) webURLDisplay(id string) string {
	if s.webURLFocused {
		return s.webURLBuf
	}
	return s.CurrentWebURL(id)
}

// CommitWebURL defocuses the address field and returns the normalised URL to
// navigate to (a bare host gains an https:// scheme) and whether it is non-empty.
func (s *Scene) CommitWebURL() (string, bool) {
	u := strings.TrimSpace(s.webURLBuf)
	s.webURLFocused = false
	s.touch()
	if u == "" {
		return "", false
	}
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		u = "https://" + u
	}
	return u, true
}

// webLinkAt maps a widget-space click at (x, y) to the href of the rendered-page
// anchor under it, or ("", false). It inverts the display transform: the page
// image fills previewImgR (the box recorded by the last Draw) scaled from its
// render width, so a click maps to render-pixel coords by the box→render scale.
func (s *Scene) webLinkAt(id string, x, y int) (string, bool) {
	box := s.previewImgR
	rw := s.previewWebRenderW[id]
	img := s.webImg(id)
	if box.W <= 0 || box.H <= 0 || rw <= 0 || img == nil || !inRect(box, x, y) {
		return "", false
	}
	// Box was sized to the image's aspect at render width, so both axes share the
	// scale render/box; map the click into render-pixel space.
	px := (x - box.X) * rw / box.W
	py := (y - box.Y) * img.Bounds().Dy() / box.H
	pt := image.Pt(px, py)
	for _, l := range s.previewWebLinks[id] {
		if pt.In(l.Rect) {
			return l.Href, true
		}
	}
	return "", false
}

// WebPreviewURL returns the external http(s) target page to render for it, or ""
// when there is none or the source isn't web-renderable. Usenet posts preview as
// reconstructed images, not web pages, so they never return a URL here.
func (s *Scene) WebPreviewURL(it source.Item) string { return webPreviewURL(it) }

// PreviewWebWidth is the pixel width to render a target page at: the pane's
// current inner content width (so the render displays 1:1), floored so a render
// kicked off before the pane's first layout still uses a usable width.
func (s *Scene) PreviewWebWidth() int {
	_, w := s.previewInner()
	if w < 320 {
		w = 320
	}
	return w
}

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
	case s.hasWeb(it.ID):
		// A rendered target page: show it full-width (scaled to the inner width),
		// its natural height, and scroll through it — the page replaces the
		// text summary, so drop the wrapped body lines.
		t := s.webImg(it.ID)
		b := t.Bounds()
		if b.Dx() > 0 {
			d.imgH = b.Dy() * w / b.Dx()
		}
		d.bodyLines = nil
	case s.previewWebPending:
		// Rendering the page: reserve a tall box for the spinner, no summary yet.
		availH := s.previewR.H - headerH - gap - m.pad
		if lo := rpxOf(s, 160); availH < lo {
			availH = lo
		}
		d.imgH = availH
		d.bodyLines = nil
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
		s.previewScroll.contentH = 0
		return
	}
	s.previewR = toolkit.Rect{X: s.W - pw, Y: m.topbarH, W: pw, H: s.feedBottom() - m.topbarH}
	// "Open" pill, top-right of the pane (fixed, over the scrolling content), shown
	// only when an item is selected and it has a full-view target.
	if s.previewHas {
		ow := m.pad*2 + m.side.width("Open")
		s.previewOpenR = toolkit.Rect{X: s.previewR.X + s.previewR.W - m.pad - ow, Y: m.topbarH + m.pad/2, W: ow, H: m.badgeH + rpxOf(s, 4)}
	} else {
		s.previewOpenR = toolkit.Rect{}
	}
	if !s.previewHas {
		s.previewScroll.contentH = 0
		s.previewImgR = toolkit.Rect{}
		return
	}
	d := s.previewContent()
	s.previewScroll.refresh(d.height, s.previewR.H)
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
	case s.webImg(w.it.ID) != nil:
		// The rendered target page, scaled to the box width (top-aligned).
		s.drawPreviewImage(w.p, th, s.webImg(w.it.ID), b)
	case s.thumb(w.it.ID) != nil:
		s.drawPreviewImage(w.p, th, s.thumb(w.it.ID), b)
	case s.previewImgPending || s.previewWebPending:
		// Clear, centred loading placeholder: a label above a sizable spinner, so a
		// slow render (a web page can take a second or more to fetch + lay out)
		// reads as "loading", not a frozen blank pane.
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
	// Navigation overlay: while a new web page renders over an already-shown one
	// (link click / Back), the case above kept painting the previous page — so
	// float a small spinner chip at the top-centre so the load is still visible.
	if s.previewWebPending && s.previewItem.ID == w.it.ID && s.webImg(w.it.ID) != nil {
		d := rpxOf(s, 20)
		pad := rpxOf(s, 6)
		chip := toolkit.Rect{X: b.X + (b.W-d)/2 - pad, Y: b.Y + pad, W: d + 2*pad, H: d + 2*pad}
		w.p.FillRoundRect(painter.Rect(chip), rpxOf(s, 6), th.SurfaceAlt)
		s.spinnerAt(toolkit.Rect{X: chip.X + pad, Y: chip.Y + pad, W: d, H: d}, toolkit.SpinnerRing).Draw(w.p, th)
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
	// When the fixed browser chrome (tab strip + toolbar) is shown, reserve its
	// height at the top so the scrolling header/page starts below it rather than
	// under it. The content already carries m.pad of top padding via SetBounds.
	if ch := s.webChromeH(); ch > m.pad {
		col.AddFixed(toolkit.NewLabel(""), ch-m.pad)
	}
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

	// Mini in-app browser toolbar, fixed at the pane's top edge, shown whenever a
	// target page is rendered: "‹ Back" / "Fwd ›" chips (enabled or dimmed by the
	// available history) followed by an editable address field spanning the rest
	// of the row.
	s.previewBackR, s.previewFwdR, s.previewReloadR, s.previewURLR = toolkit.Rect{}, toolkit.Rect{}, toolkit.Rect{}, toolkit.Rect{}
	s.webTabHits = s.webTabHits[:0]
	if s.webToolbar() {
		id := s.previewItem.ID
		h := m.badgeH + rpxOf(s, 4)
		y0 := m.topbarH + m.pad/2 // tab strip row (or the toolbar row when no strip)
		tabsH := s.tabStripH()    // 0 when fewer than two tabs are open
		y := y0
		if tabsH > 0 {
			y = y0 + tabsH + m.pad/2 // the toolbar drops below the tab strip
		}
		// Opaque band behind the whole chrome so the scrolling page (and the post's
		// badge/title beneath it) never shows through the gaps between controls —
		// the toolbar reads as fixed browser chrome. The "Open" pill is re-drawn on
		// top afterwards so it stays visible.
		p.FillRect(painter.Rect(toolkit.Rect{X: r.X + 1, Y: r.Y, W: r.W - 2, H: (y - r.Y) + h + m.pad/2}), th.Surface)
		// Tab strip on the top row, stopping before the "Open" pill at the right.
		if tabsH > 0 {
			tabsRight := r.X + r.W - m.pad
			if s.previewOpenR.W > 0 {
				tabsRight = s.previewOpenR.X - m.pad
			}
			s.drawWebTabs(p, img, m, th, r, y0, tabsRight)
		}
		bw := m.pad*2 + m.side.width("‹ Back")
		fw := m.pad*2 + m.side.width("Fwd ›")
		back := toolkit.Rect{X: r.X + m.pad, Y: y, W: bw, H: h}
		fwd := toolkit.Rect{X: back.X + bw + m.pad, Y: y, W: fw, H: h}
		reload := toolkit.Rect{X: fwd.X + fw + m.pad, Y: y, W: h, H: h} // compact square
		s.drawNavChip(p, img, m, th, back, "‹ Back", s.WebCanBack(id))
		s.drawNavChip(p, img, m, th, fwd, "Fwd ›", s.WebCanForward(id))
		// Reload is always available: a square chip with the refresh glyph.
		p.FillRoundRect(painter.Rect(reload), rpxOf(s, 6), th.SurfaceAlt)
		gp := rpxOf(s, 5)
		drawRefreshIcon(p, toolkit.Rect{X: reload.X + gp, Y: reload.Y + gp, W: reload.W - 2*gp, H: reload.H - 2*gp}, th.OnSurface, s.iconStroke())
		if s.WebCanBack(id) {
			s.previewBackR = back
		}
		if s.WebCanForward(id) {
			s.previewFwdR = fwd
		}
		s.previewReloadR = reload
		// Address field fills the remainder of the toolbar row. When there is no tab
		// strip the toolbar shares the top row with the "Open" pill, so it stops
		// before it; with a strip the toolbar has its own row and can run full width.
		urlX := reload.X + reload.W + m.pad
		urlRight := r.X + r.W - m.pad
		if tabsH == 0 && s.previewOpenR.W > 0 {
			urlRight = s.previewOpenR.X - m.pad
		}
		if urlRight-urlX > rpxOf(s, 40) { // only when there is usable width
			s.previewURLR = toolkit.Rect{X: urlX, Y: y, W: urlRight - urlX, H: h}
			s.drawAddressField(p, img, m, th, s.previewURLR, id)
		}
		// Re-draw the "Open" pill on top of the band so it stays visible.
		if s.previewOpenR.W > 0 {
			p.FillRoundRect(painter.Rect(s.previewOpenR), rpxOf(s, 6), th.Accent)
			m.side.draw(img, s.previewOpenR.X+m.pad, s.previewOpenR.Y+(s.previewOpenR.H-m.side.height)/2, "Open", themeOnAccent(th))
		}
	}
}

// drawNavChip paints one mini-browser nav chip: a rounded pill with a label,
// in enabled (interactive) or dimmed (disabled) styling.
func (s *Scene) drawNavChip(p *painter.PixelPainter, img *image.RGBA, m metrics, th *toolkit.Theme, r toolkit.Rect, label string, enabled bool) {
	p.FillRoundRect(painter.Rect(r), rpxOf(s, 6), th.SurfaceAlt)
	fg := th.OnSurface
	if !enabled {
		fg = th.Border // dimmed: the direction has no history
	}
	m.side.draw(img, r.X+m.pad, r.Y+(r.H-m.side.height)/2, label, fg)
}

// drawAddressField paints the browser toolbar's URL field: a rounded box holding
// the current (or being-typed) URL, with a focus ring + caret while focused. The
// text is right-clipped to the field so a long URL never bleeds past it.
func (s *Scene) drawAddressField(p *painter.PixelPainter, img *image.RGBA, m metrics, th *toolkit.Theme, r toolkit.Rect, id string) {
	p.FillRoundRect(painter.Rect(r), rpxOf(s, 6), th.Background)
	if s.webURLFocused {
		p.StrokeRoundRect(painter.Rect(r), rpxOf(s, 6), th.Accent, rpxOf(s, 1)) // focus ring
	}
	txt := s.webURLDisplay(id)
	fg := th.OnSurface
	if txt == "" {
		txt, fg = "Enter a URL…", th.Border // placeholder
	}
	avail := r.W - m.pad*2
	txt = clipTextRight(m.side, txt, avail)
	tx := r.X + m.pad
	ty := r.Y + (r.H-m.side.height)/2
	p.PushClip(painter.Rect(r))
	m.side.draw(img, tx, ty, txt, fg)
	if s.webURLFocused {
		caretX := tx + m.side.width(txt)
		p.FillRect(painter.Rect(toolkit.Rect{X: caretX + 1, Y: ty, W: rpxOf(s, 1), H: m.side.height}), th.Accent)
	}
	p.PopClip()
}

// clipTextRight trims the head of s so it fits within w px (keeping the tail,
// where the meaningful part of a long URL — the path — lives), prefixing an
// ellipsis when it had to cut.
func clipTextRight(f textFace, str string, w int) string {
	if w <= 0 || f.width(str) <= w {
		return str
	}
	rs := []rune(str)
	for i := 1; i < len(rs); i++ {
		if cand := "…" + string(rs[i:]); f.width(cand) <= w {
			return cand
		}
	}
	return "…"
}

// webToolbar reports whether the browser toolbar (chips + address field) should
// be shown: the current item has a rendered target page.
func (s *Scene) webToolbar() bool {
	return s.previewHas && s.hasWeb(s.previewItem.ID)
}

// webChromeH is the total height of the fixed browser chrome (tab strip +
// toolbar) measured from the pane's top, or 0 when no page is shown. It matches
// the opaque band drawn in drawPreview so the scrolling content can reserve it.
func (s *Scene) webChromeH() int {
	if !s.webToolbar() {
		return 0
	}
	m := s.m
	h := m.badgeH + rpxOf(s, 4)
	off := m.pad / 2 // the toolbar row's top offset below the pane top
	if t := s.tabStripH(); t > 0 {
		off += t + m.pad/2
	}
	return off + h + m.pad/2
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
	// Web preview tab strip (switch / close a tab) takes priority over the chrome.
	if hit, ok := s.webTabHitTest(x, y); ok {
		return hit, true
	}
	// Web preview is a mini browser: the "‹" back chip, then any anchor under the
	// cursor navigates in-pane.
	if s.previewHas {
		id := s.previewItem.ID
		if s.previewBackR.W > 0 && inRect(s.previewBackR, x, y) {
			return Hit{Kind: HitWebBack, Item: s.previewItem}, true
		}
		if s.previewFwdR.W > 0 && inRect(s.previewFwdR, x, y) {
			return Hit{Kind: HitWebFwd, Item: s.previewItem}, true
		}
		if s.previewReloadR.W > 0 && inRect(s.previewReloadR, x, y) {
			return Hit{Kind: HitWebReload, Item: s.previewItem}, true
		}
		if s.previewURLR.W > 0 && inRect(s.previewURLR, x, y) {
			return Hit{Kind: HitWebURL, Item: s.previewItem}, true
		}
		if href, ok := s.webLinkAt(id, x, y); ok {
			return Hit{Kind: HitWebLink, Item: s.previewItem, Value: href}, true
		}
	}
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
