package ui

import (
	"strconv"
	"strings"
	"testing"

	"github.com/go-widgets/toolkit"

	"github.com/go-news-reader/reader/internal/settings"
	"github.com/go-news-reader/reader/source"
)

// a11yScene is a populated feed: two sources, a few posts, one sign-in banner.
func a11yScene(t testing.TB) *Scene {
	t.Helper()
	s := New(1000, 700, ThemeFor(OSLinux, false))
	s.SetSubs([]Subscription{
		{Source: source.Twitter, Channel: "nasa"},
		{Source: source.HackerNews, Channel: "top"},
	})
	s.SetItems([]source.Item{
		{ID: "1", Source: source.Twitter, Channel: "nasa", Author: "NASA", Score: 12, Comments: 3,
			Body:  "A total solar eclipse crosses the Pacific",
			Media: []source.Media{{Kind: source.MediaImage, AltText: "The Moon's shadow on Earth"}}},
		{ID: "2", Source: source.HackerNews, Channel: "top", Title: "Show HN: a pure-Go news reader", Score: 99, Comments: 40},
	})
	s.SetAuthPrompts([]AuthPrompt{{Kind: source.Reddit, Reason: "sign-in required"}})
	s.layout()
	return s
}

func find(tree []A11yNode, name string) (A11yNode, bool) {
	for _, n := range tree {
		if n.Name == name {
			return n, true
		}
	}
	return A11yNode{}, false
}

// BenchmarkA11yTreePullCached measures what the paint loop pays per frame with
// the cache in place: a pull with no intervening change. BenchmarkA11yTreeRebuild
// measures the same pull's cost when it has to rebuild — the per-frame cost the
// go-widgets/window a11y bridge inflicted before this cache. The gap is the fix.
func BenchmarkA11yTreePullCached(b *testing.B) {
	s := a11yScene(b)
	s.A11yTree() // warm the cache
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.A11yTree()
	}
}

func BenchmarkA11yTreeRebuild(b *testing.B) {
	s := a11yScene(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.buildA11yTree()
	}
}

// TestA11yTreeCachesUntilAChange checks the tree is memoised: a host that pulls
// it on every paint frame gets the same built slice back until some state change
// bumps rev, at which point it is rebuilt. This is what keeps the a11y pull off
// the per-frame layout path that otherwise spins the CPU.
func TestA11yTreeCachesUntilAChange(t *testing.T) {
	s := a11yScene(t)
	first := s.A11yTree()
	if len(first) == 0 {
		t.Fatal("empty tree")
	}
	if again := s.A11yTree(); &again[0] != &first[0] {
		t.Error("second pull rebuilt the tree instead of serving the cache")
	}
	s.SetStatus("something changed")
	if fresh := s.A11yTree(); &fresh[0] == &first[0] {
		t.Error("a state change did not invalidate the cached tree")
	}
}

// TestA11yTreeDescribesTheFeed checks the tree says what the screen shows: the
// controls, the sources with their counts, the banner, and the posts named by
// the very headline the card renders.
func TestA11yTreeDescribesTheFeed(t *testing.T) {
	s := a11yScene(t)
	tree := s.A11yTree()
	if len(tree) < 10 {
		t.Fatalf("tree has %d nodes, too few to describe a populated feed", len(tree))
	}
	if tree[0].Role != toolkit.RoleDocument || tree[0].Name != "News" {
		t.Fatalf("first node = %+v, want the document", tree[0])
	}
	for _, want := range []string{"Toggle sidebar", "Search", "All Sources", "Accounts", "Network log", "Settings"} {
		if _, ok := find(tree, want); !ok {
			t.Errorf("no node named %q", want)
		}
	}
	// A post is named by the headline the card actually draws — for an untitled
	// social post that is its body, exactly as cardText resolves it.
	if _, ok := find(tree, "A total solar eclipse crosses the Pacific"); !ok {
		t.Error("the untitled post is not described by its body")
	}
	if n, ok := find(tree, "Show HN: a pure-Go news reader"); !ok {
		t.Error("the titled post is missing")
	} else if !strings.Contains(n.Value, "99") {
		t.Errorf("post value = %q, want the meta line with its score", n.Value)
	}
	// The sign-in banner is an alert: it is why a source looks empty.
	if n, ok := find(tree, "reddit needs sign-in"); !ok {
		t.Error("the auth banner is not exposed")
	} else if n.Role != toolkit.RoleAlert {
		t.Errorf("banner role = %q, want %q", n.Role, toolkit.RoleAlert)
	}
}

// TestA11yTreeCarriesUnseenCounts checks the sidebar's colour-coded counts reach
// a reader as text: unseen/total is otherwise conveyed only by an accent tint.
func TestA11yTreeCarriesUnseenCounts(t *testing.T) {
	s := a11yScene(t)
	for _, n := range s.A11yTree() {
		if n.Name == "nasa" {
			if !strings.Contains(n.Value, "/") {
				t.Fatalf("source row value = %q, want unseen/total", n.Value)
			}
			return
		}
	}
	t.Fatal("no sidebar row for the nasa subscription")
}

// TestA11yNodesAreReachable is the guarantee the whole design rests on: every
// interactive node carries the rect its own hit test resolves against, so "you
// can click it" and "a reader can find it" are the same statement. A node whose
// centre hit-tests to nothing is a lie in the tree.
func TestA11yNodesAreReachable(t *testing.T) {
	s := a11yScene(t)
	checked := 0
	for _, n := range s.A11yTree() {
		// Only interactive elements claim a rect; containers and text carry none.
		if n.Rect.W == 0 || n.Rect.H == 0 {
			continue
		}
		if n.Role == toolkit.RoleDocument {
			continue // the whole surface, not a target
		}
		cx, cy := n.Rect.X+n.Rect.W/2, n.Rect.Y+n.Rect.H/2
		if cy < 0 || cy >= s.H || cx < 0 || cx >= s.W {
			continue // scrolled out of the viewport; still legitimately in the tree
		}
		if h := s.HitTest(cx, cy); h.Kind == HitNone {
			t.Errorf("node %q (%s) at %+v hit-tests to nothing", n.Name, n.Role, n.Rect)
		}
		checked++
	}
	if checked < 6 {
		t.Fatalf("only %d nodes were reachable-checked; the scan is broken, not the tree", checked)
	}
}

// TestA11yTreeCoversEveryMode checks no view is silent: whatever the reader is
// showing, the tree names it and offers the way out.
func TestA11yTreeCoversEveryMode(t *testing.T) {
	cases := []struct {
		open func(*Scene)
		name string
		exit string
	}{
		{func(s *Scene) { s.OpenSettings() }, "Settings", "Done"},
		{func(s *Scene) { s.OpenAccounts() }, "Accounts", "Done"},
		{func(s *Scene) { s.OpenBrowse() }, "Browse newsgroups", "Back"},
		{func(s *Scene) { s.OpenLog() }, "Network log", "Back"},
	}
	for _, c := range cases {
		s := a11yScene(t)
		c.open(s)
		tree := s.A11yTree()
		if len(tree) < 2 {
			t.Errorf("%s produced %d nodes", c.name, len(tree))
			continue
		}
		if tree[0].Role != toolkit.RoleDocument || tree[0].Name != c.name {
			t.Errorf("%s first node = %+v, want document %q", c.name, tree[0], c.name)
		}
		if _, ok := find(tree, c.exit); !ok {
			t.Errorf("%s has no %q control", c.name, c.exit)
		}
	}
}

// TestA11yTreeReadsTheArticle checks the reading view exposes the text itself,
// not just its chrome — and the picture's description with it.
func TestA11yTreeReadsTheArticle(t *testing.T) {
	s := a11yScene(t)
	s.OpenDetail(source.Item{
		ID: "d", Source: source.Twitter, Title: "Eclipse day",
		Body:  "First paragraph.\n\nSecond paragraph.",
		Link:  "https://nasa.gov/live",
		Media: []source.Media{{Kind: source.MediaImage, AltText: "The Moon's shadow"}},
	})
	tree := s.A11yTree()

	if tree[0].Name != "Reading" {
		t.Fatalf("first node = %+v, want the reading document", tree[0])
	}
	if _, ok := find(tree, "Back"); !ok {
		t.Error("no Back control")
	}
	if n, ok := find(tree, "Open original"); !ok {
		t.Error("no Open original control")
	} else if n.Value != "https://nasa.gov/live" {
		t.Errorf("open-original value = %q, want the target URL", n.Value)
	}
	if _, ok := find(tree, "Eclipse day"); !ok {
		t.Error("the headline is not exposed")
	}
	if _, ok := find(tree, "The Moon's shadow"); !ok {
		t.Error("the picture's alt text is not exposed")
	}
	body := false
	for _, n := range tree {
		if strings.Contains(n.Name, "First paragraph.") && strings.Contains(n.Name, "Second paragraph.") {
			body = true
		}
	}
	if !body {
		t.Error("the article body is not exposed as one readable run")
	}
}

// TestA11yTreeLogUsesTheWidgetTree checks the one view with retained widgets
// collects them through the toolkit rather than re-describing them by hand.
func TestA11yTreeLogUsesTheWidgetTree(t *testing.T) {
	s := a11yScene(t)
	s.SetLogSource(func() []LogEntry {
		return []LogEntry{{Method: "GET", URL: "https://syndication.twitter.com/srv", Status: 200}}
	})
	s.OpenLog()
	tree := s.A11yTree()
	for _, n := range tree {
		if strings.Contains(n.Name, "syndication.twitter.com") {
			if n.Value != "200" {
				t.Fatalf("log row value = %q, want the status", n.Value)
			}
			return
		}
	}
	t.Fatalf("the log rows were not collected: %+v", tree)
}

// TestA11yTreeKeepsScrolledPostsInTheTree checks a card below the fold is still
// described. A reader must be able to walk the whole list; scrolling to what the
// user picks is the bridge's job, not a reason to hide it.
func TestA11yTreeKeepsScrolledPostsInTheTree(t *testing.T) {
	s := New(1000, 300, ThemeFor(OSLinux, false))
	s.SetSubs(nil)
	items := make([]source.Item, 25)
	for i := range items {
		items[i] = source.Item{ID: strconv.Itoa(i), Source: source.HackerNews,
			Title: "Post number " + strconv.Itoa(i), Score: -1, Comments: -1}
	}
	s.SetItems(items)
	s.layout()

	tree := s.A11yTree()
	seen := 0
	offscreen := 0
	for _, n := range tree {
		if strings.HasPrefix(n.Name, "Post number ") {
			seen++
			// The feed opens at the bottom (newest), so the older posts sit above the
			// fold (Y+H <= 0); a taller-than-viewport feed could also push some below.
			if n.Rect.Y+n.Rect.H <= 0 || n.Rect.Y >= s.H {
				offscreen++
			}
		}
	}
	if seen != len(items) {
		t.Fatalf("described %d of %d posts", seen, len(items))
	}
	if offscreen == 0 {
		t.Fatal("expected some posts off-screen (above/below the fold) in this viewport")
	}
}

// TestA11yTreeDescribesProfilesAndBrowse covers the sidebar's other two kinds of
// row: the profile tabs (whose active one a reader can otherwise only see by its
// tint) and the newsgroup-browser entry, which appears only with a server set.
func TestA11yTreeDescribesProfilesAndBrowse(t *testing.T) {
	s := a11yScene(t)
	s.SetProfiles([]settings.Profile{{Name: "Home"}, {Name: "Work"}}, 1)
	s.SetUsenetServer("news.example.net:119")
	s.layout()

	tree := s.A11yTree()
	home, ok := find(tree, "Home")
	if !ok {
		t.Fatal("the inactive profile tab is missing")
	}
	if home.Value != "" {
		t.Errorf("inactive profile value = %q, want empty", home.Value)
	}
	work, ok := find(tree, "Work")
	if !ok {
		t.Fatal("the active profile tab is missing")
	}
	if work.Value != "active" {
		t.Errorf("active profile value = %q, want %q", work.Value, "active")
	}
	if _, ok := find(tree, "Browse newsgroups"); !ok {
		t.Error("the browse entry is missing with a Usenet server configured")
	}
}

// TestA11yTreeDescribesAGroupCard covers the other kind of feed row: a Usenet
// multipart post collapses into one card, and must be announced as the release
// it is, with how many parts it holds.
func TestA11yTreeDescribesAGroupCard(t *testing.T) {
	s := New(1000, 700, ThemeFor(OSLinux, false))
	s.SetSubs(nil)
	// The subject form the Usenet grouping actually parses (see parseSubject).
	part := func(id, subject string) source.Item {
		return source.Item{ID: id, Source: source.Usenet, Score: -1, Comments: -1,
			Title: subject, Permalink: "news:<" + id + "@srv>"}
	}
	s.SetItems([]source.Item{
		part("d1", `[1/2] - "release.tar.zst" yEnc (1/1) 1000`),
		part("d2", `[2/2] - "release.tar.zst.par2" yEnc (1/1) 200`),
	})
	s.layout()

	for _, n := range s.A11yTree() {
		if n.Role == toolkit.RoleGroup && strings.Contains(n.Value, "parts") {
			if !strings.Contains(n.Name, "release") {
				t.Fatalf("group card named %q, want the release base", n.Name)
			}
			return
		}
	}
	t.Fatal("the multipart post was not described as a group")
}

// TestA11yTreeWithTheSidebarCollapsed checks the sidebar's rows leave the tree
// with it: announcing controls the user cannot reach would be a lie.
func TestA11yTreeWithTheSidebarCollapsed(t *testing.T) {
	s := a11yScene(t)
	s.ToggleSidebar()
	s.layout()
	tree := s.A11yTree()
	if _, ok := find(tree, "All Sources"); ok {
		t.Error("the collapsed sidebar still advertises its rows")
	}
	// The burger that brings it back must still be there.
	if _, ok := find(tree, "Toggle sidebar"); !ok {
		t.Error("no way to reopen the sidebar")
	}
}
