// Package ui renders the aggregator — a topbar with search, a sidebar of
// source subscriptions, and a unified newest-first feed of items from every
// source — into an RGBA pixel buffer. Chrome and cards are drawn with the
// go-widgets painter; text is anti-aliased TrueType (see text.go) so it stays
// clean at any zoom / Retina scale. The package has no build tag, so its
// layout, hit-testing and rendering run under native `go test` and are
// snapshot-verifiable.
package ui

import (
	"image"
	"strings"

	"github.com/go-widgets/toolkit"

	"github.com/go-news-reader/reader/internal/settings"
	"github.com/go-news-reader/reader/source"
)

// Minimum sensible surface and zoom bounds.
const (
	MinW     = 360
	MinH     = 240
	MinZoom  = 0.5
	MaxZoom  = 3.0
	ZoomStep = 0.1
)

// AllFilter is the Active value that shows every source (no filter).
const AllFilter = -1

// Subscription is one sidebar entry: a source + channel to pull, with a label.
type Subscription struct {
	Source  source.Kind
	Channel string
	Label   string // display name; falls back to Channel or the source name
}

// name returns the sidebar label for the subscription.
func (s Subscription) name() string {
	switch {
	case s.Label != "":
		return s.Label
	case s.Channel != "":
		return s.Channel
	default:
		return sourceLabel(s.Source)
	}
}

// AuthPrompt names a provider whose subscription failed because it needs the
// user to sign in or supply configuration. The feed renders one clickable
// "needs sign-in" banner per prompt; a click opens the Accounts editor for Kind.
type AuthPrompt struct {
	Kind   source.Kind
	Reason string
}

// HitKind classifies what a click landed on.
type HitKind int

const (
	HitNone           HitKind = iota
	HitItem                   // a feed item (open it in the detail view) — Item set
	HitSub                    // a sidebar subscription (filter the feed) — Sub set (AllFilter = All)
	HitSearch                 // the topbar search field (focus it)
	HitBack                   // the detail view's back button (return to the feed)
	HitOpenExternal           // the detail view's "open original" button — Item set
	HitProfile                // a sidebar profile tab (switch active profile) — Profile set
	HitSettings               // the sidebar ⚙ Settings entry (open preferences)
	HitLog                    // the sidebar Network-log entry (open the HTTP log)
	HitCloseLog               // the log view's "< Back" button (return to the feed)
	HitBurger                 // the topbar burger button (collapse/expand the sidebar)
	HitSidebarDivider         // the draggable divider at the sidebar's right edge
	HitFixAuth                // an in-feed "needs sign-in" banner row — Value = source kind
	HitToggleGroup            // a Usenet group card's header/chevron — Value = release base
	HitReconstruct            // a Usenet group card's "Reconstruct" affordance — Value = release base
	HitPreviewGroup           // a Usenet group card's body (preview it in the pane) — Value = release base
	HitOpenPreview            // the preview pane's "Open" button (full reading view) — Item set
	HitPreviewDivider         // the preview pane's left-edge resize grip (start a drag)

	// Newsgroup browser (Mode == ModeBrowse):
	HitBrowse           // the sidebar "＋ Browse newsgroups" entry (open the browser)
	HitCloseBrowse      // the browser's "‹ Back" button (return to the feed)
	HitBrowseRefresh    // the browser's Refresh button (re-fetch the full group list)
	HitBrowseFilter     // the browser's regexp filter field (focus it)
	HitToggleBrowseNode // a tree node's chevron/row (expand/collapse) — Value = node name
	HitSubscribeGroup   // a tree leaf's Subscribe affordance — Value = full group name
	HitUnsubscribeGroup // a subscribed tree leaf's ✓ (unsubscribe) — Value = full group name

	// Settings-view actions (Mode == ModeSettings):
	HitSelectProfile // Profile = index being edited
	HitNewProfile
	HitDeleteProfile // Profile = index
	HitRenameProfile // focus the rename field for Profile = index
	HitSelectKind    // Value = source kind for the add-subscription palette
	HitAddSub        // commit the channel input into the edited profile
	HitRemoveSub     // Profile = index, Sub = subscription index
	HitFocusChannel  // focus the add-channel input
	HitFocusCache    // focus the media-cache path input
	HitTheme         // Value = "system"|"light"|"dark"
	HitCloseSettings // leave the settings view

	// Accounts-view actions (Mode == ModeAccounts):
	HitAccounts          // the sidebar 👤 Accounts entry (open the accounts editor)
	HitCloseAccounts     // leave the accounts view (commit)
	HitSelectAccount     // Value = source kind for the provider being edited
	HitFocusAccountField // Value = credential field key to focus
	HitToggleAccountBool // Value = bool credential field key to flip (Usenet TLS)
)

// Mode selects which view the scene renders.
type Mode int

const (
	ModeFeed     Mode = iota // the topbar + sidebar + unified feed
	ModeDetail               // a single item's full detail / reading view
	ModeSettings             // the in-canvas preferences editor
	ModeLog                  // the in-canvas HTTP-exchange (Network) log
	ModeAccounts             // the in-canvas per-provider credentials editor
	ModeBrowse               // the in-canvas newsgroup browser / subscribe view
)

// Hit is the result of [Scene.HitTest].
type Hit struct {
	Kind    HitKind
	Item    source.Item // HitItem
	Sub     int         // HitSub: index into Subs, or AllFilter; HitRemoveSub: sub index
	Profile int         // HitProfile / HitSelectProfile / HitDeleteProfile / HitRemoveSub / HitRenameProfile
	Value   string      // HitTheme / HitSelectKind
}

// Scene is the mutable aggregator UI state.
type Scene struct {
	W, H int

	theme  *toolkit.Theme
	Items  []source.Item // the unified feed (merge newest-first before setting)
	Subs   []Subscription
	Active int // selected subscription index, or AllFilter
	Status string

	// authPrompts drive the in-feed "needs sign-in" banner (one row each).
	authPrompts []AuthPrompt
	ScrollY     int
	Scale       float64 // display scale (zoom × devicePixelRatio); 0 => 1

	// Live-loading feedback (streaming aggregation). loading is set while a
	// refresh is in progress; loadDone/loadTotal track how many sources have
	// returned. pending holds the sources whose fetch is still outstanding, keyed
	// by source+channel, so the sidebar can mark those rows. animFrame advances
	// once per presented frame WHILE loading so the indeterminate indicator moves;
	// it is otherwise frozen, so an idle scene never re-damages (see Animating).
	loading             bool
	loadDone, loadTotal int
	pending             map[string]bool
	pendRev             int
	animFrame           int
	loadStripTop        int  // feed-content Y of the progress strip (loading + items)
	showStrip           bool // whether the top-of-feed progress strip is laid out

	// Profiles are the named sidebar tabs; the active one supplies Subs.
	Profiles   []settings.Profile
	activeProf int    // active profile index (drives the sidebar + feed)
	themeName  string // "system"|"light"|"dark" (persisted)
	cachePath  string // media cache dir (persisted, repositionable)

	// Settings editor (ModeSettings) state.
	selEdit      int          // profile being edited
	sf           Focus        // which text field has keyboard focus
	channelInput string       // add-subscription channel buffer
	renameInput  string       // rename-profile buffer
	cacheInput   string       // cache-path buffer
	newKind      source.Kind  // selected source for the add-subscription palette
	sButtons     []sButton    // clickable regions in the settings view
	sLabels      []sLabel     // section labels in the settings view
	sChips       []sChip      // subscription chips in the settings view
	sChannelR    toolkit.Rect // add-channel input rect
	sCacheR      toolkit.Rect // cache-path input rect
	sRenameR     toolkit.Rect // rename input rect
	sDoneR       toolkit.Rect // "Done" button rect

	// Accounts editor (ModeAccounts) state. accBuf holds the editable credential
	// values per provider (seeded from settings.Accounts); accSel is the provider
	// being edited; accFocus is the focused field key ("" = none).
	accBuf      map[source.Kind]map[string]string
	accSel      source.Kind
	accFocus    string
	accScrollY  int
	accContentH int
	accLabels   []sLabel
	accProvBtns []accProvBtn
	accRows     []accFieldRow
	accBackR    toolkit.Rect
	accDoneR    toolkit.Rect

	// Optional decoded thumbnails keyed by Item.ID (blitted when present).
	Thumbs map[string]*image.RGBA

	// Topbar search/filter. The text is owned by a toolkit.SearchEntry widget so
	// the topbar demonstrates a real mvvm-bound widget (the app binds vm.Search to
	// searchEntry.Text/OnChange); the scene renders the widget and routes topbar
	// keystrokes into it. searchFocused is the scene-side focus flag the binder
	// reflects vm.SearchFocus onto (SearchEntry is itself focus-agnostic).
	searchEntry   *toolkit.SearchEntry
	searchFocused bool

	// Detail (reading) view: ModeDetail shows a single opened item in-app.
	mode           Mode
	detail         source.Item
	detailScrollY  int
	detailContentH int
	backR, openR   toolkit.Rect

	// Settings (ModeSettings) scroll: the editor can be taller than the surface
	// (many subscriptions, high zoom, or a short window), so it scrolls like the
	// other views. settingsContentH is the laid-out height below the topbar.
	settingsScrollY  int
	settingsContentH int

	// Right-hand preview/details pane (feed view). It is docked on the right and
	// shows the item last clicked in the feed — its badge, title, meta, an image
	// (from Thumbs, e.g. a decoded Usenet binary) and body — with an "Open" button
	// for the full-screen reading view. previewHas is false before any selection
	// (the pane shows an empty prompt). previewImgPending marks that an async image
	// fetch was requested for the current item, so the pane shows a spinner until
	// SetThumb lands. previewOpenR/previewImgR are the laid-out button/image rects.
	previewItem       source.Item
	previewHas        bool
	previewScrollY    int
	previewContentH   int
	previewImgPending bool
	previewR          toolkit.Rect // the whole pane
	previewOpenR      toolkit.Rect // "Open" (full detail) button
	previewImgR       toolkit.Rect // image area (for hit-testing / geometry)
	// previewUserW is the user-dragged pane width in device px (0 => default),
	// clamped at read time; draggingPreview is set while its divider is dragged.
	previewUserW    int
	draggingPreview bool

	// Network-log (ModeLog) view: a scrollable, newest-first list of the HTTP
	// exchanges the providers made, fed live from an injected source so the app
	// need not push updates. logSource is nil when no recorder is wired.
	logSource   func() []LogEntry
	logScrollY  int
	logContentH int
	logRowH     int
	logBackR    toolkit.Rect

	// Unified sidebar width model (feed view). The effective sidebar width is 0
	// when collapsed, else the user-dragged width (device px, clamped) or the
	// default. draggingSidebar is set while the divider is being dragged.
	sidebarCollapsed bool
	sidebarUserW     int // device px; 0 => default width
	draggingSidebar  bool

	// Sidebar subscription-list scroll. When the "All Sources" + subscription +
	// "Browse" rows are taller than the band between the tab strip and the pinned
	// footer (Accounts/Log/Settings), that band scrolls independently of the feed
	// so overflowing subscriptions stay reachable and never draw over the footer.
	// sideMaxScroll is 0 (and everything is a no-op) whenever the rows fit.
	// lastMouseX/Y is the most recent pointer position, used to route wheel
	// scrolling to the sidebar when the pointer is over it.
	sideScrollY   int
	sideMaxScroll int
	sideBandTop   int
	sideBandBot   int
	lastMouseX    int
	lastMouseY    int

	// groupExpanded records which Usenet post groups (keyed by release base) are
	// expanded in the feed; it survives re-renders and scrolling. Absent/false =
	// collapsed (the default).
	groupExpanded map[string]bool

	// Newsgroup browser (ModeBrowse) state. browseGroups is the server's full
	// active group list (set by the app after a fetch); browseServer names the
	// server for the title. browseEntry is the regexp filter field (a real
	// toolkit.SearchEntry, so it can be mvvm-bound like the topbar search).
	// browseExpanded records which tree nodes are open (keyed by full node name);
	// the filtered tree auto-expands so this only applies to the unfiltered view.
	// usenetAddr is non-empty when a Usenet server is configured, which is what
	// gates the sidebar "Browse newsgroups" entry.
	browseGroups   []string
	browseServer   string
	usenetAddr     string
	browseEntry    *toolkit.SearchEntry
	browseFocused  bool
	browseExpanded map[string]bool
	browseScrollY  int
	browseContentH int
	browseRows     []browseRowLayout
	browseBackR    toolkit.Rect
	browseRefreshR toolkit.Rect
	browseFilterR  toolkit.Rect
	browseR        toolkit.Rect // sidebar "Browse newsgroups" entry
	browseCountY   int
	browseTreeTop  int

	// Cached tree materialisation (built once per group-list change, re-filtered
	// per filter-text change) so layoutBrowse — called on both Draw and HitTest —
	// does not rebuild the (tens-of-thousands-of-node) tree every frame.
	browseGroupsRev  int
	browseTree       *groupNode // full tree
	browseTreeRev    int        // browseGroupsRev the full tree was built from
	browseView       *groupNode // full or filtered tree currently shown
	browseViewKey    browseViewKey
	browseFiltered   bool
	browseFilterErr  string
	browseMatchCount int

	m         metrics
	subs      []subHit
	profTabs  []profTabHit
	settingsR toolkit.Rect
	logR      toolkit.Rect // sidebar Network-log entry
	accountsR toolkit.Rect // sidebar Accounts entry
	burgerR   toolkit.Rect // topbar burger button (feed view)
	searchR   toolkit.Rect
	rows      []feedRow
	authRows  []authRowLayout // in-feed sign-in banner rows (above the cards)
	contentH  int

	// cardCache holds rendered card sprites so scrolling is a memcpy-blit
	// rather than a re-rasterisation of every glyph. Invalidated whenever the
	// content, width, scale or theme changes. The chrome (sidebar/topbar) is
	// cached the same way in single slots — like Evas smart-object surfaces —
	// so scrolling never re-rasterises any text.
	cardCache  map[cardKey]*image.RGBA
	sidebarSpr *image.RGBA
	sidebarKey sidebarKey
	topbarSpr  *image.RGBA
	topbarKey  topbarKey
	subsRev    int
	profRev    int

	// rev is a monotonically increasing damage/commit sequence bumped on every
	// state change (the Wayland commit-seq / Evas dirty model). A present layer
	// double-buffers and only re-draws/uploads when rev advances.
	rev int
}

// invalidateCards drops the sprite cache after an appearance/content change.
func (s *Scene) invalidateCards() { s.cardCache = nil }

// Rev returns the current damage/commit sequence. It advances whenever any
// state that affects the rendered frame changes, so a double-buffered present
// loop can skip redundant redraws/uploads.
func (s *Scene) Rev() int { return s.rev }

// touch bumps the damage sequence.
func (s *Scene) touch() { s.rev++ }

// New returns a Scene of the given size with the given theme (system default if nil).
func New(w, h int, theme *toolkit.Theme) *Scene {
	if theme == nil {
		theme = toolkit.DefaultLight()
	}
	s := &Scene{W: w, H: h, theme: theme, Active: AllFilter, Scale: 1,
		themeName: settings.ThemeSystem, newKind: source.Reddit,
		searchEntry: toolkit.NewSearchEntry(""),
		browseEntry: toolkit.NewSearchEntry("")}
	// The topbar SearchEntry paints its left prefix with a real Iconoir
	// magnifier instead of the toolkit's "?" bitmap-font stand-in.
	s.searchEntry.Icon = drawSearchIcon
	s.browseEntry.Icon = drawSearchIcon
	s.clampSize()
	return s
}

// SetTheme swaps the palette.
func (s *Scene) SetTheme(t *toolkit.Theme) {
	if t != nil {
		s.theme = t
		s.invalidateCards()
		s.touch()
	}
}

// SetItems replaces the feed (caller merges/sorts newest-first).
func (s *Scene) SetItems(items []source.Item) {
	s.Items = items
	s.ScrollY = 0
	s.invalidateCards()
	s.touch()
}

// SetAuthPrompts replaces the set of providers that need sign-in/configuration,
// as collected by the app after an aggregate. The feed renders one clickable
// banner per prompt above the cards; an empty slice removes the banner.
func (s *Scene) SetAuthPrompts(p []AuthPrompt) { s.authPrompts = p; s.touch() }

// AuthPrompts returns the current in-feed sign-in prompts.
func (s *Scene) AuthPrompts() []AuthPrompt { return s.authPrompts }

// SetStatus sets the status-line message and marks the scene dirty. The Status
// field stays exported (the renderer reads it directly); this setter is the
// write path the view-model binds to, so a status change triggers a redraw on
// its own rather than relying on an adjacent SetItems/SetLoading to touch.
func (s *Scene) SetStatus(v string) { s.Status = v; s.touch() }

// SetLoading sets the live-loading feedback. active marks a refresh as in
// progress (which drives the in-feed indicator and the per-frame animation);
// done/total report how many sources have returned. Turning loading off clears
// the pending-source markers so nothing is left dimmed.
func (s *Scene) SetLoading(active bool, done, total int) {
	s.loading = active
	s.loadDone, s.loadTotal = done, total
	if !active && s.pending != nil {
		s.pending = nil
		s.pendRev++
	}
	s.touch()
}

// Loading reports whether a refresh is in progress.
func (s *Scene) Loading() bool { return s.loading }

// LoadingProgress returns how many sources have returned and the total.
func (s *Scene) LoadingProgress() (done, total int) { return s.loadDone, s.loadTotal }

// Animating reports whether the scene wants a fresh frame every tick (the
// loading indicator is moving). When false the scene is idle and a
// damage-gated present loop never re-draws — so animation costs nothing when
// nothing is loading.
func (s *Scene) Animating() bool { return s.loading }

// AdvanceAnim advances the animation clock by one frame and marks the scene
// dirty. A present loop calls it once per tick while [Animating] is true; the
// indeterminate indicator derives its position from the frame counter.
func (s *Scene) AdvanceAnim() { s.animFrame++; s.touch() }

// AnimFrame returns the current animation frame counter (for tests).
func (s *Scene) AnimFrame() int { return s.animFrame }

// subPendKey keys the pending set by source kind + channel.
func subPendKey(k source.Kind, ch string) string { return string(k) + "\x00" + ch }

// SetPendingSources marks every given subscription as still-loading, so the
// sidebar rows for those sources show a pending marker until they return.
func (s *Scene) SetPendingSources(subs []source.Subscription) {
	// Build the new set fully before swapping it in, so a present/read on another
	// goroutine sees either the old or the new set — never a half-populated map.
	m := make(map[string]bool, len(subs))
	for _, su := range subs {
		m[subPendKey(su.Source, su.Channel)] = true
	}
	s.pending = m
	s.pendRev++
	s.touch()
}

// ClearPendingSource clears the pending marker for one source+channel once it
// has returned (with items or an error).
func (s *Scene) ClearPendingSource(k source.Kind, ch string) {
	key := subPendKey(k, ch)
	if s.pending != nil && s.pending[key] {
		delete(s.pending, key)
		s.pendRev++
		s.touch()
	}
}

// IsPendingSub reports whether the given source+channel is still loading.
func (s *Scene) IsPendingSub(k source.Kind, ch string) bool { return s.pending[subPendKey(k, ch)] }

// PendingCount returns how many sources are still loading.
func (s *Scene) PendingCount() int { return len(s.pending) }

// SetSubs replaces the sidebar subscriptions.
func (s *Scene) SetSubs(subs []Subscription) { s.Subs = subs; s.subsRev++; s.touch() }

// SetActive selects the sidebar filter (a subscription index, or AllFilter).
func (s *Scene) SetActive(i int) { s.Active = i; s.touch() }

// touchProfiles marks the profile list dirty so the sidebar sprite re-renders.
func (s *Scene) touchProfiles() { s.profRev++; s.touch() }

// SetProfiles replaces the profile list and selects the active index (clamped),
// deriving the sidebar subscriptions from the newly-active profile.
func (s *Scene) SetProfiles(profiles []settings.Profile, active int) {
	s.Profiles = profiles
	s.touchProfiles()
	s.setActiveProfile(active)
}

// ActiveProfileIndex returns the active profile index.
func (s *Scene) ActiveProfileIndex() int { return s.activeProf }

// ActiveProfile returns the active profile (a safe fallback when empty).
func (s *Scene) ActiveProfile() settings.Profile {
	if s.activeProf >= 0 && s.activeProf < len(s.Profiles) {
		return s.Profiles[s.activeProf]
	}
	return settings.Profile{Name: "All"}
}

// SetActiveProfile switches the active profile (clamped) and rebuilds the
// sidebar subscriptions from it.
func (s *Scene) SetActiveProfile(i int) { s.setActiveProfile(i) }

// setActiveProfile clamps i, records it, and re-derives the sidebar Subs.
func (s *Scene) setActiveProfile(i int) {
	if i < 0 || i >= len(s.Profiles) {
		i = 0
	}
	s.activeProf = i
	s.selEdit = i
	s.rebuildSubs()
}

// rebuildSubs derives the display Subs (with labels) from the active profile
// and resets the sub filter to "All".
func (s *Scene) rebuildSubs() {
	p := s.ActiveProfile()
	subs := make([]Subscription, 0, len(p.Subs))
	for _, su := range p.Subs {
		subs = append(subs, Subscription{Source: su.Source, Channel: su.Channel})
	}
	s.SetSubs(subs)
	s.SetActive(AllFilter)
}

// ThemeName returns the persisted theme choice ("system"|"light"|"dark").
func (s *Scene) ThemeName() string { return s.themeName }

// SetThemeName records the theme choice (the host resolves it to a palette).
func (s *Scene) SetThemeName(name string) { s.themeName = name; s.touch() }

// CachePath returns the media cache directory.
func (s *Scene) CachePath() string { return s.cachePath }

// SetCachePath records the media cache directory.
func (s *Scene) SetCachePath(p string) { s.cachePath = p; s.touch() }

// Settings snapshots the editor state for persistence.
func (s *Scene) Settings() *settings.Settings {
	return &settings.Settings{
		Profiles:  s.Profiles,
		Active:    s.activeProf,
		Theme:     s.themeName,
		CachePath: s.cachePath,
		Accounts:  s.EditedAccounts(),
	}
}

// SetThumb attaches a decoded thumbnail for an item and invalidates its sprite
// so the next Draw picks it up.
func (s *Scene) SetThumb(id string, img *image.RGBA) {
	if s.Thumbs == nil {
		s.Thumbs = map[string]*image.RGBA{}
	}
	s.Thumbs[id] = img
	s.invalidateCards()
	s.touch()
}

// Resize updates the surface size, clamped to the minimum.
func (s *Scene) Resize(w, h int) { s.W, s.H = w, h; s.clampSize(); s.invalidateCards(); s.touch() }

// SetScale sets the display scale, clamped to [MinZoom, MaxZoom].
func (s *Scene) SetScale(f float64) {
	if f < MinZoom {
		f = MinZoom
	}
	if f > MaxZoom {
		f = MaxZoom
	}
	if f != s.Scale {
		s.invalidateCards()
		s.touch()
	}
	s.Scale = f
}

func (s *Scene) clampSize() {
	if s.W < MinW {
		s.W = MinW
	}
	if s.H < MinH {
		s.H = MinH
	}
	if s.Scale == 0 {
		s.Scale = 1
	}
}

// Search returns the current filter text (the search widget's text).
func (s *Scene) Search() string { return s.searchEntry.Text }

// SetSearch replaces the filter text on the search widget.
func (s *Scene) SetSearch(v string) { s.searchEntry.Text = v; s.touch() }

// SearchEntry exposes the topbar's search widget so the app can two-way bind
// vm.Search to it (mvvm.BindField(&entry.Text, &entry.OnChange)).
func (s *Scene) SearchEntry() *toolkit.SearchEntry { return s.searchEntry }

// InvalidateSearch bumps the damage sequence after the search binder writes
// SearchEntry.Text directly (bypassing SetSearch). The app's search binder passes
// it as mvvm.BindField's invalidate hook so a ViewModel-originated search change
// repaints the topbar (whose sprite cache keys on the widget text).
func (s *Scene) InvalidateSearch() { s.touch() }

// SearchFocused reports whether the search field has keyboard focus.
func (s *Scene) SearchFocused() bool { return s.searchFocused }

// FocusSearch gives (or removes) keyboard focus to the search field.
func (s *Scene) FocusSearch(v bool) { s.searchFocused = v; s.touch() }

// TypeRune appends r to whichever text field currently has focus: the topbar
// search (feed view) or the channel/rename/cache field (settings view).
func (s *Scene) TypeRune(r rune) {
	if s.mode == ModeSettings {
		if f := s.focusedField(); f != nil {
			*f += string(r)
			s.touch()
		}
		return
	}
	if s.mode == ModeAccounts {
		if s.accFocus != "" {
			s.accSetField(s.accFocus, s.accFieldValue(s.accSel, s.accFocus)+string(r))
			s.touch()
		}
		return
	}
	if s.mode == ModeBrowse {
		if s.browseFocused {
			s.browseEntry.OnEvent(toolkit.Event{Kind: toolkit.EventChar, Code: string(r)})
			s.browseScrollY = 0
			s.touch()
		}
		return
	}
	if s.searchFocused {
		// Feed the printable rune through the SearchEntry widget itself, so the
		// widget's OnChange (bound to vm.Search) fires exactly as a real widget
		// keypress would.
		s.searchEntry.OnEvent(toolkit.Event{Kind: toolkit.EventChar, Code: string(r)})
		s.touch()
	}
}

// Backspace removes the last rune of the focused text field.
func (s *Scene) Backspace() {
	if s.mode == ModeSettings {
		if f := s.focusedField(); f != nil && *f != "" {
			*f = trimLastRune(*f)
			s.touch()
		}
		return
	}
	if s.mode == ModeAccounts {
		if cur := s.accFieldValue(s.accSel, s.accFocus); s.accFocus != "" && cur != "" {
			s.accSetField(s.accFocus, trimLastRune(cur))
			s.touch()
		}
		return
	}
	if s.mode == ModeBrowse {
		if s.browseFocused && s.browseEntry.Text != "" {
			s.browseEntry.OnEvent(toolkit.Event{Kind: toolkit.EventKeyDown, Code: "Backspace"})
			s.browseScrollY = 0
			s.touch()
		}
		return
	}
	if s.searchFocused && s.searchEntry.Text != "" {
		s.searchEntry.OnEvent(toolkit.Event{Kind: toolkit.EventKeyDown, Code: "Backspace"})
		s.touch()
	}
}

// focusedField returns a pointer to the settings text buffer that has focus, or
// nil when no field is focused.
func (s *Scene) focusedField() *string {
	switch s.sf {
	case FocusChannel:
		return &s.channelInput
	case FocusRename:
		return &s.renameInput
	case FocusCache:
		return &s.cacheInput
	default:
		return nil
	}
}

// trimLastRune drops the final rune of str.
func trimLastRune(str string) string {
	r := []rune(str)
	if len(r) == 0 {
		return str
	}
	return string(r[:len(r)-1])
}

// Mode reports whether the feed or the detail view is showing.
func (s *Scene) Mode() Mode { return s.mode }

// Detail returns the item currently open in the detail view.
func (s *Scene) Detail() source.Item { return s.detail }

// OpenDetail switches to the in-app reading view for it (instead of a browser).
func (s *Scene) OpenDetail(it source.Item) {
	s.mode = ModeDetail
	s.detail = it
	s.detailScrollY = 0
	s.touch()
}

// CloseDetail returns from the detail view to the feed.
func (s *Scene) CloseDetail() {
	s.mode = ModeFeed
	s.touch()
}

// ToggleGroup expands or collapses the Usenet post group with the given release
// base. The expanded set is keyed by base so the state survives re-layout and
// scrolling; expanding grows the card to list its member parts.
func (s *Scene) ToggleGroup(base string) {
	if s.groupExpanded == nil {
		s.groupExpanded = map[string]bool{}
	}
	s.groupExpanded[base] = !s.groupExpanded[base]
	s.touch()
}

// GroupExpanded reports whether the group with the given base is expanded.
func (s *Scene) GroupExpanded(base string) bool { return s.groupExpanded[base] }

// Scroll adjusts the vertical scroll of whichever view is showing, clamped to
// its content height.
func (s *Scene) Scroll(dy int) {
	if s.mode == ModeSettings {
		s.settingsScrollY += dy
		s.layoutSettings() // self-clamps settingsScrollY against the content height
		s.touch()
		return
	}
	if s.mode == ModeDetail {
		s.detailScrollY += dy
		s.layoutDetail()
		s.detailScrollY = clampScroll(s.detailScrollY, s.detailContentH-(s.H-s.m.topbarH))
		s.touch()
		return
	}
	if s.mode == ModeLog {
		s.logScrollY += dy
		s.layoutLog()
		s.logScrollY = clampScroll(s.logScrollY, s.logContentH-(s.H-s.m.topbarH))
		s.touch()
		return
	}
	if s.mode == ModeAccounts {
		s.accScrollY += dy
		s.layoutAccounts()
		s.accScrollY = clampScroll(s.accScrollY, s.accContentH-(s.H-s.m.topbarH))
		s.touch()
		return
	}
	if s.mode == ModeBrowse {
		s.browseScrollY += dy
		s.layoutBrowse()
		s.browseScrollY = clampScroll(s.browseScrollY, s.browseContentH-(s.H-s.m.topbarH))
		s.touch()
		return
	}
	// Feed view: a wheel over the preview pane scrolls the pane's content.
	if s.previewR.W > 0 && s.lastMouseX >= s.previewR.X && s.previewContentH > s.previewR.H {
		s.previewScrollY = clampScroll(s.previewScrollY+dy, s.previewContentH-s.previewR.H)
		s.touch()
		return
	}
	// A wheel over the sidebar scrolls its (overflowing) subscription list; anywhere
	// else scrolls the feed. sideMaxScroll is 0 when the sub list fits, so this only
	// diverts the wheel when there is actually overflow.
	if !s.sidebarCollapsed && s.lastMouseX < s.m.sidebarW && s.sideMaxScroll > 0 {
		s.sideScrollY = clampScroll(s.sideScrollY+dy, s.sideMaxScroll)
		s.layout()
		s.touch()
		return
	}
	s.ScrollY += dy
	s.layout()
	s.ScrollY = clampScroll(s.ScrollY, s.contentH-(s.H-s.m.topbarH))
	s.touch()
}

// clampScroll bounds v to [0, max] (max<0 => 0).
func clampScroll(v, max int) int {
	if max < 0 {
		max = 0
	}
	if v > max {
		v = max
	}
	if v < 0 {
		v = 0
	}
	return v
}

// filtered returns the items matching the active subscription filter and the
// search text (case-insensitive substring of the title).
func (s *Scene) filtered() []source.Item {
	q := strings.ToLower(strings.TrimSpace(s.searchEntry.Text))
	var sub *Subscription
	if s.Active >= 0 && s.Active < len(s.Subs) {
		sub = &s.Subs[s.Active]
	}
	out := make([]source.Item, 0, len(s.Items))
	for _, it := range s.Items {
		if sub != nil {
			if it.Source != sub.Source {
				continue
			}
			if sub.Channel != "" && !strings.EqualFold(it.Channel, sub.Channel) {
				continue
			}
		}
		if q != "" && !strings.Contains(strings.ToLower(it.Title), q) {
			continue
		}
		out = append(out, it)
	}
	return out
}
