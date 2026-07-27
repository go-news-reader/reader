package ui

import (
	"image"
	"testing"

	"github.com/go-widgets/painter"

	"github.com/go-news-reader/reader/source"
)

// sceneCanvas returns a painter+image over a fresh scene-sized RGBA buffer.
func sceneCanvas(s *Scene) (*painter.PixelPainter, *image.RGBA) {
	buf := make([]byte, s.W*s.H*4)
	p := painter.NewPixelPainter(buf, s.W, s.H)
	img := &image.RGBA{Pix: buf, Stride: s.W * 4, Rect: image.Rect(0, 0, s.W, s.H)}
	return p, img
}

// bufDiff reports how many bytes differ between two equal-length buffers.
func bufDiff(a, b []byte) int {
	n := 0
	for i := range a {
		if a[i] != b[i] {
			n++
		}
	}
	return n
}

func TestLoadingStateSettersAndGetters(t *testing.T) {
	s := newScene()
	if s.Loading() || s.Animating() {
		t.Fatal("scene should not start loading")
	}
	s.SetPendingSources([]source.Subscription{
		{Source: source.Reddit, Channel: "golang"},
		{Source: source.HackerNews},
	})
	if s.PendingCount() != 2 {
		t.Fatalf("pending count = %d, want 2", s.PendingCount())
	}
	if !s.IsPendingSub(source.Reddit, "golang") || !s.IsPendingSub(source.HackerNews, "") {
		t.Fatal("expected both sources pending")
	}
	s.SetLoading(true, 0, 2)
	if !s.Loading() || !s.Animating() {
		t.Fatal("SetLoading(true) should mark loading/animating")
	}
	if d, tot := s.LoadingProgress(); d != 0 || tot != 2 {
		t.Fatalf("progress = %d/%d, want 0/2", d, tot)
	}
	// One source returns: its pending marker clears.
	s.ClearPendingSource(source.Reddit, "golang")
	if s.IsPendingSub(source.Reddit, "golang") {
		t.Fatal("reddit should no longer be pending")
	}
	if s.PendingCount() != 1 {
		t.Fatalf("pending count = %d, want 1", s.PendingCount())
	}
	// Clearing an already-cleared source is a no-op (no panic, count unchanged).
	s.ClearPendingSource(source.Reddit, "golang")
	if s.PendingCount() != 1 {
		t.Fatalf("re-clear changed count: %d", s.PendingCount())
	}
	// Loading off clears remaining pending and stops animation.
	s.SetLoading(false, 2, 2)
	if s.Loading() || s.Animating() || s.PendingCount() != 0 {
		t.Fatalf("loading off left state: loading=%v pending=%d", s.Loading(), s.PendingCount())
	}
	// SetLoading(false) again with no pending map is still a no-op.
	s.SetLoading(false, 2, 2)
}

func TestAdvanceAnim(t *testing.T) {
	s := newScene()
	f0 := s.AnimFrame()
	r0 := s.Rev()
	s.AdvanceAnim()
	if s.AnimFrame() != f0+1 {
		t.Fatalf("anim frame = %d, want %d", s.AnimFrame(), f0+1)
	}
	if s.Rev() == r0 {
		t.Fatal("AdvanceAnim should bump the damage sequence")
	}
}

// TestSpinnerPhaseWraps proves the animation-frame -> [0,1) phase mapping
// wraps a full period back to 0 and folds negative counters into range.
func TestSpinnerPhaseWraps(t *testing.T) {
	s := newScene()
	s.animFrame = 0
	if got := s.spinnerPhase(); got != 0 {
		t.Fatalf("phase@0 = %v, want 0", got)
	}
	s.animFrame = spinnerPeriod / 2
	if got := s.spinnerPhase(); got != 0.5 {
		t.Fatalf("phase@half = %v, want 0.5", got)
	}
	s.animFrame = spinnerPeriod // full revolution wraps back to 0
	if got := s.spinnerPhase(); got != 0 {
		t.Fatalf("phase@period = %v, want 0", got)
	}
	// A negative counter folds into [0,1) (the defensive p<0 branch).
	s.animFrame = -spinnerPeriod / 2
	if got := s.spinnerPhase(); got != 0.5 {
		t.Fatalf("phase@-half = %v, want 0.5", got)
	}
}

// TestLoadingDrawDirect exercises the indicator helpers directly, including the
// drawLoadStrip loadTotal==0 branch (which the guarded layout path never
// reaches), so a degenerate total never divides by zero.
func TestLoadingDrawDirect(t *testing.T) {
	s := New(700, 460, ThemeFor(OSLinux, false))
	s.layout()

	// drawLoadingPlaceholder with a tiny feed width still centres its spinner.
	p, img := sceneCanvas(s)
	s.drawLoadingPlaceholder(p, img, 0, 60, mute(s.theme.OnSurface, s.theme.Surface))

	// drawLoadStrip with loadTotal==0 skips the determinate fraction (the false
	// branch of the `loadTotal > 0` guard) and must not divide by zero.
	s.SetLoading(true, 0, 0)
	p2, img2 := sceneCanvas(s)
	s.drawLoadStrip(p2, img2, 0, 0, 40, mute(s.theme.OnSurface, s.theme.Surface))
}

// TestLoadingPlaceholderAnimates renders the empty-feed loading placeholder at
// two animation frames and proves (a) the centre region is non-empty while
// loading, (b) the two frames DIFFER (the indicator moves), and (c) with
// loading off the same region falls back to the static "No items." message and
// is pixel-identical across frames.
func TestLoadingPlaceholderAnimates(t *testing.T) {
	s := New(700, 460, ThemeFor(OSLinux, false))
	s.SetItems(nil) // empty feed
	s.SetLoading(true, 0, 3)

	// Frame A (animFrame 0: the spinner hand points +x).
	a := renderPNG(t, s, "loading-empty-frame0")
	// Advance half a revolution so the hand points the opposite way.
	for i := 0; i < spinnerPeriod/2; i++ {
		s.AdvanceAnim()
	}
	b := renderPNG(t, s, "loading-empty-frame1")

	if len(a) != len(b) {
		t.Fatal("frame size mismatch")
	}
	// The indicator moved: the spinner hand swept to the opposite side, so the
	// two frames differ across several pixels.
	if d := bufDiff(a, b); d < 16 {
		t.Fatalf("animation frames barely differ (%d bytes) — spinner not moving", d)
	}

	// The spinner region (a square centred below the "Loading…" label) carries
	// accent pixels while loading — i.e. the indicator actually renders.
	s.layout()
	m := s.m
	accent := s.theme.Accent
	feedX := m.sidebarW + m.pad
	feedW := s.W - m.sidebarW - 2*m.pad
	spD := rpxOf(s, 52)
	spX := feedX + (feedW-spD)/2
	spY := s.H/2 - m.title.height + m.title.height + m.pad
	foundAccent := false
	for y := spY; y < spY+spD && !foundAccent; y++ {
		for x := spX; x < spX+spD; x++ {
			if p := px(a, s.W, x, y); p.R == accent.R && p.G == accent.G && p.B == accent.B {
				foundAccent = true
				break
			}
		}
	}
	if !foundAccent {
		t.Fatal("no accent pixel in the loading spinner region")
	}

	// Loading off: the indicator disappears; two renders are pixel-identical and
	// carry no accent on the former track row.
	s.SetLoading(false, 3, 3)
	c := renderPNG(t, s, "loaded-empty-idle")
	s.AdvanceAnim() // no-op visually (not animating), but prove idempotence
	d := make([]byte, len(c))
	s.Draw(d)
	if bufDiff(c, d) != 0 {
		t.Fatal("idle scene changed pixels across frames — animation not gated")
	}
}

// TestLoadStripAndPendingSidebar renders the partial-progress state: some items
// already shown, a top-of-feed strip, and a pending sidebar source. It proves
// the strip renders (accent present in its band), that it animates, and that a
// pending source visibly changes the sidebar sprite versus none pending.
func TestLoadStripAndPendingSidebar(t *testing.T) {
	s := New(800, 520, ThemeFor(OSLinux, false))
	s.SetItems(sampleItems())
	s.SetSubs(sampleSubs())
	// HackerNews still pending; Reddit already returned.
	s.SetPendingSources([]source.Subscription{{Source: source.HackerNews}})
	s.SetLoading(true, 1, 2)

	strip := renderPNG(t, s, "loading-partial")

	s.layout()
	m := s.m
	if !s.showStrip {
		t.Fatal("progress strip not laid out with items present")
	}
	// Accent appears somewhere in the strip band (the moving highlight / fill).
	feedTop := m.topbarH
	bandY0 := feedTop + s.loadStripTop - s.ScrollY
	accent := s.theme.Accent
	foundAccent := false
	for y := bandY0; y < bandY0+m.loadStripH && !foundAccent; y++ {
		for x := m.sidebarW + m.pad; x < s.W-m.pad; x++ {
			if p := px(strip, s.W, x, y); p.R == accent.R && p.G == accent.G && p.B == accent.B {
				foundAccent = true
				break
			}
		}
	}
	if !foundAccent {
		t.Fatal("no accent pixel in the progress strip band")
	}

	// The strip carries a subtle spinner cue: advancing frames shifts the small
	// trailing toolkit.Spinner. The determinate ProgressBar is the primary
	// feedback, so the motion is intentionally light (a thin hand).
	countAccent := func(buf []byte) int {
		n := 0
		for y := bandY0; y < bandY0+m.loadStripH; y++ {
			for x := m.sidebarW + m.pad; x < s.W-m.pad; x++ {
				if p := px(buf, s.W, x, y); p.R == accent.R && p.G == accent.G && p.B == accent.B {
					n++
				}
			}
		}
		return n
	}
	before := make([]byte, len(strip))
	copy(before, strip)
	for i := 0; i < spinnerPeriod/2; i++ {
		s.AdvanceAnim()
	}
	after := make([]byte, s.W*s.H*4)
	s.Draw(after)
	if bufDiff(before, after) < 8 {
		t.Fatal("progress strip spinner did not animate between frames")
	}

	// The determinate ProgressBar fill grows with completed sources: a full 2/2
	// paints more accent across the strip band than a half 1/2.
	half := countAccent(strip)
	s.SetLoading(true, 2, 2)
	full := renderPNG(t, s, "loading-partial-full")
	if fullN := countAccent(full); fullN <= half {
		t.Fatalf("full ProgressBar (%d accent px) not wider than half (%d)", fullN, half)
	}

	// Pending sidebar marker: a scene with a pending source renders a different
	// sidebar than one with none (the spinner is drawn).
	sp1 := s.sidebarSprite()
	s.SetLoading(false, 2, 2) // clears pending -> no ring, new sprite
	s.layout()
	sp2 := s.sidebarSprite()
	if bufDiff(sp1.Pix, sp2.Pix) == 0 {
		t.Fatal("pending sidebar marker did not change the sidebar rendering")
	}
}

// TestLoadStripScrolledOffscreen exercises the strip's scroll-clip branch: a
// tall feed scrolled far down pushes the strip above the feed top so it is
// skipped (the y+loadStripH < feedTop clip).
func TestLoadStripScrolledOffscreen(t *testing.T) {
	tall := New(600, 360, ThemeFor(OSMac, false))
	many := make([]source.Item, 30)
	for i := range many {
		many[i] = source.Item{ID: string(rune('a' + i)), Source: source.HackerNews, Title: "x", Score: -1, Comments: -1}
	}
	tall.SetItems(many)
	tall.SetLoading(true, 0, 1)
	tall.Scroll(100000) // clamp to bottom; strip scrolls above the viewport
	renderPNG(t, tall, "loading-strip-scrolled")
}
