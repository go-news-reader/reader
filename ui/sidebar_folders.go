package ui

// Virtual sidebar folders: named, collapsible groups of subscriptions the user
// builds from the sidebar context menu. The model is a []settings.Folder (a
// name + the stable subscription keys it holds); the reader mutates it, keeps a
// session collapse state per folder, and persists through the usual settings
// snapshot. buildSideTree (sidebar_tree.go) groups the subscriptions under their
// folder nodes; anything not in a folder shows at the sidebar root.

import (
	"strings"

	"github.com/go-widgets/toolkit"

	"github.com/go-news-reader/reader/internal/settings"
	"github.com/go-news-reader/reader/source"
)

// SidebarFolders returns a copy of the current virtual-folder set (a stable
// snapshot for the settings persistence layer).
func (s *Scene) SidebarFolders() []settings.Folder {
	if len(s.folders) == 0 {
		return nil
	}
	out := make([]settings.Folder, len(s.folders))
	for i, f := range s.folders {
		subs := append([]string(nil), f.Subs...)
		out[i] = settings.Folder{Name: f.Name, Subs: subs}
	}
	return out
}

// SetSidebarFolders replaces the virtual-folder set (seeded from the persisted
// settings at startup) and re-renders the sidebar.
func (s *Scene) SetSidebarFolders(folders []settings.Folder) {
	s.folders = folders
	s.foldRev++
	s.touch()
}

// FolderNames returns the folder names in their current order (for the "Move to
// folder ▸" submenu).
func (s *Scene) FolderNames() []string {
	out := make([]string, 0, len(s.folders))
	for _, f := range s.folders {
		out = append(out, f.Name)
	}
	return out
}

// folderIndex returns the index of the folder named name, or -1.
func (s *Scene) folderIndex(name string) int {
	for i, f := range s.folders {
		if f.Name == name {
			return i
		}
	}
	return -1
}

// uniqueFolderName returns base, or base with a numeric suffix, so a new folder
// never collides with an existing name.
func (s *Scene) uniqueFolderName(base string) string {
	if s.folderIndex(base) < 0 {
		return base
	}
	for n := 2; ; n++ {
		cand := base + " " + itoa(n)
		if s.folderIndex(cand) < 0 {
			return cand
		}
	}
}

// NewSidebarFolder creates a new (empty) folder with a unique name derived from
// base (blank → "New Folder") and returns its final name.
func (s *Scene) NewSidebarFolder(base string) string {
	if base == "" {
		base = "New Folder"
	}
	name := s.uniqueFolderName(base)
	s.folders = append(s.folders, settings.Folder{Name: name})
	s.foldRev++
	s.touch()
	return name
}

// MoveSubToFolder moves the subscription at sub index into the folder named
// folderName (creating the folder if it does not yet exist), removing the
// subscription from any other folder first. A bad sub index is a no-op.
func (s *Scene) MoveSubToFolder(sub int, folderName string) {
	key, ok := s.subKeyAt(sub)
	if !ok {
		return
	}
	s.removeKeyFromFolders(key)
	i := s.folderIndex(folderName)
	if i < 0 {
		folderName = s.NewSidebarFolder(folderName)
		i = s.folderIndex(folderName)
	}
	s.folders[i].Subs = append(s.folders[i].Subs, key)
	s.foldRev++
	s.touch()
}

// RemoveSubFromFolder takes the subscription at sub index out of whatever folder
// holds it, so it returns to the sidebar root. A bad index (or a subscription in
// no folder) is a no-op beyond the redraw.
func (s *Scene) RemoveSubFromFolder(sub int) {
	key, ok := s.subKeyAt(sub)
	if !ok {
		return
	}
	s.removeKeyFromFolders(key)
	s.foldRev++
	s.touch()
}

// RenameFolder renames the folder named old to newName (unique-ified), carrying
// its collapse state across. A blank new name, an unknown old name, or old ==
// new is a no-op.
func (s *Scene) RenameFolder(old, newName string) {
	if newName == "" || old == newName {
		return
	}
	i := s.folderIndex(old)
	if i < 0 {
		return
	}
	name := s.uniqueFolderName(newName)
	s.folders[i].Name = name
	if s.folderCollapsed != nil && s.folderCollapsed[old] {
		delete(s.folderCollapsed, old)
		s.folderCollapsed[name] = true
	}
	s.foldRev++
	s.touch()
}

// FolderRenaming reports whether a folder is currently being renamed inline.
func (s *Scene) FolderRenaming() bool { return s.renamingFolder != "" }

// RenamingFolder returns the folder currently being renamed inline, or "" when
// none is (for tests / front-ends).
func (s *Scene) RenamingFolder() string { return s.renamingFolder }

// BeginFolderRename starts an inline rename of the folder named name: it seeds a
// focused toolkit.Entry with the current name (caret at the end) as the edit
// buffer. An unknown folder name is a no-op.
func (s *Scene) BeginFolderRename(name string) {
	if s.folderIndex(name) < 0 {
		return
	}
	s.renamingFolder = name
	s.renameFolderEntry = toolkit.NewEntry(name)
	s.renameFolderEntry.SetFocused(true)
	s.foldRev++
	s.touch()
}

// CommitFolderRename applies the inline rename when the edit buffer is non-blank
// and actually changed, then clears the rename state. The window layer persists
// the settings afterwards (App.PersistSettings), matching the other folder edits.
func (s *Scene) CommitFolderRename() {
	if s.renamingFolder == "" {
		return
	}
	old := s.renamingFolder
	buf := strings.TrimSpace(s.renameFolderEntry.Text().Get())
	if buf != "" && buf != old {
		s.RenameFolder(old, buf)
	}
	s.renamingFolder = ""
	s.renameFolderEntry = nil
	s.foldRev++
	s.touch()
}

// CancelFolderRename abandons an in-progress inline rename, discarding the edit
// buffer and leaving the folder's name unchanged.
func (s *Scene) CancelFolderRename() {
	if s.renamingFolder == "" {
		return
	}
	s.renamingFolder = ""
	s.renameFolderEntry = nil
	s.foldRev++
	s.touch()
}

// DeleteFolder removes the folder named name; its subscriptions return to the
// sidebar root. An unknown name is a no-op.
func (s *Scene) DeleteFolder(name string) {
	i := s.folderIndex(name)
	if i < 0 {
		return
	}
	s.folders = append(s.folders[:i], s.folders[i+1:]...)
	if s.folderCollapsed != nil {
		delete(s.folderCollapsed, name)
	}
	s.foldRev++
	s.touch()
}

// ToggleSidebarFolder flips a folder's collapse state (session-only). It is the
// action a click on a folder row runs.
func (s *Scene) ToggleSidebarFolder(name string) {
	if s.folderCollapsed == nil {
		s.folderCollapsed = map[string]bool{}
	}
	s.folderCollapsed[name] = !s.folderCollapsed[name]
	s.foldRev++
	s.touch()
}

// FolderCollapsed reports whether the folder named name is currently collapsed
// (for tests / front-ends).
func (s *Scene) FolderCollapsed(name string) bool { return s.folderCollapsed[name] }

// ToggleSidebarSource drives the sidebar's source accordion: it expands kind's
// auto-group (collapsing whatever source was open), or collapses it if it was
// already the open one. Session-only, the action a click on a source header row
// runs; only one source group is ever open, keeping a huge subscription list
// short.
func (s *Scene) ToggleSidebarSource(kind source.Kind) {
	if s.sourceOpen == kind {
		s.sourceOpen = ""
	} else {
		s.sourceOpen = kind
	}
	s.sourceSubScroll = 0 // a freshly opened section starts at its first account
	s.foldRev++
	s.touch()
}

// SidebarSourceOpen reports which source group is currently expanded in the
// sidebar accordion, or "" when they are all collapsed (for tests / front-ends).
func (s *Scene) SidebarSourceOpen() source.Kind { return s.sourceOpen }

// removeKeyFromFolders drops key from every folder that lists it.
func (s *Scene) removeKeyFromFolders(key string) {
	for i := range s.folders {
		out := s.folders[i].Subs[:0]
		for _, k := range s.folders[i].Subs {
			if k != key {
				out = append(out, k)
			}
		}
		s.folders[i].Subs = out
	}
}

// subKeyAt returns the stable subscription key for the sub at index, or ok=false
// for an out-of-range index (e.g. AllFilter).
func (s *Scene) subKeyAt(sub int) (string, bool) {
	if sub < 0 || sub >= len(s.Subs) {
		return "", false
	}
	su := s.Subs[sub]
	return subKey(su.Source, su.Channel), true
}

// SidebarContext identifies what a sidebar right-click landed on, so the window
// layer can build the appropriate context menu. Kind is one of the exported
// SidebarCtx* constants; Sub/Source/Channel describe a subscription target and
// Folder names a folder target.
type SidebarContext struct {
	Kind    SidebarCtxKind
	Sub     int
	Source  source.Kind
	Channel string
	Folder  string
}

// SidebarCtxKind classifies a sidebar right-click target.
type SidebarCtxKind int

const (
	SidebarCtxNone   SidebarCtxKind = iota // empty space or a non-menuable entry
	SidebarCtxSub                          // a subscription row
	SidebarCtxFolder                       // a folder row
)

// SidebarContextAt maps a screen point to the sidebar context-menu target under
// it: a subscription, a folder, or nothing. The window layer calls it on a
// secondary click.
func (s *Scene) SidebarContextAt(x, y int) SidebarContext {
	node := s.sidebarNodeAt(x, y)
	if node == nil {
		return SidebarContext{}
	}
	switch d := sideData(node); d.Kind {
	case sideSub:
		if d.Sub >= 0 && d.Sub < len(s.Subs) {
			sub := s.Subs[d.Sub]
			return SidebarContext{Kind: SidebarCtxSub, Sub: d.Sub, Source: sub.Source, Channel: sub.Channel}
		}
	case sideFolder:
		return SidebarContext{Kind: SidebarCtxFolder, Folder: d.Folder}
	}
	return SidebarContext{}
}

// sidebarNodeAt returns the sidebar TreeView node under screen point (x, y), or
// nil when the point is outside the sidebar band or over empty space. It lays
// the scene out first so the tree + band are current.
func (s *Scene) sidebarNodeAt(x, y int) *toolkit.TreeNode {
	if s.mode != ModeFeed {
		return nil
	}
	s.layout()
	if s.sideTree == nil || s.m.sidebarW == 0 {
		return nil
	}
	if x < 0 || x >= s.m.sidebarW || y < s.sideBandTop || y >= s.sideBandBot {
		return nil
	}
	return s.sideTree.NodeAt(x, y-s.sideBandTop)
}
