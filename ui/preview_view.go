package ui

// The right-hand preview/details pane (feed view). It is a docked, always-visible
// column on the right that reflects the item last clicked in the feed: source
// badge, title, meta, an image (whatever has been decoded into Thumbs — e.g. a
// Usenet binary the app reconstructed) and the body text, with an "Open" button
// for the full-screen reading view. The feed list narrows to make room; on a
// window too narrow to keep a usable feed the pane hides itself (previewWidth 0).

import (
	"fmt"
	"image"
	"strings"
	"time"

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
// FeedGeom is the feed pane's horizontal extent in scene pixels: where the
// cards, the scrollbar and the empty-state label all live. Exported so a caller
// -- a test, a host laying something over the pane -- can ask for the geometry
// the renderer actually uses instead of recomputing it and being subtly wrong.
func (s *Scene) FeedGeom() (x, w int) { return s.feedGeom() }

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
	return s.feedContentH() > s.feedViewportH()
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
	// The button is now held inside the browser: subsequent motion drags it (so a
	// grabbed scrollbar thumb tracks the pointer) until the release clears this.
	s.browserPressed = true
	s.touch()
	return true
}

// ForwardBrowserDrag forwards pointer motion at screen coords (x, y) into the
// embedded browser as an EventMouseDrag, but ONLY while a browser press is active
// (a prior ForwardBrowserClick consumed a press inside the widget). This is what
// lets a grabbed scrollbar thumb — vertical OR horizontal — follow the mouse.
// It returns true when the drag was routed to the browser. A drag with no active
// browser press is ignored (returns false) so the caller keeps its own drag
// handling (e.g. text selection or a pane-divider resize).
func (s *Scene) ForwardBrowserDrag(x, y int) bool {
	if !s.browserPressed || !s.webPreviewItem() {
		return false
	}
	b := s.browser.Bounds()
	if b.W <= 0 {
		return false
	}
	s.browser.OnEvent(toolkit.Event{Kind: toolkit.EventMouseDrag, X: x - b.X, Y: y - b.Y})
	s.touch()
	return true
}

// ForwardBrowserRelease forwards a mouse-button release into the embedded
// browser when the web preview is active, so a toolbar button pressed on the
// preceding click clears its press-feedback face. Coordinates are screen-space
// (translated to widget-local); a release anywhere clears the press.
func (s *Scene) ForwardBrowserRelease(x, y int) {
	// The button is no longer held: end any in-progress browser drag regardless of
	// where the release lands, so a thumb drag that wandered outside the widget
	// still stops grabbing.
	s.browserPressed = false
	if !s.webPreviewItem() {
		return
	}
	b := s.browser.Bounds()
	if b.W <= 0 {
		return
	}
	s.browser.OnEvent(toolkit.Event{Kind: toolkit.EventMouseUp, X: x - b.X, Y: y - b.Y})
	s.touch()
}

// browserWheelStep is how many device pixels of wheel travel map to one browser
// content row (the widget scrolls BrowserScrollStep px per row).
const browserWheelStep = 20

// ForwardBrowserScroll forwards a wheel delta (device px) into the embedded
// browser when the web preview is active and the pointer is over the widget,
// converting pixels to the widget's row-based scroll intent. Returns true when
// the browser consumed the wheel. It is the plain (unshifted) wheel path — a
// convenience over ForwardBrowserScrollShift for callers with no modifier state.
func (s *Scene) ForwardBrowserScroll(dy int) bool {
	return s.ForwardBrowserScrollShift(dy, false)
}

// ForwardBrowserScrollShift is ForwardBrowserScroll with the Shift modifier: when
// shift is true the wheel is delivered to the browser as a shifted EventScroll,
// which the toolkit Browser interprets as HORIZONTAL scrolling (the vertical bar
// has a plain-wheel path; the horizontal bar rides shift+wheel). The drag path
// (ForwardBrowserDrag) already moves either scrollbar thumb; this adds the
// wheel affordance once a back-end can report the Shift state on a scroll event.
func (s *Scene) ForwardBrowserScrollShift(dy int, shift bool) bool {
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
	s.browser.OnEvent(toolkit.Event{Kind: toolkit.EventScroll, X: s.lastMouseX - b.X, Y: s.lastMouseY - b.Y, Delta: rows, Shift: shift})
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

// PrefetchWebURLs returns up to n distinct web-renderable target URLs for the
// feed items immediately adjacent to it (previous, then next, then the ones just
// beyond), for background pre-rendering so selecting a neighbour is instant. It
// has no side effects — it does not scroll or change the selection, unlike
// [Scene.NavItem]. Items with no web target (Usenet posts, text-only posts) are
// skipped; a mismatch (it not in the feed) yields nothing.
func (s *Scene) PrefetchWebURLs(it source.Item, n int) []string {
	if n <= 0 {
		return nil
	}
	s.ensureFeed()
	// The web-renderable cards are the standalone (non-group) feed entries; a
	// Usenet group summary has no web target, so it is skipped like a text post.
	items := make([]source.Item, 0, len(s.feed.display))
	for _, e := range s.feed.display {
		if e.group == nil {
			items = append(items, e.item)
		}
	}
	cur := -1
	for i := range items {
		if sameItem(items[i], it) {
			cur = i
			break
		}
	}
	if cur < 0 {
		return nil
	}
	var out []string
	// Nearest neighbours first (prev, next), then one step further out.
	for _, off := range []int{-1, 1, -2, 2} {
		if len(out) >= n {
			break
		}
		j := cur + off
		if j < 0 || j >= len(items) {
			continue
		}
		u := webPreviewURL(items[j])
		if u == "" {
			continue
		}
		dup := false
		for _, e := range out {
			if e == u {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, u)
		}
	}
	return out
}

func webPreviewURL(it source.Item) string {
	if it.Source == source.Usenet {
		return ""
	}
	u := it.Link
	if u == "" {
		// No external link. A self/text post (e.g. a Reddit text post) carries its
		// content in Body, so it is shown as text — do NOT web-render its platform
		// permalink, which for Reddit and other JS-app sites is an unhydratable page
		// that would paint blank in place of the post's text. Only fall back to the
		// permalink for a post with no readable body of its own.
		if strings.TrimSpace(it.Body) != "" {
			return ""
		}
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
	metaFace            textFace // scaled meta face (draw + row height + truncation)
	titleLines          []string
	bodyLines           []string
	meta                string
	imgH                int // height reserved for the image box (0 when none)
	height              int

	// Comment thread (Reddit text posts): the flattened, indented comments to
	// paint below the body, a "N comments" heading, and whether the loading line
	// shows. sectionH is the vertical extent the whole section occupies (already
	// folded into height); it is 0 when there is no comment section.
	comments         []previewComment
	commentsHeading  string
	commentsLoading  bool
	commentsHeadFace textFace
	sectionH         int
}

// previewComment is one laid-out comment block: its wrapped meta + body lines
// and the left indentation (in px) its depth earns.
type previewComment struct {
	inset     int
	metaFace  textFace
	bodyFace  textFace
	meta      string
	bodyLines []string
	height    int
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
	titleFace := getFace(s.ptPx(17), true)
	bodyFace := getFace(s.ptPx(14), false)
	metaFace := getFace(s.ptPx(12), false)
	it := s.previewItem
	// Wrap with the SAME fonts drawPreview draws these lines with (stock toolkit
	// Labels carrying ttFont), not with the textFaces whose heights size the
	// rows — see wrapMeasured. Sizes route through ptPx so the A−/A+ pills scale
	// title/body/meta together while wrapping and row heights stay consistent.
	d := previewBody{
		innerX: x, innerW: w, titleFace: titleFace, bodyFace: bodyFace, metaFace: metaFace,
		titleLines: wrapMeasured(ttFont(true, s.ptPx(17)).Measure, it.Title, w),
		bodyLines:  wrapMeasured(ttFont(false, s.ptPx(14)).Measure, stripHTML(it.Body), w),
		meta:       metaLine(it),
	}
	gap := rpxOf(s, 8)
	// Height consumed by the header above the image (badge + title + meta).
	headerH := m.pad + m.badgeH + gap
	headerH += len(d.titleLines) * (titleFace.height + rpxOf(s, 2))
	headerH += gap + metaFace.height + gap
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
	// Comment thread (Reddit text posts): laid out below the body, indented by
	// depth. sectionH is added to the content height so the pane scrolls over it.
	s.layoutComments(&d)
	h += d.sectionH
	h += m.pad
	d.height = h
	return d
}

// Comment-section spacing, in logical px (scaled through rpxOf at use).
const (
	commentIndentStep = 14 // left inset added per nesting depth
	commentGapPt      = 8  // gap before the section and between comment blocks
	commentTightPt    = 6  // gap under the divider and under the heading
)

// layoutComments wraps and measures the previewed item's comment thread into
// d.comments, sets the "N comments" heading and the loading flag, and records
// the section's total height in d.sectionH (0 when there is no section). Only a
// Reddit post that is loading or has a delivered thread gets a section; every
// other item leaves the section empty. Each comment is indented by its depth (a
// per-level inset, clamped so the wrapped text keeps a usable width) and wrapped
// at that reduced width, so no line can overflow the pane's right edge.
func (s *Scene) layoutComments(d *previewBody) {
	id := s.previewItem.ID
	thread, fetched := s.commentsFor(id)
	loading := s.commentsPending(id)
	if !fetched && !loading {
		return // no comment section for this item
	}

	metaFace := getFace(s.ptPx(11), false)
	bodyFace := getFace(s.ptPx(13), false)
	d.commentsHeadFace = getFace(s.ptPx(13), true)
	d.commentsLoading = loading && !fetched
	if fetched {
		d.commentsHeading = commentsHeading(len(thread))
	}

	step := rpxOf(s, commentIndentStep)
	for _, c := range thread {
		// Indent by depth, but never past 3/4 of the column so the wrapped text
		// keeps at least a quarter-width to live in — a very deep thread stops
		// marching right instead of squeezing to nothing.
		inset := c.Depth * step
		if capX := d.innerW * 3 / 4; inset > capX {
			inset = capX
		}
		wrapW := d.innerW - inset
		pc := previewComment{
			inset:     inset,
			metaFace:  metaFace,
			bodyFace:  bodyFace,
			meta:      commentMetaLine(c),
			bodyLines: wrapMeasured(ttFont(false, s.ptPx(13)).Measure, stripHTML(c.Body), wrapW),
		}
		pc.height = metaFace.height + rpxOf(s, 2) + len(pc.bodyLines)*(bodyFace.height+rpxOf(s, 2))
		d.comments = append(d.comments, pc)
	}

	// Section height: a gap + a divider rule + a tight gap, then the heading (when
	// shown) + a tight gap, the loading line (when shown), and each comment block
	// followed by an inter-block gap. This MUST match the widgets drawPreview
	// appends to the scrolling column, so the pane scrolls over exactly what it
	// paints.
	sec := rpxOf(s, commentGapPt) + rpxOf(s, 1) + rpxOf(s, commentTightPt)
	if d.commentsHeading != "" {
		sec += d.commentsHeadFace.height + rpxOf(s, commentTightPt)
	}
	if d.commentsLoading {
		sec += metaFace.height
	}
	for _, c := range d.comments {
		sec += c.height + rpxOf(s, commentGapPt)
	}
	d.sectionH = sec
}

// commentsHeading is the section heading for n loaded comments ("No comments",
// "1 comment", or "N comments").
func commentsHeading(n int) string {
	switch n {
	case 0:
		return "No comments"
	case 1:
		return "1 comment"
	default:
		return fmt.Sprintf("%d comments", n)
	}
}

// commentMetaLine is a comment's muted "author · N pts · date" line.
func commentMetaLine(c source.Comment) string {
	author := c.Author
	if author == "" {
		author = "[deleted]"
	}
	parts := []string{author, fmt.Sprintf("%d pts", c.Score)}
	if c.Created > 0 {
		parts = append(parts, time.Unix(c.Created, 0).UTC().Format("2 Jan 2006 15:04"))
	}
	return strings.Join(parts, " · ")
}

// layoutPreview computes the pane rect, its Open button, image rect and the
// scrollable content height. It is a no-op (empty previewR) when the pane hides.
func (s *Scene) layoutPreview() {
	m := s.m
	pw := s.previewWidth()
	if pw == 0 {
		s.previewR = toolkit.Rect{}
		s.previewOpenR = toolkit.Rect{}
		s.sTextSmallerR = toolkit.Rect{}
		s.sTextLargerR = toolkit.Rect{}
		s.previewImgR = toolkit.Rect{}
		s.browser.SetBounds(toolkit.Rect{})
		s.previewScroll.contentH = 0
		return
	}
	s.previewR = toolkit.Rect{X: s.W - pw, Y: m.topbarH, W: pw, H: s.feedBottom() - m.topbarH}
	// "Open" pill, top-right of the pane (fixed, over the content), shown only when
	// an item is selected and it has a full-view target.
	if s.previewHas {
		btnY := m.topbarH + m.pad/2
		btnH := m.badgeH + rpxOf(s, 4)
		ow := m.pad*2 + m.side.width("Open")
		s.previewOpenR = toolkit.Rect{X: s.previewR.X + s.previewR.W - m.pad - ow, Y: btnY, W: ow, H: btnH}
		// A−/A+ reader-text zoom pills, laid out just left of the Open pill (both
		// same width so they read as a pair). They never overlap the Open pill.
		aw := m.pad + max(m.side.width("A−"), m.side.width("A+"))
		gap := rpxOf(s, 6)
		s.sTextLargerR = toolkit.Rect{X: s.previewOpenR.X - gap - aw, Y: btnY, W: aw, H: btnH}
		s.sTextSmallerR = toolkit.Rect{X: s.sTextLargerR.X - gap - aw, Y: btnY, W: aw, H: btnH}
	} else {
		s.previewOpenR = toolkit.Rect{}
		s.sTextSmallerR = toolkit.Rect{}
		s.sTextLargerR = toolkit.Rect{}
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
		s.browser.Scale = s.Scale // scale the chrome (toolbar/tabs/buttons) to the display
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
	lines := wrapMeasured(ttFont(true, s.ptPx(17)).Measure, s.previewItem.Title, w)
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
	titleFace := getFace(s.ptPx(17), true)
	metaFace := getFace(s.ptPx(12), false)
	h := m.pad + m.badgeH + gap
	h += len(s.previewHeaderLines()) * (titleFace.height + rpxOf(s, 2))
	h += gap + metaFace.height + gap
	return h
}

// drawPreview paints the pane: its surface, left divider, and either the empty
// prompt or the selected item's badge/title/meta/image/body plus the Open button.
// previewImage is the preview pane's image cell as a toolkit widget so the box
// layout positions it: it records its rect (for the click-to-open hit-test) and
// draws the decoded thumbnail, a spinner while it loads, or a kind placeholder.
type previewImage struct {
	toolkit.Base
	s  *Scene
	it source.Item
	p  *painter.PixelPainter
}

// Draw composes the image cell — a SurfaceAlt toolkit.Backdrop ground, then
// either the decoded thumbnail (through the toolkit Image widget), a loading
// Label above an animated Spinner, or a centred kind Label — with nothing
// hand-drawn.
func (w *previewImage) Draw(pt painter.Painter, th *toolkit.Theme) {
	s, b := w.s, w.Bounds()
	s.previewImgR = b
	ground := &toolkit.Backdrop{Fill: th.SurfaceAlt}
	ground.SetBounds(b)
	ground.Draw(pt, th)
	switch {
	case s.thumb(w.it.ID) != nil:
		s.drawPreviewImage(w.p, th, s.thumb(w.it.ID), b)
	case s.previewImgPending:
		// Clear, centred loading placeholder: a Label above a sizable Spinner, so a
		// slow reconstruct reads as "loading", not a frozen blank pane.
		msg := "Loading preview…"
		lw := s.m.meta.width(msg)
		d := rpxOf(s, 44)
		gap := rpxOf(s, 12)
		blockH := s.m.meta.height + gap + d
		top := b.Y + (b.H-blockH)/2
		lbl := toolkit.NewLabel(msg)
		lbl.Font, lbl.Ink, lbl.VAlign = s.m.meta.font, mute(th.OnSurface, th.SurfaceAlt), toolkit.VTop
		lbl.SetBounds(toolkit.Rect{X: b.X + (b.W-lw)/2, Y: top, W: lw, H: s.m.meta.height})
		lbl.Draw(pt, th)
		s.spinnerAt(toolkit.Rect{X: b.X + (b.W-d)/2, Y: top + s.m.meta.height + gap, W: d, H: d}, toolkit.SpinnerDots).Draw(pt, th)
	default:
		kind := "image"
		if len(w.it.Media) > 0 {
			kind = string(w.it.Media[0].Kind)
		}
		lw := s.m.meta.width(kind)
		lbl := toolkit.NewLabel(kind)
		lbl.Font, lbl.Ink, lbl.VAlign = s.m.meta.font, mute(th.OnSurface, th.Surface), toolkit.VTop
		lbl.SetBounds(toolkit.Rect{X: b.X + (b.W-lw)/2, Y: b.Y + (b.H-s.m.meta.height)/2, W: lw, H: s.m.meta.height})
		lbl.Draw(pt, th)
	}
}

func (s *Scene) drawPreview(p *painter.PixelPainter) {
	if s.previewR.W == 0 {
		return
	}
	m := s.m
	th := s.theme
	muteS := mute(th.OnSurface, th.Surface)
	r := s.previewR
	// Pane ground and left divider as composed toolkit.Backdrops, not hand-drawn
	// FillRects.
	surface := &toolkit.Backdrop{Fill: th.Surface}
	surface.SetBounds(r)
	surface.Draw(p, th)
	divider := &toolkit.Backdrop{Fill: th.Border}
	divider.SetBounds(toolkit.Rect{X: r.X, Y: r.Y, W: 1, H: r.H})
	divider.Draw(p, th)
	s.drawGripHandle(p, r.X) // resize grab handle

	if !s.previewHas {
		msg := "Select an item to preview"
		lw := m.side.width(msg)
		lbl := toolkit.NewLabel(msg)
		lbl.Font, lbl.Ink, lbl.VAlign = m.side.font, muteS, toolkit.VTop
		lbl.SetBounds(toolkit.Rect{X: r.X + (r.W-lw)/2, Y: r.Y + r.H/2 - m.side.height/2, W: lw, H: m.side.height})
		lbl.Draw(p, th)
		return
	}

	// A web-linked item: a fixed header over the embedded browser (its own chrome,
	// page and progress bar), not the scrolling image/text column.
	if s.webPreviewItem() {
		s.drawWebPreview(p)
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
	// Reader text scales with the A−/A+ pills (ptPx); the badge-row channel chip
	// stays at the base meta size so it keeps fitting the badge row's height.
	titleFont, bodyFont, metaFont := ttFont(true, s.ptPx(17)), ttFont(false, s.ptPx(14)), ttFont(false, s.ptPx(12))
	chanFont := ttFont(false, rpxOf(s, 12))
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
	badgeRow.AddFlex(mkLabel(truncate(m.meta, it.Channel, d.innerW-bw-rpxOf(s, 6)), chanFont, muteS), 1)

	col := toolkit.NewVBox()
	col.Spacing = -1
	col.AddFixed(badgeRow, m.badgeH)
	col.AddFixed(toolkit.NewLabel(""), gap)
	for _, ln := range d.titleLines {
		col.AddFixed(mkLabel(ln, titleFont, th.OnSurface), d.titleFace.height+rpxOf(s, 2))
	}
	col.AddFixed(toolkit.NewLabel(""), gap)
	col.AddFixed(mkLabel(truncate(d.metaFace, d.meta, d.innerW), metaFont, muteS), d.metaFace.height)
	col.AddFixed(toolkit.NewLabel(""), gap)
	if d.imgH > 0 {
		col.AddFixed(&previewImage{s: s, it: it, p: p}, d.imgH)
		col.AddFixed(toolkit.NewLabel(""), gap)
	}
	for _, ln := range d.bodyLines {
		col.AddFixed(mkLabel(ln, bodyFont, th.OnSurface), d.bodyFace.height+rpxOf(s, 3))
	}
	// Comment thread (Reddit text posts), appended to the SAME scrolling column so
	// it participates in the pane scroll and the cross-surface text selection.
	s.appendComments(col, &d, mkLabel)
	col.SetBounds(toolkit.Rect{X: x, Y: r.Y + m.pad - s.previewScroll.offset, W: d.innerW, H: d.height})
	// Clip the scrolling content to the pane so overflow (notably a tall
	// rendered web page, but also scrolled-up header/body) never paints over the
	// topbar above or the download panel below.
	p.PushClip(painter.Rect(r))
	// Text selection: the preview text joins the feed's cross-surface selection.
	// Its runs are already in screen coords, so add them to the frame accumulator
	// (offset 0,0); the highlight is painted once, translucent, over the whole
	// feed at the end of Draw (drawSelectionOverHighlight), not behind the glyphs.
	s.addSelectableRuns(toolkit.CollectRuns(col), 0, 0)
	col.Draw(p, th)
	p.PopClip()

	// Scrollbar down the pane's right edge when the preview overflows.
	s.drawVScrollbar(p, r, 0, s.previewScroll.contentH, s.previewScroll.offset)

	// "Open" pill (fixed, over the content).
	if s.previewOpenR.W > 0 {
		pill := &previewOpenPill{s: s}
		pill.SetBounds(s.previewOpenR)
		pill.Draw(p, th)
	}
	s.drawTextZoomButtons(p)
}

// appendComments appends the previewed post's comment thread to the scrolling
// column col: a divider, a "N comments" heading (or a loading line while the
// fetch is in flight), then each comment as a muted meta line above its wrapped
// body, indented by depth with a subtle vertical guide. Every piece is a toolkit
// widget the VBox positions, so the comments join the pane scroll and the
// cross-surface text selection (CollectRuns walks the nested boxes). It is a
// no-op when the item has no comment section. The appended heights sum to
// d.sectionH, which layoutComments already folded into the content height.
func (s *Scene) appendComments(col *toolkit.VBox, d *previewBody, mkLabel func(string, toolkit.Font, toolkit.RGBA) *toolkit.Label) {
	if d.commentsHeading == "" && !d.commentsLoading {
		return
	}
	th := s.theme
	muteS := mute(th.OnSurface, th.Surface)
	metaFont := ttFont(false, s.ptPx(11))
	bodyFont := ttFont(false, s.ptPx(13))
	headFont := ttFont(true, s.ptPx(13))
	metaFace := getFace(s.ptPx(11), false)
	bodyFace := getFace(s.ptPx(13), false)

	col.AddFixed(toolkit.NewLabel(""), rpxOf(s, commentGapPt))
	col.AddFixed(&commentDivider{}, rpxOf(s, 1))
	col.AddFixed(toolkit.NewLabel(""), rpxOf(s, commentTightPt))
	if d.commentsHeading != "" {
		col.AddFixed(mkLabel(d.commentsHeading, headFont, th.OnSurface), d.commentsHeadFace.height)
		col.AddFixed(toolkit.NewLabel(""), rpxOf(s, commentTightPt))
	}
	if d.commentsLoading {
		col.AddFixed(mkLabel("Loading comments…", metaFont, muteS), metaFace.height)
	}
	for _, c := range d.comments {
		inner := toolkit.NewVBox()
		inner.Spacing = -1
		inner.AddFixed(mkLabel(c.meta, metaFont, muteS), metaFace.height+rpxOf(s, 2))
		for _, ln := range c.bodyLines {
			inner.AddFixed(mkLabel(ln, bodyFont, th.OnSurface), bodyFace.height+rpxOf(s, 2))
		}
		if c.inset > 0 {
			row := toolkit.NewHBox()
			row.Spacing = 0
			row.AddFixed(&commentGuide{s: s}, c.inset)
			row.AddFlex(inner, 1)
			col.AddFixed(row, c.height)
		} else {
			col.AddFixed(inner, c.height)
		}
		col.AddFixed(toolkit.NewLabel(""), rpxOf(s, commentGapPt))
	}
}

// commentDivider paints a 1px horizontal rule (Border colour) across its bounds:
// the subtle separator between the post body and the comment thread, as a
// composed toolkit.Backdrop rather than a hand-drawn FillRect.
type commentDivider struct {
	toolkit.Base
}

func (w *commentDivider) Draw(pt painter.Painter, th *toolkit.Theme) {
	ground := &toolkit.Backdrop{Fill: th.Border}
	ground.SetBounds(w.Bounds())
	ground.Draw(pt, th)
}

// commentGuide paints a subtle vertical thread guide down the right edge of a
// nested comment's left inset, so replies read as indented under their parent.
// It is a composed toolkit.Backdrop, not a hand-drawn FillRect.
type commentGuide struct {
	toolkit.Base
	s *Scene
}

func (w *commentGuide) Draw(pt painter.Painter, th *toolkit.Theme) {
	b := w.Bounds()
	gx := b.X + b.W - rpxOf(w.s, 1)
	ground := &toolkit.Backdrop{Fill: mute(th.OnSurface, th.Surface)}
	ground.SetBounds(toolkit.Rect{X: gx, Y: b.Y, W: rpxOf(w.s, 1), H: b.H})
	ground.Draw(pt, th)
}

// previewOpenPill is the preview pane's accent "Open" affordance as a
// self-contained composing widget: a rounded accent Backdrop ground carrying a
// left-inset on-accent Label. It is the on-accent inverse of the reading view's
// detailPill (Surface ground + accent label); Button would centre the label and
// stroke a border whose corner anti-aliasing diverges from the old bare fill, so
// this thin pill reproduces the old FillRoundRect + m.side.draw byte for byte.
// Clicks stay routed by previewHitTest (HitOpenPreview), so the Open flow in
// internal/windowapp is unchanged; this widget only paints the visual.
type previewOpenPill struct {
	toolkit.Base
	s *Scene
}

func (w *previewOpenPill) Draw(pt painter.Painter, th *toolkit.Theme) {
	s, b := w.s, w.Bounds()
	m := s.m
	ground := &toolkit.Backdrop{Fill: th.Accent, Radius: rpxOf(s, 6)}
	ground.SetBounds(b)
	ground.Draw(pt, th)
	lbl := toolkit.NewLabel("Open")
	lbl.Font, lbl.Ink, lbl.VAlign = m.side.font, themeOnAccent(th), toolkit.VTop
	lbl.SetBounds(toolkit.Rect{X: b.X + m.pad, Y: b.Y + (b.H-m.side.height)/2, W: b.W - m.pad, H: m.side.height})
	lbl.Draw(pt, th)
}

// previewZoomPill is one A−/A+ reader-text zoom pill: neutral chrome — a
// SurfaceAlt rounded fill with a Border outline and a centred OnSurface Label —
// so it reads as a secondary control beside the accent Open pill. Like the
// reading view's detailPill it is a thin composing widget (there is no stock
// borderless, self-strokable rounded pill); it reproduces the old FillRoundRect
// + StrokeRoundRect + m.side.draw byte for byte through the painter it is handed.
type previewZoomPill struct {
	toolkit.Base
	s     *Scene
	label string
}

func (w *previewZoomPill) Draw(pt painter.Painter, th *toolkit.Theme) {
	s, b := w.s, w.Bounds()
	m := s.m
	rad := rpxOf(s, 6)
	pt.FillRoundRect(painter.Rect{X: b.X, Y: b.Y, W: b.W, H: b.H}, rad, th.SurfaceAlt)
	pt.StrokeRoundRect(painter.Rect{X: b.X, Y: b.Y, W: b.W, H: b.H}, rad, th.Border, rpxOf(s, 1))
	lw := m.side.width(w.label)
	lbl := toolkit.NewLabel(w.label)
	lbl.Font, lbl.Ink, lbl.VAlign = m.side.font, th.OnSurface, toolkit.VTop
	lbl.SetBounds(toolkit.Rect{X: b.X + (b.W-lw)/2, Y: b.Y + (b.H-m.side.height)/2, W: lw, H: m.side.height})
	lbl.Draw(pt, th)
}

// drawTextZoomButtons paints the A−/A+ reader-text zoom pills at the top of the
// preview pane (laid out by layoutPreview, left of the Open pill) as composed
// previewZoomPill widgets. Shared by the text and web preview draws so the pills
// show for any previewed post. Both call sites run only with an item selected,
// so layoutPreview has sized the pills.
func (s *Scene) drawTextZoomButtons(p *painter.PixelPainter) {
	for _, b := range []struct {
		r     toolkit.Rect
		label string
	}{{s.sTextSmallerR, "A−"}, {s.sTextLargerR, "A+"}} {
		pill := &previewZoomPill{s: s, label: b.label}
		pill.SetBounds(b.r)
		pill.Draw(p, s.theme)
	}
}

// drawWebPreview paints a web-linked item: a fixed header (badge / title / meta)
// across the top, then the embedded toolkit.Browser (its own tab strip, toolbar,
// address field, scrollable page and loading bar) filling the rest, with the
// "Open" pill floated on top. The browser was positioned by layoutPreview.
func (s *Scene) drawWebPreview(p *painter.PixelPainter) {
	m := s.m
	th := s.theme
	muteS := mute(th.OnSurface, th.Surface)
	r := s.previewR
	it := s.previewItem
	gap := rpxOf(s, 8)
	x, innerW := s.previewInner()

	// Header text scales with the A−/A+ pills (ptPx); the badge-row channel chip
	// stays at the base meta size so it keeps fitting the badge row's height.
	metaFont := ttFont(false, s.ptPx(12))
	titleFont := ttFont(true, s.ptPx(17))
	chanFont := ttFont(false, rpxOf(s, 12))
	metaFace := getFace(s.ptPx(12), false)
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
	badgeRow.AddFlex(mkLabel(truncate(m.meta, it.Channel, innerW-bw-rpxOf(s, 6)), chanFont, muteS), 1)

	titleFace := getFace(s.ptPx(17), true)
	col := toolkit.NewVBox()
	col.Spacing = -1
	col.AddFixed(badgeRow, m.badgeH)
	col.AddFixed(toolkit.NewLabel(""), gap)
	for _, ln := range s.previewHeaderLines() {
		col.AddFixed(mkLabel(ln, titleFont, th.OnSurface), titleFace.height+rpxOf(s, 2))
	}
	col.AddFixed(toolkit.NewLabel(""), gap)
	col.AddFixed(mkLabel(truncate(metaFace, metaLine(it), innerW), metaFont, muteS), metaFace.height)
	hh := s.previewHeaderH()
	col.SetBounds(toolkit.Rect{X: x, Y: r.Y + m.pad, W: innerW, H: hh - m.pad})
	col.Draw(p, th)

	// The embedded browser fills the pane below the header.
	s.browser.Draw(p, th)

	// "Open" pill floated over the header, top-right.
	if s.previewOpenR.W > 0 {
		pill := &previewOpenPill{s: s}
		pill.SetBounds(s.previewOpenR)
		pill.Draw(p, th)
	}
	s.drawTextZoomButtons(p)
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
	// The A−/A+ reader-text zoom pills (shown for any previewed post).
	if s.sTextSmallerR.W > 0 && inRect(s.sTextSmallerR, x, y) {
		return Hit{Kind: HitPreviewTextSmaller}, true
	}
	if s.sTextLargerR.W > 0 && inRect(s.sTextLargerR, x, y) {
		return Hit{Kind: HitPreviewTextLarger}, true
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
