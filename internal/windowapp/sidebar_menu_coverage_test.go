package windowapp

import (
	"testing"

	"github.com/go-widgets/toolkit"

	"github.com/go-news-reader/reader/app"
	"github.com/go-news-reader/reader/source"
	"github.com/go-news-reader/reader/ui"
)

// menuItem returns the context-menu item with the given label, or nil.
func menuItem(menu *toolkit.ContextMenu, label string) *toolkit.MenuItem {
	for i := range menu.Menu.Items {
		if menu.Menu.Items[i].Label == label {
			return &menu.Menu.Items[i]
		}
	}
	return nil
}

// findSidebarFolder scans the sidebar for a folder row's screen coordinates.
func findSidebarFolder(s *ui.Scene) (int, int, bool) {
	for y := 0; y < s.H; y += 2 {
		for x := 0; x < s.W; x += 2 {
			if ctx := s.SidebarContextAt(x, y); ctx.Kind == ui.SidebarCtxFolder {
				return x, y, true
			}
		}
	}
	return 0, 0, false
}

// TestRunHitItemAndToggleFolder covers the two runHit arms no other test reaches:
// selecting a feed item, and toggling a sidebar virtual folder's collapse state.
func TestRunHitItemAndToggleFolder(t *testing.T) {
	a := app.New(app.Config{Registry: source.NewRegistry(), Width: 800, Height: 600})
	a.DeferSceneWrites() // serialise any async preview writes the select spawns
	h := New(a)

	h.runHit(ui.Hit{Kind: ui.HitItem, Item: source.Item{ID: "x", Source: source.Reddit, Body: "hi"}})
	if _, ok := a.Scene().PreviewItem(); !ok {
		t.Fatal("HitItem should select a preview item")
	}

	// HitToggleFolder flips a folder's collapse flag; it must not panic on an
	// unknown folder name (the state is a plain map).
	h.runHit(ui.Hit{Kind: ui.HitToggleFolder, Value: "Work"})
}

// TestSubContextMenuMoveToFolder covers subContextMenu's "Move to folder"
// submenu branch (only built when a folder exists) and the New folder / submenu /
// Remove from folder action closures.
func TestSubContextMenuMoveToFolder(t *testing.T) {
	a := app.New(app.Config{Registry: source.NewRegistry(), Width: 800, Height: 600})
	a.DeferSceneWrites()
	h := New(a)
	s := a.Scene()
	s.SetSubs([]ui.Subscription{{Source: source.HackerNews, Channel: "top"}})
	s.NewSidebarFolder("Work") // FolderNames non-empty → the submenu appears

	menu := h.subContextMenu(0, source.HackerNews, "top")
	var move, newFolder, remove *toolkit.MenuItem
	for i := range menu.Menu.Items {
		switch menu.Menu.Items[i].Label {
		case "Move to folder":
			move = &menu.Menu.Items[i]
		case "New folder":
			newFolder = &menu.Menu.Items[i]
		case "Remove from folder":
			remove = &menu.Menu.Items[i]
		}
	}
	if move == nil || move.Submenu == nil || len(move.Submenu.Items) == 0 {
		t.Fatal("a Move to folder submenu should list the existing folder")
	}
	move.Submenu.Items[0].Action() // move the sub into "Work"
	if newFolder == nil {
		t.Fatal("expected a New folder item")
	}
	newFolder.Action() // create + move into a fresh folder
	if remove == nil {
		t.Fatal("expected a Remove from folder item")
	}
	remove.Action() // take the sub back out to the root
}

// TestFolderContextMenu covers folderContextMenu (Delete folder) and the
// SidebarCtxFolder arm of SecondaryClick.
func TestFolderContextMenu(t *testing.T) {
	a := app.New(app.Config{Registry: source.NewRegistry(), Width: 1000, Height: 700})
	a.DeferSceneWrites()
	h := New(a)
	s := a.Scene()
	s.SetSubs([]ui.Subscription{{Source: source.HackerNews, Channel: "top"}})
	s.MoveSubToFolder(0, "Work") // "Work" now holds the sub → it renders as a folder row

	// Direct: the Delete folder action removes the folder.
	menu := h.folderContextMenu("Work")
	del := menuItem(menu, "Delete folder")
	if del == nil {
		t.Fatal("folder menu should offer Delete folder")
	}
	del.Action() // Delete folder
	for _, n := range s.FolderNames() {
		if n == "Work" {
			t.Fatal("Delete folder should have removed the folder")
		}
	}

	// Right-click a folder row opens its context menu (SecondaryClick folder arm).
	s.MoveSubToFolder(0, "Work") // recreate the folder row
	fx, fy, ok := findSidebarFolder(s)
	if !ok {
		t.Fatal("no folder row found in the sidebar")
	}
	h.SecondaryClick(fx, fy)
	if !h.ContextMenuActive() {
		t.Fatal("right-click on a folder should open its context menu")
	}
}

// TestKeyFeedPaging covers the PageUp/PageDown/Home/End arm of Key, which routes
// the paging keys into the feed CardList while in the feed view.
func TestKeyFeedPaging(t *testing.T) {
	a := app.New(app.Config{Registry: source.NewRegistry(), Width: 800, Height: 600})
	h := New(a)
	a.Scene().SetItems([]source.Item{
		{ID: "1", Source: source.Reddit, Title: "a", Score: -1, Comments: -1},
		{ID: "2", Source: source.Reddit, Title: "b", Score: -1, Comments: -1},
	})
	for _, k := range []string{"PageDown", "PageUp", "Home", "End"} {
		h.Key(k, 0) // ModeFeed → FocusSearch(false) + FeedEvent
	}
}
