package ui

import (
	"strings"
	"testing"

	"github.com/go-widgets/toolkit"

	"github.com/go-news-reader/reader/internal/settings"
	"github.com/go-news-reader/reader/source"
)

// textPost is a Reddit self/text post (a non-empty Body and no external Link),
// so the pane renders it as scrollable reader text — the content the A−/A+ pills
// scale — rather than as an embedded web page.
func textPost() source.Item {
	return source.Item{
		ID: "t", Source: source.Reddit, Title: "A reasonably long headline that wraps",
		Author: "alice", Channel: "golang", Score: 42,
		Body: strings.Repeat("the quick brown fox jumps over the lazy dog ", 30),
	}
}

// TestPreviewTextScaleClamp guards the scene setter/getter: values outside the
// supported range clamp to the bounds, a value inside is kept verbatim.
func TestPreviewTextScaleClamp(t *testing.T) {
	s := New(1200, 800, ThemeFor(OSMac, false))
	if got := s.PreviewTextScale(); got != settings.DefaultPreviewTextScale {
		t.Fatalf("fresh scene scale = %v, want default %v", got, settings.DefaultPreviewTextScale)
	}
	s.SetPreviewTextScale(1.4)
	if got := s.PreviewTextScale(); got != 1.4 {
		t.Fatalf("in-range scale = %v, want 1.4", got)
	}
	s.SetPreviewTextScale(9) // above max
	if got := s.PreviewTextScale(); got != settings.MaxPreviewTextScale {
		t.Fatalf("over-max scale = %v, want %v", got, settings.MaxPreviewTextScale)
	}
	s.SetPreviewTextScale(0.1) // below min
	if got := s.PreviewTextScale(); got != settings.MinPreviewTextScale {
		t.Fatalf("under-min scale = %v, want %v", got, settings.MinPreviewTextScale)
	}
}

// TestPreviewTextScaleReflows asserts a larger scale grows the body font and
// re-wraps the text into MORE lines (taller content), never a wider column.
func TestPreviewTextScaleReflows(t *testing.T) {
	s := New(1200, 800, ThemeFor(OSMac, false))
	s.SetSubs(nil)
	it := textPost()
	s.SetItems([]source.Item{it})
	s.SelectPreview(it)

	buf := make([]byte, s.W*s.H*4)
	s.SetPreviewTextScale(1.0)
	s.Draw(buf)
	small := s.previewContent()

	s.SetPreviewTextScale(settings.MaxPreviewTextScale)
	s.Draw(buf)
	big := s.previewContent()

	if !(big.bodyFace.height > small.bodyFace.height) {
		t.Fatalf("body font did not grow with scale: %d !> %d", big.bodyFace.height, small.bodyFace.height)
	}
	if !(len(big.bodyLines) > len(small.bodyLines)) {
		t.Fatalf("bigger font should wrap into more lines: %d !> %d", len(big.bodyLines), len(small.bodyLines))
	}
	if !(big.height > small.height) {
		t.Fatalf("content height should grow with scale: %d !> %d", big.height, small.height)
	}
}

// TestPreviewTextScaleNoHorizontalShift is the regression test for the report
// that enlarging the font shifted everything right off-screen: the content
// column's x-origin and width must stay pinned to the (fixed) pane inner box at
// every scale, and every wrapped line must measure within that width — so only
// the VERTICAL extent grows.
func TestPreviewTextScaleNoHorizontalShift(t *testing.T) {
	s := New(1200, 800, ThemeFor(OSMac, false))
	s.SetSubs(nil)
	it := textPost()
	s.SetItems([]source.Item{it})
	s.SelectPreview(it)

	buf := make([]byte, s.W*s.H*4)
	s.SetPreviewTextScale(1.0)
	s.Draw(buf)
	base := s.previewContent()
	innerX, innerW := s.previewInner()

	s.SetPreviewTextScale(settings.MaxPreviewTextScale)
	s.Draw(buf)
	big := s.previewContent()

	// Pane inner box is unchanged by the font scale.
	if bx, bw := s.previewInner(); bx != innerX || bw != innerW {
		t.Fatalf("pane inner box moved with scale: (%d,%d) != (%d,%d)", bx, bw, innerX, innerW)
	}
	// Content column stays pinned to the pane inner box (no rightward offset, no
	// widening) even at max scale.
	if big.innerX != innerX {
		t.Fatalf("content x-origin shifted with scale: %d != %d", big.innerX, innerX)
	}
	if big.innerX != base.innerX {
		t.Fatalf("content x-origin changed between scales: %d != %d", big.innerX, base.innerX)
	}
	if big.innerW > innerW {
		t.Fatalf("content width overflowed the pane: %d > %d", big.innerW, innerW)
	}
	// Every wrapped title/body line fits inside the pane inner width at max scale.
	for _, ln := range big.titleLines {
		if w := big.titleFace.width(ln); w > innerW {
			t.Fatalf("title line overflows pane width: %q %d > %d", ln, w, innerW)
		}
	}
	for _, ln := range big.bodyLines {
		if w := big.bodyFace.width(ln); w > innerW {
			t.Fatalf("body line overflows pane width: %q %d > %d", ln, w, innerW)
		}
	}
	// The vertical extent must have grown (the whole point of the scale-up).
	if !(big.height > base.height) {
		t.Fatalf("content height did not grow: %d !> %d", big.height, base.height)
	}
}

// TestTextZoomButtonsDrawAndHit checks the A−/A+ pills are laid out (non-empty,
// same size, left of the Open pill without overlap) and hit-test to their kinds
// at their centres, for both a text post and a web post.
func TestTextZoomButtonsDrawAndHit(t *testing.T) {
	check := func(t *testing.T, it source.Item) {
		th := ThemeFor(OSMac, false)
		s := New(1200, 800, th)
		s.SetSubs(nil)
		s.SetItems([]source.Item{it})
		s.SelectPreview(it)
		buf := make([]byte, s.W*s.H*4)
		s.Draw(buf)

		smaller, larger, shown := s.TextZoomButtons()
		if !shown || smaller.W == 0 || larger.W == 0 {
			t.Fatalf("zoom buttons not shown: %+v %+v shown=%v", smaller, larger, shown)
		}
		if smaller.W != larger.W || smaller.H != larger.H {
			t.Fatalf("zoom pills not equal-sized: %+v vs %+v", smaller, larger)
		}
		// Both pills sit inside the pane and left of (not overlapping) the Open pill.
		open, _ := s.PreviewOpenButton()
		if smaller.X < s.previewR.X || larger.X+larger.W > open.X {
			t.Fatalf("pills overlap the pane edge / Open pill: A-%+v A+%+v open%+v pane%+v", smaller, larger, open, s.previewR)
		}
		if smaller.X+smaller.W > larger.X {
			t.Fatalf("A− overlaps A+: %+v %+v", smaller, larger)
		}
		if h := s.HitTest(smaller.X+smaller.W/2, smaller.Y+smaller.H/2); h.Kind != HitPreviewTextSmaller {
			t.Fatalf("A− centre hit = %v, want HitPreviewTextSmaller", h.Kind)
		}
		if h := s.HitTest(larger.X+larger.W/2, larger.Y+larger.H/2); h.Kind != HitPreviewTextLarger {
			t.Fatalf("A+ centre hit = %v, want HitPreviewTextLarger", h.Kind)
		}
		// Visual proof the pills actually painted: each pill's interior carries
		// its SurfaceAlt fill (distinct from the pane Surface behind it), and its
		// glyph ink (OnSurface, distinct from the fill) shows somewhere inside.
		hasPixel := func(r toolkit.Rect, c toolkit.RGBA) bool {
			for y := r.Y + 2; y < r.Y+r.H-2; y++ {
				for x := r.X + 2; x < r.X+r.W-2; x++ {
					i := (y*s.W + x) * 4
					if buf[i] == c.R && buf[i+1] == c.G && buf[i+2] == c.B {
						return true
					}
				}
			}
			return false
		}
		for _, pr := range []toolkit.Rect{smaller, larger} {
			if th.SurfaceAlt == th.Surface {
				t.Fatal("test theme cannot distinguish pill fill from pane surface")
			}
			if !hasPixel(pr, th.SurfaceAlt) {
				t.Fatalf("pill %+v has no SurfaceAlt fill (not painted)", pr)
			}
			if !hasPixel(pr, th.OnSurface) {
				t.Fatalf("pill %+v has no OnSurface glyph ink (label not drawn)", pr)
			}
		}
	}
	t.Run("text", func(t *testing.T) { check(t, textPost()) })
	t.Run("web", func(t *testing.T) {
		check(t, source.Item{ID: "w", Source: source.HackerNews, Title: "linked", Link: "https://example.com/a"})
	})
}

// TestTextZoomButtonsHiddenWithoutSelection covers the no-selection path: with
// nothing previewed the pills are not laid out and drawTextZoomButtons is a
// no-op, and a click where they would sit does not resolve to a zoom hit.
func TestTextZoomButtonsHiddenWithoutSelection(t *testing.T) {
	s := New(1200, 800, ThemeFor(OSMac, false))
	s.SetSubs(nil)
	s.SetItems([]source.Item{textPost()})
	s.Draw(make([]byte, s.W*s.H*4)) // no SelectPreview → empty-prompt pane

	if _, _, shown := s.TextZoomButtons(); shown {
		t.Fatal("zoom buttons should be hidden with no selection")
	}
	// A click in the pane's top-right band resolves to nothing (no zoom hit).
	h := s.HitTest(s.previewR.X+s.previewR.W-5, s.m.topbarH+5)
	if h.Kind == HitPreviewTextSmaller || h.Kind == HitPreviewTextLarger {
		t.Fatalf("hidden pills should not hit-test: %v", h.Kind)
	}
}

// TestPreviewPaneHiddenClearsZoomButtons covers the pane-hidden layout branch:
// a window too narrow to show the pane clears the pill rects.
func TestPreviewPaneHiddenClearsZoomButtons(t *testing.T) {
	s := New(1200, 800, ThemeFor(OSMac, false))
	s.SetSubs(nil)
	it := textPost()
	s.SetItems([]source.Item{it})
	s.SelectPreview(it)
	s.Draw(make([]byte, s.W*s.H*4))
	if _, _, shown := s.TextZoomButtons(); !shown {
		t.Fatal("precondition: pills shown at full width")
	}
	s.Resize(MinW, 800) // too narrow to keep both feed and pane
	s.Draw(make([]byte, s.W*s.H*4))
	if s.previewR.W != 0 {
		t.Fatalf("precondition: pane should be hidden, got W=%d", s.previewR.W)
	}
	if smaller, larger, shown := s.TextZoomButtons(); shown || smaller.W != 0 || larger.W != 0 {
		t.Fatalf("hidden pane should clear pills: %+v %+v shown=%v", smaller, larger, shown)
	}
}
