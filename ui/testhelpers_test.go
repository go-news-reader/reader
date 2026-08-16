package ui

import (
	"github.com/go-widgets/toolkit"

	"github.com/go-news-reader/reader/source"
)

// regionHas reports whether the RGBA buffer has a pixel of colour c inside rect.
func regionHas(buf []byte, w int, r toolkit.Rect, c toolkit.RGBA) bool {
	for y := r.Y; y < r.Y+r.H; y++ {
		for x := r.X; x < r.X+r.W; x++ {
			if y < 0 || x < 0 {
				continue
			}
			p := px(buf, w, x, y)
			if p.R == c.R && p.G == c.G && p.B == c.B {
				return true
			}
		}
	}
	return false
}

// wrapScene is a feed scene with no sidebar filter, ready for feed assertions.
func wrapScene() *Scene {
	s := New(700, 600, ThemeFor(OSLinux, false))
	s.SetSubs(nil)
	return s
}

// itoaTest is a tiny base-10 int formatter for building distinct test item IDs.
func itoaTest(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

// groupScene builds a feed with one groupable 3-part Usenet post followed by a
// standalone (non-usenet) card, on a scene with no sidebar filter. The feed now
// renders the multipart post as a single summary card (a follow-up GroupCard
// will restore inline expand/download).
func groupScene() *Scene {
	s := New(900, 600, ThemeFor(OSLinux, false))
	s.SetSubs(nil)
	s.SetItems([]source.Item{
		usenetItem("d1", `[1/3] - "release.tar.zst" yEnc (1/1) 100000`),
		usenetItem("d2", `[2/3] - "release.tar.zst.par2" yEnc (1/1) 940`),
		usenetItem("d3", `[3/3] - "release.tar.zst.vol00+01.par2" yEnc (1/1) 2048`),
		{ID: "r1", Source: source.Reddit, Channel: "golang", Title: "a normal card", Author: "gopher", Score: 3, Comments: 1},
	})
	s.layout()
	return s
}

// groupDisplayIndex returns the feed-display row index of the (single) Usenet
// group summary card in s, or -1 when the feed shows no group.
func groupDisplayIndex(s *Scene) int {
	s.ensureFeed()
	s.layout()
	for i, e := range s.feed.display {
		if e.group != nil {
			return i
		}
	}
	return -1
}

// feedRowScreenY returns the on-screen Y centre of feed display row i (through
// the CardList's placement + scroll offset), for hit-testing a card.
func feedRowScreenY(s *Scene, i int) int {
	lr := s.feed.list.Bounds()
	acc := 0
	for j := 0; j < i; j++ {
		acc += s.feedRowHeight(j)
	}
	return lr.Y - s.FeedScrollOffset() + acc + s.feedRowHeight(i)/2
}
