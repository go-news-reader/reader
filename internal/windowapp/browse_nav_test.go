package windowapp

import (
	"testing"

	"github.com/go-news-reader/reader/source"
	"github.com/go-news-reader/reader/ui"
)

// TestRouteBrowseKeyboardNav covers arrow-key navigation + Enter activation in
// the newsgroup browser: move the selection, expand a hierarchy, subscribe and
// unsubscribe a leaf group, and Enter with nothing selected.
func TestRouteBrowseKeyboardNav(t *testing.T) {
	h := browseHandler(t) // tree: alt (→ binaries→test, test), comp→lang→go
	s := h.a.Scene()
	h.a.VM().OpenBrowse.Execute()
	s.OpenBrowse()

	// Down/Up move the selection across the top-level hierarchies.
	h.Key("Down", 0)
	if n, _, _, _, _ := s.BrowseSelectedNode(); n != "comp" {
		t.Fatalf("after Down = %q, want comp", n)
	}
	h.Key("Up", 0)
	if n, _, _, _, _ := s.BrowseSelectedNode(); n != "alt" {
		t.Fatalf("after Up = %q, want alt", n)
	}

	// Enter on a hierarchy expands it.
	h.Key("Enter", 0)
	if !s.BrowseNodeExpanded("alt") {
		t.Fatal("Enter on a hierarchy should expand it")
	}

	// Navigate down to the alt.test leaf group and subscribe it with Enter.
	h.Key("Down", 0) // alt.binaries
	h.Key("Down", 0) // alt.test
	if n, _, isGroup, _, _ := s.BrowseSelectedNode(); n != "alt.test" || !isGroup {
		t.Fatalf("selection = %q group=%v, want alt.test group", n, isGroup)
	}
	h.Key("Enter", 0)
	if !s.IsSubscribed(source.Usenet, "alt.test") {
		t.Fatal("Enter on a leaf group should subscribe it")
	}
	// Enter again unsubscribes.
	h.Key("Enter", 0)
	if s.IsSubscribed(source.Usenet, "alt.test") {
		t.Fatal("Enter again should unsubscribe the group")
	}

	// Enter with no selectable row just defocuses the filter (no panic).
	s.SetBrowseGroups(nil)
	s.OpenBrowse()
	s.FocusBrowseFilter(true)
	h.Key("Enter", 0)
	if s.BrowseFocused() {
		t.Fatal("Enter with no selection should defocus the filter")
	}
	_ = ui.ModeBrowse
}
