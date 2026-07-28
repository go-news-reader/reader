package ui

// A thin status bar across the very bottom of the feed showing, per newsgroup in
// view, how many posts (articles) the group carries — the NNTP GROUP estimate
// surfaced on each item. Rendered with the toolkit Statusbar widget.

import (
	"fmt"
	"image"
	"sort"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"

	"github.com/go-news-reader/reader/source"
)

// statusBarH is the bottom status bar's height (feed view, when there are group
// post-counts to show), else 0.
func (s *Scene) statusBarH() int {
	if s.mode != ModeFeed || len(s.statusSegs) == 0 {
		return 0
	}
	return rpxOf(s, 20)
}

// statusBarRect is the bar's rect (docked at the very bottom, feed+preview
// width), or empty when hidden.
func (s *Scene) statusBarRect() toolkit.Rect {
	h := s.statusBarH()
	if h == 0 {
		return toolkit.Rect{}
	}
	return toolkit.Rect{X: s.m.sidebarW, Y: s.H - h, W: s.W - s.m.sidebarW, H: h}
}

// drawStatusBar paints the per-group post counts via the toolkit Statusbar.
func (s *Scene) drawStatusBar(p *painter.PixelPainter, _ *image.RGBA) {
	r := s.statusBarRect()
	if r.W == 0 {
		return
	}
	sb := &toolkit.Statusbar{Segments: s.statusSegs}
	sb.Font = ttFont(false, rpxOf(s, 11))
	sb.SetBounds(r)
	sb.Draw(p, s.theme)
}

// groupCountSegs builds "<group>  N posts" segments for the newsgroups present in
// items (those that carry a GroupCount), sorted by group name.
func groupCountSegs(items []source.Item) []string {
	counts := map[string]int{}
	for _, it := range items {
		if it.GroupCount > 0 && it.Channel != "" && it.GroupCount > counts[it.Channel] {
			counts[it.Channel] = it.GroupCount
		}
	}
	if len(counts) == 0 {
		return nil
	}
	names := make([]string, 0, len(counts))
	for n := range counts {
		names = append(names, n)
	}
	sort.Strings(names)
	segs := make([]string, len(names))
	for i, n := range names {
		segs[i] = fmt.Sprintf("%s  %d posts", n, counts[n])
	}
	return segs
}
