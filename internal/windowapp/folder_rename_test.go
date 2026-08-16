package windowapp

import (
	"testing"

	"github.com/go-news-reader/reader/app"
	"github.com/go-news-reader/reader/source"
	"github.com/go-news-reader/reader/ui"
)

// renameScene builds an app + handler whose active profile holds one folder
// ("Work") with a subscription in it, so the folder renders as a sidebar row.
func renameHandler(t *testing.T) (*Handler, *ui.Scene) {
	t.Helper()
	a := app.New(app.Config{Registry: source.NewRegistry(), Width: 1000, Height: 700})
	a.DeferSceneWrites()
	h := New(a)
	s := a.Scene()
	s.SetSubs([]ui.Subscription{{Source: source.HackerNews, Channel: "top"}})
	s.MoveSubToFolder(0, "Work") // "Work" now holds the sub → it renders as a folder row
	return h, s
}

// TestFolderContextMenuRename covers folderContextMenu's Rename item, which opens
// the inline editor via Scene.BeginFolderRename.
func TestFolderContextMenuRename(t *testing.T) {
	h, s := renameHandler(t)
	menu := h.folderContextMenu("Work")
	rename := menuItem(menu, "Rename")
	if rename == nil {
		t.Fatal("folder menu should offer Rename")
	}
	rename.Action()
	if !s.FolderRenaming() || s.RenamingFolder() != "Work" {
		t.Fatalf("Rename should begin an inline rename of Work, renaming=%q", s.RenamingFolder())
	}
}

// TestFolderRenameKeyCommit types a new name into the inline editor and commits
// it with Enter, covering the printable-rune, Backspace and Enter arms of
// folderRenameKey and the persist that follows.
func TestFolderRenameKeyCommit(t *testing.T) {
	h, s := renameHandler(t)
	s.BeginFolderRename("Work")
	// Backspace deletes the seeded name's last rune ("Work" → "Wor"), then typing
	// appends — proving Backspace + printable runes both reach the Entry buffer.
	h.Key("Backspace", 0)
	for _, r := range "ld" { // "Wor" + "ld" → "World"
		h.Key("", r)
	}
	h.Key("Enter", 0)
	if s.FolderRenaming() {
		t.Fatal("Enter should end the rename")
	}
	names := s.FolderNames()
	if len(names) != 1 || names[0] != "World" {
		t.Fatalf("committed folder names = %v, want [World]", names)
	}
}

// TestFolderRenameKeyEscape cancels an inline rename with Escape, leaving the
// folder name unchanged.
func TestFolderRenameKeyEscape(t *testing.T) {
	h, s := renameHandler(t)
	s.BeginFolderRename("Work")
	h.Key("", 'Z')     // edit the buffer
	h.Key("Escape", 0) // cancel
	if s.FolderRenaming() {
		t.Fatal("Escape should end the rename")
	}
	if names := s.FolderNames(); len(names) != 1 || names[0] != "Work" {
		t.Fatalf("cancelled rename changed the folder to %v, want [Work]", names)
	}
}

// TestFolderRenameKeyFallThrough covers the two folderRenameKey arms that decline
// the event: no rename in progress, and an arrow key while renaming (which must
// fall through to feed navigation rather than be swallowed).
func TestFolderRenameKeyFallThrough(t *testing.T) {
	h, _ := renameHandler(t)
	if h.folderRenameKey("Down", 0) {
		t.Fatal("with no rename in progress the key must not be consumed")
	}
	h.a.Scene().BeginFolderRename("Work")
	if h.folderRenameKey("Down", 0) {
		t.Fatal("an arrow key must fall through to feed navigation, not be consumed")
	}
	if !h.folderRenameKey("x", 'x') {
		t.Fatal("a printable rune must be consumed by the active rename")
	}
}

// TestFolderRenameCommitOnBlur commits an in-progress rename when a press lands
// elsewhere (MouseDown), matching the browse/settings commit-on-blur precedent.
func TestFolderRenameCommitOnBlur(t *testing.T) {
	h, s := renameHandler(t)
	s.BeginFolderRename("Work")
	h.Key("", 'X') // "WorkX"
	h.MouseDown(5, 5)
	if s.FolderRenaming() {
		t.Fatal("a press elsewhere should commit + end the rename")
	}
	if names := s.FolderNames(); len(names) != 1 || names[0] != "WorkX" {
		t.Fatalf("blur-committed folder names = %v, want [WorkX]", names)
	}
}
