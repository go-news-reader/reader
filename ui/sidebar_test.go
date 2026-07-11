package ui

import "testing"

func TestToggleSidebarCollapse(t *testing.T) {
	s := newScene()
	def := s.computeMetrics().sidebarW
	if def == 0 {
		t.Fatal("default sidebar width should be non-zero")
	}
	if s.SidebarCollapsed() {
		t.Fatal("sidebar should start expanded")
	}
	s.ToggleSidebar()
	if !s.SidebarCollapsed() || s.computeMetrics().sidebarW != 0 {
		t.Fatalf("collapse: collapsed=%v width=%d", s.SidebarCollapsed(), s.computeMetrics().sidebarW)
	}
	s.ToggleSidebar()
	if s.SidebarCollapsed() || s.computeMetrics().sidebarW != def {
		t.Fatal("expand should restore the default width")
	}
}

func TestDrawCollapsed(t *testing.T) {
	s := newScene()
	s.ToggleSidebar()
	buf := renderPNG(t, s, "collapsed")
	// The burger stays in the topbar (accent) and the feed reflows to x≈0: a card
	// row now begins where the sidebar used to be. Sample a card-body pixel near
	// the far left of the feed area.
	m := s.computeMetrics()
	if m.sidebarW != 0 {
		t.Fatalf("collapsed sidebar width = %d, want 0", m.sidebarW)
	}
	// The burger hit is still reachable so the sidebar can be reopened.
	if s.HitTest(5, m.topbarH/2).Kind != HitBurger {
		t.Fatal("burger must remain hittable when collapsed")
	}
	// A feed card now starts near x=0 (below the topbar). It is opaque, not the
	// page background — assert some non-background pixel appears in the left strip.
	bg := s.theme.Background
	found := false
	for y := m.topbarH + 1; y < s.H && !found; y++ {
		if p := px(buf, s.W, m.pad+2, y); p.R != bg.R || p.G != bg.G || p.B != bg.B {
			found = true
		}
	}
	if !found {
		t.Fatal("feed did not reflow into the space the sidebar vacated")
	}
	// The divider is not offered while collapsed.
	if s.HitTest(0, m.topbarH+20).Kind == HitSidebarDivider {
		t.Fatal("collapsed sidebar should offer no divider")
	}
}

func TestSidebarDividerDragResize(t *testing.T) {
	s := newScene()
	def := s.computeMetrics().sidebarW
	// The divider grip sits at the sidebar's right edge, below the topbar.
	m := s.computeMetrics()
	if h := s.HitTest(m.sidebarW, m.topbarH+20); h.Kind != HitSidebarDivider {
		t.Fatalf("divider hit = %+v", h)
	}
	// A move without a drag in progress does nothing.
	s.MouseMove(m.sidebarW+100, m.topbarH+20)
	if s.computeMetrics().sidebarW != def {
		t.Fatal("move without drag must not resize")
	}
	// Begin the drag, widen it, and confirm the width grew.
	s.BeginSidebarResize()
	if !s.DraggingSidebar() {
		t.Fatal("BeginSidebarResize should set the dragging flag")
	}
	s.MouseMove(def+80, m.topbarH+20)
	if w := s.computeMetrics().sidebarW; w <= def {
		t.Fatalf("drag wider: width %d, want > %d", w, def)
	}
	// Drag past the maximum clamps to the cap (min(maxSidebarW, W/2)).
	s.MouseMove(100000, m.topbarH+20)
	wide := s.computeMetrics().sidebarW
	cap := rpxOf(s, maxSidebarW)
	if half := s.W / 2; half < cap {
		cap = half
	}
	if wide != cap {
		t.Fatalf("over-max width = %d, want clamp %d", wide, cap)
	}
	// Drag below the minimum clamps to the floor.
	s.MouseMove(1, m.topbarH+20)
	if narrow := s.computeMetrics().sidebarW; narrow != rpxOf(s, minSidebarW) {
		t.Fatalf("under-min width = %d, want %d", narrow, rpxOf(s, minSidebarW))
	}
	// Release ends the drag; further moves no longer resize.
	s.EndSidebarResize()
	if s.DraggingSidebar() {
		t.Fatal("EndSidebarResize should clear the dragging flag")
	}
	held := s.computeMetrics().sidebarW
	s.MouseMove(def+200, m.topbarH+20)
	if s.computeMetrics().sidebarW != held {
		t.Fatal("move after release must not resize")
	}
}

func TestDrawResized(t *testing.T) {
	s := newScene()
	def := s.computeMetrics().sidebarW
	s.BeginSidebarResize()
	s.MouseMove(def+120, s.computeMetrics().topbarH+20) // widen well within bounds
	s.EndSidebarResize()
	widened := s.computeMetrics().sidebarW
	if widened <= def {
		t.Fatalf("resize did not widen: %d <= %d", widened, def)
	}
	renderPNG(t, s, "resized")
	// The divider grip now sits further right than the default width.
	if s.HitTest(widened, s.computeMetrics().topbarH+20).Kind != HitSidebarDivider {
		t.Fatal("divider should track the widened edge")
	}
}

func TestSidebarWidthNarrowWindowClamp(t *testing.T) {
	// A window so narrow that W/2 < min forces the hi<lo branch (hi := lo).
	s := New(MinW, MinH, nil) // 360 wide -> W/2 = 180 > min(140); still exercise user drag
	s.BeginSidebarResize()
	s.MouseMove(10, 100) // below min -> clamps up to the floor
	if got := s.computeMetrics().sidebarW; got != rpxOf(s, minSidebarW) {
		t.Fatalf("narrow clamp = %d, want %d", got, rpxOf(s, minSidebarW))
	}
	s.EndSidebarResize()
	// Directly exercise the hi<lo guard with a tiny surface via a zero-value scene.
	z := &Scene{W: 200, H: 200, Scale: 1}
	// user width huge -> hi = W/2 = 100 which is < min(140) -> hi := lo=140.
	z.sidebarUserW = 100000
	if got := z.sidebarWidthPx(); got != rpxOf(z, minSidebarW) {
		t.Fatalf("hi<lo guard width = %d, want %d", got, rpxOf(z, minSidebarW))
	}
}
