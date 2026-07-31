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
