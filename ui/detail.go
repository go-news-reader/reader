package ui

import (
	"image"
	"strings"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"

	"github.com/go-news-reader/reader/source"
)

// detailURL is the external link for the open item (Link preferred, else the
// canonical permalink). Empty means there is nothing to open externally.
func (s *Scene) detailURL() string {
	if s.detail.Link != "" {
		return s.detail.Link
	}
	return s.detail.Permalink
}

// layoutDetail computes metrics, the back / open-original button rects, and the
// total content height (for scroll clamping).
func (s *Scene) layoutDetail() {
	s.clampSize()
	s.m = s.computeMetrics()
	m := s.m
	y := (m.topbarH - m.searchH) / 2
	s.backR = toolkit.Rect{X: m.pad, Y: y, W: m.pad*2 + m.side.width("< Back"), H: m.searchH}
	if s.detailURL() != "" {
		ow := m.pad*2 + m.side.width("Open original")
		s.openR = toolkit.Rect{X: s.W - m.pad - ow, Y: y, W: ow, H: m.searchH}
	} else {
		s.openR = toolkit.Rect{}
	}
	s.detailScroll.refresh(s.detailContent().height, s.H-m.topbarH)
}

// detailContent lays out (wraps + measures) the reading-view body. It is shared
// by layoutDetail (for the scroll height) and drawDetail (for painting).
type detailBody struct {
	x, w                int
	titleFace, bodyFace textFace
	titleLines          []string
	bodyLines           []string
	meta                string
	height              int
}

func (s *Scene) detailContent() detailBody {
	m := s.m
	x := m.pad * 3
	w := s.W - x*2
	if max := rpxOf(s, 720); w > max {
		w = max
	}
	titleFace := getFace(rpxOf(s, 22), true)
	bodyFace := getFace(rpxOf(s, 15), false)
	it := s.detail
	// Wrap with the SAME fonts drawDetail draws these lines with (stock toolkit
	// Labels carrying ttFont), not with the textFace whose heights size the rows.
	// wrapMeasured says so explicitly, and the feed card already obeys it: a line
	// that "fits" one rasteriser can overflow the other and be clipped mid-word.
	// Composed emoji widened the gap — a ZWJ sequence is two glyphs to the
	// x/image measurer and one to the shaper that actually draws it.
	titleWrap := ttFont(true, rpxOf(s, 22))
	bodyWrap := ttFont(false, rpxOf(s, 15))
	d := detailBody{
		x: x, w: w, titleFace: titleFace, bodyFace: bodyFace,
		titleLines: wrapMeasured(titleWrap.Measure, it.Title, w),
		bodyLines:  wrapMeasured(bodyWrap.Measure, stripHTML(it.Body), w),
		meta:       metaLine(it),
	}
	gap := rpxOf(s, 10)
	h := m.pad + m.badgeH + gap
	h += len(d.titleLines) * (titleFace.height + rpxOf(s, 2))
	h += gap + m.meta.height + gap
	if len(it.Media) > 0 {
		h += m.thumbH*2 + gap
	}
	h += len(d.bodyLines) * (bodyFace.height + rpxOf(s, 3))
	h += m.pad
	d.height = h
	return d
}

// detailImage is the reading view's image cell as a composed toolkit widget so
// the box layout positions it: a SurfaceAlt Backdrop ground carrying either the
// decoded thumbnail or a kind Label placeholder.
type detailImage struct {
	toolkit.Base
	s   *Scene
	it  source.Item
	img *image.RGBA
}

// Draw composes the image cell — a solid toolkit.Backdrop ground, then either the
// decoded thumbnail or a centred kind Label — with nothing hand-drawn. The
// thumbnail is blitted at native size, top-left (the reading view crops it to the
// box, unlike the feed card's aspect-fit), so it keeps the reader's own image
// primitive rather than toolkit.Image's stretch/fit.
func (w *detailImage) Draw(pt painter.Painter, th *toolkit.Theme) {
	b := w.Bounds()
	ground := &toolkit.Backdrop{Fill: th.SurfaceAlt}
	ground.SetBounds(b)
	ground.Draw(pt, th)
	if t, ok := w.s.Thumbs[w.it.ID]; ok && t != nil {
		blit(w.img, t, b.X, b.Y, b.W, b.H)
		return
	}
	lbl := string(w.it.Media[0].Kind)
	m := w.s.m
	tw := m.meta.width(lbl)
	ph := toolkit.NewLabel(lbl)
	ph.Font, ph.Ink, ph.VAlign = m.meta.font, mute(th.OnSurface, th.Surface), toolkit.VTop
	ph.SetBounds(toolkit.Rect{X: b.X + (b.W-tw)/2, Y: b.Y + b.H/2 - m.meta.height/2, W: tw, H: m.meta.height})
	ph.Draw(pt, th)
}

// drawDetail renders the in-app reading view for the open item.
func (s *Scene) drawDetail(buf []byte) {
	s.layoutDetail()
	m := s.m
	th := s.theme
	muteS := mute(th.OnSurface, th.Surface)
	p := painter.NewPixelPainter(buf, s.W, s.H)
	img := &image.RGBA{Pix: buf, Stride: s.W * 4, Rect: image.Rect(0, 0, s.W, s.H)}
	d := s.detailContent()
	it := s.detail
	gap := rpxOf(s, 10)

	// Window ground: a solid toolkit.Backdrop panel, not a hand-drawn FillRect.
	bg := &toolkit.Backdrop{Fill: th.Background}
	bg.SetBounds(toolkit.Rect{X: 0, Y: 0, W: s.W, H: s.H})
	bg.Draw(p, th)

	// --- content (scrolled, below the topbar), composed as a toolkit VBox ---
	x := d.x
	// Content text is stock toolkit.Label carrying the reader's fallback fonts.
	titleFont, bodyFont, metaFont := ttFont(true, rpxOf(s, 22)), ttFont(false, rpxOf(s, 15)), ttFont(false, rpxOf(s, 12))
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
	badgeRow.AddFlex(mkLabel(truncate(m.meta, it.Channel, d.w-bw-rpxOf(s, 6)), metaFont, muteS), 1)

	col := toolkit.NewVBox()
	col.Spacing = -1
	col.AddFixed(badgeRow, m.badgeH)
	col.AddFixed(toolkit.NewLabel(""), gap)
	for _, ln := range d.titleLines {
		col.AddFixed(mkLabel(ln, titleFont, th.OnSurface), d.titleFace.height+rpxOf(s, 2))
	}
	col.AddFixed(toolkit.NewLabel(""), gap)
	col.AddFixed(mkLabel(d.meta, metaFont, muteS), m.meta.height)
	col.AddFixed(toolkit.NewLabel(""), gap)
	if len(it.Media) > 0 {
		col.AddFixed(&detailImage{s: s, it: it, img: img}, m.thumbH*2)
		col.AddFixed(toolkit.NewLabel(""), gap)
	}
	for _, ln := range d.bodyLines {
		col.AddFixed(mkLabel(ln, bodyFont, th.OnSurface), d.bodyFace.height+rpxOf(s, 3))
	}
	col.SetBounds(toolkit.Rect{X: x, Y: m.topbarH + m.pad - s.detailScroll.offset, W: d.w, H: d.height})
	// Text selection: collect the runs just laid out, paint the highlight behind
	// the text (opaque tint reads clean under the glyphs), then draw the text.
	s.setSelectableRuns(toolkit.CollectRuns(col))
	s.drawSelectionHighlight(p)
	col.Draw(p, th)

	// Scrollbar down the right edge when the article overflows the viewport.
	s.drawVScrollbar(p, toolkit.Rect{X: 0, Y: m.topbarH, W: s.W, H: s.H - m.topbarH}, 0, s.detailScroll.contentH, s.detailScroll.offset)

	// --- topbar chrome (over content), fully composed ---
	// Accent band + Back / Open original as composed detailPill widgets (a rounded
	// Surface pill carrying an accent Label). Nothing here is hand-drawn.
	band := &toolkit.Backdrop{Fill: th.Accent}
	band.SetBounds(toolkit.Rect{X: 0, Y: 0, W: s.W, H: m.topbarH})
	band.Draw(p, th)

	back := &detailPill{s: s, label: "< Back"}
	back.SetBounds(s.backR)
	back.Draw(p, th)
	if s.detailURL() != "" {
		open := &detailPill{s: s, label: "Open original"}
		open.SetBounds(s.openR)
		open.Draw(p, th)
	}
}

// detailPill is the reading view's topbar affordance (Back / Open original) as a
// self-contained composing widget: a rounded Surface pill carrying an accent
// Label, left-inset by m.pad. There is no stock toolkit widget for a borderless,
// left-aligned rounded pill — Button centres its label and strokes a border,
// whose corner anti-aliasing would diverge from the old bare fill — so this thin
// composing widget mirrors the merged logBackButton and reproduces the old
// FillRoundRect + m.side.draw byte for byte. Clicks stay routed by detailHitTest
// (HitBack / HitOpenExternal), so the Back / Open flows in internal/windowapp are
// unchanged; this widget only paints the visual.
type detailPill struct {
	toolkit.Base
	s     *Scene
	label string
}

func (w *detailPill) Draw(pt painter.Painter, th *toolkit.Theme) {
	s, b := w.s, w.Bounds()
	m := s.m
	ground := &toolkit.Backdrop{Fill: th.Surface, Radius: rpxOf(s, 6)}
	ground.SetBounds(b)
	ground.Draw(pt, th)
	lbl := toolkit.NewLabel(w.label)
	lbl.Font, lbl.Ink, lbl.VAlign = m.side.font, th.Accent, toolkit.VTop
	lbl.SetBounds(toolkit.Rect{X: b.X + m.pad, Y: b.Y + (b.H-m.side.height)/2, W: b.W - m.pad, H: m.side.height})
	lbl.Draw(pt, th)
}

// detailHitTest maps a click in the detail view to Back / OpenExternal / None.
func (s *Scene) detailHitTest(x, y int) Hit {
	s.layoutDetail()
	if inRect(s.backR, x, y) {
		return Hit{Kind: HitBack}
	}
	if s.detailURL() != "" && inRect(s.openR, x, y) {
		return Hit{Kind: HitOpenExternal, Item: s.detail}
	}
	return Hit{Kind: HitNone}
}

// wrapMeasured is the width-measurer-agnostic core of wrapText: it greedily
// word-wraps text to maxW pixels, deciding each break with measure(s). Callers
// whose text is rendered by a toolkit.Font (not a textFace) must pass that
// font's Measure so the wrap is computed against the SAME metrics the glyphs
// draw with — otherwise a line that "fits" the measurer can still overflow (and
// be clipped mid-word by) the render font. Paragraph breaks ("\n") are kept; a
// word wider than maxW is left un-broken on its own line.
func wrapMeasured(measure func(string) int, text string, maxW int) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	var out []string
	for _, para := range strings.Split(text, "\n") {
		if strings.TrimSpace(para) == "" {
			out = append(out, "")
			continue
		}
		line := ""
		for _, word := range strings.Fields(para) {
			try := word
			if line != "" {
				try = line + " " + word
			}
			if line == "" || measure(try) <= maxW {
				line = try
			} else {
				out = append(out, line)
				line = word
			}
		}
		out = append(out, line)
	}
	return out
}

// stripHTML turns a fragment of HTML (Mastodon/HN bodies) into readable plain
// text: block/break tags become newlines, other tags are dropped, and common
// entities are decoded.
func stripHTML(s string) string {
	if s == "" {
		return ""
	}
	s = strings.NewReplacer(
		"<br>", "\n", "<br/>", "\n", "<br />", "\n",
		"</p>", "\n\n", "</div>", "\n", "</li>", "\n",
	).Replace(s)
	var b strings.Builder
	depth := 0
	for _, r := range s {
		switch {
		case r == '<':
			depth++
		case r == '>':
			if depth > 0 {
				depth--
			}
		case depth == 0:
			b.WriteRune(r)
		}
	}
	return strings.NewReplacer(
		"&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", "\"",
		"&#39;", "'", "&apos;", "'", "&nbsp;", " ",
	).Replace(b.String())
}
