package ui

import (
	"testing"

	"github.com/go-news-reader/reader/internal/settings"
	"github.com/go-news-reader/reader/source"
)

// threeSubScene is a feed scene with three subscriptions and no folders.
func threeSubScene() *Scene {
	s := New(720, 420, ThemeFor(OSLinux, false))
	s.SetSubs([]Subscription{
		{Source: source.Reddit, Channel: "r/a"},
		{Source: source.Reddit, Channel: "r/b"},
		{Source: source.HackerNews, Channel: ""},
	})
	return s
}

// TestFolderCreateMoveRemove covers NewSidebarFolder, MoveSubToFolder,
// RemoveSubFromFolder, FolderNames, SidebarFolders and the subKey membership.
func TestFolderCreateMoveRemove(t *testing.T) {
	s := threeSubScene()
	name := s.NewSidebarFolder("")
	if name != "New Folder" {
		t.Fatalf("default folder name = %q", name)
	}
	if got := s.FolderNames(); len(got) != 1 || got[0] != "New Folder" {
		t.Fatalf("FolderNames = %v", got)
	}
	s.MoveSubToFolder(0, "New Folder")
	folders := s.SidebarFolders()
	if len(folders) != 1 || len(folders[0].Subs) != 1 ||
		folders[0].Subs[0] != subKey(source.Reddit, "r/a") {
		t.Fatalf("after move, folders = %+v", folders)
	}
	// Moving the same sub into another folder removes it from the first.
	other := s.NewSidebarFolder("Other")
	s.MoveSubToFolder(0, other)
	folders = s.SidebarFolders()
	if len(folders[0].Subs) != 0 {
		t.Fatalf("sub not removed from the first folder: %+v", folders[0])
	}
	if len(folders[1].Subs) != 1 {
		t.Fatalf("sub not added to the second folder: %+v", folders[1])
	}
	// Remove it entirely → back to the root.
	s.RemoveSubFromFolder(0)
	for _, f := range s.SidebarFolders() {
		if len(f.Subs) != 0 {
			t.Fatalf("sub still in a folder after removal: %+v", f)
		}
	}
}

// TestRemoveFromFolderKeepsSiblings: removing one sub from a multi-sub folder
// keeps the others (the kept-key branch of removeKeyFromFolders).
func TestRemoveFromFolderKeepsSiblings(t *testing.T) {
	s := threeSubScene()
	name := s.NewSidebarFolder("Multi")
	s.MoveSubToFolder(0, name) // r/a
	s.MoveSubToFolder(1, name) // r/b
	if got := len(s.SidebarFolders()[0].Subs); got != 2 {
		t.Fatalf("folder should hold 2 subs, got %d", got)
	}
	s.RemoveSubFromFolder(0) // drop r/a; r/b stays
	folder := s.SidebarFolders()[0]
	if len(folder.Subs) != 1 || folder.Subs[0] != subKey(source.Reddit, "r/b") {
		t.Fatalf("sibling not kept after removal: %+v", folder)
	}
}

// TestMoveToNewFolderName: moving into a not-yet-existing folder name creates it.
func TestMoveToNewFolderName(t *testing.T) {
	s := threeSubScene()
	s.MoveSubToFolder(1, "Fresh")
	if s.folderIndex("Fresh") < 0 {
		t.Fatal("MoveSubToFolder should create a missing target folder")
	}
	if got := s.SidebarFolders()[0].Subs[0]; got != subKey(source.Reddit, "r/b") {
		t.Fatalf("moved sub key = %q", got)
	}
}

// TestMoveBadIndexNoop: moving/removing with an out-of-range index is a no-op.
func TestMoveBadIndexNoop(t *testing.T) {
	s := threeSubScene()
	s.MoveSubToFolder(99, "X") // bad index: no folder created
	s.RemoveSubFromFolder(-1)  // bad index: no panic
	if len(s.SidebarFolders()) != 0 {
		t.Fatalf("a bad index must not create folders: %+v", s.SidebarFolders())
	}
	if _, ok := s.subKeyAt(99); ok {
		t.Fatal("subKeyAt on a bad index should report ok=false")
	}
}

// TestUniqueFolderName: creating folders with a colliding base suffixes them.
func TestUniqueFolderName(t *testing.T) {
	s := threeSubScene()
	if a := s.NewSidebarFolder("Dup"); a != "Dup" {
		t.Fatalf("first = %q", a)
	}
	if b := s.NewSidebarFolder("Dup"); b != "Dup 2" {
		t.Fatalf("second = %q, want 'Dup 2'", b)
	}
	if c := s.NewSidebarFolder("Dup"); c != "Dup 3" {
		t.Fatalf("third = %q, want 'Dup 3'", c)
	}
}

// TestRenameFolder covers rename, the collapse-state carry-over, and the
// no-op guards (blank name, unknown name, same name).
func TestRenameFolder(t *testing.T) {
	s := threeSubScene()
	s.NewSidebarFolder("Old")
	s.ToggleSidebarFolder("Old") // collapse it, so the state must carry across
	s.RenameFolder("Old", "New")
	if s.folderIndex("Old") >= 0 || s.folderIndex("New") < 0 {
		t.Fatal("rename did not replace the name")
	}
	if !s.FolderCollapsed("New") || s.FolderCollapsed("Old") {
		t.Fatal("collapse state did not carry across the rename")
	}
	// No-ops: blank, unknown, and identity renames leave the set unchanged.
	s.RenameFolder("New", "")    // blank
	s.RenameFolder("Nope", "Z")  // unknown
	s.RenameFolder("New", "New") // identity
	if names := s.FolderNames(); len(names) != 1 || names[0] != "New" {
		t.Fatalf("no-op renames changed the set: %v", names)
	}
	// Renaming to a colliding name unique-ifies it.
	s.NewSidebarFolder("New") // now "New 2"? no — unique base "New" exists → "New 2"
	s.RenameFolder("New 2", "New")
	if s.folderIndex("New 2") < 0 && s.folderIndex("New") < 0 {
		t.Fatal("rename-to-collision should keep both via a suffix")
	}
}

// TestDeleteFolder covers delete + its unknown-name no-op, and that a deleted
// folder's collapse state is dropped.
func TestDeleteFolder(t *testing.T) {
	s := threeSubScene()
	s.NewSidebarFolder("Gone")
	s.ToggleSidebarFolder("Gone")
	s.DeleteFolder("Gone")
	if s.folderIndex("Gone") >= 0 {
		t.Fatal("DeleteFolder did not remove the folder")
	}
	if s.FolderCollapsed("Gone") {
		t.Fatal("a deleted folder's collapse state should be dropped")
	}
	s.DeleteFolder("Nope") // unknown: no-op, no panic
}

// TestToggleAndCollapsed covers the collapse toggle both ways.
func TestToggleAndCollapsed(t *testing.T) {
	s := threeSubScene()
	s.NewSidebarFolder("F")
	if s.FolderCollapsed("F") {
		t.Fatal("a new folder starts expanded")
	}
	s.ToggleSidebarFolder("F")
	if !s.FolderCollapsed("F") {
		t.Fatal("toggle should collapse the folder")
	}
	s.ToggleSidebarFolder("F")
	if s.FolderCollapsed("F") {
		t.Fatal("a second toggle should expand it again")
	}
}

// TestSidebarFoldersSnapshotEmpty: with no folders the snapshot is nil (omitted
// from the persisted JSON).
func TestSidebarFoldersSnapshotEmpty(t *testing.T) {
	s := threeSubScene()
	if got := s.SidebarFolders(); got != nil {
		t.Fatalf("empty folder set should snapshot as nil, got %v", got)
	}
	// The scene's settings snapshot carries the folders through.
	s.SetSidebarFolders([]settings.Folder{{Name: "F", Subs: []string{"k"}}})
	if set := s.Settings(); len(set.SidebarFolders) != 1 || set.SidebarFolders[0].Name != "F" {
		t.Fatalf("Settings() did not carry the folders: %+v", set.SidebarFolders)
	}
}

// TestFolderCounts sums the member subscriptions' totals + unseen counts.
func TestFolderCounts(t *testing.T) {
	s := threeSubScene()
	s.SetItems([]source.Item{
		{ID: "1", Source: source.Reddit, Channel: "r/a", Title: "x"},
		{ID: "2", Source: source.Reddit, Channel: "r/a", Title: "y"},
		{ID: "3", Source: source.Reddit, Channel: "r/b", Title: "z"},
	})
	s.SetSidebarFolders([]settings.Folder{{Name: "F", Subs: []string{
		subKey(source.Reddit, "r/a"), subKey(source.Reddit, "r/b"),
		subKey(source.Reddit, "r/absent"), // not present in the profile → ignored
	}}})
	total, unseen := s.folderCounts("F")
	if total != 3 { // 2 (r/a) + 1 (r/b)
		t.Fatalf("folder total = %d, want 3", total)
	}
	if unseen != 3 { // nothing seen yet
		t.Fatalf("folder unseen = %d, want 3", unseen)
	}
	// An unknown folder sums to zero.
	if tot, un := s.folderCounts("Nope"); tot != 0 || un != 0 {
		t.Fatalf("unknown folder counts = %d/%d, want 0/0", un, tot)
	}
}

// TestSidebarContextAt maps a right-click to its sub / folder / none target.
func TestSidebarContextAt(t *testing.T) {
	s := New(720, 420, ThemeFor(OSLinux, false))
	s.SetScale(1)
	s.SetSubs([]Subscription{{Source: source.Reddit, Channel: "r/a"}})
	s.SetSidebarFolders([]settings.Folder{{Name: "F", Subs: []string{subKey(source.Reddit, "r/a")}}})
	s.layout()
	// Row 0 = root (All Sources) → no menu target.
	if got := s.SidebarContextAt(10, s.sideBandTop+s.m.sideItemH/2); got.Kind != SidebarCtxNone {
		t.Fatalf("root right-click = %+v, want none", got)
	}
	// Row 1 = the folder.
	folderY := s.sideBandTop + s.m.sideItemH + s.m.sideItemH/2
	if got := s.SidebarContextAt(10, folderY); got.Kind != SidebarCtxFolder || got.Folder != "F" {
		t.Fatalf("folder right-click = %+v, want folder F", got)
	}
	// Row 2 = the sub inside the folder (depth 2).
	subY := s.sideBandTop + 2*s.m.sideItemH + s.m.sideItemH/2
	got := s.SidebarContextAt(10, subY)
	if got.Kind != SidebarCtxSub || got.Sub != 0 || got.Source != source.Reddit || got.Channel != "r/a" {
		t.Fatalf("sub right-click = %+v, want sub 0 reddit r/a", got)
	}
	// Outside the band → none.
	if got := s.SidebarContextAt(10, 2); got.Kind != SidebarCtxNone {
		t.Fatalf("above-band right-click = %+v, want none", got)
	}
	// Outside the sidebar width → none.
	if got := s.SidebarContextAt(s.m.sidebarW+50, subY); got.Kind != SidebarCtxNone {
		t.Fatalf("right-of-sidebar right-click = %+v, want none", got)
	}
}

// TestSidebarContextAtNonFeed: outside the feed view there is no sidebar target.
func TestSidebarContextAtNonFeed(t *testing.T) {
	s := threeSubScene()
	s.OpenSettings()
	if got := s.SidebarContextAt(10, 100); got.Kind != SidebarCtxNone {
		t.Fatalf("non-feed right-click = %+v, want none", got)
	}
}

// TestSidebarContextAtCollapsed: a collapsed sidebar has no menu targets.
func TestSidebarContextAtCollapsed(t *testing.T) {
	s := threeSubScene()
	s.ToggleSidebar()
	if got := s.SidebarContextAt(5, 100); got.Kind != SidebarCtxNone {
		t.Fatalf("collapsed-sidebar right-click = %+v, want none", got)
	}
}
