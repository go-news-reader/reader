// Package windowapp adapts an *app.App to the window.Handler contract: it
// exposes the app's double-buffered framebuffer to a native presenter and
// routes native mouse/scroll/key events into the ui.Scene. It is deliberately
// decoupled from the window package (it satisfies the interface structurally),
// so both build on every platform and the routing logic is unit-testable
// without any Cocoa dependency.
package windowapp

import (
	"os/exec"
	"runtime"

	"github.com/go-news-reader/reader/app"
	"github.com/go-news-reader/reader/internal/window"
	"github.com/go-news-reader/reader/source"
	"github.com/go-news-reader/reader/ui"
)

// execStart runs a command detached; a seam so tests avoid spawning processes.
var execStart = func(name string, args ...string) error {
	return exec.Command(name, args...).Start()
}

// openURL opens a URL in the user's default browser. A package var so callers
// (and tests) can substitute it.
var openURL = defaultOpenURL

// browserCommand returns the opener command and arguments for a given GOOS.
func browserCommand(goos, url string) (string, []string) {
	switch goos {
	case "darwin":
		return "open", []string{url}
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", url}
	default:
		return "xdg-open", []string{url}
	}
}

// defaultOpenURL launches the platform browser opener for url.
func defaultOpenURL(url string) error {
	name, args := browserCommand(runtime.GOOS, url)
	return execStart(name, args...)
}

// Handler adapts an app to the presenter's data-source/input-sink interface.
type Handler struct{ a *app.App }

// New wraps a to be driven by a native window.
func New(a *app.App) *Handler { return &Handler{a: a} }

// Frame returns the current framebuffer, its device dimensions, and whether it
// changed since the last call.
func (h *Handler) Frame() ([]byte, int, int, bool) {
	buf, changed := h.a.Frame()
	s := h.a.Scene()
	return buf, s.W, s.H, changed
}

// Resize maps a logical size to device pixels via the backing scale.
func (h *Handler) Resize(w, height int, scale float64) {
	s := h.a.Scene()
	s.SetScale(scale)
	s.Resize(w, height)
}

// MouseDown routes a click across the feed, detail and settings views: open an
// item, follow a link, switch a filter or profile, or drive the settings editor.
func (h *Handler) MouseDown(x, y int) {
	s, vm := h.a.Scene(), h.a.VM()
	// A click inside the embedded web-preview browser is forwarded to it (a link,
	// a toolbar button, the address field, a tab) and consumes the event; a click
	// anywhere else blurs the browser's keyboard focus and falls through to the
	// normal hit-testing below.
	if s.ForwardBrowserClick(x, y) {
		return
	}
	switch hit := s.HitTest(x, y); hit.Kind {
	case ui.HitItem:
		h.a.SelectPreview(hit.Item) // show it in the right preview pane (fetch image if any)
	case ui.HitPreviewGroup:
		h.a.PreviewGroup(hit.Value) // preview a Usenet post: reconstruct its image into the pane
	case ui.HitOpenPreview:
		vm.OpenDetail(hit.Item) // the pane's "Open" button → full-screen reading view
	case ui.HitBack:
		vm.CloseView.Execute()
	case ui.HitOpenExternal:
		// HitOpenExternal only fires when the item has a URL, so one is present.
		url := hit.Item.Link
		if url == "" {
			url = hit.Item.Permalink
		}
		_ = openURL(url)
	case ui.HitSub:
		h.a.ViewSub(hit.Sub) // switch the group filter + mark it seen (unseen count → 0)
		vm.FocusSearch(false)
	case ui.HitSearch:
		vm.FocusSearch(true)
	case ui.HitProfile:
		h.a.SelectProfile(hit.Profile) // switch + persist + re-aggregate through the app/VM
	case ui.HitSettings:
		vm.OpenSettings.Execute()
	case ui.HitLog:
		vm.OpenLog.Execute()
	case ui.HitCloseLog:
		vm.CloseView.Execute()
	case ui.HitBurger:
		vm.ToggleSidebar.Execute()
	case ui.HitSidebarDivider:
		s.BeginSidebarResize()
	case ui.HitPreviewDivider:
		s.BeginPreviewResize()
	case ui.HitFixAuth:
		// A click on an in-feed "needs sign-in" banner opens the Accounts editor
		// pre-selected on the provider that needs fixing.
		vm.FixAuth(source.Kind(hit.Value))
	case ui.HitToggleGroup:
		s.ToggleGroup(hit.Value) // expand/collapse a Usenet post group
	case ui.HitReconstruct:
		h.a.ReconstructGroup(hit.Value) // download parts + reassemble + PAR2 verify/repair
	case ui.HitToggleDownload:
		h.a.ToggleDownload(hit.Value) // queue/cancel a complete post in the download panel
	case ui.HitClearDownloads:
		h.a.ClearDownloads() // drop finished rows from the download panel
	case ui.HitBrowse:
		vm.OpenBrowse.Execute() // open the newsgroup browser
		h.a.LoadGroups()        // fetch the server's full group list (cached)
	case ui.HitCloseBrowse:
		vm.CloseView.Execute()
		s.FocusBrowseFilter(false)
	case ui.HitBrowseRefresh:
		s.FocusBrowseFilter(false)
		h.a.RefreshGroups() // re-fetch the group list, bypassing the cache
	case ui.HitBrowseFilter:
		s.FocusBrowseFilter(true) // focus the regexp filter field
	case ui.HitToggleBrowseNode:
		s.FocusBrowseFilter(false)
		s.ToggleBrowseNode(hit.Value) // expand/collapse a tree hierarchy
	case ui.HitSubscribeGroup:
		s.FocusBrowseFilter(false)
		h.a.SubscribeGroup(hit.Value) // add usenet:<group> to the active profile
	case ui.HitUnsubscribeGroup:
		s.FocusBrowseFilter(false)
		h.a.UnsubscribeGroup(hit.Value) // remove usenet:<group> from the active profile
	case ui.HitCloseSettings:
		s.CommitRename()
		s.CommitCache()
		vm.CloseView.Execute()
		h.a.ApplySceneSettings()
	case ui.HitSelectProfile:
		s.SelectEditProfile(hit.Profile)
	case ui.HitNewProfile:
		s.NewProfile()
	case ui.HitDeleteProfile:
		h.a.DeleteProfile(hit.Profile)
	case ui.HitRenameProfile:
		s.FocusRename()
	case ui.HitSelectKind:
		s.SelectKind(source.Kind(hit.Value))
	case ui.HitAddSub:
		s.AddInputSub()
		h.a.ApplySceneSettings()
	case ui.HitRemoveSub:
		s.RemoveSub(hit.Profile, hit.Sub)
		h.a.ApplySceneSettings()
	case ui.HitFocusChannel:
		s.FocusChannel()
	case ui.HitFocusCache:
		s.FocusCache()
	case ui.HitTheme:
		s.SetThemeName(hit.Value)
		h.a.ApplySceneSettings()
	case ui.HitBrowserTabs:
		s.SetBrowserSingleTab(hit.Value == "single") // web-preview tab mode
		h.a.ApplySceneSettings()
	case ui.HitAccounts:
		vm.OpenAccounts.Execute()
	case ui.HitCloseAccounts:
		vm.CloseView.Execute()
		h.a.ApplyAccounts() // persist creds + rebuild registry (Reddit→OAuth) + re-aggregate
	case ui.HitSelectAccount:
		vm.SelectAccount(source.Kind(hit.Value))
	case ui.HitFocusAccountField:
		s.FocusAccountField(hit.Value)
	case ui.HitToggleAccountBool:
		s.ToggleAccountBool(hit.Value)
	default:
		vm.FocusSearch(false)
	}
}

// MouseMove forwards pointer motion to the scene, which applies it only while a
// sidebar-divider drag is in progress.
func (h *Handler) MouseMove(x, y int) { h.a.Scene().MouseMove(x, y) }

// MouseUp ends any in-progress sidebar-divider drag.
func (h *Handler) MouseUp(x, y int) {
	s := h.a.Scene()
	s.EndSidebarResize()
	s.EndPreviewResize()
}

// Scroll scrolls the feed by a device-pixel wheel delta, unless the pointer is
// over the embedded web-preview browser — then the wheel scrolls the page.
func (h *Handler) Scroll(dy int) {
	s := h.a.Scene()
	if s.ForwardBrowserScroll(dy) {
		return
	}
	s.Scroll(dy)
}

// SystemAppearance applies host look-and-feel harvested by the native back-end
// (dark/light, accent, system font) to the app. This satisfies the optional
// window.AppearanceSink capability; the compile-time assertion below keeps the
// signature in step with the contract.
func (h *Handler) SystemAppearance(a window.SystemAppearance) {
	h.a.SetSystemAppearance(a.Dark, a.Accent, a.HasAccent, a.FontTTF)
}

var _ window.AppearanceSink = (*Handler)(nil)

// Key handles editing keys and printable runes for whichever view/field is
// focused (topbar search in the feed, or the settings text fields).
func (h *Handler) Key(name string, r rune) {
	s, vm := h.a.Scene(), h.a.VM()
	// In the feed view, an active + focused embedded browser captures editing keys
	// and printable runes for its address field (Enter commits/navigates).
	if s.Mode() == ui.ModeFeed && s.ForwardBrowserKey(name, r) {
		return
	}
	switch name {
	case "Backspace":
		s.Backspace()
	case "Escape":
		switch s.Mode() {
		case ui.ModeDetail:
			vm.CloseView.Execute() // Esc returns from the reading view to the feed
		case ui.ModeLog:
			vm.CloseView.Execute() // Esc returns from the Network log to the feed
		case ui.ModeSettings:
			s.CommitRename()
			s.CommitCache()
			vm.CloseView.Execute()
			h.a.ApplySceneSettings()
		case ui.ModeAccounts:
			vm.CloseView.Execute() // Esc commits the accounts editor, like Settings
			h.a.ApplyAccounts()
		case ui.ModeBrowse:
			vm.CloseView.Execute() // Esc returns from the browser to the feed
			s.FocusBrowseFilter(false)
		default:
			vm.FocusSearch(false)
		}
	case "Enter":
		switch s.Mode() {
		case ui.ModeSettings:
			h.commitSettingsField()
		case ui.ModeAccounts:
			h.a.ApplyAccounts() // apply credentials in place (re-aggregate without leaving)
		case ui.ModeBrowse:
			h.activateBrowseSelection() // Enter expands a hierarchy or (un)subscribes a group
		case ui.ModeFeed:
			if _, hasPrev := s.PreviewItem(); hasPrev {
				h.a.OpenSelected() // Enter opens the selected post's reading view
			} else {
				vm.FocusSearch(false)
			}
		default:
			vm.FocusSearch(false)
		}
	case "Up":
		switch s.Mode() {
		case ui.ModeFeed:
			s.FocusSearch(false)
			h.a.SelectAdjacent(-1) // move the feed selection up (previous post)
		case ui.ModeBrowse:
			s.NavBrowse(-1) // move the newsgroup-tree selection up
			h.a.ScanBrowseGroup()
		}
	case "Down":
		switch s.Mode() {
		case ui.ModeFeed:
			s.FocusSearch(false)
			h.a.SelectAdjacent(1) // move the feed selection down (next post)
		case ui.ModeBrowse:
			s.NavBrowse(1) // move the newsgroup-tree selection down
			h.a.ScanBrowseGroup()
		}
	default:
		if r != 0 {
			s.TypeRune(r)
		}
	}
}

// activateBrowseSelection acts on the keyboard-selected newsgroup-tree node
// (Enter): an expandable hierarchy toggles open/closed, a real group is
// subscribed (or unsubscribed if already), and with no selection the filter
// field just defocuses — mirroring what a click on that row would do.
func (h *Handler) activateBrowseSelection() {
	s := h.a.Scene()
	s.FocusBrowseFilter(false) // Enter always leaves the filter field
	name, hasChildren, isGroup, subscribed, ok := s.BrowseSelectedNode()
	switch {
	case !ok:
		// Nothing selected: the defocus above is the whole action.
	case hasChildren:
		s.ToggleBrowseNode(name)
	case isGroup && subscribed:
		h.a.UnsubscribeGroup(name)
	case isGroup:
		h.a.SubscribeGroup(name)
	}
}

// commitSettingsField applies the settings text field that currently has focus
// (Enter), persisting the change and re-aggregating when it affects the feed.
func (h *Handler) commitSettingsField() {
	s := h.a.Scene()
	switch s.Focus() {
	case ui.FocusChannel:
		s.AddInputSub()
		h.a.ApplySceneSettings()
	case ui.FocusRename:
		s.CommitRename()
		h.a.ApplySceneSettings()
	case ui.FocusCache:
		s.CommitCache()
		h.a.ApplySceneSettings()
	}
}
