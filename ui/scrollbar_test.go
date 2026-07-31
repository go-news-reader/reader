package ui

import "testing"

// TestScrollGapStandard locks the single placement rule the sidebar and the feed
// (and any future panel) share: when a resize grip is present the scrollbar's
// right edge sits exactly one scrollGripGap left of that grip — the same gap
// regardless of the panel, which is precisely what "la distance entre la
// scrollbar et le grip" being equal in the sidebar and the central area requires.
func TestScrollGapStandard(t *testing.T) {
	s := New(1200, 700, ThemeFor(OSMac, false))
	gap := s.scrollGripGap()

	// Two different grip positions (sidebar edge vs. preview divider) must both
	// leave exactly one standard gap between the grip and the bar's right edge.
	for _, gripX := range []int{200, 640, 933} {
		if got := gripX - s.scrollbarRightX(9999, gripX); got != gap {
			t.Fatalf("gripX=%d: grip→bar gap=%d, want the standard %d", gripX, got, gap)
		}
	}

	// No grip (gripX=0): the bar tucks a small inset inside the panel's right edge,
	// not a full grip gap.
	const panelRight = 500
	if right := s.scrollbarRightX(panelRight, 0); right != panelRight-rpxOf(s, 2) {
		t.Fatalf("no-grip right=%d, want %d", right, panelRight-rpxOf(s, 2))
	}
}

// TestScrollClampRight covers the shared content-gutter helper every panel uses:
// no clamp when the bar is hidden, pull the edge in to the bar when shown and the
// content would otherwise reach it, and no-op when content already stops short.
func TestScrollClampRight(t *testing.T) {
	s := New(1200, 700, ThemeFor(OSMac, false))
	const panelRight = 800

	// Bar hidden → the natural edge is untouched.
	if got := s.scrollClampRight(panelRight, panelRight, 0, false); got != panelRight {
		t.Fatalf("hidden bar: got %d, want %d unchanged", got, panelRight)
	}

	limit := s.scrollbarRightX(panelRight, 0) - s.scrollbarW() - rpxOf(s, 6)

	// Bar shown, content reaches the edge → pulled in to the shared limit.
	if got := s.scrollClampRight(panelRight, panelRight, 0, true); got != limit {
		t.Fatalf("shown bar, wide content: got %d, want limit %d", got, limit)
	}

	// Bar shown but content already stops short of the limit → left as-is.
	short := limit - 40
	if got := s.scrollClampRight(short, panelRight, 0, true); got != short {
		t.Fatalf("shown bar, short content: got %d, want %d unchanged", got, short)
	}
}

// TestScrollbarNeeded covers the overflow predicate the panels gate the gutter on.
func TestScrollbarNeeded(t *testing.T) {
	s := New(400, 300, ThemeFor(OSMac, false))
	if s.scrollbarNeeded(100, 200) {
		t.Fatal("content shorter than viewport should not need a bar")
	}
	if !s.scrollbarNeeded(300, 200) {
		t.Fatal("content taller than viewport should need a bar")
	}
	if s.scrollbarNeeded(300, 0) {
		t.Fatal("a zero viewport should never report a bar")
	}
}
