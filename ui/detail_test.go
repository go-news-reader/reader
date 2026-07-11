package ui

import (
	"image"
	"strings"
	"testing"

	"github.com/go-widgets/toolkit"

	"github.com/go-news-reader/reader/source"
)

func TestOpenCloseDetail(t *testing.T) {
	s := newScene()
	if s.Mode() != ModeFeed {
		t.Fatal("default mode should be feed")
	}
	it := source.Item{ID: "x", Source: source.Reddit, Title: "Deep dive", Link: "https://x"}
	s.ScrollY = 40
	s.OpenDetail(it)
	if s.Mode() != ModeDetail || s.Detail().ID != "x" || s.detailScrollY != 0 {
		t.Fatalf("open detail state: mode=%v item=%q", s.Mode(), s.Detail().ID)
	}
	s.CloseDetail()
	if s.Mode() != ModeFeed {
		t.Fatal("close should return to feed")
	}
}

func TestDetailURL(t *testing.T) {
	s := newScene()
	s.detail = source.Item{Link: "https://l", Permalink: "https://p"}
	if s.detailURL() != "https://l" {
		t.Fatal("Link preferred")
	}
	s.detail = source.Item{Permalink: "https://p"}
	if s.detailURL() != "https://p" {
		t.Fatal("Permalink fallback")
	}
	s.detail = source.Item{}
	if s.detailURL() != "" {
		t.Fatal("no url")
	}
}

func TestDrawDetail(t *testing.T) {
	s := newScene()
	s.OpenDetail(source.Item{
		ID: "1", Source: source.Mastodon, Channel: "@a@m", Title: "A long headline that certainly needs wrapping across several lines of the reading view",
		Author: "a@m", Score: 12, Comments: 3, Link: "https://example.com/post",
		Body:  "<p>First paragraph with <b>markup</b> &amp; entities.</p><p>Second paragraph.</p>",
		Media: []source.Media{{Kind: source.MediaImage, URL: "u"}},
	})
	buf := renderPNG(t, s, "detail")
	// The detail topbar is an accent fill; sample a strip clear of the buttons.
	acc := s.theme.Accent
	got := px(buf, s.W, s.W/2, 4)
	if got.R != acc.R || got.G != acc.G || got.B != acc.B {
		t.Fatalf("detail topbar pixel = %v, want accent %v", got, acc)
	}
	// Back button hits.
	m := s.computeMetrics()
	if s.HitTest(m.pad+5, m.topbarH/2).Kind != HitBack {
		t.Fatal("back button hit")
	}
	// Open-original hits (right side).
	if h := s.HitTest(s.W-m.pad-10, m.topbarH/2); h.Kind != HitOpenExternal || h.Item.ID != "1" {
		t.Fatalf("open-external hit = %+v", h)
	}
	// A click in the body area is none.
	if s.HitTest(s.W/2, s.H-10).Kind != HitNone {
		t.Fatal("body click should be none")
	}
}

func TestDrawDetailMinimalAndThumb(t *testing.T) {
	// No URL, no media, no body -> open button absent; body loop empty.
	s := New(500, 360, nil)
	s.OpenDetail(source.Item{ID: "m", Source: source.HackerNews, Title: "Bare", Score: -1, Comments: -1})
	renderPNG(t, s, "detail-min")
	m := s.computeMetrics()
	if s.openR != (toolkit.Rect{}) {
		t.Fatalf("open rect should be zero without a URL: %+v", s.openR)
	}
	// no-URL open area -> none
	if s.HitTest(s.W-m.pad-10, m.topbarH/2).Kind != HitNone {
		t.Fatal("no-url open area should be none")
	}
	// With a decoded thumbnail, the media slot blits it.
	th := image.NewRGBA(image.Rect(0, 0, 40, 40))
	s.OpenDetail(source.Item{ID: "t", Title: "Pic", Media: []source.Media{{Kind: source.MediaImage}}})
	s.Thumbs = map[string]*image.RGBA{"t": th}
	renderPNG(t, s, "detail-thumb")
}

func TestDetailScroll(t *testing.T) {
	s := New(400, 300, nil)
	body := strings.Repeat("A reasonably long sentence of body copy that wraps. ", 80)
	s.OpenDetail(source.Item{ID: "long", Title: "Long", Body: body, Score: -1, Comments: -1})
	buf := make([]byte, s.W*s.H*4)
	s.Draw(buf) // establishes detailContentH + exercises the offscreen body-skip
	s.Scroll(1 << 20)
	if s.detailScrollY <= 0 {
		t.Fatalf("scroll did not advance: %d", s.detailScrollY)
	}
	max := s.detailScrollY
	s.Scroll(-1 << 20)
	if s.detailScrollY != 0 {
		t.Fatalf("neg clamp = %d", s.detailScrollY)
	}
	s.Scroll(max / 2)
	s.Draw(buf) // draw scrolled (top body lines now above the viewport)
	if s.detailScrollY != max/2 {
		t.Fatalf("mid scroll = %d", s.detailScrollY)
	}
}

func TestWrapText(t *testing.T) {
	f := getFace(14, false)
	if wrapText(f, "   ", 100) != nil {
		t.Fatal("blank -> nil")
	}
	if got := wrapText(f, "hi", 10000); len(got) != 1 || got[0] != "hi" {
		t.Fatalf("fits = %v", got)
	}
	if got := wrapText(f, "one two three four five six seven eight nine ten", 60); len(got) < 2 {
		t.Fatalf("should wrap: %v", got)
	}
	// A single word wider than maxW stays on its own line.
	if got := wrapText(f, "supercalifragilisticexpialidocious", 20); len(got) != 1 {
		t.Fatalf("long word = %v", got)
	}
	// Paragraph break preserved as a blank line.
	got := wrapText(f, "a\n\nb", 1000)
	if len(got) != 3 || got[1] != "" {
		t.Fatalf("paragraphs = %v", got)
	}
}

func TestStripHTML(t *testing.T) {
	if stripHTML("") != "" {
		t.Fatal("empty")
	}
	if got := stripHTML("<p>Hi</p>"); !strings.HasPrefix(got, "Hi") {
		t.Fatalf("p = %q", got)
	}
	if stripHTML("a<br>b") != "a\nb" {
		t.Fatalf("br")
	}
	if got := stripHTML(`<a href="x">link</a>`); got != "link" {
		t.Fatalf("tag strip = %q", got)
	}
	if got := stripHTML("&amp;&lt;&gt;&quot;&#39;&nbsp;z"); got != "&<>\"' z" {
		t.Fatalf("entities = %q", got)
	}
	if got := stripHTML("</li>x"); got != "\nx" {
		t.Fatalf("li = %q", got)
	}
}
