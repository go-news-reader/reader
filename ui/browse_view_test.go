package ui

import (
	"testing"

	"github.com/go-widgets/toolkit"

	"github.com/go-news-reader/reader/internal/settings"
	"github.com/go-news-reader/reader/source"
)

func browseSample() []string {
	return []string{"alt.binaries.cd.image", "alt.binaries.test", "alt.test", "comp.lang.go", "fr.test"}
}

// browseScene builds a browser scene with a configured server, one profile and
// the sample group list loaded, already in ModeBrowse.
func browseScene() *Scene {
	s := New(900, 640, ThemeFor(OSLinux, false))
	s.SetProfiles([]settings.Profile{{Name: "Home"}}, 0)
	s.SetUsenetServer("news.free.fr:119")
	s.SetBrowseGroups(browseSample())
	s.OpenBrowse()
	return s
}

// firstBrowseHit scans the surface for the first coordinate whose HitTest yields
// kind, returning the Hit too (for its Value).
func firstBrowseHit(s *Scene, kind HitKind) (Hit, bool) {
	for y := 0; y < s.H; y += 2 {
		for x := 0; x < s.W; x += 2 {
			if h := s.HitTest(x, y); h.Kind == kind {
				return h, true
			}
		}
	}
	return Hit{}, false
}

func TestBrowseCollapsedSnapshot(t *testing.T) {
	s := browseScene()
	renderPNG(t, s, "browse-collapsed")
	if s.Mode() != ModeBrowse {
		t.Fatalf("mode = %v", s.Mode())
	}
	s.layoutBrowse()
	// Collapsed: only the three top-level hierarchies (alt, comp, fr).
	if len(s.browseRows) != 3 {
		t.Fatalf("collapsed rows = %d, want 3", len(s.browseRows))
	}
	if s.browseMatchCount != 5 {
		t.Fatalf("match count = %d, want 5", s.browseMatchCount)
	}
	// A top-level node indents at depth 0; its child (once expanded) indents deeper.
	s.ToggleBrowseNode("alt")
	s.layoutBrowse()
	var altBin browseRowLayout
	for _, r := range s.browseRows {
		if r.node.Name == "alt.binaries" {
			altBin = r
		}
	}
	if altBin.depth != 1 {
		t.Fatalf("alt.binaries depth = %d, want 1", altBin.depth)
	}
	c0 := s.browseChevronRect(s.m.pad, 0, 0)
	c1 := s.browseChevronRect(s.m.pad, 0, 1)
	if c1.X <= c0.X {
		t.Fatalf("depth-1 chevron (%d) not indented past depth-0 (%d)", c1.X, c0.X)
	}
}

func TestBrowseFilterNarrowsAndSubscribes(t *testing.T) {
	s := browseScene()
	// Type "binaries" into the focused filter field (routes through TypeRune).
	s.FocusBrowseFilter(true)
	for _, r := range "binaries" {
		s.TypeRune(r)
	}
	renderPNG(t, s, "browse-filtered")
	s.layoutBrowse()
	if s.browseMatchCount != 2 {
		t.Fatalf("filtered match count = %d, want 2", s.browseMatchCount)
	}
	// Auto-expanded to the matches: alt, alt.binaries, alt.binaries.cd,
	// alt.binaries.cd.image, alt.binaries.test.
	if len(s.browseRows) != 5 {
		t.Fatalf("filtered rows = %d, want 5", len(s.browseRows))
	}
	// A matching leaf offers Subscribe.
	h, ok := firstBrowseHit(s, HitSubscribeGroup)
	if !ok {
		t.Fatal("no Subscribe affordance in the filtered tree")
	}
	if h.Value != "alt.binaries.cd.image" && h.Value != "alt.binaries.test" {
		t.Fatalf("subscribe target = %q", h.Value)
	}
}

func TestBrowseInvalidRegexp(t *testing.T) {
	s := browseScene()
	s.FocusBrowseFilter(true)
	s.TypeRune('(') // an unterminated group: invalid
	renderPNG(t, s, "browse-badregexp")
	s.layoutBrowse()
	if s.browseFilterErr == "" {
		t.Fatal("invalid pattern should set an inline hint")
	}
	if s.browseMatchCount != 0 || len(s.browseRows) != 0 {
		t.Fatalf("invalid pattern should show no rows: count=%d rows=%d", s.browseMatchCount, len(s.browseRows))
	}
	// Recovering to a valid pattern clears the error.
	s.TypeRune(')')
	s.layoutBrowse()
	if s.browseFilterErr != "" {
		t.Fatalf("valid continuation should clear the error, got %q", s.browseFilterErr)
	}
}

func TestBrowseBackspace(t *testing.T) {
	s := browseScene()
	// Not focused: Backspace is a no-op.
	s.Backspace()
	s.FocusBrowseFilter(true)
	for _, r := range "abc" {
		s.TypeRune(r)
	}
	s.Backspace()
	if got := s.BrowseEntry().Text; got != "ab" {
		t.Fatalf("filter text = %q, want ab", got)
	}
}

func TestBrowseToggleHit(t *testing.T) {
	s := browseScene()
	h, ok := firstBrowseHit(s, HitToggleBrowseNode)
	if !ok {
		t.Fatal("no toggle hit for a top-level hierarchy")
	}
	if h.Value != "alt" {
		t.Fatalf("first toggle = %q, want alt", h.Value)
	}
	if s.BrowseNodeExpanded("alt") {
		t.Fatal("alt should start collapsed")
	}
	s.ToggleBrowseNode("alt")
	if !s.BrowseNodeExpanded("alt") {
		t.Fatal("alt should be expanded after toggle")
	}
}

func TestBrowseSubscribeMarksAndDisables(t *testing.T) {
	s := browseScene()
	s.ToggleBrowseNode("alt") // reveal alt.test leaf
	h, ok := firstBrowseHit(s, HitSubscribeGroup)
	if !ok {
		t.Fatal("no subscribe hit")
	}
	if !s.SubscribeActive(source.Usenet, h.Value) {
		t.Fatalf("SubscribeActive(%q) returned false", h.Value)
	}
	if !s.IsSubscribed(source.Usenet, h.Value) {
		t.Fatal("group not marked subscribed")
	}
	renderPNG(t, s, "browse-subscribed")
	// The subscribed leaf no longer hit-tests as a Subscribe target.
	s.layoutBrowse()
	for _, r := range s.browseRows {
		if r.node.Name == h.Value {
			top := s.browseTreeTop + r.top - s.browseScrollY
			sr := s.browseSubscribeRect(s.m.pad, top, s.W-2*s.m.pad)
			if got := s.browseHitTest(sr.X+sr.W/2, top+2); got.Kind == HitSubscribeGroup {
				t.Fatal("subscribed leaf must not offer Subscribe again")
			}
		}
	}
}

func TestBrowseChromeHits(t *testing.T) {
	s := browseScene()
	if _, ok := firstBrowseHit(s, HitCloseBrowse); !ok {
		t.Fatal("no Back hit")
	}
	if _, ok := firstBrowseHit(s, HitBrowseRefresh); !ok {
		t.Fatal("no Refresh hit")
	}
	if _, ok := firstBrowseHit(s, HitBrowseFilter); !ok {
		t.Fatal("no filter-field hit")
	}
}

func TestBrowseScrollClamps(t *testing.T) {
	// A long group list makes the tree taller than the viewport.
	names := make([]string, 0, 400)
	for i := 0; i < 400; i++ {
		names = append(names, "group"+itoa(i)) // flat top-level leaves -> many rows
	}
	s := browseScene()
	s.SetBrowseGroups(names)
	s.OpenBrowse()
	s.Scroll(100000) // clamp to the bottom
	bottom := s.browseScrollY
	if bottom <= 0 {
		t.Fatalf("scroll did not advance: %d", bottom)
	}
	s.Scroll(600) // scroll partway so early rows fall above the viewport
	renderPNG(t, s, "browse-scrolled")
	if s.browseScrollY == 0 {
		t.Fatal("partial scroll should be non-zero")
	}
	s.Scroll(-100000) // clamp back to the top
	if s.browseScrollY != 0 {
		t.Fatalf("scroll not clamped to 0: %d", s.browseScrollY)
	}
}

func TestBrowseFocusedGetter(t *testing.T) {
	s := browseScene()
	if s.BrowseFocused() {
		t.Fatal("filter should start unfocused")
	}
	s.FocusBrowseFilter(true)
	if !s.BrowseFocused() {
		t.Fatal("filter should be focused")
	}
}

func TestBrowseSubscribeRectOnGroupWithChildren(t *testing.T) {
	// "alt.test" is both a real group and a parent (alt.test.foo exists): clicking
	// its Subscribe control subscribes, while clicking elsewhere on the row toggles.
	s := New(900, 640, ThemeFor(OSLinux, false))
	s.SetProfiles([]settings.Profile{{Name: "Home"}}, 0)
	s.SetUsenetServer("news.free.fr:119")
	s.SetBrowseGroups([]string{"alt.test", "alt.test.foo"})
	s.OpenBrowse()
	s.ToggleBrowseNode("alt")
	s.layoutBrowse()
	var row browseRowLayout
	for _, r := range s.browseRows {
		if r.node.Name == "alt.test" {
			row = r
		}
	}
	if row.node == nil || !row.node.IsGroup || len(row.node.Children) == 0 {
		t.Fatalf("alt.test not a group-with-children: %+v", row.node)
	}
	top := s.browseTreeTop + row.top - s.browseScrollY
	sr := s.browseSubscribeRect(s.m.pad, top, s.W-2*s.m.pad)
	// The Subscribe control subscribes (branch 1).
	if got := s.browseHitTest(sr.X+sr.W/2, sr.Y+sr.H/2); got.Kind != HitSubscribeGroup || got.Value != "alt.test" {
		t.Fatalf("subscribe-rect hit = %+v", got)
	}
	// The row body (left of the control) toggles (branch 2).
	if got := s.browseHitTest(s.m.pad+s.browseIndentW(), top+s.m.sideItemH/2); got.Kind != HitToggleBrowseNode {
		t.Fatalf("row-body hit = %+v", got)
	}
}

func TestBrowseEmptyStates(t *testing.T) {
	s := browseScene()
	s.SetBrowseGroups(nil)
	// Not loading: "No groups" hint.
	renderPNG(t, s, "browse-empty")
	s.layoutBrowse()
	if len(s.browseRows) != 0 {
		t.Fatalf("empty list rows = %d", len(s.browseRows))
	}
	// Loading: "Loading group list…" hint.
	s.SetLoading(true, 0, 1)
	renderPNG(t, s, "browse-loading")
}

func TestBrowseNoServer(t *testing.T) {
	s := New(900, 640, ThemeFor(OSLinux, false))
	s.SetProfiles([]settings.Profile{{Name: "Home"}}, 0)
	// No SetUsenetServer -> usenetAddr == "".
	s.OpenBrowse()
	renderPNG(t, s, "browse-noserver")
	// The body routes to Accounts; Back still works.
	if got := s.HitTest(s.W/2, s.H/2); got.Kind != HitAccounts {
		t.Fatalf("no-server body hit = %v, want Accounts", got.Kind)
	}
	if _, ok := firstBrowseHit(s, HitCloseBrowse); !ok {
		t.Fatal("Back must still work with no server")
	}
	// The empty-area (below chrome) also routes to Accounts, not a tree row.
	if got := s.HitTest(s.W-4, s.H-4); got.Kind != HitAccounts {
		t.Fatalf("no-server corner = %v", got.Kind)
	}
}

func TestBrowseSidebarEntry(t *testing.T) {
	// With a server configured, the feed sidebar shows the Browse entry.
	s := New(900, 640, ThemeFor(OSLinux, false))
	s.SetProfiles([]settings.Profile{{Name: "Home", Subs: []source.Subscription{{Source: source.Usenet, Channel: "alt.test"}}}}, 0)
	s.SetUsenetServer("news.free.fr:119")
	renderPNG(t, s, "feed-with-browse-entry")
	if _, ok := firstBrowseHit(s, HitBrowse); !ok {
		t.Fatal("sidebar Browse entry missing when a server is configured")
	}
	// Without a server, the entry disappears.
	s.SetUsenetServer("")
	if _, ok := firstBrowseHit(s, HitBrowse); ok {
		t.Fatal("Browse entry present without a configured server")
	}
}

func TestSubscribeActiveEdges(t *testing.T) {
	// No profiles: SubscribeActive is a no-op returning false.
	s := New(400, 300, nil)
	if s.SubscribeActive(source.Usenet, "x") {
		t.Fatal("SubscribeActive with no profile should return false")
	}
	// A duplicate returns false.
	s.SetProfiles([]settings.Profile{{Name: "Home"}}, 0)
	if !s.SubscribeActive(source.Usenet, "alt.test") {
		t.Fatal("first subscribe should return true")
	}
	if s.SubscribeActive(source.Usenet, "ALT.TEST") {
		t.Fatal("case-insensitive duplicate should return false")
	}
}

func TestSetUsenetServerNoOp(t *testing.T) {
	s := browseScene()
	rev := s.subsRev
	s.SetUsenetServer("news.free.fr:119") // same value
	if s.subsRev != rev {
		t.Fatal("setting the same server should be a no-op")
	}
	if s.UsenetServer() != "news.free.fr:119" {
		t.Fatalf("server = %q", s.UsenetServer())
	}
	if len(s.BrowseGroups()) != 5 {
		t.Fatalf("browse groups = %d", len(s.BrowseGroups()))
	}
}

func TestBrowseInvalidateAndClose(t *testing.T) {
	s := browseScene()
	s.browseScrollY = 40
	s.InvalidateBrowse()
	if s.browseScrollY != 0 {
		t.Fatal("InvalidateBrowse should reset scroll")
	}
	s.CloseBrowse()
	if s.Mode() != ModeFeed {
		t.Fatalf("CloseBrowse mode = %v", s.Mode())
	}
}

func TestBrowseThemeWithoutOnAccent(t *testing.T) {
	// A code-built theme with no Extra map exercises the OnAccent-absent branches
	// in drawBrowse / drawBrowseRow.
	s := browseScene()
	s.SetTheme(toolkit.DefaultLight())
	renderPNG(t, s, "browse-default-theme")
}

func TestThemeSuccessErrorColors(t *testing.T) {
	green := toolkit.RGBA{R: 1, G: 2, B: 3, A: 0xFF}
	red := toolkit.RGBA{R: 4, G: 5, B: 6, A: 0xFF}
	th := &toolkit.Theme{Extra: map[string]toolkit.RGBA{"success_color": green, "error_color": red}}
	if successColor(th) != green {
		t.Fatal("success_color from Extra not used")
	}
	if errorColor(th) != red {
		t.Fatal("error_color from Extra not used")
	}
	// Defaults when the theme carries no such entries.
	plain := &toolkit.Theme{}
	if successColor(plain) != rgb(0x2FA84F) {
		t.Fatal("default success colour wrong")
	}
	if errorColor(plain) != rgb(0xD03030) {
		t.Fatal("default error colour wrong")
	}
}
