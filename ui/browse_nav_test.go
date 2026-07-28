package ui

import (
	"strconv"
	"testing"

	"github.com/go-news-reader/reader/internal/settings"
	"github.com/go-news-reader/reader/source"
)

func TestBrowseKeyboardNav(t *testing.T) {
	s := browseScene() // top-level hierarchies: alt, comp, fr
	s.layoutBrowse()
	if n, hc, _, _, ok := s.BrowseSelectedNode(); !ok || n != "alt" || !hc {
		t.Fatalf("initial row = %q hasChildren=%v ok=%v", n, hc, ok)
	}
	s.NavBrowse(1)
	if n, _, _, _, _ := s.BrowseSelectedNode(); n != "comp" {
		t.Fatalf("after Down = %q, want comp", n)
	}
	s.NavBrowse(-5) // clamp at the top
	if n, _, _, _, _ := s.BrowseSelectedNode(); n != "alt" {
		t.Fatalf("clamp-top = %q, want alt", n)
	}
	s.NavBrowse(9) // clamp at the bottom
	if n, _, _, _, _ := s.BrowseSelectedNode(); n != "fr" {
		t.Fatalf("clamp-bottom = %q, want fr", n)
	}

	// Expand alt and select the alt.test leaf group (a real group, no children).
	s.ToggleBrowseNode("alt")
	s.layoutBrowse()
	s.browseSel = 0
	s.NavBrowse(2) // alt, alt.binaries, alt.test
	n, hc, isGroup, subbed, ok := s.BrowseSelectedNode()
	if !ok || n != "alt.test" || hc || !isGroup || subbed {
		t.Fatalf("alt.test = %q hasChildren=%v group=%v subscribed=%v", n, hc, isGroup, subbed)
	}
	// Subscribing marks it, which BrowseSelectedNode then reports.
	s.SubscribeActive(source.Usenet, n)
	if _, _, _, subbed, _ := s.BrowseSelectedNode(); !subbed {
		t.Fatal("alt.test should read as subscribed after SubscribeActive")
	}

	// An empty tree yields no selection and NavBrowse is a no-op.
	empty := New(900, 640, ThemeFor(OSLinux, false))
	empty.SetProfiles([]settings.Profile{{Name: "Home"}}, 0)
	empty.SetUsenetServer("x:119")
	empty.SetBrowseGroups(nil)
	empty.OpenBrowse()
	empty.NavBrowse(1)
	if _, _, _, _, ok := empty.BrowseSelectedNode(); ok {
		t.Fatal("empty tree must have no selection")
	}
}

func TestBrowseNavScrollsIntoView(t *testing.T) {
	s := New(760, 300, ThemeFor(OSLinux, false)) // short window → the tree overflows
	s.SetProfiles([]settings.Profile{{Name: "Home"}}, 0)
	s.SetUsenetServer("news.free.fr:119")
	groups := make([]source.GroupInfo, 60)
	for i := range groups {
		groups[i] = source.GroupInfo{Name: "grp" + strconv.Itoa(i)} // flat top-level leaves
	}
	s.SetBrowseGroups(groups)
	s.OpenBrowse()

	for range groups {
		s.NavBrowse(1) // walk to the bottom
	}
	if s.browseScrollY == 0 {
		t.Fatal("navigating to the bottom should scroll the tree")
	}
	for range groups {
		s.NavBrowse(-1) // walk back to the top
	}
	if s.browseScrollY != 0 {
		t.Fatalf("navigating to the top should scroll back to 0, got %d", s.browseScrollY)
	}
}
