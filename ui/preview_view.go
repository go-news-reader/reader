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

// SelectPreview loads it into the preview pane (clicking a feed item), resetting
// the pane scroll. The app calls SetPreviewLoading before an async image fetch.
func (s *Scene) SelectPreview(it source.Item) {
	s.previewItem = it
	s.previewHas = true
	s.previewScrollY = 0
	s.previewImgPending = false
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
	// Reserve the image box whenever the item declares media (or one is already
	// decoded). Once the picture is decoded the box grows to its fitted height
	// (aspect-preserved within the pane width, capped) via toolkit.FitBounds, so a
	// wide image wastes no vertical space; before it loads, a placeholder box hosts
	// the spinner / label.
	if len(it.Media) > 0 || s.hasThumb(it.ID) {
		if t := s.thumb(it.ID); t != nil {
			// Fit within the pane width and a share of its height, so the image
			// grows/shrinks as the pane is resized (a landscape image is width-bound
			// and fills the pane).
			capH := s.previewR.H * 3 / 5
			b := t.Bounds()
			fit := toolkit.FitBounds(b.Dx(), b.Dy(), toolkit.Rect{W: w, H: capH})
			d.imgH = fit.H
		} else {
			d.imgH = rpxOf(s, 160)
		}
	}
	gap := rpxOf(s, 8)
	h := m.pad + m.badgeH + gap
	h += len(d.titleLines) * (titleFace.height + rpxOf(s, 2))
	h += gap + m.meta.height + gap
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
		s.previewContentH = 0
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
		s.previewContentH = 0
		s.previewImgR = toolkit.Rect{}
		return
	}
	d := s.previewContent()
	s.previewContentH = d.height
	s.previewScrollY = clampScroll(s.previewScrollY, s.previewContentH-s.previewR.H)
}

// drawPreview paints the pane: its surface, left divider, and either the empty
// prompt or the selected item's badge/title/meta/image/body plus the Open button.
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
	y := r.Y + m.pad - s.previewScrollY

	// Source badge + channel.
	label := sourceLabel(it.Source)
	bw := m.badge.width(label) + m.pad
	p.FillRoundRect(painter.Rect{X: x, Y: y, W: bw, H: m.badgeH}, m.badgeH/2, sourceColor(it.Source))
	m.badge.draw(img, x+m.pad/2, y+(m.badgeH-m.badge.height)/2, label, onAccentFor(sourceColor(it.Source)))
	if it.Channel != "" {
		cw := r.X + r.W - m.pad - (x + bw + m.pad/2)
		m.meta.draw(img, x+bw+m.pad/2, y+(m.badgeH-m.meta.height)/2, truncate(m.meta, it.Channel, cw), muteS)
	}
	y += m.badgeH + gap

	tlh := d.titleFace.height + rpxOf(s, 2)
	for _, ln := range d.titleLines {
		d.titleFace.draw(img, x, y, ln, th.OnSurface)
		y += tlh
	}
	y += gap
	m.meta.draw(img, x, y, truncate(m.meta, d.meta, d.innerW), muteS)
	y += m.meta.height + gap

	if d.imgH > 0 {
		ir := toolkit.Rect{X: x, Y: y, W: d.innerW, H: d.imgH}
		s.previewImgR = ir
		p.FillRect(painter.Rect(ir), th.SurfaceAlt)
		if t := s.thumb(it.ID); t != nil {
			s.drawPreviewImage(p, th, t, ir)
		} else if s.previewImgPending {
			d := rpxOf(s, 22)
			s.spinnerAt(toolkit.Rect{X: ir.X + (ir.W-d)/2, Y: ir.Y + (ir.H-d)/2, W: d, H: d}).Draw(p, th)
		} else {
			lbl := "image"
			if len(it.Media) > 0 {
				lbl = string(it.Media[0].Kind)
			}
			m.meta.draw(img, ir.X+(ir.W-m.meta.width(lbl))/2, ir.Y+(ir.H-m.meta.height)/2, lbl, muteS)
		}
		y += d.imgH + gap
	}

	blh := d.bodyFace.height + rpxOf(s, 3)
	for _, ln := range d.bodyLines {
		if y+blh >= r.Y && y < s.H {
			d.bodyFace.draw(img, x, y, ln, th.OnSurface)
		}
		y += blh
	}

	// "Open" pill (fixed, over the content).
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
