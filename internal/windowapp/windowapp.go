// Package windowapp adapts an *app.App to the window.Handler contract: it
// exposes the app's double-buffered framebuffer to a native presenter and
// routes native mouse/scroll/key events into the ui.Scene. It is deliberately
// decoupled from the window package (it satisfies the interface structurally),
// so both build on every platform and the routing logic is unit-testable
// without any Cocoa dependency.
package windowapp

import (
	"context"
	"os/exec"
	"runtime"

	"github.com/go-widgets/toolkit"

	"github.com/go-news-reader/reader/app"
	"github.com/go-news-reader/reader/internal/settings"
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

// openURLIn opens a URL in a specific browser. A package var so tests can
// substitute it; the app calls it (via the SetURLOpener seam) for sign-in flows.
var openURLIn = defaultOpenURLIn

// openURLInBrowser returns the opener command + arguments that launch url in a
// specific browser on goos. browser is one of the settings.SignInBrowser* values;
// "default" (and any browser with no sensible command on this OS, e.g. Safari off
// macOS) falls back to the default opener via browserCommand.
func openURLInBrowser(goos, browser, url string) (string, []string) {
	if browser == "" || browser == settings.SignInBrowserDefault {
		return browserCommand(goos, url)
	}
	switch goos {
	case "darwin":
		if appName, ok := map[string]string{
			settings.SignInBrowserFirefox: "Firefox",
			settings.SignInBrowserChrome:  "Google Chrome",
			settings.SignInBrowserSafari:  "Safari",
			settings.SignInBrowserEdge:    "Microsoft Edge",
		}[browser]; ok {
			return "open", []string{"-a", appName, url}
		}
	case "windows":
		// `start ""` opens via the shell's default handler for the given program;
		// the empty first quoted argument is start's (ignored) window title.
		if exe, ok := map[string]string{
			settings.SignInBrowserFirefox: "firefox",
			settings.SignInBrowserChrome:  "chrome",
			settings.SignInBrowserEdge:    "msedge",
		}[browser]; ok {
			return "cmd", []string{"/c", "start", "", exe, url}
		}
	default:
		if bin, ok := map[string]string{
			settings.SignInBrowserFirefox: "firefox",
			settings.SignInBrowserChrome:  "google-chrome",
			settings.SignInBrowserEdge:    "microsoft-edge",
		}[browser]; ok {
			return bin, []string{url}
		}
	}
	// Named browser with no sensible command on this OS (e.g. Safari on Linux, or
	// Windows Safari): fall back to the platform default opener.
	return browserCommand(goos, url)
}

// defaultOpenURLIn launches url in the named browser for the current platform.
func defaultOpenURLIn(browser, url string) error {
	name, args := openURLInBrowser(runtime.GOOS, browser, url)
	return execStart(name, args...)
}

// Handler adapts an app to the presenter's data-source/input-sink interface.
//
// pending* records a text-selectable hit (a feed card, a sidebar row, or a
// non-interactive reading surface) whose action is DEFERRED from the press to
// the release: a press begins a text selection instead of acting, and MouseUp
// decides — a drag that produced a selection suppresses the action and keeps the
// selection for a copy; a plain click (no drag) runs the recorded action.
type Handler struct {
	a          *app.App
	pending    ui.Hit
	pendingHit bool
	pendingX   int // press coordinates for a deferred feed-card click (routed to
	pendingY   int // the CardList on release, so the click selects the card under it)
}

// New wraps a to be driven by a native window. It installs the platform browser
// opener as the app's sign-in URL opener (through the openURLIn seam, so tests
// substituting it are honoured), letting the app start a browser sign-in without
// importing this package.
func New(a *app.App) *Handler {
	a.SetURLOpener(func(browser, url string) error { return openURLIn(browser, url) })
	return &Handler{a: a}
}

// Frame returns the current framebuffer, its device dimensions, and whether it
// changed since the last call.
func (h *Handler) Frame() ([]byte, int, int, bool) {
	buf, changed := h.a.Frame()
	s := h.a.Scene()
	return buf, s.W, s.H, changed
}

// NeedsPresent reports whether the present loop should repaint now: a queued
// scene write is waiting, or the scene is still animating. A gated loop uses it
// to skip the blit on an idle, unchanged window; see internal/window's ticker.
func (h *Handler) NeedsPresent() bool { return h.a.NeedsPresent() }

// PresentImmediate reports that queued content must be blitted without throttle
// delay; the gated present loop uses it to override its spinner throttle.
func (h *Handler) PresentImmediate() bool { return h.a.PresentImmediate() }

// PresentThrottle reports that the only motion is the loading spinner, which the
// gated present loop may redraw below its tick rate; see internal/window.
func (h *Handler) PresentThrottle() bool { return h.a.PresentThrottle() }

// Resize maps a logical size to device pixels via the backing scale.
func (h *Handler) Resize(w, height int, scale float64) {
	s := h.a.Scene()
	s.SetScale(scale)
	s.Resize(w, height)
}

// MouseDown routes a click across the feed, detail and settings views: open an
// item, follow a link, switch a filter or profile, or drive the settings editor.
func (h *Handler) MouseDown(x, y int) {
	s := h.a.Scene()
	// An in-progress inline folder rename commits on blur: any press elsewhere
	// ends the edit cleanly (keeping the typed name) before the click acts.
	if s.FolderRenaming() {
		s.CommitFolderRename()
		h.a.PersistSettings()
	}
	// A click inside the embedded web-preview browser is forwarded to it (a link,
	// a toolbar button, the address field, a tab) and consumes the event; a click
	// anywhere else blurs the browser's keyboard focus and falls through to the
	// normal hit-testing below.
	if s.ForwardBrowserClick(x, y) {
		return
	}
	hit := s.HitTest(x, y)
	// Text-selectable hits (a feed card, a sidebar row, or a non-interactive
	// reading surface) DEFER their action to MouseUp so a drag can select the
	// text instead: begin a selection now, remember the hit, and decide on
	// release. Every other control acts immediately, exactly as before.
	if h.textSelectableHit(hit, x, y) {
		s.SelectionBegin(x, y)
		h.pending, h.pendingHit = hit, true
		h.pendingX, h.pendingY = x, y
		return
	}
	h.runHit(hit)
}

// textSelectableHit reports whether a press on hit at (x, y) should defer to a
// click-vs-drag decision: an item card, a subscription row, or a press that
// resolved to no interactive target over a reading surface (detail view, the
// preview text pane, the feed list, or the sidebar labels).
func (h *Handler) textSelectableHit(hit ui.Hit, x, y int) bool {
	switch hit.Kind {
	case ui.HitItem:
		return true
	case ui.HitNone:
		return h.a.Scene().SelectableAt(x, y)
	default:
		// A sidebar row (HitSub / HitToggleFolder) resolves through the TreeView to
		// an action, not selectable text, so it acts immediately on press.
		return false
	}
}

// runHit executes a hit's action. It is called immediately on MouseDown for
// non-deferred controls, and on MouseUp for a deferred text-selectable hit that
// turned out to be a plain click (no drag).
func (h *Handler) runHit(hit ui.Hit) {
	s, vm := h.a.Scene(), h.a.VM()
	switch hit.Kind {
	case ui.HitItem:
		h.a.SelectPreview(hit.Item) // show it in the right preview pane (fetch image if any)
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
	case ui.HitRefresh:
		// The visible topbar twin of the pull-to-refresh overscroll gesture: fire
		// the very same scene seam so the button and the gesture do one thing.
		if fn := s.OnPullRefresh; fn != nil {
			fn()
		}
	case ui.HitSidebarDivider:
		s.BeginSidebarResize()
	case ui.HitPreviewDivider:
		s.BeginPreviewResize()
	case ui.HitPreviewTextSmaller:
		h.a.AdjustPreviewTextScale(-app.PreviewTextStep) // shrink the reader/preview text + persist
	case ui.HitPreviewTextLarger:
		h.a.AdjustPreviewTextScale(app.PreviewTextStep) // grow the reader/preview text + persist
	case ui.HitFixAuth:
		// A click on an in-feed "needs sign-in" banner opens the Accounts editor
		// pre-selected on the provider that needs fixing.
		vm.FixAuth(source.Kind(hit.Value))
	case ui.HitToggleGroup:
		s.ToggleGroup(hit.Value) // expand/collapse a Usenet post group's member list
	case ui.HitReconstruct:
		h.a.ReconstructGroup(hit.Value) // download parts + reassemble + PAR2 verify/repair
	case ui.HitToggleDownload:
		h.a.ToggleDownload(hit.Value) // queue/cancel a complete post in the download panel
	case ui.HitClearDownloads:
		h.a.ClearDownloads() // drop finished rows from the download panel
	case ui.HitToggleFolder:
		s.ToggleSidebarFolder(hit.Value) // collapse/expand a sidebar virtual folder
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
	case ui.HitSearchReddit:
		vm.OpenSearch.Execute() // open the Reddit search / discover view
	case ui.HitCloseSearch:
		vm.CloseView.Execute()
		s.FocusSearchQuery(false)
		s.FocusSearchRegex(false)
	case ui.HitSearchTab:
		s.SetSearchTab(hit.Value == "posts") // switch the subreddits/posts results tab
	case ui.HitFocusSearchQuery:
		s.FocusSearchQuery(true) // focus the search-query field
	case ui.HitFocusSearchRegex:
		s.FocusSearchRegex(true) // focus the subreddit-results regexp filter
	case ui.HitRunSearch:
		s.FocusSearchQuery(false)
		s.FocusSearchRegex(false)
		h.a.RunSearch() // run the query against the Reddit provider (async)
	case ui.HitSubscribeSubreddit:
		h.a.SubscribeSubreddit(hit.Value) // add r/<name> to the active profile
	case ui.HitSubscribePostSearch:
		h.a.SubscribePostSearch(hit.Value) // save search:<query> as a live subscription
	case ui.HitCloseSettings:
		s.CommitRename()
		s.CommitCache()
		s.CommitCacheSize()
		s.CommitCacheBackend()
		s.CommitZoomKeys()
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
	case ui.HitFocusCacheSize:
		s.FocusCacheSize()
	case ui.HitFocusCacheBackend:
		s.FocusCacheBackend()
	case ui.HitFocusZoomIn:
		s.FocusZoomIn()
	case ui.HitFocusZoomOut:
		s.FocusZoomOut()
	case ui.HitTheme:
		s.SetThemeName(hit.Value)
		h.a.ApplySceneSettings()
	case ui.HitSignInBrowser:
		s.SetSignInBrowser(hit.Value) // which browser a provider sign-in launches
		h.a.ApplySceneSettings()
	case ui.HitBrowserTabs:
		s.SetBrowserSingleTab(hit.Value == "single") // web-preview tab mode
		h.a.ApplySceneSettings()
	case ui.HitBrowserChrome:
		s.SetBrowserChromeHidden(hit.Value == "hidden") // web-preview toolbar visibility
		h.a.ApplySceneSettings()
	case ui.HitInfiniteScroll:
		s.SetInfiniteScroll(hit.Value == "on") // fetch next page on scroll-to-bottom
		h.a.ApplySceneSettings()
	case ui.HitClearRenderCache:
		h.a.ClearRenderCache() // drop every cached web-preview render
	case ui.HitAccounts:
		vm.OpenAccounts.Execute()
	case ui.HitCloseAccounts:
		vm.CloseView.Execute()
		h.a.ApplyAccounts() // persist creds + rebuild registry (Reddit→session cookie) + re-aggregate
	case ui.HitSelectAccount:
		vm.SelectAccount(source.Kind(hit.Value))
	case ui.HitFocusAccountField:
		s.FocusAccountField(hit.Value)
	case ui.HitToggleAccountBool:
		s.ToggleAccountBool(hit.Value)
	case ui.HitImportRedditFirefox:
		// Import the logged-in reddit_session cookie from the user's Firefox profile
		// into the Reddit account; the app persists it, rebuilds the registry and
		// re-aggregates, reporting success/failure on the status line.
		_, _ = h.a.ImportRedditSessionFromFirefox()
	case ui.HitImportFollows:
		// Pull the connected account's follows for the selected provider in the
		// background (network); the app adds any new subscriptions, persists,
		// re-aggregates, and reports on the status line. The hit carries the source
		// kind whose "Import subscriptions" button was pressed.
		kind := source.Kind(hit.Value)
		go func() { _, _ = h.a.ImportFollows(context.Background(), kind) }()
	case ui.HitRedditSignIn:
		// Launch Reddit's login page in the configured sign-in browser so the user
		// can authenticate there (then, with Firefox, import the session cookie).
		_ = h.a.LaunchRedditSignIn()
	case ui.HitImportSession:
		// Import the logged-in session cookie of the selected social provider
		// (Instagram/TikTok/X) from the user's Firefox profile; the app persists it,
		// rebuilds the registry and re-aggregates, reporting on the status line.
		_, _ = h.a.ImportSessionFromFirefox()
	default:
		// A press over no interactive target (HitNone) that is NOT a selectable
		// reading surface just blurs the topbar search. When it IS selectable the
		// press was deferred (textSelectableHit → SelectionBegin), and this runs
		// only if the release was a plain click, so blurring search here is still
		// the right click action.
		vm.FocusSearch(false)
	}
}

// MouseMove forwards pointer motion to the scene. Back-ends deliver a left-button
// drag as MouseMove, so while a press is held inside the embedded web-preview
// browser the motion is routed there first as a drag (letting a grabbed scrollbar
// thumb — horizontal or vertical — track the pointer); otherwise it falls through
// to the scene, which applies it only while a sidebar/preview-divider drag is in
// progress.
func (h *Handler) MouseMove(x, y int) {
	s := h.a.Scene()
	if s.ForwardBrowserDrag(x, y) {
		return
	}
	s.MouseMove(x, y)
}

// MouseUp ends any in-progress sidebar-divider drag and releases a pressed
// web-preview toolbar button (clearing its click-feedback face).
func (h *Handler) MouseUp(x, y int) {
	s := h.a.Scene()
	s.EndSidebarResize()
	s.EndPreviewResize()
	s.SelectionEnd()
	s.ForwardBrowserRelease(x, y)
	// Resolve a deferred text-selectable press: a drag that produced a selection
	// suppresses the click action (and keeps the selection for a copy); a plain
	// click with no drag runs the recorded action now.
	if h.pendingHit {
		h.pendingHit = false
		if !s.HasSelection() {
			// A feed-card click selects the card THROUGH the CardList (so the
			// selection ring + keyboard cursor track it), which fires the feed select
			// hook → preview. Every other deferred hit acts as before.
			if h.pending.Kind == ui.HitItem {
				s.FeedClickAt(h.pendingX, h.pendingY)
			} else {
				h.runHit(h.pending)
			}
		}
	}
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

// SecondaryClick opens a context menu when the secondary press landed on a
// sidebar subscription (a real source, not the All filter) or a virtual folder.
// The menu is the toolkit's own ContextMenu widget — this only builds its items
// and their actions, which reuse the same scene/app operations the rest of the
// UI uses, and hands it to the scene to present. A press anywhere else is
// ignored.
func (h *Handler) SecondaryClick(x, y int) {
	s := h.a.Scene()
	ctx := s.SidebarContextAt(x, y)
	switch ctx.Kind {
	case ui.SidebarCtxSub:
		s.OpenContextMenu(h.subContextMenu(ctx.Sub, ctx.Source, ctx.Channel), x, y)
	case ui.SidebarCtxFolder:
		s.OpenContextMenu(h.folderContextMenu(ctx.Folder), x, y)
	}
}

// subContextMenu builds the sidebar-subscription context menu for the source at
// index. The first group reuses operations the rest of the UI already exposes (a
// full refresh, marking the group's unseen count to zero, unsubscribing); the
// second group manages the subscription's virtual-folder membership (move into a
// new or existing folder, or take it back out to the sidebar root). Folder edits
// are a pure-display preference, so they persist without re-aggregating.
func (h *Handler) subContextMenu(index int, kind source.Kind, channel string) *toolkit.ContextMenu {
	s := h.a.Scene()
	items := []toolkit.MenuItem{
		{Label: "Refresh", Icon: ui.MenuIconRefresh, Action: func() { go h.a.Refresh(context.Background()) }},
		{Label: "Mark as read", Icon: ui.MenuIconMarkRead, Action: func() { h.a.MarkSubSeen(index) }},
		{Separator: true},
		{Label: "New folder", Icon: ui.MenuIconNewFolder, Action: func() {
			s.MoveSubToFolder(index, s.NewSidebarFolder(""))
			h.a.PersistSettings()
		}},
	}
	if names := s.FolderNames(); len(names) > 0 {
		sub := make([]toolkit.MenuItem, 0, len(names))
		for _, name := range names {
			name := name
			sub = append(sub, toolkit.MenuItem{Label: name, Icon: ui.MenuIconFolder, Action: func() {
				s.MoveSubToFolder(index, name)
				h.a.PersistSettings()
			}})
		}
		items = append(items, toolkit.MenuItem{Label: "Move to folder", Icon: ui.MenuIconFolder, Submenu: toolkit.NewMenu(sub)})
	}
	items = append(items,
		toolkit.MenuItem{Label: "Remove from folder", Action: func() {
			s.RemoveSubFromFolder(index)
			h.a.PersistSettings()
		}},
		toolkit.MenuItem{Separator: true},
		toolkit.MenuItem{Label: "Unsubscribe", Icon: ui.MenuIconUnsubscribe, Action: func() {
			if s.UnsubscribeActive(kind, channel) {
				h.a.ApplySceneSettings()
			}
		}},
	)
	return toolkit.NewContextMenu(toolkit.NewMenu(items))
}

// folderContextMenu builds the context menu for a virtual folder row: rename it
// inline (BeginFolderRename opens a focused text Entry on the row) or delete it
// (its subscriptions return to the sidebar root).
func (h *Handler) folderContextMenu(name string) *toolkit.ContextMenu {
	s := h.a.Scene()
	return toolkit.NewContextMenu(toolkit.NewMenu([]toolkit.MenuItem{
		{Label: "Rename", Icon: ui.MenuIconFolder, Action: func() {
			s.BeginFolderRename(name)
		}},
		{Label: "Delete folder", Icon: ui.MenuIconUnsubscribe, Action: func() {
			s.DeleteFolder(name)
			h.a.PersistSettings()
		}},
	}))
}

// ContextMenuActive reports whether a context menu is popped up, so the window
// routes input to it rather than the scene. Satisfies window.ContextMenuHost.
func (h *Handler) ContextMenuActive() bool { return h.a.Scene().ContextMenuActive() }

// ContextMenuEvent forwards one input event to the open context menu.
func (h *Handler) ContextMenuEvent(ev toolkit.Event) { h.a.Scene().ContextMenuEvent(ev) }

// SystemAppearance applies host look-and-feel harvested by the native back-end
// (dark/light, accent, system font) to the app. This satisfies the optional
// window.AppearanceSink capability; the compile-time assertion below keeps the
// signature in step with the contract.
func (h *Handler) SystemAppearance(a window.SystemAppearance) {
	h.a.SetSystemAppearance(a.Dark, a.Accent, a.HasAccent, a.FontTTF)
}

var _ window.AppearanceSink = (*Handler)(nil)
var _ window.SecondaryClicker = (*Handler)(nil)
var _ window.ContextMenuHost = (*Handler)(nil)

// Shortcut handles a command-style modifier chord (Ctrl/Cmd + a base key)
// forwarded by the native back-end. In the feed view with the embedded web
// preview active, a chord whose base rune matches the configured zoom-in /
// zoom-out key zooms the browser through its MVVM commands; every other chord is
// ignored so ordinary shortcuts fall through untouched. This satisfies the
// optional window.ShortcutSink capability (asserted below).
func (h *Handler) Shortcut(r rune, ctrl, meta bool) {
	if !(ctrl || meta) {
		return
	}
	s := h.a.Scene()
	// Copy chord (Cmd/Ctrl+C) works in every view: copy the current selection /
	// article to the system clipboard. Consume it when something was copied.
	if r == 'c' || r == 'C' {
		if s.Copy() {
			return
		}
	}
	if s.Mode() != ui.ModeFeed || !s.WebPreviewActive() {
		return
	}
	switch s.ZoomKeyDir(r) {
	case 1:
		s.ZoomBrowserIn()
	case -1:
		s.ZoomBrowserOut()
	}
}

var _ window.ShortcutSink = (*Handler)(nil)

// SetSystemClipboard installs the native back-end's OS pasteboard as the
// toolkit-wide clipboard, so every text widget's copy/cut/paste and the app's
// copy actions go through it. It satisfies the optional
// window.ClipboardController capability. window.SystemClipboard has the same
// method set as toolkit.Clipboard, so it installs directly.
func (h *Handler) SetSystemClipboard(c window.SystemClipboard) {
	toolkit.SetClipboard(c)
}

var _ window.ClipboardController = (*Handler)(nil)

// Key handles editing keys and printable runes for whichever view/field is
// focused (topbar search in the feed, or the settings text fields).
func (h *Handler) Key(name string, r rune) {
	s, vm := h.a.Scene(), h.a.VM()
	// An in-progress inline folder rename (feed view) captures editing keys ahead
	// of the feed: Enter commits, Escape cancels, Backspace + printable runes edit
	// the buffer. Non-editing keys (arrows, paging) fall through to feed nav,
	// exactly as the browse filter lets arrows drive its list while focused.
	if h.folderRenameKey(name, r) {
		return
	}
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
			s.CommitCacheSize()
			s.CommitCacheBackend()
			s.CommitZoomKeys()
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
	case "PageUp", "PageDown", "Home", "End":
		// Feed paging / jump keys route straight into the CardList (its selection
		// cursor moves a viewport or to an end, firing the feed select hook →
		// preview). The toolkit's selectMove codes match these names verbatim.
		if s.Mode() == ui.ModeFeed {
			s.FocusSearch(false)
			s.FeedEvent(toolkit.Event{Kind: toolkit.EventKeyDown, Code: name})
		}
	default:
		if r != 0 {
			s.TypeRune(r)
		}
	}
}

// folderRenameKey routes a key into an in-progress inline folder rename (feed
// view only), returning whether it consumed the event. Enter commits (and
// persists), Escape cancels, Backspace + printable runes edit the buffer through
// the scene's shared editing path; every other key (arrows, paging) is left for
// the normal feed handling.
func (h *Handler) folderRenameKey(name string, r rune) bool {
	s := h.a.Scene()
	if s.Mode() != ui.ModeFeed || !s.FolderRenaming() {
		return false
	}
	switch name {
	case "Enter":
		s.CommitFolderRename()
		h.a.PersistSettings()
	case "Escape":
		s.CancelFolderRename()
	case "Backspace":
		s.Backspace()
	default:
		if r == 0 {
			return false // arrows / paging fall through to feed navigation
		}
		s.TypeRune(r)
	}
	return true
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
	case ui.FocusCacheSize:
		s.CommitCacheSize()
		h.a.ApplySceneSettings()
	case ui.FocusCacheBackend:
		s.CommitCacheBackend()
		h.a.ApplySceneSettings()
	case ui.FocusZoomIn, ui.FocusZoomOut:
		s.CommitZoomKeys()
		h.a.ApplySceneSettings()
	}
}

// A11yElements describes what the app is showing, satisfying window.Accessible
// so a back-end with an accessibility API can expose it.
//
// It is the ui package's tree flattened into the window package's neutral shape.
// The translation exists precisely so window stays a generic surface that never
// imports ui: the presenter learns the roles and labels without learning what a
// feed or a subscription is.
//
// The rects pass through unchanged. ui.A11yNode already carries device-pixel,
// top-left coordinates — the same space the framebuffer and MouseDown use — so
// there is nothing to convert here, and the back-end owns the trip to whatever
// its platform wants.
func (h *Handler) A11yElements() []window.A11yElement {
	tree := h.a.Scene().A11yTree()
	out := make([]window.A11yElement, 0, len(tree))
	for _, n := range tree {
		out = append(out, window.A11yElement{
			Role:  string(n.Role),
			Name:  n.Name,
			Value: n.Value,
			X:     n.Rect.X, Y: n.Rect.Y, W: n.Rect.W, H: n.Rect.H,
		})
	}
	return out
}
