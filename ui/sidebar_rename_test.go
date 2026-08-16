package ui

import "testing"

// TestBeginFolderRenameUnknown: renaming a folder that does not exist is a no-op.
func TestBeginFolderRenameUnknown(t *testing.T) {
	s := foldersScene(t)
	s.BeginFolderRename("Ghost")
	if s.FolderRenaming() {
		t.Fatal("renaming an unknown folder must not begin an edit")
	}
}

// TestFolderRenameLifecycle drives Begin → type → Commit and asserts the folder
// was renamed and the state cleared.
func TestFolderRenameLifecycle(t *testing.T) {
	s := foldersScene(t)
	s.BeginFolderRename("Langs")
	if !s.FolderRenaming() || s.RenamingFolder() != "Langs" {
		t.Fatalf("BeginFolderRename did not arm the editor: %q", s.RenamingFolder())
	}
	// Typing + Backspace edit the Entry buffer through the shared key path.
	s.TypeRune('!') // "Langs!"
	s.Backspace()   // "Langs"
	for _, r := range " x" {
		s.TypeRune(r) // "Langs x"
	}
	s.CommitFolderRename()
	if s.FolderRenaming() {
		t.Fatal("CommitFolderRename must clear the rename state")
	}
	if names := s.FolderNames(); len(names) != 1 || names[0] != "Langs x" {
		t.Fatalf("committed folder names = %v, want [Langs x]", names)
	}
}

// TestFolderRenameBlankKept: a buffer that trims to empty leaves the name alone.
func TestFolderRenameBlankKept(t *testing.T) {
	s := foldersScene(t)
	s.BeginFolderRename("Langs")
	// Delete the whole seeded name, then leave whitespace only.
	for range "Langs" {
		s.Backspace()
	}
	s.TypeRune(' ')
	s.CommitFolderRename()
	if names := s.FolderNames(); len(names) != 1 || names[0] != "Langs" {
		t.Fatalf("blank rename changed the folder to %v, want [Langs]", names)
	}
}

// TestFolderRenameUnchangedKept: committing the identical name is a no-op rename.
func TestFolderRenameUnchangedKept(t *testing.T) {
	s := foldersScene(t)
	s.BeginFolderRename("Langs")
	s.CommitFolderRename() // buffer still "Langs" → unchanged
	if names := s.FolderNames(); len(names) != 1 || names[0] != "Langs" {
		t.Fatalf("unchanged rename altered the folder to %v, want [Langs]", names)
	}
}

// TestFolderRenameCommitCancelNoop: Commit/Cancel with no rename in progress are
// no-ops (covering their empty-state guards).
func TestFolderRenameCommitCancelNoop(t *testing.T) {
	s := foldersScene(t)
	s.CommitFolderRename() // no rename armed
	s.CancelFolderRename() // no rename armed
	if s.FolderRenaming() {
		t.Fatal("no rename should be in progress")
	}
}

// TestFolderRenameCancel: Cancel discards the buffer and keeps the folder name.
func TestFolderRenameCancel(t *testing.T) {
	s := foldersScene(t)
	s.BeginFolderRename("Langs")
	s.TypeRune('Z')
	s.CancelFolderRename()
	if s.FolderRenaming() {
		t.Fatal("CancelFolderRename must clear the rename state")
	}
	if names := s.FolderNames(); len(names) != 1 || names[0] != "Langs" {
		t.Fatalf("cancelled rename changed the folder to %v, want [Langs]", names)
	}
}

// TestFolderRenameDrawsEntry: while a folder is being renamed, its row draws the
// focused Entry (over the label), so the Entry's bounds get laid out into the
// row after a Draw.
func TestFolderRenameDrawsEntry(t *testing.T) {
	s := foldersScene(t)
	s.BeginFolderRename("Langs")
	buf := make([]byte, s.W*s.H*4)
	s.Draw(buf)
	e := s.renameFolderEntry
	if e == nil {
		t.Fatal("the rename Entry must persist across the draw")
	}
	b := e.Bounds()
	// "Langs" is the folder at visible row 1 (row 0 is "All Sources"), so its row
	// content is laid out one row height down from the tree's top, past the folder
	// icon (X > 0), with a real width + height.
	if b.Y != s.m.sideItemH {
		t.Fatalf("rename Entry row Y = %d, want row 1 at %d", b.Y, s.m.sideItemH)
	}
	if b.X <= 0 || b.W <= 0 || b.H != s.m.sideItemH {
		t.Fatalf("rename Entry rect %+v not laid into the folder row (h=%d)", b, s.m.sideItemH)
	}
}
