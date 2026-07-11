package ui

import (
	"image"
	"strings"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
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
	s.detailContentH = s.detailContent().height
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
	d := detailBody{
		x: x, w: w, titleFace: titleFace, bodyFace: bodyFace,
		titleLines: wrapText(titleFace, it.Title, w),
		bodyLines:  wrapText(bodyFace, stripHTML(it.Body), w),
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

	p.FillRect(painter.Rect{X: 0, Y: 0, W: s.W, H: s.H}, th.Background)

	// --- content (scrolled, below the topbar) ---
	x := d.x
	y := m.topbarH + m.pad - s.detailScrollY
	label := sourceLabel(it.Source)
	bw := m.badge.width(label) + m.pad
	p.FillRoundRect(painter.Rect{X: x, Y: y, W: bw, H: m.badgeH}, m.badgeH/2, sourceColor(it.Source))
	m.badge.draw(img, x+m.pad/2, y+(m.badgeH-m.badge.height)/2, label, rgb(0xFFFFFF))
	if it.Channel != "" {
		m.meta.draw(img, x+bw+m.pad/2, y+(m.badgeH-m.meta.height)/2, it.Channel, muteS)
	}
	y += m.badgeH + gap

	tlh := d.titleFace.height + rpxOf(s, 2)
	for _, ln := range d.titleLines {
		d.titleFace.draw(img, x, y, ln, th.OnSurface)
		y += tlh
	}
	y += gap
	m.meta.draw(img, x, y, d.meta, muteS)
	y += m.meta.height + gap

	if len(it.Media) > 0 {
		r := toolkit.Rect{X: x, Y: y, W: d.w, H: m.thumbH * 2}
		p.FillRect(painter.Rect(r), th.SurfaceAlt)
		if t, ok := s.Thumbs[it.ID]; ok && t != nil {
			blit(img, t, r.X, r.Y, r.W, r.H)
		} else {
			lbl := string(it.Media[0].Kind)
			m.meta.draw(img, x+(d.w-m.meta.width(lbl))/2, y+m.thumbH-m.meta.height/2, lbl, muteS)
		}
		y += m.thumbH*2 + gap
	}

	blh := d.bodyFace.height + rpxOf(s, 3)
	for _, ln := range d.bodyLines {
		if y+blh >= m.topbarH && y < s.H {
			d.bodyFace.draw(img, x, y, ln, th.OnSurface)
		}
		y += blh
	}

	// --- topbar chrome (over content) ---
	p.FillRect(painter.Rect{X: 0, Y: 0, W: s.W, H: m.topbarH}, th.Accent)
	p.FillRoundRect(painter.Rect(s.backR), rpxOf(s, 6), th.Surface)
	m.side.draw(img, s.backR.X+m.pad, s.backR.Y+(s.backR.H-m.side.height)/2, "< Back", th.Accent)
	if s.detailURL() != "" {
		p.FillRoundRect(painter.Rect(s.openR), rpxOf(s, 6), th.Surface)
		m.side.draw(img, s.openR.X+m.pad, s.openR.Y+(s.openR.H-m.side.height)/2, "Open original", th.Accent)
	}
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

// wrapText greedily word-wraps text to maxW pixels in face, preserving paragraph
// breaks ("\n"). A word longer than maxW is left un-broken on its own line.
func wrapText(face textFace, text string, maxW int) []string {
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
			if line == "" || face.width(try) <= maxW {
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
