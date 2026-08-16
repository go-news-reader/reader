package ui

import (
	"strings"
	"testing"

	"github.com/go-widgets/toolkit"

	"github.com/go-news-reader/reader/source"
)

// groupRowRect returns the on-screen rectangle of the (single) Usenet group
// summary row in s, mirroring feedRowAt's placement (list origin − scroll offset
// + the heights of the rows above it). It fatals when the feed shows no group.
func groupRowRect(t *testing.T, s *Scene) (string, toolkit.Rect) {
	t.Helper()
	s.ensureFeed()
	s.layout()
	lr := s.feed.list.Bounds()
	off := s.FeedScrollOffset()
	acc := 0
	for i, e := range s.feed.display {
		h := s.feedRowHeight(i)
		if e.group != nil {
			return e.group.Base, toolkit.Rect{X: lr.X, Y: lr.Y - off + acc, W: lr.W, H: h}
		}
		acc += h
	}
	t.Fatal("the feed shows no Usenet group")
	return "", toolkit.Rect{}
}

// TestToggleGroupExpandsAndRemeasures drives the feed group expand state: a
// collapsed group shows only its header; toggling lists the members and grows
// the laid-out row (the CardList re-measures because the height key folds the
// expand state in), and toggling again restores it.
func TestToggleGroupExpandsAndRemeasures(t *testing.T) {
	s := groupScene()
	base, _ := groupRowRect(t, s)

	if s.GroupExpanded(base) {
		t.Fatal("a group starts collapsed")
	}
	var groupRow int
	for i, e := range s.feed.display {
		if e.group != nil {
			groupRow = i
		}
	}
	collapsedH := s.feedRowHeight(groupRow)

	// First toggle (groupExpanded map is nil on the very first call — the lazy-init
	// branch) expands it.
	s.ToggleGroup(base)
	if !s.GroupExpanded(base) {
		t.Fatal("ToggleGroup should have expanded the group")
	}
	s.layout()
	expandedH := s.feedRowHeight(groupRow)
	if expandedH <= collapsedH {
		t.Fatalf("expanded height %d should exceed collapsed %d (members listed)", expandedH, collapsedH)
	}

	// Toggling back collapses it and returns the row to its collapsed height.
	s.ToggleGroup(base)
	if s.GroupExpanded(base) {
		t.Fatal("ToggleGroup should have collapsed the group")
	}
	s.layout()
	if got := s.feedRowHeight(groupRow); got != collapsedH {
		t.Fatalf("re-collapsed height %d, want %d", got, collapsedH)
	}
}

// TestInvalidateFeedGroupNoFeed covers the nil-feed guard (nothing to
// re-measure before the feed exists) and the nil-heights guard in dropHeightMemo
// (nothing measured yet).
func TestInvalidateFeedGroupNoFeed(t *testing.T) {
	s := New(900, 600, feedTheme())
	s.SetSubs(nil)
	s.invalidateFeedGroup("release") // s.feed == nil → no-op

	s = groupScene()
	s.feed.heights = nil // force the "nothing measured yet" branch of dropHeightMemo
	for _, e := range s.feed.display {
		s.dropHeightMemo(e) // both the item and the group entry hit the nil-heights return
	}
	s.invalidateFeedGroup("release") // dropHeightMemo early-returns, model.Set still runs
}

// TestFeedGroupHitAffordances checks a click on a complete group card's chevron,
// download checkbox and Reconstruct pill resolves — through HitTest, the reader's
// real feed hit path — to the matching group hit, all carrying the release base;
// a click on the card body (no affordance) previews it like any card.
func TestFeedGroupHitAffordances(t *testing.T) {
	s := groupScene()
	base, rect := groupRowRect(t, s)

	gc := s.feedGroupCard(groupOf(s, base))
	gc.SetBounds(rect)

	center := func(r toolkit.Rect) (int, int) { return r.X + r.W/2, r.Y + r.H/2 }

	// The disclosure chevron toggles expand/collapse.
	cx, cy := center(gc.ChevronRect())
	if h := s.HitTest(cx, cy); h.Kind != HitToggleGroup || h.Value != base {
		t.Fatalf("chevron hit = %+v, want HitToggleGroup %q", h, base)
	}
	// A complete post exposes the download checkbox and the Reconstruct pill.
	dx, dy := center(gc.CheckRect())
	if h := s.HitTest(dx, dy); h.Kind != HitToggleDownload || h.Value != base {
		t.Fatalf("checkbox hit = %+v, want HitToggleDownload %q", h, base)
	}
	ax, ay := center(gc.ActionRect())
	if h := s.HitTest(ax, ay); h.Kind != HitReconstruct || h.Value != base {
		t.Fatalf("action hit = %+v, want HitReconstruct %q", h, base)
	}

	// A click on the title/meta body (no affordance) falls through to the card
	// item — the group previews like any other card.
	tx, ty := rect.X+rect.W/2, rect.Y+rect.H-3
	if h := s.HitTest(tx, ty); h.Kind != HitItem {
		t.Fatalf("body hit = %+v, want HitItem", h)
	}
}

// TestFeedGroupHitIncompleteNoAffordance checks an incomplete post exposes
// neither the checkbox nor the Reconstruct pill: a click anywhere but the
// chevron falls through to the card body (it cannot be rebuilt), matching the
// old hitGroup rule.
func TestFeedGroupHitIncompleteNoAffordance(t *testing.T) {
	s := New(900, 600, feedTheme())
	s.SetSubs(nil)
	// [1/3] + [2/3]: file 3 is never present, so the post is incomplete.
	s.SetItems([]source.Item{
		usenetItem("d1", `[1/3] - "rel.part1.rar" yEnc (1/1) 100`),
		usenetItem("d2", `[2/3] - "rel.part2.rar" yEnc (1/1) 100`),
	})
	s.layout()
	base, rect := groupRowRect(t, s)
	if groupOf(s, base).Complete() {
		t.Fatal("this post should be incomplete")
	}
	gc := s.feedGroupCard(groupOf(s, base))
	gc.SetBounds(rect)
	if r := gc.CheckRect(); r.W != 0 {
		t.Fatalf("incomplete post should draw no checkbox; got %+v", r)
	}
	if r := gc.ActionRect(); r.W != 0 {
		t.Fatalf("incomplete post should draw no action pill; got %+v", r)
	}
	// The chevron still toggles; the rest of the header previews.
	cx, cy := gc.ChevronRect().X+1, gc.ChevronRect().Y+1
	if h := s.HitTest(cx, cy); h.Kind != HitToggleGroup {
		t.Fatalf("chevron hit = %+v, want HitToggleGroup", h)
	}
	if h := s.HitTest(rect.X+rect.W/2, rect.Y+rect.H-3); h.Kind != HitItem {
		t.Fatalf("incomplete body hit = %+v, want HitItem", h)
	}
}

// TestFeedGroupHitNonGroup checks feedGroupHit reports no affordance over a
// standalone (non-group) card and outside the feed list entirely.
func TestFeedGroupHitNonGroup(t *testing.T) {
	s := groupScene()
	s.layout()
	// The standalone reddit row is display index 0 (newest at the bottom, so the
	// group is last); a click on it is a plain item, not a group affordance.
	lr := s.feed.list.Bounds()
	if h, ok := s.feedGroupHit(lr.X+lr.W/2, lr.Y+2); ok {
		t.Fatalf("standalone card reported a group affordance: %+v", h)
	}
	// A point outside the list is no affordance.
	if _, ok := s.feedGroupHit(-5, -5); ok {
		t.Fatal("a point outside the feed reported a group affordance")
	}
}

// TestFeedGroupMemberClickPreviews checks a click on an expanded group's member
// row previews that specific member article (not the whole post), the old
// member-row behaviour, routed through FeedClickAt.
func TestFeedGroupMemberClickPreviews(t *testing.T) {
	s := groupScene()
	base, _ := groupRowRect(t, s)
	s.ToggleGroup(base)
	_, rect := groupRowRect(t, s) // re-measure: the expanded row is taller

	g := groupOf(s, base)
	gc := s.feedGroupCard(g)
	gc.SetBounds(rect)

	// Capture what FeedClickAt previews via the select hook.
	var got source.Item
	var viaKB bool
	s.onSelectItem = func(it source.Item, kb bool) { got, viaKB = it, kb }

	mr := gc.MemberRect(0)
	s.FeedClickAt(mr.X+mr.W/2, mr.Y+mr.H/2)
	if got.ID != g.Members[0].Item.ID {
		t.Fatalf("member click previewed %q, want %q", got.ID, g.Members[0].Item.ID)
	}
	if viaKB {
		t.Fatal("a member click is not a keyboard selection")
	}

	// A click in the expanded header (not a member row) is NOT a member preview:
	// feedGroupMemberAt returns false there, so FeedClickAt routes to the CardList.
	if _, ok := s.feedGroupMemberAt(rect.X+rect.W/2, rect.Y+2); ok {
		t.Fatal("the header row must not be treated as a member")
	}
}

// TestFeedGroupMemberCollapsedNotPreviewed checks a collapsed group has no
// member rows to hit: feedGroupMemberAt reports nothing.
func TestFeedGroupMemberCollapsedNotPreviewed(t *testing.T) {
	s := groupScene()
	_, rect := groupRowRect(t, s)
	if _, ok := s.feedGroupMemberAt(rect.X+rect.W/2, rect.Y+rect.H/2); ok {
		t.Fatal("a collapsed group exposes no member rows")
	}
}

// TestFeedPreviewItemBareScene checks feedPreviewItem falls back to the scene's
// own SelectPreview when no app select hook is installed.
func TestFeedPreviewItemBareScene(t *testing.T) {
	s := groupScene()
	base, _ := groupRowRect(t, s)
	s.ToggleGroup(base)
	_, rect := groupRowRect(t, s)
	g := groupOf(s, base)
	gc := s.feedGroupCard(g)
	gc.SetBounds(rect)
	mr := gc.MemberRect(0)
	s.FeedClickAt(mr.X+mr.W/2, mr.Y+mr.H/2) // no onSelectItem → SelectPreview
	if it, ok := s.PreviewItem(); !ok || it.ID != g.Members[0].Item.ID {
		t.Fatalf("bare-scene member click preview = %+v ok=%v", it, ok)
	}
}

// TestA11yGroupExpandedListsMembers checks the a11y tree announces a group's
// expand state and, when expanded, adds one text node per member part at its
// on-screen rect.
func TestA11yGroupExpandedListsMembers(t *testing.T) {
	s := groupScene()
	base, _ := groupRowRect(t, s)

	// Collapsed: the group node says so, and no member text nodes appear.
	collapsed := s.A11yTree()
	if n, ok := findGroupNode(collapsed, base); !ok || !strings.Contains(n.Value, "collapsed") {
		t.Fatalf("collapsed group node = %+v ok=%v", n, ok)
	}

	s.ToggleGroup(base)
	s.layout()
	expanded := s.A11yTree()
	n, ok := findGroupNode(expanded, base)
	if !ok || !strings.Contains(n.Value, "expanded") {
		t.Fatalf("expanded group node = %+v ok=%v", n, ok)
	}
	g := groupOf(s, base)
	want := memberLine(g.Members[0])
	found := false
	for _, nd := range expanded {
		if nd.Role == toolkit.RoleText && nd.Name == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("expanded a11y tree lists no member row %q", want)
	}
}

// groupOf returns the itemGroup with the given base currently shown in s.
func groupOf(s *Scene, base string) *itemGroup {
	for _, e := range s.feed.display {
		if e.group != nil && e.group.Base == base {
			return e.group
		}
	}
	return nil
}

// findGroupNode finds the a11y group node named base (a RoleGroup whose Value
// carries the "parts" summary).
func findGroupNode(tree []A11yNode, base string) (A11yNode, bool) {
	for _, n := range tree {
		if n.Role == toolkit.RoleGroup && n.Name == base && strings.Contains(n.Value, "parts") {
			return n, true
		}
	}
	return A11yNode{}, false
}
