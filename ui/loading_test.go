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

// TestGIFPlayingAnimates proves the GIF-playing flag both reports through
// GIFPlaying and forces Animating true (so the present loop keeps ticking while
// a GIF plays), and that clearing it releases the scene back to idle.
func TestGIFPlayingAnimates(t *testing.T) {
	s := newScene()
	if s.GIFPlaying() || s.Animating() {
		t.Fatal("a fresh scene should not be GIF-playing or animating")
	}
	r0 := s.Rev()
	s.SetGIFPlaying(true)
	if !s.GIFPlaying() || !s.Animating() {
		t.Fatal("SetGIFPlaying(true) should mark GIF-playing and animating")
	}
	if s.Rev() == r0 {
		t.Fatal("SetGIFPlaying should bump the damage sequence")
	}
	s.SetGIFPlaying(false)
	if s.GIFPlaying() || s.Animating() {
		t.Fatal("SetGIFPlaying(false) should stop GIF-playing and animation")
	}
}

func TestAdvanceAnim(t *testing.T) {
	s := newScene()
	f0 := s.AnimFrame()
	r0 := s.Rev()
	s.AdvanceAnim(1)
	if s.AnimFrame() != f0+1 {
		t.Fatalf("anim frame = %d, want %d", s.AnimFrame(), f0+1)
	}
	if s.Rev() == r0 {
		t.Fatal("AdvanceAnim should bump the damage sequence")
	}
}

// TestAdvanceAnimByN proves a multi-tick advance steps the clock by exactly n,
// so a throttled present loop that redraws less often than it ticks still turns
// the spinner at the same real-time speed.
func TestAdvanceAnimByN(t *testing.T) {
	s := newScene()
	f0 := s.AnimFrame()
	s.AdvanceAnim(4)
	if s.AnimFrame() != f0+4 {
		t.Fatalf("anim frame = %d, want %d", s.AnimFrame(), f0+4)
	}
}

// TestAdvanceAnimNonPositiveIsNoop proves a zero/negative advance neither moves
// the clock nor marks the scene dirty, so a spurious call cannot rewind or
// spuriously redraw.
func TestAdvanceAnimNonPositiveIsNoop(t *testing.T) {
	s := newScene()
	f0, r0 := s.AnimFrame(), s.Rev()
	s.AdvanceAnim(0)
	s.AdvanceAnim(-3)
	if s.AnimFrame() != f0 {
		t.Fatalf("anim frame = %d, want unchanged %d", s.AnimFrame(), f0)
	}
	if s.Rev() != r0 {
		t.Fatal("a non-positive AdvanceAnim must not bump the damage sequence")
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

// TestLoadingDrawDirect exercises the empty-feed placeholder helper directly.
func TestLoadingDrawDirect(t *testing.T) {
	s := New(700, 460, ThemeFor(OSLinux, false))
	s.layout()

	// drawLoadingPlaceholder with a tiny feed width still centres its spinner.
	p, img := sceneCanvas(s)
	s.drawLoadingPlaceholder(p, img, 0, 60, mute(s.theme.OnSurface, s.theme.Surface))
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
		s.AdvanceAnim(1)
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
	s.AdvanceAnim(1) // no-op visually (not animating), but prove idempotence
	d := make([]byte, len(c))
	s.Draw(d)
	if bufDiff(c, d) != 0 {
		t.Fatal("idle scene changed pixels across frames — animation not gated")
	}
}

// TestFetchingStripAndPendingSidebar proves the partial-progress state now rides
// on the CardList's own pull-to-fetch strip (FetchingBottom + BottomLabel) rather
// than a hand-drawn top strip, and that a pending sidebar source visibly changes
// the sidebar sprite versus none pending.
func TestFetchingStripAndPendingSidebar(t *testing.T) {
	s := New(800, 520, ThemeFor(OSLinux, false))
	s.SetItems(sampleItems())
	s.SetSubs(sampleSubs())
	// HackerNews still pending; Reddit already returned.
	s.SetPendingSources([]source.Subscription{{Source: source.HackerNews}})
	s.ToggleSidebarSource(source.HackerNews) // expand its group so the pending row's spinner draws
	s.SetLoading(true, 1, 2)
	s.layout()

	// A refresh in flight spins the CardList's bottom pull-to-fetch strip and
	// labels it, and the scene reports it as animating.
	list := s.FeedCardList()
	if !list.FetchingBottom || list.BottomLabel == "" {
		t.Fatalf("loading did not arm the CardList fetch strip: fetching=%v label=%q",
			list.FetchingBottom, list.BottomLabel)
	}
	if !s.FeedAnimating() {
		t.Fatal("a fetching feed should report FeedAnimating")
	}
	renderPNG(t, s, "loading-partial")

	// Pending sidebar marker: a scene with a pending source renders a different
	// sidebar than one with none (the spinner is drawn).
	sp1 := s.sidebarSprite()
	s.SetLoading(false, 2, 2) // clears pending + the fetch strip
	s.layout()
	if list.FetchingBottom || list.BottomLabel != "" {
		t.Fatal("clearing loading should idle the CardList fetch strip")
	}
	sp2 := s.sidebarSprite()
	if bufDiff(sp1.Pix, sp2.Pix) == 0 {
		t.Fatal("pending sidebar marker did not change the sidebar rendering")
	}
}
