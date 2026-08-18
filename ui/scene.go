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
	"sort"
	"strings"
	"time"

	"github.com/go-widgets/mvvm"
	"github.com/go-widgets/mvvm/tkbind"
	"github.com/go-widgets/painter"
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

// WebLink is one clickable anchor in a rendered web preview: Rect is its bounds
// in the rendered image's own pixel coordinates (at the width it was rendered),
// Href the resolved absolute target. The pane maps a click through the image's
// display rect + scale back to these to find the anchor under the cursor.
type WebLink struct {
	Rect image.Rectangle
	Href string
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
	HitNone               HitKind = iota
	HitItem                       // a feed item (open it in the detail view) — Item set
	HitSub                        // a sidebar subscription (filter the feed) — Sub set (AllFilter = All)
	HitSearch                     // the topbar search field (focus it)
	HitBack                       // the detail view's back button (return to the feed)
	HitOpenExternal               // the detail view's "open original" button — Item set
	HitProfile                    // a sidebar profile tab (switch active profile) — Profile set
	HitSettings                   // the sidebar ⚙ Settings entry (open preferences)
	HitLog                        // the sidebar Network-log entry (open the HTTP log)
	HitCloseLog                   // the log view's "< Back" button (return to the feed)
	HitBurger                     // the topbar burger button (collapse/expand the sidebar)
	HitRefresh                    // the topbar refresh button (re-fetch the feed) — mirrors the pull-to-refresh gesture
	HitSidebarDivider             // the draggable divider at the sidebar's right edge
	HitFixAuth                    // an in-feed "needs sign-in" banner row — Value = source kind
	HitToggleGroup                // a Usenet group card's header/chevron — Value = release base
	HitReconstruct                // a Usenet group card's "Reconstruct" affordance — Value = release base
	HitPreviewGroup               // a Usenet group card's body (preview it in the pane) — Value = release base
	HitOpenPreview                // the preview pane's "Open" button (full reading view) — Item set
	HitPreviewDivider             // the preview pane's left-edge resize grip (start a drag)
	HitPreviewTextSmaller         // the preview toolbar's "A−" pill (shrink the reader text)
	HitPreviewTextLarger          // the preview toolbar's "A+" pill (grow the reader text)
	HitToggleDownload             // a complete post's download checkbox — Value = release base
	HitClearDownloads             // the download panel's "Clear" button
	HitToggleFolder               // a sidebar virtual-folder row (collapse/expand) — Value = folder name

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
	HitDeleteProfile     // Profile = index
	HitRenameProfile     // focus the rename field for Profile = index
	HitSelectKind        // Value = source kind for the add-subscription palette
	HitAddSub            // commit the channel input into the edited profile
	HitRemoveSub         // Profile = index, Sub = subscription index
	HitFocusChannel      // focus the add-channel input
	HitFocusCache        // focus the media-cache path input
	HitFocusCacheSize    // focus the media-cache size-limit (MB) input
	HitFocusCacheBackend // focus the media-cache backend (plugin path) input
	HitFocusZoomIn       // focus the zoom-in browser-shortcut key input
	HitFocusZoomOut      // focus the zoom-out browser-shortcut key input
	HitTheme             // Value = "system"|"light"|"dark"
	HitSignInBrowser     // Value = "default"|"firefox"|"chrome"|"safari"|"edge" (sign-in browser)
	HitBrowserTabs       // Value = "multi"|"single" (web-preview browser tab mode)
	HitBrowserChrome     // Value = "shown"|"hidden" (web-preview toolbar/urlbar visibility)
	HitInfiniteScroll    // Value = "on"|"off" (fetch next page on scroll-to-bottom)
	HitClearRenderCache  // empty the web-preview render cache
	HitCloseSettings     // leave the settings view

	// Accounts-view actions (Mode == ModeAccounts):
	HitAccounts            // the sidebar 👤 Accounts entry (open the accounts editor)
	HitCloseAccounts       // leave the accounts view (commit)
	HitSelectAccount       // Value = source kind for the provider being edited
	HitFocusAccountField   // Value = credential field key to focus
	HitToggleAccountBool   // Value = bool credential field key to flip (Usenet TLS)
	HitImportRedditFirefox // the Reddit editor's "Import session from Firefox" button
	HitImportFollows       // a follow-capable provider's "Import subscriptions" button (Value = source kind)
	HitRedditSignIn        // the Reddit editor's "Sign in to Reddit in browser" button
	HitImportSession       // the Instagram/TikTok/X editor's "Import session from Firefox" button

	// Reddit search view actions (Mode == ModeSearch):
	HitSearchReddit        // the sidebar "🔍 Search Reddit" entry (open the search view)
	HitCloseSearch         // the search view's "‹ Back" button (return to the feed)
	HitSearchTab           // switch the results tab — Value = "subreddits"|"posts"
	HitFocusSearchQuery    // focus the search-query input
	HitFocusSearchRegex    // focus the subreddit-results regexp filter input
	HitRunSearch           // run the search for the current query + tab
	HitSubscribeSubreddit  // subscribe to a subreddit result — Value = subreddit name
	HitSubscribePostSearch // save the post keyword search as a live channel — Value = query
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
	ModeSearch               // the in-canvas Reddit search / discover view
)

// Hit is the result of [Scene.HitTest].
type Hit struct {
	Kind    HitKind
	Item    source.Item // HitItem
	Sub     int         // HitSub: index into Subs, or AllFilter; HitRemoveSub: sub index
	Profile int         // HitProfile / HitSelectProfile / HitDeleteProfile / HitRemoveSub / HitRenameProfile
	Value   string      // HitTheme / HitSelectKind / HitImportFollows (source kind)
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
	Scale       float64 // display scale (zoom × devicePixelRatio); 0 => 1

	// feed migration (step 1): the unified feed rendered by a virtual.CardList
	// over a live mvvm.ObservableList (see feedcards.go). feedBottomPending arms a
	// scroll-to-bottom (newest post) for the next sync after a filter/tab change;
	// onSelectItem is the app hook fired on a card selection (its bool is true for
	// a keyboard cursor move); feedSelectViaKeyboard carries that flag through the
	// CardList's OnSelect callback for the current event.
	feed                  *feedCards
	onSelectItem          func(it source.Item, viaKeyboard bool)
	feedSelectViaKeyboard bool
	feedBottomPending     bool

	// groupExpanded records which Usenet post groups (keyed by release base) have
	// their member parts listed beneath the header. A collapsed group (the zero
	// value) shows only its summary header; toggling re-measures its feed row so
	// the CardList accounts for the members appearing/disappearing.
	groupExpanded map[string]bool

	// Feed-loading triggers, driven by the CardList's own edge accumulators.
	// OnReachBottom is wired to the list's OnReachTop (a pull toward the top loads
	// OLDER posts, since the newest sits at the bottom); OnPullRefresh is wired to
	// the list's OnReachBottom (a pull past the newest re-aggregates). Both are
	// nil-safe (only called when installed). infiniteScroll gates whether the
	// next-page trigger is offered.
	OnReachBottom  func()
	OnPullRefresh  func()
	infiniteScroll bool

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

	// Profiles are the named sidebar tabs; the active one supplies Subs.
	Profiles     []settings.Profile
	activeProf   int    // active profile index (drives the sidebar + feed)
	themeName    string // "system"|"light"|"dark" (persisted)
	cachePath    string // media cache dir (persisted, repositionable)
	cacheSizeMB  int    // media cache size limit in MB (persisted, configurable)
	cacheBackend string // media-cache backend: "" / "local" = disk, a path = plugin

	// signInBrowser is the browser launched for a provider's browser sign-in flow
	// (today: Reddit) — default|firefox|chrome|safari|edge (persisted).
	signInBrowser string

	// Settings editor (ModeSettings) state.
	selEdit           int          // profile being edited
	sf                Focus        // which text field has keyboard focus
	channelInput      string       // add-subscription channel buffer
	renameInput       string       // rename-profile buffer
	cacheInput        string       // cache-path buffer
	cacheSizeInput    string       // cache-size-limit (MB) buffer
	cacheBackendInput string       // cache-backend (plugin path) buffer
	zoomInInput       string       // zoom-in shortcut key buffer (single rune)
	zoomOutInput      string       // zoom-out shortcut key buffer (single rune)
	newKind           source.Kind  // selected source for the add-subscription palette
	sButtons          []sButton    // clickable regions in the settings view
	sLabels           []sLabel     // section labels in the settings view
	sChips            []sChip      // subscription chips in the settings view
	sChannelR         toolkit.Rect // add-channel input rect
	sCacheR           toolkit.Rect // cache-path input rect
	sCacheSizeR       toolkit.Rect // cache-size-limit (MB) input rect
	sCacheBackendR    toolkit.Rect // cache-backend (plugin path) input rect
	sRenameR          toolkit.Rect // rename input rect
	sZoomInR          toolkit.Rect // zoom-in shortcut key input rect
	sZoomOutR         toolkit.Rect // zoom-out shortcut key input rect
	sDoneR            toolkit.Rect // "Done" button rect

	// Accounts editor (ModeAccounts) state. accBuf holds the editable credential
	// values per provider (seeded from settings.Accounts); accSel is the provider
	// being edited; accFocus is the focused field key ("" = none).
	accBuf            map[source.Kind]map[string]string
	accSel            source.Kind
	accFocus          string
	accScroll         panelScroll
	accLabels         []sLabel
	accProvBtns       []accProvBtn
	accRows           []accFieldRow
	accBackR          toolkit.Rect
	accDoneR          toolkit.Rect
	accImportR        toolkit.Rect // Reddit-only "Import session from Firefox" button (zero when hidden)
	accImportSubsR    toolkit.Rect // "Import subscriptions" button (zero when hidden); shown per follow-capable source
	accSignInR        toolkit.Rect // Reddit-only "Sign in to Reddit in browser" button (zero when hidden)
	accImportSessionR toolkit.Rect // Instagram/TikTok/X "Import session from Firefox" button (zero when hidden)
	// followImportKinds is the set of source kinds whose registered provider can
	// import the connected account's follows (implements source.FollowImporter);
	// the accounts editor shows the "Import subscriptions" affordance for exactly
	// these. Nil/empty means none, so no source shows the button.
	followImportKinds map[source.Kind]bool

	// Optional decoded thumbnails keyed by Item.ID (blitted when present).
	Thumbs map[string]*image.RGBA

	// Topbar search/filter. The text is owned by a toolkit.SearchEntry widget so
	// the topbar demonstrates a real mvvm-bound widget (the app binds vm.Search to
	// searchEntry.Text/OnChange); the scene renders the widget and routes topbar
	// keystrokes into it. searchFocused is the scene-side focus flag the binder
	// reflects vm.SearchFocus onto (SearchEntry is itself focus-agnostic).
	searchEntry   *toolkit.SearchEntry
	searchFocused bool
	// searchCopied marks that Cmd/Ctrl+C just copied the search query, so the
	// topbar paints a select-all highlight over it (visual feedback of what was
	// copied). Cleared on any edit or focus change.
	searchCopied bool

	// Detail (reading) view: ModeDetail shows a single opened item in-app.
	mode         Mode
	detail       source.Item
	detailScroll panelScroll
	backR, openR toolkit.Rect

	// Settings (ModeSettings) scroll: the editor can be taller than the surface
	// (many subscriptions, high zoom, or a short window), so it scrolls like the
	// other views.
	settingsScroll panelScroll

	// Right-hand preview/details pane (feed view). It is docked on the right and
	// shows the item last clicked in the feed — its badge, title, meta, an image
	// (from Thumbs, e.g. a decoded Usenet binary) and body — with an "Open" button
	// for the full-screen reading view. previewHas is false before any selection
	// (the pane shows an empty prompt). previewImgPending marks that an async image
	// fetch was requested for the current item, so the pane shows a spinner until
	// SetThumb lands. previewOpenR/previewImgR are the laid-out button/image rects.
	previewItem       source.Item
	previewHas        bool
	previewScroll     panelScroll
	previewImgPending bool
	previewR          toolkit.Rect // the whole pane
	previewOpenR      toolkit.Rect // "Open" (full detail) button
	previewImgR       toolkit.Rect // image area (for hit-testing / geometry)
	sTextSmallerR     toolkit.Rect // "A−" reader-text zoom-out pill
	sTextLargerR      toolkit.Rect // "A+" reader-text zoom-in pill

	// previewComments holds the flattened comment thread per item ID (delivered by
	// the app for a Reddit post), and commentsLoading marks the items whose fetch
	// is in flight so the pane shows a loading line. Both are keyed by Item.ID and
	// touched only on the UI/render thread (SetComments / SetCommentsLoading run
	// there, like SetThumb). The text-post preview appends the thread to its
	// scrolling column; an item with no entry simply shows no comments section.
	previewComments map[string][]source.Comment
	commentsLoading map[string]bool

	// previewTextScale multiplies the reader/preview text size (a text post's
	// title, body and meta, and the header of any previewed post). It is seeded
	// from settings.PreviewTextScale and adjusted by the A−/A+ preview-toolbar
	// pills; ptPx applies it so measure and draw stay consistent.
	previewTextScale float64

	// Web-page preview: for a non-Usenet item that links out (HackerNews /
	// Reddit / RSS …), the target article page is rendered (via go-webengine) and
	// shown in an embedded mini-browser widget — the reusable toolkit.Browser,
	// which owns the tab strip, the Back/Forward/Reload toolbar, the editable
	// address field, the scrollable page area and the indeterminate loading bar.
	// The scene holds it plus its MVVM view-model (BindBrowser mirrors the
	// widget's navigable state onto Observables/Commands and calls touch() so a
	// widget change requests a redraw). The reader drives it: SelectPreview →
	// browser.Open(url,title); OnNavigate → the async page render; Deliver hands
	// the finished render back. browserFocused records that a click landed inside
	// the browser so subsequent keystrokes route to it (its address field) rather
	// than to the topbar search.
	browser        *toolkit.Browser
	browserVM      *tkbind.BrowserVM
	browserFocused bool
	// previewFocused records that the right-hand preview pane holds keyboard focus
	// (toggled with Tab). While set, the scroll keys (arrows / PageUp / PageDown /
	// Space) drive the pane's content rather than the feed card list, and the pane
	// paints an accent focus ring. For a web-linked item the embedded browser is the
	// scroll target, so focusing a web preview also arms browserFocused; for a
	// text/image item previewScroll is the target.
	previewFocused bool
	// browserPressed records that a mouse press landed inside the browser and the
	// button is still held, so pointer motion is forwarded to the browser as an
	// EventMouseDrag (letting a scrollbar thumb track the pointer). Set when
	// ForwardBrowserClick consumes a press inside the browser, cleared on release.
	browserPressed bool
	// gifPlaying is set while the app is driving an animated GIF's frames into the
	// embedded browser itself (bypassing the page renderer, which only produces a
	// single static frame). It keeps Animating true so the present loop keeps
	// ticking — otherwise the GIF would freeze on its first frame the moment the
	// scene went idle. The app sets/clears it via SetGIFPlaying.
	gifPlaying bool
	// bookmarks is the set of bookmarked page URLs (persisted via Settings); the
	// address bar's star reflects/toggles membership for the current page.
	bookmarks map[string]bool
	// zoomInKey / zoomOutKey are the base runes that, with a command-style modifier
	// (Ctrl/Cmd), zoom the embedded web preview in / out. Seeded to '='/'-' and
	// overridable via SetBrowserZoomKeys from the persisted settings.
	zoomInKey  rune
	zoomOutKey rune
	// previewUserW is the user-dragged pane width in device px (0 => default),
	// clamped at read time; draggingPreview is set while its divider is dragged.
	previewUserW    int
	draggingPreview bool

	// textSel is the cross-widget text selection over the reading surfaces. It is
	// refilled each frame with the runs actually drawn (via toolkit.CollectRuns),
	// so a resize / scroll / re-layout keeps it consistent. selecting is set while
	// a pointer drag is extending it (so MouseMove routes to it).
	textSel   toolkit.TextSelection
	selecting bool
	// selAccum accumulates the frame's selectable runs (screen coords) from every
	// surface — the sidebar labels, the visible cards and the preview text — so
	// one commit (commitSelectableRuns) lets a single selection span them all.
	selAccum []toolkit.TextRun

	// Download manager: a docked panel across the bottom of the feed/preview area
	// showing queued/active/finished downloads. Populated by the app via
	// SetDownloads; the feed and preview shrink vertically to make room (feedBottom).
	downloads []DownloadItem
	dlClearR  toolkit.Rect // the panel's "Clear" button

	// statusSegs are the bottom status-bar segments ("<group> N posts") computed
	// each layout from the feed's newsgroup article counts.
	statusSegs []string

	// seen is the per-subscription last-seen high-water marker (set by the app from
	// disk), used to derive the unseen/new count shown next to each sidebar group.
	seen map[string]int

	// viewed is the set of item IDs the user has opened in the preview pane this
	// session; their feed cards are drawn muted ("already read"), via feedDimmed.
	viewed map[string]bool

	// Network-log (ModeLog) view: a scrollable, newest-first list of the HTTP
	// exchanges the providers made, fed live from an injected source so the app
	// need not push updates. logSource is nil when no recorder is wired.
	logSource func() []LogEntry
	logScroll panelScroll

	// renderCacheStats reports the web-preview render cache's current size (pages,
	// bytes) for the Settings caption. Injected by the app (nil under the CLI /
	// bare-scene paths, where the caption shows an empty cache).
	renderCacheStats func() (pages, bytes int)
	logRowH          int
	logBackR         toolkit.Rect

	// The log row list is the reader's first surface on the toolkit's container/layout
	// stack: an mvvm.ObservableList drives a toolkit.Container (VBox layout) via
	// tkbind.BindContainer, so the rows are retained widgets rebuilt only when the
	// exchanges change (synced from logSource in layoutLog), not per frame.
	logList      *mvvm.ObservableList[LogEntry]
	logContainer *toolkit.Container
	logSyncN     int       // entries count at the last list sync
	logSyncTop   time.Time // newest entry's timestamp at the last sync
	logSyncRowH  int       // row height at the last sync (rebuild on zoom)

	// Unified sidebar width model (feed view). The effective sidebar width is 0
	// when collapsed, else the user-dragged width (device px, clamped) or the
	// default. draggingSidebar is set while the divider is being dragged.
	sidebarCollapsed bool
	sidebarUserW     int // device px; 0 => default width
	draggingSidebar  bool

	// Sidebar middle list. The "All Sources" root, the virtual folders, the
	// subscriptions and the "Browse newsgroups" / "Search Reddit" discovery
	// entries are a toolkit.TreeView (sideTree) laid out inside the band between
	// the profile tab strip and the pinned footer (Accounts/Log/Settings). The
	// TreeView owns the scroll window, the scrollbar gutter and hit-testing; the
	// reader supplies each row's rich content through its RowRenderer. folders is
	// the persisted set of virtual folders; folderCollapsed keeps their (session)
	// collapse state keyed by name; foldRev bumps on any folder/collapse change so
	// the cached sidebar sprite re-rasterises. lastMouseX/Y is the most recent
	// pointer position, used to route wheel scrolling to the sidebar when the
	// pointer is over it.
	sideTree        *toolkit.TreeView
	folders         []settings.Folder
	folderCollapsed map[string]bool
	foldRev         int
	// renamingFolder is the folder currently being renamed inline in the sidebar
	// (empty = none); renameFolderEntry is the focused toolkit.Entry that holds the
	// edit buffer (its Text) and owns caret/Backspace/Arrows/Home/End behaviour.
	renamingFolder    string
	renameFolderEntry *toolkit.Entry
	sideBandTop       int
	sideBandBot       int
	lastMouseX        int
	lastMouseY        int

	// ctxMenu is the toolkit ContextMenu currently popped up over the scene (a
	// sidebar right-click), or nil. The scene only holds, draws and forwards
	// events to it; the menu widget owns all its own layout, hit-testing, hover,
	// keyboard nav and dismissal, and the Handler owns the items' actions.
	ctxMenu *toolkit.ContextMenu

	// Newsgroup browser (ModeBrowse) state. browseGroups is the server's full
	// active group list (set by the app after a fetch); browseServer names the
	// server for the title. browseEntry is the regexp filter field (a real
	// toolkit.SearchEntry, so it can be mvvm-bound like the topbar search).
	// browseExpanded records which tree nodes are open (keyed by full node name);
	// the filtered tree auto-expands so this only applies to the unfiltered view.
	// usenetAddr is non-empty when a Usenet server is configured, which is what
	// gates the sidebar "Browse newsgroups" entry.
	browseGroups []source.GroupInfo
	browseServer string
	usenetAddr   string
	browseEntry  *toolkit.SearchEntry

	// browseStats caches the sampled content-mix estimate (binaries/images) per
	// group name, filled asynchronously as groups are focused in the browser and
	// shown as a "~B% bin · ~I% img" suffix on the group's row. Empty until a
	// scan lands; a group without an entry shows only its post count.
	browseStats    map[string]source.GroupStats
	browseFocused  bool
	browseExpanded map[string]bool
	// browseTreeView is the scrolling newsgroup hierarchy: a toolkit.TreeView (a
	// hidden synthetic Root whose children are the top-level hierarchies, like the
	// sidebar's) that owns the chevrons, the row window, virtualization and the
	// scrollbar gutter, while the reader supplies each row's rich content (label +
	// post count + Subscribe affordance) through its RowRenderer. Its ScrollRow is
	// the tree's vertical position; browseRows mirrors its flattened, expand-aware
	// row order for hit-testing + keyboard navigation.
	browseTreeView *toolkit.TreeView
	browseRows     []browseRowLayout
	browseSel      int // keyboard-selected row index into browseRows
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

	// Reddit search (ModeSearch) view state. The bulk of it — the query/regex
	// widgets, the active tab, the result slices and the laid-out rects — lives in
	// searchState (defined in search_view.go) so this struct stays lean. The
	// sidebar "Search Reddit" and "Browse newsgroups" entries are now nodes of the
	// sidebar TreeView (sideTree), not separate rects.
	search searchState

	m         metrics
	profTabs  []profTabHit
	settingsR toolkit.Rect
	logR      toolkit.Rect // sidebar Network-log entry
	accountsR toolkit.Rect // sidebar Accounts entry
	burgerR   toolkit.Rect // topbar burger button (feed view)
	refreshR  toolkit.Rect // topbar refresh button (feed view)
	searchR   toolkit.Rect

	// The chrome (sidebar/topbar) is cached in single sprite slots — like Evas
	// smart-object surfaces — so scrolling never re-rasterises any text. The
	// sidebar's middle list is a live toolkit.TreeView drawn into the sprite.
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

	// a11yCache memoises the accessibility tree so a host that pulls it on every
	// paint frame (go-widgets/window's a11y bridge does) does not pay a full
	// re-layout 60 times a second. It is valid only for a11yRev; any state change
	// bumps rev, so a stale rev forces a rebuild on the next pull.
	a11yCache []A11yNode
	a11yRev   int
}

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
		themeName: settings.ThemeSystem, signInBrowser: settings.DefaultSignInBrowser,
		previewTextScale: settings.DefaultPreviewTextScale,
		newKind:          source.Reddit,
		zoomInKey:        '=', zoomOutKey: '-',
		searchEntry: toolkit.NewSearchEntry(""),
		browseEntry: toolkit.NewSearchEntry("")}
	// The topbar SearchEntry paints its left prefix with a real Iconoir
	// magnifier instead of the toolkit's "?" bitmap-font stand-in.
	s.searchEntry.Icon = drawSearchIcon
	s.browseEntry.Icon = drawSearchIcon
	// The embedded web-preview browser, with its MVVM view-model bound so a
	// navigation / load / tab change requests a redraw through touch(). The app
	// wires browser.OnNavigate to the async page render.
	s.browser = toolkit.NewBrowser()
	// Draw the preview's vertical scrollbar with the SAME shared toolkit.Scrollbar
	// every other panel uses (drawWebPreview → drawVScrollbar), not the browser's
	// own embedded house-style bar — so the preview pane matches the feed and
	// sidebar exactly. The browser keeps its wheel scrolling.
	s.browser.HideScrollbar = true
	// Real Iconoir glyphs on the toolbar buttons (instead of the text-label
	// fallback): Back / Forward / Reload + the two zoom magnifiers. Each is sized
	// to the topbar burger (chromeIcon) so the preview chrome reads at one weight.
	s.browser.BackIcon = s.chromeIcon(iconNavLeft)
	s.browser.ForwardIcon = s.chromeIcon(iconNavRight)
	s.browser.ReloadIcon = s.chromeIcon(iconRefresh)
	s.browser.ZoomOutIcon = s.chromeIcon(iconZoomOut)
	s.browser.ZoomInIcon = s.chromeIcon(iconZoomIn)
	s.browser.FitIcon = s.chromeIcon(iconFit) // best-fit zoom (third zoom-group button)
	// Address-bar status/bookmark icons: a leading padlock (secure https vs a
	// crossed lock otherwise) and a trailing bookmark star (accent when saved),
	// both burger-sized via chromeGlyphBox to match the toolbar buttons.
	s.browser.LeadingIcon = func(p painter.Painter, r toolkit.Rect, ink toolkit.RGBA) {
		ic := iconLockSlash
		if strings.HasPrefix(s.browser.CurrentURL(), "https://") {
			ic = iconLock
		}
		drawIcon(p, s.chromeGlyphBox(r), ic, ink)
	}
	s.browser.BookmarkIcon = func(p painter.Painter, r toolkit.Rect, ink toolkit.RGBA, on bool) {
		if on {
			ink = bookmarkGold // a saved page's star stays clearly visible (accent washed out)
		}
		drawIcon(p, s.chromeGlyphBox(r), iconStar, ink)
	}
	s.browserVM, _ = tkbind.BindBrowser(s.browser, s.touch)
	s.clampSize()
	return s
}

// Browser exposes the embedded web-preview browser so the app can wire its
// OnNavigate render seam and drive it (Open / Deliver).
func (s *Scene) Browser() *toolkit.Browser { return s.browser }

// BrowserVM exposes the browser's MVVM view-model (observable URL / loading /
// history-guards and the Back/Forward/Reload commands).
func (s *Scene) BrowserVM() *tkbind.BrowserVM { return s.browserVM }

// SetBookmarks seeds the bookmarked-URL set from the persisted settings.
func (s *Scene) SetBookmarks(urls []string) {
	s.bookmarks = make(map[string]bool, len(urls))
	for _, u := range urls {
		s.bookmarks[u] = true
	}
}

// IsBookmarked reports whether url is in the bookmark set.
func (s *Scene) IsBookmarked(url string) bool { return s.bookmarks[url] }

// SetBookmarked adds or removes url from the bookmark set (lazily allocating).
func (s *Scene) SetBookmarked(url string, on bool) {
	if s.bookmarks == nil {
		s.bookmarks = map[string]bool{}
	}
	if on {
		s.bookmarks[url] = true
	} else {
		delete(s.bookmarks, url)
	}
}

// BookmarkedURLs returns the bookmarked URLs in sorted order (stable snapshot
// for persistence).
func (s *Scene) BookmarkedURLs() []string {
	out := make([]string, 0, len(s.bookmarks))
	for u := range s.bookmarks {
		out = append(out, u)
	}
	sort.Strings(out)
	return out
}

// SyncBookmarkStar sets the address-bar star to reflect whether the browser's
// current page is bookmarked (called after each navigation).
func (s *Scene) SyncBookmarkStar() {
	s.browser.Bookmarked().Set(s.IsBookmarked(s.browser.CurrentURL()))
}

// SetBrowserSingleTab selects the embedded browser's tab mode: single-tab reuses
// one tab per page (no strip), multi-tab (the default) keeps a switchable strip.
func (s *Scene) SetBrowserSingleTab(v bool) {
	if v {
		s.browser.SetTabMode(toolkit.SingleTab)
	} else {
		s.browser.SetTabMode(toolkit.MultiTab)
	}
	s.touch()
}

// BrowserSingleTab reports whether the embedded browser is in single-tab mode.
func (s *Scene) BrowserSingleTab() bool { return s.browser.Mode() == toolkit.SingleTab }

// SetInfiniteScroll enables or disables fetching the next page of posts when the
// feed list is scrolled to the bottom (the OnReachBottom trigger). Pull-to-refresh
// is always on and unaffected — only the bottom trigger is gated.
func (s *Scene) SetInfiniteScroll(v bool) { s.infiniteScroll = v; s.touch() }

// InfiniteScroll reports whether the bottom-of-feed next-page trigger is enabled.
func (s *Scene) InfiniteScroll() bool { return s.infiniteScroll }

// SetBrowserChromeHidden hides (v=true) or shows the embedded browser's toolbar
// + tab strip, so a page can render chrome-free in the preview.
func (s *Scene) SetBrowserChromeHidden(v bool) { s.browser.HideChrome = v; s.touch() }

// BrowserChromeHidden reports whether the embedded browser's chrome is hidden.
func (s *Scene) BrowserChromeHidden() bool { return s.browser.HideChrome }

// SetBrowserZoomKeys configures the base runes that, held with a command-style
// modifier (Ctrl/Cmd), zoom the embedded web preview in / out. A blank or
// multi-rune string leaves the corresponding binding unchanged, so a missing
// persisted value keeps the '='/'-' default.
func (s *Scene) SetBrowserZoomKeys(in, out string) {
	if r, ok := singleRune(in); ok {
		s.zoomInKey = r
	}
	if r, ok := singleRune(out); ok {
		s.zoomOutKey = r
	}
	s.touch()
}

// BrowserZoomInKey / BrowserZoomOutKey return the configured base keys as 1-rune
// strings ("" when unset) for the settings snapshot and the settings editor.
func (s *Scene) BrowserZoomInKey() string  { return runeStr(s.zoomInKey) }
func (s *Scene) BrowserZoomOutKey() string { return runeStr(s.zoomOutKey) }

// ZoomKeyDir reports the zoom direction bound to rune r: +1 for the zoom-in key,
// -1 for the zoom-out key, and 0 for anything else (including the zero rune). If
// the two keys are misconfigured to the same rune, zoom-in wins.
func (s *Scene) ZoomKeyDir(r rune) int {
	switch {
	case r != 0 && r == s.zoomInKey:
		return 1
	case r != 0 && r == s.zoomOutKey:
		return -1
	default:
		return 0
	}
}

// ZoomBrowserIn / ZoomBrowserOut zoom the embedded web preview through its MVVM
// commands — the same path the toolbar +/- buttons drive — so the zoom stays
// clamped and mirrors into the view-model.
func (s *Scene) ZoomBrowserIn()  { s.browserVM.ZoomIn.Execute() }
func (s *Scene) ZoomBrowserOut() { s.browserVM.ZoomOut.Execute() }

// SetPreviewTextScale sets the reader/preview text zoom (a text post's title,
// body and meta, and the header of any previewed post), clamped to the supported
// range, and requests a redraw. The A−/A+ preview-toolbar pills drive it.
func (s *Scene) SetPreviewTextScale(f float64) {
	s.previewTextScale = settings.ClampPreviewTextScale(f)
	s.touch()
}

// PreviewTextScale returns the current reader/preview text zoom multiplier.
func (s *Scene) PreviewTextScale() float64 { return s.previewTextScale }

// TextZoomButtons returns the A−/A+ preview-toolbar pill rects and whether they
// are shown (an item is previewed and the pane is visible). Front-ends/tests
// call it after a Draw/HitTest.
func (s *Scene) TextZoomButtons() (smaller, larger toolkit.Rect, shown bool) {
	return s.sTextSmallerR, s.sTextLargerR, s.sTextSmallerR.W > 0
}

// ptPx scales a base preview font size (logical px) by the user's preview-text
// zoom, then to device pixels. Measure and draw both route the preview title /
// body / meta sizes through it, so wrapping and row heights stay consistent as
// the A−/A+ pills change the scale.
func (s *Scene) ptPx(base int) int {
	return rpxOf(s, int(float64(base)*s.previewTextScale+0.5))
}

// SetTheme swaps the palette.
func (s *Scene) SetTheme(t *toolkit.Theme) {
	if t != nil {
		s.theme = t
		s.touch()
	}
}

// SetItems replaces the feed (caller merges/sorts newest-first).
func (s *Scene) SetItems(items []source.Item) {
	s.Items = items
	s.feedBottomPending = true // open the refreshed feed at its newest (bottom) post
	s.subsRev++                // the sidebar shows per-group post counts derived from the items
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

// Animating reports whether the scene wants a fresh frame every tick (a loading
// indicator is moving): a feed refresh, OR a preview image / web-page render in
// flight (whose spinner must keep spinning). When false the scene is idle and a
// damage-gated present loop never re-draws — so animation costs nothing when
// nothing is loading.
func (s *Scene) Animating() bool {
	return s.loading || s.previewImgPending || s.browser.Loading() || s.gifPlaying || s.search.loading
}

// SetGIFPlaying marks whether the app is currently driving an animated GIF's
// frames into the embedded browser. While true, Animating stays true so the
// present loop keeps ticking and the GIF advances; the app clears it when the
// preview navigates away or closes.
func (s *Scene) SetGIFPlaying(v bool) { s.gifPlaying = v; s.touch() }

// GIFPlaying reports whether an animated GIF is currently being driven into the
// preview (front-ends/tests observe it).
func (s *Scene) GIFPlaying() bool { return s.gifPlaying }

// AdvanceAnim advances the animation clock by n frames and marks the scene
// dirty. A present loop calls it while [Animating] is true; the indeterminate
// indicator derives its position from the frame counter. n is the number of
// tick intervals that have elapsed since the last advance, so a loop that
// throttles its redraws to below the tick rate (advancing every few ticks
// instead of every one) still turns the spinner at the same real-time speed —
// it just steps it in larger, less frequent increments. n <= 0 is a no-op, so a
// spurious call can never rewind or freeze the clock.
func (s *Scene) AdvanceAnim(n int) {
	if n <= 0 {
		return
	}
	s.animFrame += n
	// Advance the embedded browser's indeterminate loading bar in step with the
	// spinner cadence (one revolution per spinnerPeriod frames), by the same n
	// frames so its speed matches the spinner's regardless of the redraw cadence.
	s.browser.Tick(float64(n) / float64(spinnerPeriod))
	s.touch()
}

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

// SetActive selects the sidebar filter (a subscription index, or AllFilter). A
// filter switch opens the (re-filtered) feed at its newest post.
func (s *Scene) SetActive(i int) { s.Active = i; s.feedBottomPending = true; s.touch() }

// ActiveFilter returns the selected sidebar filter — a subscription index, or
// [AllFilter] when no single subscription is selected. Pull-to-refresh reads it
// so it can re-fetch only the subscription currently in view.
func (s *Scene) ActiveFilter() int { return s.Active }

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

// SignInBrowser returns the browser launched for a provider sign-in flow
// (default|firefox|chrome|safari|edge).
func (s *Scene) SignInBrowser() string { return s.signInBrowser }

// SetSignInBrowser records the sign-in browser choice (unrecognised values fall
// back to the default so the picker can never persist a bad value).
func (s *Scene) SetSignInBrowser(name string) {
	if !settings.ValidSignInBrowser(name) {
		name = settings.DefaultSignInBrowser
	}
	s.signInBrowser = name
	s.touch()
}

// CachePath returns the media cache directory.
func (s *Scene) CachePath() string { return s.cachePath }

// SetCachePath records the media cache directory.
func (s *Scene) SetCachePath(p string) { s.cachePath = p; s.touch() }

// MediaCacheMB returns the media cache size limit, in MB.
func (s *Scene) MediaCacheMB() int { return s.cacheSizeMB }

// SetMediaCacheMB records the media cache size limit, in MB.
func (s *Scene) SetMediaCacheMB(mb int) { s.cacheSizeMB = mb; s.touch() }

// CacheBackend returns the media-cache backend selector ("" / "local" for the
// built-in disk cache, or a filesystem path to a cache-plugin binary).
func (s *Scene) CacheBackend() string { return s.cacheBackend }

// SetCacheBackend records the media-cache backend selector.
func (s *Scene) SetCacheBackend(b string) { s.cacheBackend = b; s.touch() }

// Settings snapshots the editor state for persistence.
func (s *Scene) Settings() *settings.Settings {
	// Persist the tab mode explicitly (a *bool) so an opt-out to multiple tabs is
	// stored as false and is not re-defaulted to single-tab on the next load.
	singleTab := s.BrowserSingleTab()
	infinite := s.infiniteScroll
	return &settings.Settings{
		Profiles:          s.Profiles,
		Active:            s.activeProf,
		Theme:             s.themeName,
		CachePath:         s.cachePath,
		MediaCacheMB:      s.cacheSizeMB,
		CacheBackend:      s.cacheBackend,
		Accounts:          s.EditedAccounts(),
		BrowserSingleTab:  &singleTab,
		InfiniteScroll:    &infinite,
		HideBrowserChrome: s.BrowserChromeHidden(),
		SignInBrowser:     s.signInBrowser,
		ZoomInKey:         s.BrowserZoomInKey(),
		ZoomOutKey:        s.BrowserZoomOutKey(),
		Bookmarks:         s.BookmarkedURLs(),
		PreviewTextScale:  s.previewTextScale,
		SidebarFolders:    s.SidebarFolders(),
	}
}

// SetThumb attaches a decoded thumbnail for an item so the next Draw picks it up
// (the CardList re-renders the card from the live thumbnail cache each frame).
func (s *Scene) SetThumb(id string, img *image.RGBA) {
	if s.Thumbs == nil {
		s.Thumbs = map[string]*image.RGBA{}
	}
	s.Thumbs[id] = img
	// A card reserves its thumbnail column from the moment its item declares media,
	// but its rows were measured while the decoded image was still absent; re-measure
	// + re-lay-out them now so the card mounts the freshly-arrived thumbnail at the
	// right height rather than clipping it.
	s.invalidateFeedThumb(id)
	s.touch()
}

// SetComments attaches the flattened comment thread for an item id (the app
// delivers it after an async fetch) and clears that id's loading marker, so the
// preview pane paints the thread in place of the "loading comments" line. A nil
// slice is stored as an explicit "fetched, none" so the pane shows "0 comments"
// rather than spinning.
func (s *Scene) SetComments(id string, comments []source.Comment) {
	if s.previewComments == nil {
		s.previewComments = map[string][]source.Comment{}
	}
	s.previewComments[id] = comments
	if s.commentsLoading != nil {
		delete(s.commentsLoading, id)
	}
	s.touch()
}

// SetCommentsLoading marks (v=true) or clears that a comment fetch is in flight
// for item id, so the pane shows a loading line beneath the post until
// SetComments lands.
func (s *Scene) SetCommentsLoading(id string, v bool) {
	if s.commentsLoading == nil {
		s.commentsLoading = map[string]bool{}
	}
	if v {
		s.commentsLoading[id] = true
	} else {
		delete(s.commentsLoading, id)
	}
	s.touch()
}

// commentsFor returns the fetched comment thread for id and whether a fetch has
// completed for it (an entry exists, even if empty).
func (s *Scene) commentsFor(id string) ([]source.Comment, bool) {
	c, ok := s.previewComments[id]
	return c, ok
}

// commentsPending reports whether a comment fetch is in flight for id.
func (s *Scene) commentsPending(id string) bool { return s.commentsLoading[id] }

// PreviewComments returns the fetched comment thread for item id and whether a
// fetch has completed (an entry exists, possibly empty). Front-ends/tests use it
// to observe what the app delivered.
func (s *Scene) PreviewComments(id string) ([]source.Comment, bool) { return s.commentsFor(id) }

// CommentsLoading reports whether a comment fetch is in flight for item id.
func (s *Scene) CommentsLoading(id string) bool { return s.commentsPending(id) }

// Resize updates the surface size, clamped to the minimum.
func (s *Scene) Resize(w, h int) { s.W, s.H = w, h; s.clampSize(); s.touch() }

// SetScale sets the display scale, clamped to [MinZoom, MaxZoom].
func (s *Scene) SetScale(f float64) {
	if f < MinZoom {
		f = MinZoom
	}
	if f > MaxZoom {
		f = MaxZoom
	}
	if f != s.Scale {
		s.touch()
	}
	s.Scale = f
	// Pin the toolkit's global metric scale to the reader's device scale so every
	// scale-aware toolkit widget (the feed's PostCard padding/gaps/radius/thumbnail
	// column, badges, …) lays out and paints at the same resolution as the reader's
	// own rpx-scaled fonts. This is the density-change entry point too: a window
	// dragged onto a denser display re-enters here via Handler.Resize (#165).
	toolkit.SetMetricScale(s.Scale)
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

// FocusSearch gives (or removes) keyboard focus to the search field. A focus
// change dismisses the copied-highlight.
func (s *Scene) FocusSearch(v bool) { s.searchFocused = v; s.searchCopied = false; s.touch() }

// TypeRune appends r to whichever text field currently has focus: the topbar
// search (feed view) or the channel/rename/cache field (settings view).
func (s *Scene) TypeRune(r rune) {
	if s.mode == ModeSettings {
		// The zoom-key fields hold exactly one printable rune, so a keystroke
		// replaces the buffer instead of appending.
		switch s.sf {
		case FocusZoomIn:
			s.zoomInInput = string(r)
			s.touch()
		case FocusZoomOut:
			s.zoomOutInput = string(r)
			s.touch()
		default:
			if f := s.focusedField(); f != nil {
				*f += string(r)
				s.touch()
			}
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
			s.resetBrowseScroll()
			s.touch()
		}
		return
	}
	if s.mode == ModeSearch {
		s.search.typeRune(s, r)
		return
	}
	// An inline folder rename (feed view) captures printable runes into its Entry
	// ahead of the topbar search, mirroring how the browse filter captures them.
	if s.renamingFolder != "" {
		s.renameFolderEntry.OnEvent(toolkit.Event{Kind: toolkit.EventChar, Code: string(r)})
		s.foldRev++
		s.touch()
		return
	}
	if s.searchFocused {
		// Feed the printable rune through the SearchEntry widget itself, so the
		// widget's OnChange (bound to vm.Search) fires exactly as a real widget
		// keypress would.
		s.searchCopied = false // an edit dismisses the copied-highlight
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
			s.resetBrowseScroll()
			s.touch()
		}
		return
	}
	if s.mode == ModeSearch {
		s.search.backspace(s)
		return
	}
	// An inline folder rename captures Backspace into its Entry (which deletes at
	// the caret), ahead of the topbar search.
	if s.renamingFolder != "" {
		s.renameFolderEntry.OnEvent(toolkit.Event{Kind: toolkit.EventKeyDown, Code: "Backspace"})
		s.foldRev++
		s.touch()
		return
	}
	if s.searchFocused && s.searchEntry.Text != "" {
		s.searchCopied = false // an edit dismisses the copied-highlight
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
	case FocusCacheSize:
		return &s.cacheSizeInput
	case FocusCacheBackend:
		return &s.cacheBackendInput
	case FocusZoomIn:
		return &s.zoomInInput
	case FocusZoomOut:
		return &s.zoomOutInput
	default:
		return nil
	}
}

// singleRune returns the sole rune of str with ok=true, or (0,false) when str is
// empty or holds more than one rune — so a blank or malformed value leaves a
// binding unchanged.
func singleRune(str string) (rune, bool) {
	rs := []rune(str)
	if len(rs) != 1 {
		return 0, false
	}
	return rs[0], true
}

// runeStr renders a base rune as a 1-rune string, or "" for the zero rune.
func runeStr(r rune) string {
	if r == 0 {
		return ""
	}
	return string(r)
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
	s.detailScroll.offset = 0
	s.touch()
}

// CloseDetail returns from the detail view to the feed.
func (s *Scene) CloseDetail() {
	s.mode = ModeFeed
	s.touch()
}

// Scroll adjusts the vertical scroll of whichever view is showing, clamped to
// its content height.
func (s *Scene) Scroll(dy int) {
	if s.mode == ModeSettings {
		s.settingsScroll.offset += dy
		s.layoutSettings() // self-clamps settingsScrollY against the content height
		s.touch()
		return
	}
	if s.mode == ModeDetail {
		s.detailScroll.offset += dy
		s.layoutDetail() // self-clamps detailScrollY (the refresh-time clamp)
		s.touch()
		return
	}
	if s.mode == ModeLog {
		s.logScroll.offset += dy
		s.layoutLog() // self-clamps logScrollY
		s.touch()
		return
	}
	if s.mode == ModeAccounts {
		s.accScroll.offset += dy
		s.layoutAccounts() // self-clamps accScrollY
		s.touch()
		return
	}
	if s.mode == ModeBrowse {
		// The tree scrolls by whole rows through its TreeView (the device-pixel wheel
		// delta becomes a row count), then layoutBrowse re-clamps it — mirroring the
		// sidebar list's wheel handling.
		s.layoutBrowse()
		s.browseTreeView.ScrollBy(wheelRows(dy, s.m.sideItemH))
		s.layoutBrowse()
		s.touch()
		return
	}
	if s.mode == ModeSearch {
		s.search.scroll.offset += dy
		s.layoutSearch() // self-clamps the results scroll
		s.touch()
		return
	}
	// Feed view: a wheel over the preview pane scrolls the pane's content.
	if s.previewR.W > 0 && s.lastMouseX >= s.previewR.X && s.previewScroll.needsBar() {
		s.previewScroll.scrollBy(dy)
		s.touch()
		return
	}
	// A wheel over the sidebar scrolls its (overflowing) list through the TreeView;
	// anywhere else scrolls the feed. The list only diverts the wheel when it
	// actually overflows the band, so a wheel over a short list still reaches the
	// feed. The device-pixel delta is converted to whole rows (at least one in the
	// wheel direction) since the TreeView scrolls by row.
	if !s.sidebarCollapsed && s.lastMouseX < s.m.sidebarW {
		s.layout() // refresh the tree, band + TreeView bounds before scrolling it
		if s.sidebarListOverflows() {
			s.sideTree.ScrollBy(wheelRows(dy, s.m.sideItemH))
			s.touch()
			return
		}
	}
	// Feed cards: the virtual.CardList owns the scroll anchor and the
	// infinite-scroll / pull-to-refresh edge accumulators, so route the wheel to it
	// (FeedWheel converts the device-pixel delta to a whole-row scroll).
	s.FeedWheel(dy)
}

// panelScroll owns one scrollable panel's vertical position — the reader's analog
// of a scroller. It bundles the offset with the last measured
// content and viewport heights so the offset is never stored apart from the sizes
// that bound it. Panels call refresh() at the end of their layout (the
// Scroller.refresh() analog), and the wheel handler calls scrollBy(); both keep
// the offset clamped, so a resize or content change can never leave it out of range.
type panelScroll struct {
	offset   int // current scroll Y, always within [0, max()]
	contentH int // total content height, measured at layout
	viewport int // visible height, measured at layout
}

// refresh records freshly measured sizes and re-clamps the offset. It is the last
// step of a panel's layout — clamping is tied to the geometry changing, not to the
// wheel event, so one rule in one place keeps every panel resize-safe.
func (ps *panelScroll) refresh(contentH, viewport int) {
	ps.contentH, ps.viewport = contentH, viewport
	ps.offset = clampScroll(ps.offset, ps.max())
}

// max is the largest valid offset — content beyond the viewport (clampScroll
// floors a negative to 0 when everything fits).
func (ps *panelScroll) max() int { return ps.contentH - ps.viewport }

// scrollBy nudges the offset by dy and re-clamps against the current sizes.
func (ps *panelScroll) scrollBy(dy int) { ps.offset = clampScroll(ps.offset+dy, ps.max()) }

// needsBar reports whether the content overflows the viewport (a scrollbar shows).
func (ps *panelScroll) needsBar() bool { return ps.viewport > 0 && ps.contentH > ps.viewport }

// ScrollY exposes the feed's current scroll offset (used by the window app
// layer). It reads the virtual.CardList's pixel scroll offset.
func (s *Scene) ScrollY() int { return s.FeedScrollOffset() }

// wheelRows converts a device-pixel wheel delta to a whole-row scroll amount at
// row height rowH, moving at least one row in the wheel's direction so a small
// notch still scrolls. rowH ≤ 0 (an unlaid-out scene) yields a single row.
func wheelRows(dy, rowH int) int {
	if rowH <= 0 {
		rowH = 1
	}
	rows := dy / rowH
	if rows == 0 {
		switch {
		case dy > 0:
			return 1
		case dy < 0:
			return -1
		}
	}
	return rows
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
