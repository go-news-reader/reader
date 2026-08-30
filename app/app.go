// Package app wires the aggregator together: it aggregates the active profile's
// subscriptions across the provider registry and renders the unified feed into
// the go-widgets UI scene. It is windowing-agnostic — the same App is driven by
// the CLI (render to PNG / serve) and by the native window front-end.
package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-newsgroups/par2"

	"github.com/go-news-reader/reader/feeds"
	"github.com/go-news-reader/reader/internal/biometric"
	"github.com/go-news-reader/reader/internal/browsercookies"
	"github.com/go-news-reader/reader/internal/httplog"
	"github.com/go-news-reader/reader/internal/settings"
	"github.com/go-news-reader/reader/internal/viewmodel"
	"github.com/go-news-reader/reader/internal/webrender"
	"github.com/go-news-reader/reader/mediacache"
	"github.com/go-news-reader/reader/provider/usenet"
	"github.com/go-news-reader/reader/source"
	"github.com/go-news-reader/reader/sourceplugin"
	"github.com/go-news-reader/reader/ui"
)

// App holds the runtime state: the provider registry, the user's persisted
// settings (profiles/theme/cache), the active profile's subscriptions, and the
// UI scene they render into.
type App struct {
	reg   *source.Registry
	store *settings.Store // optional; nil disables persistence (CLI PNG/JSON/serve)
	set   *settings.Settings
	subs  []source.Subscription // == the active profile's subscriptions
	scene *ui.Scene

	// vm is the MVVM view-model: the single source of truth for the core state
	// flow (feed items, loading/pending, search, view mode, status, auth prompts).
	// The app drives it; bindScene subscribes the scene to it, so data flows
	// app logic → view-model → subscription → scene.
	vm *viewmodel.ViewModel

	// Live-rebuild inputs (window path). recorder is the shared HTTP recorder the
	// registry logs into; baseOpts is the flag-derived feeds config; newRegistry
	// is the registry builder (a seam so tests avoid constructing real providers).
	// Together they let ApplyAccounts rebuild the registry with new credentials
	// while keeping the same recorder wired, so the Network log keeps updating.
	recorder    *httplog.Recorder
	baseOpts    feeds.Options
	newRegistry func(feeds.Options) *source.Registry

	// pluginProviders are the feed sources discovered as external plugin binaries
	// (loaded over gRPC via hashicorp/go-plugin) at startup. They are held so each
	// registry rebuild (ApplyAccounts) re-registers the same running plugins
	// instead of respawning their subprocesses. pluginLoader is the discovery seam
	// (default sourceplugin.Load) so tests inject fakes and never spawn processes.
	pluginProviders []source.Provider
	pluginLoader    func(dir string) ([]source.Provider, func() error, error)

	// cookieFinder imports a browser session cookie (Firefox) so the user can sign
	// into a provider in their browser and have the reader pick it up. An interface
	// so tests inject a fake with no real profile on disk.
	cookieFinder cookieImporter

	// openInBrowser launches url in the named browser (default|firefox|chrome|
	// safari|edge) for a provider sign-in flow. The window front-end injects it
	// (via SetURLOpener) with the platform opener, so the app can start a browser
	// without importing the windowapp/exec layer (avoiding an import cycle); it is
	// nil under the CLI front-ends, which never trigger a browser sign-in. A field
	// so tests inject a fake asserting the URL + browser instead of spawning one.
	openInBrowser func(browser, url string) error

	osName string
	dark   bool

	// accent, when hasAccent, overrides the palette's accent with a value
	// harvested from the host UI (macOS controlAccentColor). Re-applied on every
	// theme rebuild so profile/theme switches keep the system accent.
	accent    color.RGBA
	hasAccent bool

	// refresh triggers an asynchronous re-aggregate after a settings change.
	// A field so front-ends/tests can substitute a synchronous variant.
	refresh func()

	// reconstruct triggers an asynchronous Usenet post reconstruction (download
	// the group's parts, reassemble, PAR2 verify/repair, save). A field so
	// front-ends/tests can substitute a synchronous variant.
	reconstruct func(base string)

	// loadGroups triggers an asynchronous fetch of the Usenet server's full group
	// list for the browse window (force bypasses the provider cache). A field so
	// front-ends/tests can substitute a synchronous variant.
	loadGroups func(force bool)

	// previewFetch triggers the asynchronous reconstruct+decode of a Usenet post's
	// image into the preview pane, keyed by id. A field so tests can substitute a
	// synchronous variant.
	previewFetch func(id string, parts []usenet.ReconstructPart)

	// commentFetch triggers the asynchronous fetch of a Reddit post's comment
	// thread into the preview pane, keyed by the item. A field so tests can
	// substitute a synchronous variant. commentCache memoises the flattened
	// comments per item ID (UI-thread only, like the scene) so re-selecting a post
	// does not refetch; it is bounded (commentCacheMax) and simply reset when full.
	commentFetch func(it source.Item)
	commentCache map[string][]source.Comment

	// mediaFetch triggers the asynchronous download+decode of the shown feed's
	// remote thumbnails (every source but Usenet, whose images arrive over NNTP).
	// A field so tests can substitute a synchronous, network-free variant.
	// mediaClient is the browser-fingerprint http.Client it downloads through.
	mediaFetch  func(reqs []ui.MediaRequest)
	mediaClient *http.Client

	// mediaInflight is the set of item IDs whose remote thumbnail is being
	// fetched right now, so the incremental prefetch fired on every streaming
	// update downloads each thumbnail once instead of re-requesting an in-flight
	// one on every update. An ID is released the moment its fetch finishes —
	// success OR failure — so a transient failure is retried on a later prefetch,
	// mirroring the Usenet downloader's inflight map. mediaMu guards it because
	// PrefetchMedia claims from the UI thread while loadMediaThumbs releases from
	// its delivery goroutine.
	mediaInflight map[string]bool
	mediaMu       sync.Mutex

	// mediaCache is the pluggable media-bytes store fetchThumb consults before a
	// download and fills after one. It defaults to the built-in on-disk
	// mediacache.DiskCache; when the CacheBackend setting names a cache-plugin
	// binary it is a gRPC-backed adapter to a shared/network cache instead. A
	// mediacache.Cache so the reader code path is identical either way.
	mediaCache mediacache.Cache
	// mediaCacheClose stops the cache-plugin subprocess (nil for the built-in
	// DiskCache, which has nothing to reap).
	mediaCacheClose func() error

	// webFetch triggers the asynchronous render of a target page (the embedded
	// browser's OnNavigate seam). A field so tests can substitute a synchronous
	// variant. webRender is the page renderer it uses (go-webengine by default; a
	// fake in tests).
	webFetch  func(target string, width int, created int64)
	webRender webrender.Renderer

	// rcache caches final web-preview renders keyed by (url, width) so a page is
	// not re-rendered when it is re-selected or navigated back to. lastRenderWidth
	// records the content width the browser last asked to render at (UI-thread
	// only), so a prefetch warms the same (url, width) the next navigation will
	// request. prefetchFetch triggers a background pre-render (a field so tests use
	// a synchronous variant); prefetchSem bounds it to prefetchWorkers concurrent
	// background renders, and a saturated pool simply skips rather than blocking.
	rcache          *renderCache
	lastRenderWidth int
	prefetchFetch   func(url string, width int)
	prefetchSem     chan struct{}

	// Web-preview open debounce for rapid keyboard navigation: arrowing through
	// the feed selects each item, and opening a browser tab (which renders the
	// page) per item is wasteful. SelectAdjacent arms the open instead of firing
	// it; Frame opens it once the selection has stayed put for webDebounceFrames
	// render ticks. Direct clicks (SelectPreview) still open immediately. All
	// fields are render-thread only (armed/read in SelectAdjacent + Frame, both on
	// the main thread).
	frameCount        uint64
	webArmed          bool
	webArmItem        source.Item
	webArmURL         string
	webArmAt          uint64
	webDebounceFrames uint64

	// groupStatsFetch triggers the asynchronous sampled scan of a newsgroup's
	// content mix (binaries/images) for the browser panel, keyed by group name.
	// A field so tests can substitute a synchronous variant.
	groupStatsFetch func(name string)

	// loadMoreFetch / pullRefreshFetch are the scene's feed-loading seams: the
	// bottom-of-feed next-page trigger and the top overscroll refresh trigger. Both
	// default to launching the corresponding App method on a background goroutine; a
	// field so tests substitute a synchronous variant.
	loadMoreFetch    func()
	pullRefreshFetch func()
	// tabFetch/allFetch back the per-tab lazy model: selecting a sidebar source
	// that has not been fetched yet loads only that source; selecting "All"
	// aggregates on demand. Fields so tests drive them synchronously.
	tabFetch func(source.Subscription)
	allFetch func()

	// searchFetch is the Reddit search view's seam: it runs the query (subreddits or
	// posts) against the Reddit provider off the render thread and delivers results
	// through a.post. It defaults to launching RunRedditSearch on a goroutine; a
	// field so tests substitute a synchronous variant.
	searchFetch func(query string, posts bool)

	// dl is the parallel image download manager (feed-wide thumbnail prefetch);
	// fdl is the parallel file download manager (reconstruct+save checked posts).
	dl  *downloader
	fdl *fileDownloader

	// seen is the persisted per-subscription last-seen marker driving the sidebar
	// unseen/new counts (UI-thread only).
	seen map[string]int

	// Double-buffered present state (see Frame): two framebuffers plus the
	// last-presented damage sequence, so a window/canvas front-end only redraws
	// and uploads when the scene actually changed.
	bufs    [2][]byte
	cur     int
	lastRev int
	// frameAnimOnly records that the last drawn frame's only change was the
	// animation (the spinner), so DamageRects knows a cheap incremental present
	// is worth it; a content/input frame clears it and presents in full.
	frameAnimOnly bool

	// Scene mutations arriving from background goroutines — the streaming
	// aggregation (RefreshStreaming) and the async reconstruct/refresh — must not
	// touch the scene while the render thread reads it in Frame/Draw. When
	// deferScene is set (the native-window front-end enables it via
	// DeferSceneWrites), bindScene enqueues each scene write onto queued instead
	// of applying it inline; Frame drains the queue on the render thread before
	// drawing, so the scene is only ever mutated there and Draw needs no lock.
	// The CLI (PNG/JSON/serve) and the tests leave deferScene false and apply
	// inline, since those paths are single-threaded. deferScene is set once,
	// before any background goroutine starts, then only read; queued is guarded
	// by qmu.
	qmu        sync.Mutex
	queued     []func()
	deferScene bool

	// presentWake/presentBusy let a native front-end's present loop skip the blit
	// while nothing is happening, instead of re-presenting at its tick rate. Both
	// are read from the loop's own goroutine, so both are atomic. presentWake is
	// raised whenever a background goroutine enqueues a scene write (under qmu, so
	// it never races the drain that clears it) and means "a queued mutation is
	// waiting to be drawn". presentBusy is written by Frame on the render thread
	// and means "the last frame is animating or a debounce is pending, so keep
	// ticking". NeedsPresent is their union; see internal/window's gated ticker.
	presentWake     int32
	presentBusy     int32
	presentThrottle int32

	// lastAnim is the wall-clock time the animation clock was last advanced. Frame
	// steps the indeterminate spinner by the real time elapsed since it (see
	// animFrameInterval), so the spinner turns at the same speed no matter how
	// often the present loop actually calls Frame — which lets that loop redraw the
	// spinner at a fraction of its tick rate (presentThrottle) without slowing it
	// down. Zero while not animating; established on the first animating frame.
	// Touched only on the render thread (in Frame), so it needs no lock.
	lastAnim time.Time

	// vmu serializes view-model mutations. mvvm.Observable/ObservableList are
	// intentionally not concurrency-safe, yet several background operations mutate
	// the view-model on their own goroutines — a streaming refresh, an async
	// reconstruction, and the browse group-list load can overlap (e.g. opening the
	// newsgroup browser while a feed refresh is still in flight). Every such
	// mutation runs inside vmDo so the view-model is only ever touched by one
	// goroutine at a time. This is distinct from the scene-write marshaling above:
	// post/queued protect the SCENE; vmu protects the VIEW-MODEL that feeds it.
	vmu sync.Mutex

	// Animated-GIF preview playback. When the web-preview target is an animated
	// GIF, the app decodes every frame and drives them into the embedded browser
	// itself — the page renderer only ever produces one static frame, so a GIF URL
	// navigated to directly would freeze on frame 0. activeGIF holds the frames +
	// per-frame delays of the GIF currently playing (nil when none), guarded by
	// gifMu because it is stored from the fetch goroutine and advanced on the render
	// thread (tickGIF). now is the clock seam (time.Now by default) so frame-timing
	// tests are deterministic; gifFetch downloads the GIF bytes through the media
	// client (a seam so tests inject canned bytes without touching the network).
	gifMu     sync.Mutex
	activeGIF *previewGIF
	now       func() time.Time
	gifFetch  func(ctx context.Context, url string, created int64) ([]byte, error)

	// moreMu guards the pagination state used by infinite scroll: cursors maps each
	// subscription's [source.SubKey] to its next-page token (populated by
	// RefreshStreaming, advanced by LoadMore, empty/absent = exhausted), and
	// loadingMore is the in-flight guard that keeps overlapping at-bottom scroll
	// events from firing concurrent AggregateMore calls. Held only briefly (snapshot
	// / merge / flag) and never across a network fetch or a vmDo, so it cannot
	// deadlock against vmu.
	moreMu      sync.Mutex
	cursors     map[string]string
	loadingMore bool
}

// vmDo runs a view-model mutation under the VM lock. Subscriber callbacks fired
// during fn only enqueue scene writes through post (guarded by qmu, a different
// lock, and never touching the view-model), so this cannot deadlock.
func (a *App) vmDo(fn func()) {
	a.vmu.Lock()
	defer a.vmu.Unlock()
	fn()
}

// vmStatus / vmLoad are single-mutation shortcuts over vmDo for the scattered
// status/progress updates in the reconstruct and group-load goroutines. Do NOT
// use them inside a vmDo block — sync.Mutex is not reentrant.
func (a *App) vmStatus(s string)                 { a.vmDo(func() { a.vm.SetStatus(s) }) }
func (a *App) vmLoad(active bool, done, tot int) { a.vmDo(func() { a.vm.SetLoad(active, done, tot) }) }

// Config configures a new App.
type Config struct {
	Registry *source.Registry
	// Settings supplies the profiles/theme/cache. When nil, a single "Home"
	// profile is synthesized from Subscriptions (the CLI PNG/JSON/serve paths).
	Settings *settings.Settings
	// Store persists settings edits/profile switches. Optional (nil = no disk).
	Store *settings.Store
	// Recorder, when set, feeds the scene's Network-log view. It should be the
	// same recorder the provider registry logs into.
	Recorder *httplog.Recorder
	// Subscriptions seeds the synthesized profile when Settings is nil.
	Subscriptions []source.Subscription
	// Options is the base (flag-derived) feeds configuration. The window path
	// rebuilds the registry from Options + Settings.Accounts + Recorder whenever
	// accounts change; the CLI paths leave it zero and pass a prebuilt Registry.
	Options       feeds.Options
	Width, Height int
	OS            string // ui.OSMac | ui.OSLinux | ui.OSWindows
	Dark          bool
}

// New builds an App with a scene themed and populated from the settings.
func New(cfg Config) *App {
	w, h := cfg.Width, cfg.Height
	if w == 0 {
		w = 1000
	}
	if h == 0 {
		h = 700
	}
	set := cfg.Settings
	if set == nil {
		set = &settings.Settings{
			Profiles: []settings.Profile{{Name: "Home", Subs: cfg.Subscriptions}},
			Active:   0,
			Theme:    settings.ThemeSystem,
		}
	}
	set.Normalize()

	scene := ui.New(w, h, ui.ResolveTheme(set.Theme, cfg.OS, cfg.Dark))
	scene.SetThemeName(set.Theme)
	scene.SetSignInBrowser(set.SignInBrowser)
	scene.SetCachePath(set.CachePath)
	scene.SetMediaCacheMB(set.MediaCacheMB)
	scene.SetCacheBackend(set.CacheBackend)
	scene.SetProfiles(set.Profiles, set.Active)
	scene.SetSidebarFolders(set.SidebarFolders)
	scene.SetAccounts(set.Accounts)
	if rec := cfg.Recorder; rec != nil {
		// Feed the Network-log view live, converting httplog entries into the
		// ui-local shape so the ui package need not depend on internal/httplog.
		scene.SetLogSource(func() []ui.LogEntry {
			snap := rec.Snapshot()
			out := make([]ui.LogEntry, len(snap))
			for i, e := range snap {
				out[i] = ui.LogEntry{
					When: e.When, Method: e.Method, URL: e.URL,
					Status: e.Status, Bytes: e.Bytes, Dur: e.Dur, Err: e.Err,
				}
			}
			return out
		})
	}

	a := &App{
		reg: cfg.Registry, store: cfg.Store, set: set,
		subs: set.ActiveProfile().Subs, scene: scene,
		recorder: cfg.Recorder, baseOpts: cfg.Options, newRegistry: feeds.Registry,
		pluginLoader: sourceplugin.Load,
		cookieFinder: browsercookies.New(),
		osName:       cfg.OS, dark: cfg.Dark, lastRev: -1,
		webDebounceFrames: 9, // ~150ms at 60fps: fetch only after arrowing settles
	}
	// The feed CardList drives the preview through this hook: a card selection
	// (click or keyboard cursor move) previews the post — a Usenet multipart post
	// reconstructs its image, everything else takes the standalone-item path. The
	// keyboard flag debounces the web-page render for held-arrow navigation.
	scene.SetFeedSelectHook(func(it source.Item, viaKeyboard bool) {
		if it.Source == source.Usenet && a.previewGroupIfShown(it.ID) {
			return
		}
		a.selectPreview(it, viaKeyboard)
	})
	a.refresh = func() { go a.Refresh(context.Background()) }
	a.reconstruct = func(base string) { go a.doReconstruct(context.Background(), base) }
	a.loadGroups = func(force bool) { go a.doLoadGroups(context.Background(), force) }
	a.previewFetch = func(id string, parts []usenet.ReconstructPart) {
		go a.loadPreviewImage(context.Background(), id, parts)
	}
	a.commentCache = map[string][]source.Comment{}
	a.commentFetch = func(it source.Item) {
		go a.loadComments(context.Background(), it)
	}
	a.mediaClient = feeds.MediaClient(cfg.Recorder)
	a.mediaFetch = func(reqs []ui.MediaRequest) {
		go a.loadMediaThumbs(context.Background(), reqs)
	}
	a.now = time.Now
	a.gifFetch = func(ctx context.Context, url string, created int64) ([]byte, error) {
		return a.fetchGIFBytes(ctx, url, created)
	}
	a.webRender = webrender.New()
	a.rcache = newRenderCache(renderCacheMaxBytes)
	a.webFetch = func(target string, width int, created int64) {
		go a.loadPreviewPage(context.Background(), target, width, created)
	}
	a.prefetchSem = make(chan struct{}, prefetchWorkers)
	a.prefetchFetch = func(url string, width int) {
		// Bounded, non-blocking: acquire a worker slot or skip — a prefetch must
		// never stall the UI or queue unbounded work.
		select {
		case a.prefetchSem <- struct{}{}:
		default:
			return
		}
		go func() {
			defer func() { <-a.prefetchSem }()
			a.doPrefetch(context.Background(), url, width)
		}()
	}
	// The embedded browser's navigation seam: a tab open / link click / typed
	// address / Back / Forward / Reload asks the host to render a page; run it
	// through the (test-substitutable) webFetch. Record the width so a prefetch
	// warms the same content width the next navigation will request.
	a.scene.Browser().OnNavigate = func(target string, width int) {
		a.lastRenderWidth = width
		a.scene.SyncBookmarkStar() // reflect whether the just-navigated page is bookmarked
		// Capture the previewed post's creation time on this (UI) thread and thread
		// it through the render, so a GIF the page turns out to be is cached with its
		// mtime stamped to the post's date rather than the download moment.
		a.webFetch(target, width, a.currentPostCreated())
	}
	// The Settings view shows the live render-cache size.
	a.scene.SetRenderCacheStats(a.RenderCacheStats)
	// The address-bar star toggles the current page's bookmark + persists it. The
	// bookmark state is a shared mvvm.Observable now, so subscribe once (in the
	// constructor) instead of assigning a callback. The guard skips the
	// programmatic store→star sync (SyncBookmarkStar) so only a real user toggle
	// (star diverging from the store) persists — matching the prior behaviour.
	a.scene.Browser().Bookmarked().Subscribe(func(on bool) {
		url := a.scene.Browser().CurrentURL()
		if on == a.scene.IsBookmarked(url) {
			return
		}
		a.scene.SetBookmarked(url, on)
		a.persistSettings()
	})
	a.scene.SetBrowserSingleTab(set.SingleTab())                           // apply the persisted tab-mode preference (default single-tab)
	a.scene.SetBrowserChromeHidden(set.HideBrowserChrome)                  // apply the persisted toolbar-visibility preference
	a.scene.SetBrowserZoomKeys(set.ZoomInKey, set.ZoomOutKey)              // apply the persisted browser zoom keybindings
	a.scene.SetBookmarks(set.Bookmarks)                                    // apply the persisted bookmarks
	a.scene.SetInfiniteScroll(set.InfiniteScrollEnabled())                 // apply the persisted infinite-scroll preference (default on)
	a.scene.SetDismissPreviewOnSwitch(set.DismissPreviewOnSwitchEnabled()) // apply the persisted dismiss-preview-on-switch preference (default on)
	a.scene.SetBiometricUnlock(set.BiometricUnlockEnabled())               // reflect the effective biometric-unlock preference (default ON) in the toggle
	settings.SetSecretUserPresence(false)                                  // store secrets UNGATED: the SecAccessControl user-presence flag needs a keychain entitlement AMFI rejects on a self-signed build, so biometric unlock is enforced by the LAContext gate in ActivateAfterVault, not the keychain
	a.groupStatsFetch = func(name string) {
		go a.loadGroupStats(context.Background(), name)
	}
	// Feed-loading seams: the scene fires OnReachBottom / OnPullRefresh from the
	// wheel path; run the fetches off the render thread (a field so tests drive them
	// synchronously).
	a.loadMoreFetch = func() { go a.LoadMore(context.Background()) }
	a.pullRefreshFetch = func() { go a.PullRefresh(context.Background()) }
	a.tabFetch = func(sub source.Subscription) { go a.refreshOneSub(context.Background(), sub) }
	a.allFetch = func() { go a.RefreshStreaming(context.Background()) }
	a.scene.OnReachBottom = func() { a.loadMoreFetch() }
	a.scene.OnPullRefresh = func() { a.pullRefreshFetch() }
	// The Reddit search view runs its query off the render thread; a field so tests
	// drive it synchronously.
	a.searchFetch = func(query string, posts bool) {
		go a.RunSearchFetch(context.Background(), query, posts)
	}
	a.dl = newDownloader(a)
	a.fdl = newFileDownloader(a)
	a.seen = loadSeen()
	a.scene.SetSeen(a.seen) // per-group unseen counts survive restarts
	a.applyUsenetServer()

	// The view-model is the single source of truth for the core state flow; the
	// commands' actions late-bind through a's fields (so a test's SetRefreshHook
	// still applies). bindScene routes every observable change into the scene.
	a.vm = viewmodel.New(viewmodel.Actions{
		Refresh:       func() { a.refresh() },
		ToggleSidebar: func() { a.scene.ToggleSidebar() },
	})
	a.vm.Profile.Set(set.Active) // seed the active-profile observable
	bindScene(a)

	// Select the media-cache backend (built-in disk or a gRPC cache plugin). Done
	// after the view-model exists so a plugin load failure can surface a status.
	a.initMediaCache(set)

	// Discover external feed-source plugins and register them; a missing plugins
	// directory is a no-op, so a fresh install with no plugins is unaffected.
	a.loadPlugins()
	a.syncFollowImportKinds() // seed the accounts-editor import affordance from the initial registry
	return a
}

// loadPlugins discovers external feed-source plugin binaries in the settings'
// plugins directory and registers each into the current registry. The
// discovered providers are retained so a later registry rebuild (ApplyAccounts)
// re-registers the same running plugins rather than respawning them. A directory
// that cannot be read leaves the reader running with only its built-in and
// already-loaded sources. The plugin subprocesses live for the reader's session
// and are reaped by go-plugin when the reader exits, so no close hook is held.
func (a *App) loadPlugins() {
	provs, _, err := a.pluginLoader(a.set.PluginsDirOrDefault())
	if err != nil {
		return
	}
	a.pluginProviders = provs
	a.registerPlugins()
}

// registerPlugins registers every discovered plugin provider into the current
// registry (a no-op before a registry exists, or when no plugins were found).
// Plugins register after the built-ins, so a plugin may override a built-in of
// the same kind.
func (a *App) registerPlugins() {
	if a.reg == nil {
		return
	}
	for _, p := range a.pluginProviders {
		a.reg.Register(p)
	}
}

// DeferSceneWrites routes view-model→scene mutations through a render-thread
// queue instead of applying them inline. The native-window front-end enables it
// so the background streaming-aggregation goroutine never mutates the scene the
// render thread is drawing (Frame drains the queue on the render thread). It
// must be called before the present loop and the aggregation goroutine start,
// so deferScene is set before any concurrent reader exists.
func (a *App) DeferSceneWrites() { a.deferScene = true }

// post applies a scene mutation. In deferred mode (the window front-end) it
// enqueues fn to run on the render thread at the next Frame; otherwise it runs
// fn inline, matching the single-threaded CLI/test paths.
func (a *App) post(fn func()) {
	if !a.deferScene {
		fn()
		return
	}
	a.qmu.Lock()
	a.queued = append(a.queued, fn)
	atomic.StoreInt32(&a.presentWake, 1)
	a.qmu.Unlock()
}

// drainScene applies every queued scene mutation. Frame calls it on the render
// thread before drawing, so the scene is mutated only there. It is a no-op (an
// empty queue) on the inline CLI/test paths.
func (a *App) drainScene() {
	a.qmu.Lock()
	q := a.queued
	a.queued = nil
	// Cleared under the same lock post raises it under: a write enqueued after
	// this point lands in the new queue and re-raises the flag, so the wake is
	// never lost and never spuriously stuck.
	atomic.StoreInt32(&a.presentWake, 0)
	a.qmu.Unlock()
	for _, fn := range q {
		fn()
	}
}

// VM exposes the view-model so front-ends can execute its commands and read its
// observables. The core state flow (items, loading, search, mode, status, auth
// prompts) is driven through it and reflected onto the scene by bindScene.
func (a *App) VM() *viewmodel.ViewModel { return a.vm }

// SetRefreshHook overrides the asynchronous re-aggregate trigger (tests use a
// synchronous variant for determinism).
func (a *App) SetRefreshHook(f func()) { a.refresh = f }

// SetReconstructHook overrides the asynchronous reconstruction trigger (tests
// use a synchronous variant for determinism).
func (a *App) SetReconstructHook(f func(base string)) { a.reconstruct = f }

// SetLoadMoreHook overrides the infinite-scroll (OnReachBottom) trigger's async
// launch (tests use a synchronous variant for determinism).
func (a *App) SetLoadMoreHook(f func()) { a.loadMoreFetch = f }

// SetPullRefreshHook overrides the pull-to-refresh (OnPullRefresh) trigger's async
// launch (tests use a synchronous variant for determinism).
func (a *App) SetPullRefreshHook(f func()) { a.pullRefreshFetch = f }

// SetRegistryBuilder overrides how ApplyAccounts rebuilds the provider registry
// (a seam so tests inject a fake registry instead of real providers).
func (a *App) SetRegistryBuilder(f func(feeds.Options) *source.Registry) { a.newRegistry = f }

// PersistSettings snapshots the scene's settings and writes them through the
// store, WITHOUT re-aggregating the feed. It is the persistence path for
// pure-display preference changes such as editing the sidebar's virtual folders
// (which regroup the sidebar but never change what the feed fetches).
func (a *App) PersistSettings() { a.persistSettings() }

// persistSettings snapshots the scene's settings and writes them through the
// store (a no-op when persistence is disabled, e.g. the CLI paths). Used for
// live preference changes such as toggling a bookmark.
func (a *App) persistSettings() {
	if a.store == nil {
		return
	}
	a.set = a.scene.Settings()
	_ = a.store.Save(a.set)
}

// PreviewTextStep is how much one A−/A+ preview-toolbar tap changes the
// reader/preview text zoom.
const PreviewTextStep = 0.1

// AdjustPreviewTextScale nudges the reader/preview text zoom by delta (the A−/A+
// preview-toolbar pills pass ±PreviewTextStep). The scene clamps it to the
// supported range; the change persists straight away (a pure-display preference,
// so no theme re-resolve or feed re-aggregation), and the new size is reported on
// the status line.
func (a *App) AdjustPreviewTextScale(delta float64) {
	a.scene.SetPreviewTextScale(a.scene.PreviewTextScale() + delta)
	a.persistSettings()
	a.vm.SetStatus(fmt.Sprintf("Reader text size %d%%", int(a.scene.PreviewTextScale()*100+0.5)))
}

// ApplyAccounts snapshots the scene's edited accounts into the settings,
// persists them, rebuilds the provider registry with the new credentials
// (Reddit switches to session-cookie authentication) while keeping the same HTTP
// recorder wired so the Network log keeps updating, then re-aggregates the feed. The
// front-end calls it after the accounts editor is committed.
func (a *App) ApplyAccounts() {
	a.set = a.scene.Settings() // Settings() now carries the edited Accounts
	if a.store != nil {
		_ = a.store.Save(a.set)
	}
	a.rebuildRegistry()
	a.refresh()
}

// cookieImporter reads the logged-in session cookies of the supported providers
// from a local browser (Firefox). The app depends on this narrow interface so
// tests can substitute a fake without a real browser profile.
type cookieImporter interface {
	RedditSession() (browsercookies.RedditSession, error)
	InstagramSession() (string, error)
	TikTokSession() (string, error)
	TwitterSession() (string, error)
}

// SetCookieFinder overrides the browser-cookie importer (tests inject a fake).
func (a *App) SetCookieFinder(f cookieImporter) { a.cookieFinder = f }

// SetURLOpener installs the browser opener used by browser sign-in flows: f
// launches a URL in the named browser (default|firefox|chrome|safari|edge). The
// window front-end wires it to the platform opener; tests inject a fake.
func (a *App) SetURLOpener(f func(browser, url string) error) { a.openInBrowser = f }

// errNoBrowserOpener is returned by [App.LaunchRedditSignIn] when no browser
// opener has been installed (e.g. a CLI front-end that never signs in).
var errNoBrowserOpener = errors.New("no browser opener configured")

// redditLoginURL is the page the Reddit browser sign-in flow opens.
const redditLoginURL = "https://www.reddit.com/login/"

// LaunchRedditSignIn opens Reddit's login page in the browser configured by the
// SignInBrowser setting, so the user can sign in there and (with Firefox) import
// the resulting session cookie. It surfaces progress/failure on the status line.
func (a *App) LaunchRedditSignIn() error {
	browser := a.scene.SignInBrowser()
	if a.openInBrowser == nil {
		a.vm.SetStatus("Cannot open a browser to sign in to Reddit")
		return errNoBrowserOpener
	}
	if err := a.openInBrowser(browser, redditLoginURL); err != nil {
		a.vm.SetStatus("Reddit sign-in failed: " + err.Error())
		return err
	}
	a.vm.SetStatus("Opening Reddit sign-in in " + signInBrowserLabel(browser))
	return nil
}

// signInBrowserLabel is the human-readable name of a sign-in browser value.
func signInBrowserLabel(browser string) string {
	switch browser {
	case settings.SignInBrowserFirefox:
		return "Firefox"
	case settings.SignInBrowserChrome:
		return "Chrome"
	case settings.SignInBrowserSafari:
		return "Safari"
	case settings.SignInBrowserEdge:
		return "Edge"
	default:
		return "your default browser"
	}
}

// ImportRedditSessionFromFirefox lifts the user's logged-in reddit_session
// cookie out of their Firefox profile and installs it as the Reddit account's
// session cookie: it writes the value into the accounts editor buffer (so the
// editor shows it), then commits through ApplyAccounts — persisting the account,
// rebuilding the provider registry so authenticated reads take effect
// immediately, and re-aggregating. It reports whether a cookie was imported and
// surfaces success/failure on the status line. A failure (Firefox not installed,
// no profile, or not logged into Reddit) leaves the existing credentials
// untouched.
func (a *App) ImportRedditSessionFromFirefox() (bool, error) {
	rs, err := a.cookieFinder.RedditSession()
	if err != nil {
		msg := "Reddit import failed: " + importFailureHint(err)
		// Cookie import can only read Firefox's plaintext cookies. If the user set a
		// non-Firefox sign-in browser, signing in there leaves nothing importable, so
		// spell that out rather than letting the failure look like a bug.
		if a.scene.SignInBrowser() != settings.SignInBrowserFirefox {
			msg += " — cookie import currently requires Firefox"
		}
		a.vm.SetStatus(msg)
		return false, err
	}
	a.scene.SetAccountField(source.Reddit, "session_cookie", rs.Value)
	a.ApplyAccounts() // persist + rebuild registry (Reddit→authenticated) + re-aggregate
	a.vm.SetStatus("Imported Reddit session from Firefox")
	return true, nil
}

// ImportSessionFromFirefox lifts the logged-in session cookie of the account
// currently selected in the editor (Instagram, TikTok or X) out of the user's
// Firefox profile and installs it as that provider's "session" field, then
// commits through ApplyAccounts so the authenticated home/following timeline
// takes effect immediately. Reddit uses its own richer flow
// (ImportRedditSessionFromFirefox); an unsupported selection is a no-op. It
// reports whether a cookie was imported and surfaces success/failure on the
// status line.
func (a *App) ImportSessionFromFirefox() (bool, error) {
	kind := a.scene.SelectedAccount()
	value, label, err := a.importSessionFor(kind)
	if err != nil {
		msg := label + " import failed: " + sessionImportHint(err)
		if a.scene.SignInBrowser() != settings.SignInBrowserFirefox {
			msg += " — cookie import currently requires Firefox"
		}
		a.vm.SetStatus(msg)
		return false, err
	}
	// TikTok's client needs the raw sessionid and msToken as SEPARATE fields (the
	// msToken is also appended to the signed request as a query param), so split
	// the "sessionid=…; msToken=…" cookie string into the account's `session` and
	// `ms_token` fields rather than storing the whole string in one. Other
	// providers consume the cookie string as-is.
	if kind == source.TikTok {
		a.scene.SetAccountField(kind, "session", cookieValue(value, "sessionid"))
		a.scene.SetAccountField(kind, "ms_token", cookieValue(value, "msToken"))
	} else {
		a.scene.SetAccountField(kind, "session", value)
	}
	a.ApplyAccounts() // persist + rebuild registry (provider→authenticated) + re-aggregate
	a.vm.SetStatus("Imported " + label + " session from Firefox")
	return true, nil
}

// sessionImportKinds are the providers whose per-account feeds a logged-in
// browser session unlocks, in the order AutoImportSessions fills them.
var sessionImportKinds = []source.Kind{source.Twitter, source.Instagram, source.TikTok}

// AutoImportSessions imports a browser session for every session-based source the
// active profile subscribes to but has no account for yet, so a followed X /
// Instagram / TikTok account works on launch without a manual Accounts → Import
// step. The window front-end calls it once at startup (before the lazy first
// load), gated by the AutoImportSessions setting.
//
// It only ever FILLS a missing account — it never touches a configured one, so a
// working session already in the vault is left alone — and silently skips a
// provider whose session the browser does not hold. It persists straight to the
// vault and rebuilds the registry (so the authenticated providers are live for the
// first load) WITHOUT triggering a refresh, leaving the front-end's lazy per-tab
// load to fetch; it reports what it imported on the status line.
// biometricAuth gates the vault read behind the platform's owner check (Touch
// ID, or the device password). A package-var seam so app tests substitute it
// without triggering a real prompt.
var biometricAuth = biometric.Authenticate

// ActivateAfterVault performs the deferred startup that reads the vault, but
// keeps the keychain prompt OFF the render thread so the window does not freeze
// behind it. The blocking keychain read runs on a goroutine; the state changes
// it enables — reactivating the authenticated providers, importing any missing
// browser session — are applied on the render thread via post (like every other
// background result), and onLoaded (also on the render thread) starts the feed
// load once the providers are live.
//
// When biometric unlock is enabled, the vault read is gated by a
// LocalAuthentication (Touch ID / device password) prompt run on the same
// background goroutine. A denial leaves the accounts unauthenticated rather than
// failing the launch — the reader still opens, just without the sign-ins.
func (a *App) ActivateAfterVault(onLoaded func()) {
	go func() {
		if a.store != nil {
			// Touch ID gates the read only once the vault has been unlocked once
			// under this app (BiometricPrimed): that first unlock goes through the
			// keychain's own grant (a password / "Always Allow"), after which the
			// read is silent — so from then on Touch ID is the ONLY prompt.
			// Prompting Touch ID before that grant would just stack on top of the
			// password one.
			if a.set.BiometricUnlockEnabled() && a.set.BiometricPrimed {
				// A denial leaves the accounts unauthenticated rather than failing
				// the launch — the reader still opens, just without the sign-ins.
				if biometricAuth("Unlock your saved sign-ins") != nil {
					a.post(func() {
						if onLoaded != nil {
							onLoaded()
						}
					})
					return
				}
			}
			// The keychain access happens here, on a background goroutine, so it
			// never blocks the render thread.
			_ = a.store.HydrateSecrets(a.set)
			// First successful unlock under this app: record it so the NEXT launch
			// gates with Touch ID (the keychain grant is now in place). Save also
			// persists the flag.
			if a.set.BiometricUnlockEnabled() && !a.set.BiometricPrimed && a.set.HasStoredSecret() {
				a.set.BiometricPrimed = true
				_ = a.store.Save(a.set)
			}
		}
		a.post(func() {
			a.rebuildRegistry()    // stored vault accounts are now authenticated
			a.AutoImportSessions() // fill any still-missing session from the browser
			if onLoaded != nil {
				onLoaded()
			}
		})
	}()
}

func (a *App) AutoImportSessions() {
	// a.set and a.scene are always set (New builds a default when Config.Settings is
	// nil), so only the preference is checked here.
	if !a.set.AutoImportSessionsEnabled() {
		return
	}
	// Seed the editor buffer from the persisted (vault-hydrated) accounts so the
	// SetAccountField writes below ADD to them: EditedAccounts projects the buffer
	// back, and an unseeded buffer would drop every other configured account.
	a.scene.SetAccounts(a.set.Accounts)
	var imported []string
	for _, kind := range sessionImportKinds {
		if !a.profileSubscribesTo(kind) || a.hasConfiguredAccount(kind) {
			continue
		}
		value, label, err := a.importSessionFor(kind)
		if err != nil || value == "" {
			continue
		}
		if kind == source.TikTok {
			// TikTok's client wants the sessionid and msToken as separate fields.
			a.scene.SetAccountField(kind, "session", cookieValue(value, "sessionid"))
			a.scene.SetAccountField(kind, "ms_token", cookieValue(value, "msToken"))
		} else {
			a.scene.SetAccountField(kind, "session", value)
		}
		imported = append(imported, label)
	}
	if len(imported) == 0 {
		return
	}
	a.set = a.scene.Settings() // now carries the newly filled accounts
	if a.store != nil {
		_ = a.store.Save(a.set) // pushes the session secrets into the vault
	}
	a.rebuildRegistry() // providers rebuilt authenticated; no refresh — the lazy load follows
	a.vm.SetStatus("Imported your " + strings.Join(imported, ", ") + " session from the browser")
}

// profileSubscribesTo reports whether the active profile has any subscription for
// kind. Normalize (run by New) guarantees Active indexes a real profile.
func (a *App) profileSubscribesTo(kind source.Kind) bool {
	for _, sub := range a.set.Profiles[a.set.Active].Subs {
		if sub.Source == kind {
			return true
		}
	}
	return false
}

// hasConfiguredAccount reports whether an account of kind is already stored.
func (a *App) hasConfiguredAccount(kind source.Kind) bool {
	for _, acc := range a.set.Accounts {
		if acc.Kind == kind {
			return true
		}
	}
	return false
}

// cookieValue extracts the value of the named cookie from a "k=v; k=v" cookie
// string (as produced by the browser-cookie importers), or "" if it is absent.
func cookieValue(cookies, name string) string {
	for _, part := range strings.Split(cookies, ";") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) == 2 && kv[0] == name {
			return kv[1]
		}
	}
	return ""
}

// errUnsupportedSessionImport is returned by [App.ImportSessionFromFirefox] when
// the selected provider has no browser-session import (e.g. Reddit, which has its
// own flow, or a provider without a session cookie).
var errUnsupportedSessionImport = errors.New("no session import for this provider")

// importSessionFor imports the selected provider's session cookie string from
// Firefox and returns it with the provider's display label.
func (a *App) importSessionFor(kind source.Kind) (value, label string, err error) {
	switch kind {
	case source.Instagram:
		value, err = a.cookieFinder.InstagramSession()
		return value, "Instagram", err
	case source.TikTok:
		value, err = a.cookieFinder.TikTokSession()
		return value, "TikTok", err
	case source.Twitter:
		value, err = a.cookieFinder.TwitterSession()
		return value, "X", err
	default:
		return "", string(kind), errUnsupportedSessionImport
	}
}

// redditSubImporter is the narrow slice of the Reddit provider that lists the
// authenticated account's subreddit subscriptions.
type redditSubImporter interface {
	MySubscriptions(ctx context.Context) ([]string, error)
}

// errNoRedditProvider is returned by [App.ImportRedditSubscriptions] when no
// Reddit provider capable of listing subscriptions is registered (e.g. no
// account is connected yet).
var errNoRedditProvider = errors.New("no reddit subscription importer available")

// ImportRedditSubscriptions pulls every subreddit the connected Reddit account
// follows and adds any not-yet-subscribed ones as r/<name> subscriptions on the
// active profile, then persists, re-aggregates, and reports on the status line.
// It requires a connected (session-cookie) Reddit provider; without one it
// prompts the user to connect an account. It runs the network call on the
// caller's goroutine (the front-end launches it off the render thread) and
// applies the scene mutations through a.post so they land on the render thread.
// The returned count is meaningful only on the inline (CLI/test) path.
func (a *App) ImportRedditSubscriptions(ctx context.Context) (int, error) {
	prov, ok := a.reg.Get(source.Reddit)
	imp, isImp := prov.(redditSubImporter)
	if !ok || !isImp {
		a.post(func() {
			a.vm.SetStatus("Connect a Reddit account first to import subscriptions")
		})
		return 0, errNoRedditProvider
	}
	channels, err := imp.MySubscriptions(ctx)
	if err != nil {
		a.post(func() { a.vm.SetStatus("Reddit import failed: " + err.Error()) })
		return 0, err
	}
	// apply adds the new channels, persists+re-aggregates once, and reports the
	// count. It must run on the render thread, so it is routed through a.post.
	var added int
	apply := func() {
		if len(channels) == 0 {
			a.vm.SetStatus("No Reddit subscriptions found")
			return
		}
		n := 0
		for _, ch := range channels {
			if a.scene.SubscribeActive(source.Reddit, ch) {
				n++
			}
		}
		// ApplySceneSettings persists, re-syncs a.subs, and re-aggregates in one
		// shot — do not also call a.refresh separately.
		a.ApplySceneSettings()
		a.vm.SetStatus(fmt.Sprintf("Imported %d new subreddit(s) from Reddit", n))
		added = n
	}
	// In deferred mode apply runs later on the render thread, so `added` would be
	// written there — do not read it back here (the caller ignores it). On the
	// inline (CLI/test) path a.post runs apply synchronously, so `added` is set
	// before we return the real count.
	if a.deferScene {
		a.post(apply)
		return 0, nil
	}
	apply()
	return added, nil
}

// errNoFollowImporter is returned by [App.ImportFollows] when the provider for
// the requested kind is not registered or does not implement
// [source.FollowImporter] (e.g. no account is connected yet).
var errNoFollowImporter = errors.New("no follow importer available for this source")

// followKindLabel is the human-readable name of a source kind (from the
// credential schema), used in the import status messages. It falls back to the
// raw kind for a source that has no credential schema entry.
func followKindLabel(kind source.Kind) string {
	for _, pc := range settings.CredentialSchema() {
		if pc.Kind == kind {
			return pc.Label
		}
	}
	return string(kind)
}

// ImportFollows pulls every account/subreddit/feed the connected provider for
// kind follows — via its [source.FollowImporter] capability — and adds any
// not-yet-subscribed ones to the active profile, then persists, re-aggregates,
// and reports on the status line. Each returned [source.Subscription] carries
// its own source+channel, so a provider that follows several channel forms
// (Reddit's r/ subreddits and u/ redditors) imports them all in one action. It
// requires a connected/authenticated provider; without one it prompts the user
// to connect an account. The network call runs on the caller's goroutine (the
// front-end launches it off the render thread) and the scene mutations are
// applied through a.post so they land on the render thread. The returned count
// is meaningful only on the inline (CLI/test) path.
func (a *App) ImportFollows(ctx context.Context, kind source.Kind) (int, error) {
	prov, ok := a.reg.Get(kind)
	imp, isImp := prov.(source.FollowImporter)
	if !ok || !isImp {
		a.post(func() {
			a.vm.SetStatus("Connect a " + followKindLabel(kind) + " account first to import subscriptions")
		})
		return 0, errNoFollowImporter
	}
	subs, err := imp.MyFollows(ctx)
	if err != nil {
		a.post(func() { a.vm.SetStatus(followKindLabel(kind) + " import failed: " + err.Error()) })
		return 0, err
	}
	// apply adds the new subscriptions, persists+re-aggregates once, and reports
	// the count. It must run on the render thread, so it is routed through a.post.
	var added int
	apply := func() {
		if len(subs) == 0 {
			a.vm.SetStatus("No " + followKindLabel(kind) + " subscriptions found")
			return
		}
		n := 0
		for _, sub := range subs {
			if a.scene.SubscribeActive(sub.Source, sub.Channel) {
				n++
			}
		}
		// ApplySceneSettings persists, re-syncs a.subs, and re-aggregates in one
		// shot — do not also call a.refresh separately.
		a.ApplySceneSettings()
		a.vm.SetStatus(fmt.Sprintf("Imported %d new subscription(s) from %s", n, followKindLabel(kind)))
		added = n
	}
	// In deferred mode apply runs later on the render thread, so `added` would be
	// written there — do not read it back here (the caller ignores it). On the
	// inline (CLI/test) path a.post runs apply synchronously, so `added` is set
	// before we return the real count.
	if a.deferScene {
		a.post(apply)
		return 0, nil
	}
	apply()
	return added, nil
}

// sessionImportHint turns a browser-cookie session-import failure into a short,
// provider-neutral status message (used by the Instagram/TikTok/X import).
func sessionImportHint(err error) string {
	switch {
	case errors.Is(err, browsercookies.ErrNoCookie):
		return "not signed in in Firefox"
	case errors.Is(err, browsercookies.ErrNoFirefox):
		return "Firefox not found"
	case errors.Is(err, browsercookies.ErrNoProfile):
		return "no readable Firefox profile"
	default:
		return err.Error()
	}
}

// importFailureHint turns a browser-cookie import error into a short,
// user-actionable status message.
func importFailureHint(err error) string {
	switch {
	case errors.Is(err, browsercookies.ErrNoCookie):
		return "log into Reddit in Firefox first"
	case errors.Is(err, browsercookies.ErrNoFirefox):
		return "Firefox not found"
	case errors.Is(err, browsercookies.ErrNoProfile):
		return "no readable Firefox profile"
	default:
		return err.Error()
	}
}

// rebuildRegistry constructs a fresh registry from the base options overlaid
// with the current accounts and the shared recorder, so authenticated providers
// come up live without a restart.
func (a *App) rebuildRegistry() {
	opts := AccountsToOptions(a.baseOpts, a.set.Accounts)
	opts.Recorder = a.recorder
	a.reg = a.newRegistry(opts)
	a.registerPlugins() // re-register the running plugins into the fresh registry
	a.applyUsenetServer()
	a.syncFollowImportKinds()
}

// syncFollowImportKinds tells the scene which registered providers can import
// the connected account's follows (implement [source.FollowImporter]), so the
// accounts editor offers the "Import my subscriptions" affordance for exactly
// those sources and no others. It is a no-op before a registry exists.
func (a *App) syncFollowImportKinds() {
	if a.reg == nil {
		return
	}
	var kinds []source.Kind
	for _, k := range a.reg.Kinds() {
		if p, ok := a.reg.Get(k); ok {
			if _, isImp := p.(source.FollowImporter); isImp {
				kinds = append(kinds, k)
			}
		}
	}
	a.scene.SetFollowImportKinds(kinds)
}

// applyUsenetServer tells the scene which Usenet server is configured (the base
// options overlaid with the stored account), which gates the sidebar "Browse
// newsgroups" entry and titles the browse view.
func (a *App) applyUsenetServer() {
	addr := AccountsToOptions(a.baseOpts, a.set.Accounts).UsenetAddr
	a.scene.SetUsenetServer(addr)
}

// AccountsToOptions overlays stored per-provider credentials onto base feeds
// options, mapping each account's well-known field keys onto the matching
// Options fields. Absent fields leave the base value untouched, so flags still
// apply where no account overrides them.
func AccountsToOptions(base feeds.Options, accts []settings.Account) feeds.Options {
	out := base
	for _, a := range accts {
		switch a.Kind {
		case source.Reddit:
			setIf(&out.RedditSessionCookie, a.Fields["session_cookie"])
		case source.Mastodon:
			setIf(&out.MastodonInstance, a.Fields["instance"])
			setIf(&out.MastodonToken, a.Fields["token"])
		case source.Lemmy:
			setIf(&out.LemmyInstance, a.Fields["instance"])
		case source.Usenet:
			setIf(&out.UsenetAddr, a.Fields["addr"])
			if v, ok := a.Fields["tls"]; ok {
				out.UsenetTLS = v == "true"
			}
			setIf(&out.UsenetUsername, a.Fields["username"])
			setIf(&out.UsenetPassword, a.Fields["password"])
			setIf(&out.UsenetIndexerURL, a.Fields["indexer_url"])
			setIf(&out.UsenetIndexerAPIKey, a.Fields["indexer_key"])
		case source.Instagram:
			setIf(&out.InstagramSession, a.Fields["session"])
		case source.TikTok:
			setIf(&out.TikTokMSToken, a.Fields["ms_token"])
			setIf(&out.TikTokSession, a.Fields["session"])
		case source.Twitter:
			setIf(&out.TwitterSession, a.Fields["session"])
		}
	}
	return out
}

// setIf copies v into *dst only when v is non-empty.
func setIf(dst *string, v string) {
	if v != "" {
		*dst = v
	}
}

// SelectProfile switches the active profile: it updates the scene (which
// re-derives the sidebar subscriptions), keeps the view-model's Profile
// observable in step, and re-aggregates the newly-active profile's feed. The
// window/CLI front-ends route a profile-tab click here instead of poking the
// scene, so the active profile flows through the app rather than the view.
func (a *App) SelectProfile(i int) {
	a.scene.SetActiveProfile(i)
	a.vm.SelectProfile(i)
	a.ApplySceneSettings()
}

// DeleteProfile removes the profile at i from the settings editor, re-syncs the
// active-profile observable with the scene's re-clamped selection, and persists +
// re-aggregates. Front-ends route the settings editor's delete affordance here.
func (a *App) DeleteProfile(i int) {
	a.scene.DeleteProfile(i)
	a.vm.SelectProfile(a.scene.ActiveProfileIndex())
	a.ApplySceneSettings()
}

// ApplySceneSettings snapshots the scene's edited settings, persists them (when
// a store is configured), reselects the active profile's subscriptions and
// theme, and triggers a re-aggregate. Front-ends call it after any profile
// switch or settings-view edit.
// SetBiometricUnlock toggles biometric unlock: it records the preference on the
// scene, then persists and re-applies. Secrets are always stored UNGATED (the
// SecAccessControl user-presence flag needs a keychain entitlement AMFI rejects
// on a self-signed build); the preference instead gates the vault READ behind
// the LAContext prompt in ActivateAfterVault, so the change takes effect on the
// next launch's read.
func (a *App) SetBiometricUnlock(v bool) {
	a.scene.SetBiometricUnlock(v)
	settings.SetSecretUserPresence(false) // keep storage ungated regardless; the LAContext read-gate is the biometric now
	a.ApplySceneSettings()
}

func (a *App) ApplySceneSettings() {
	set := a.scene.Settings()
	a.set = set
	a.applyMediaCacheBudget(set)
	if a.store != nil {
		_ = a.store.Save(set)
	}
	a.subs = set.ActiveProfile().Subs
	a.applyTheme()
	a.refresh()
}

// applyTheme resolves the palette from the persisted theme name, the host OS and
// the current dark preference, then layers the harvested system accent on top so
// it survives every profile/theme switch. All theme changes funnel through here.
func (a *App) applyTheme() {
	t := ui.ResolveTheme(a.scene.ThemeName(), a.osName, a.dark)
	if a.hasAccent {
		t = ui.WithAccent(t, a.accent.R, a.accent.G, a.accent.B)
	}
	a.scene.SetTheme(t)
	// Frame web-rendered previews (a Reddit image/link post) on the app's surface
	// colour, so a background-less page or a bare image matches the theme instead
	// of flashing white in dark mode. Only the go-webengine renderer supports it.
	if br, ok := a.webRender.(interface{ SetBackdrop(color.RGBA) }); ok {
		br.SetBackdrop(color.RGBA{R: t.Surface.R, G: t.Surface.G, B: t.Surface.B, A: 0xFF})
	}
}

// SetSystemAppearance applies look-and-feel harvested from the host UI: the
// effective dark/light mode (honoured only when the user's theme is "system"),
// the accent colour, and — when fontTTF parses — the system font. Called by the
// native window back-end at launch and whenever the system appearance changes.
func (a *App) SetSystemAppearance(dark bool, accent color.RGBA, hasAccent bool, fontTTF []byte) {
	a.dark = dark
	a.accent, a.hasAccent = accent, hasAccent
	if len(fontTTF) > 0 {
		ui.SetSystemFont(fontTTF)
	}
	a.applyTheme()
}

// animFrameInterval is one animation frame of real time. The indeterminate
// spinner advances one frame per interval (spinnerPeriod frames per revolution),
// so stepping it by the whole-number of intervals elapsed since the last advance
// turns it at the same speed whether the present loop calls Frame 60 or 15 times
// a second — which is what lets that loop throttle the spinner's redraws (see
// presentThrottle) without slowing the spinner down.
const animFrameInterval = time.Second / 60

// Frame returns the current framebuffer, redrawing into the back buffer only
// when the scene's damage sequence has advanced since the last call. While the
// scene is animating (a refresh is streaming in), it advances the animation
// clock by the real time elapsed since the last advance, so the moving indicator
// keeps a steady real-time speed however often the present loop repaints it;
// when nothing is loading the scene is idle and the damage gate skips the redraw
// entirely.
func (a *App) Frame() (buf []byte, changed bool) {
	// Apply any scene mutations enqueued by background goroutines first, on this
	// (the render) thread, so the scene is only ever touched here.
	a.drainScene()
	a.tickGIF()         // advance the playing animated-GIF preview (if any) to its due frame
	a.tickWebDebounce() // fire a debounced page render once the selection settles
	s := a.scene
	// Whether the ONLY thing that changed since the last drawn frame is the
	// animation about to be stepped below: drainScene (a queued content write),
	// tickGIF (a real GIF frame), tickWebDebounce (a page render) and any input
	// handled since have all run and bumped Rev already, so if Rev still equals
	// the last drawn frame's, nothing but the spinner moves this frame. Only then
	// is a damage diff worth it — see DamageRects.
	animOnly := s.Rev() == a.lastRev
	// Advance the animation clock by the real time elapsed since it last stepped,
	// so the indeterminate spinner keeps a steady speed no matter how often the
	// present loop redraws it (it throttles the spinner's redraws — see the
	// presentThrottle signal below). The first animating frame only establishes
	// the clock; later frames step whole animation frames and carry the remainder
	// forward. A real content change is never throttled — it arrives as a drained
	// scene write that bumps Rev directly, redrawing on its own present.
	if s.Animating() {
		if a.lastAnim.IsZero() {
			a.lastAnim = a.now()
		} else if n := int(a.now().Sub(a.lastAnim) / animFrameInterval); n > 0 {
			s.AdvanceAnim(n)
			// Spin the feed CardList's pull-to-fetch strip in step: it is a toolkit
			// Animator the scene's damage clock does not reach, so tick its tree while
			// it is fetching, by the same elapsed interval so it keeps pace with the
			// spinner. AdvanceAnim above already marked the frame dirty (a feed fetch
			// implies Animating), so the advanced spinner is picked up by this frame.
			if s.FeedAnimating() {
				s.FeedTick(float64(n) * animFrameInterval.Seconds())
			}
			a.lastAnim = a.lastAnim.Add(time.Duration(n) * animFrameInterval)
		}
	} else {
		a.lastAnim = time.Time{}
	}
	// Tell a gated present loop whether to keep ticking on its own: an animating
	// scene advances every frame, and an armed web-preview debounce must be given
	// the frames it counts before it fires. Neither is signalled by a queued
	// write, so without this the loop would idle out mid-animation.
	if s.Animating() || a.webArmed {
		atomic.StoreInt32(&a.presentBusy, 1)
	} else {
		atomic.StoreInt32(&a.presentBusy, 0)
	}
	// Tell the present loop when it may redraw below its tick rate: the only thing
	// moving is the indeterminate loading spinner. That is throttle-safe because
	// its clock is real-time (above), so a slower redraw does not slow it. It is
	// NOT safe while a GIF is playing (real content, whose frames the loop must
	// present promptly) or a web-preview debounce is counting render ticks (it
	// times out in frames, so it needs them at the full rate) — both keep the full
	// cadence. A queued content write overrides this via presentWake, redrawing at
	// once.
	if s.Animating() && !s.GIFPlaying() && !a.webArmed {
		atomic.StoreInt32(&a.presentThrottle, 1)
	} else {
		atomic.StoreInt32(&a.presentThrottle, 0)
	}
	size := s.W * s.H * 4
	if len(a.bufs[0]) != size {
		a.bufs[0] = make([]byte, size)
		a.bufs[1] = make([]byte, size)
		a.lastRev = -1 // force a redraw after a resize
	}
	if s.Rev() == a.lastRev {
		return a.bufs[a.cur], false
	}
	back := 1 - a.cur
	s.Draw(a.bufs[back])
	a.cur = back
	a.lastRev = s.Rev()
	a.frameAnimOnly = animOnly
	return a.bufs[a.cur], true
}

// Scene exposes the scene so front-ends can dispatch input to it.
func (a *App) Scene() *ui.Scene { return a.scene }

// NeedsPresent reports whether the next Frame would do anything worth blitting:
// a background write is queued, or the last frame was still animating or waiting
// on a debounce. A native present loop that can ask this before it repaints will
// stop re-blitting an idle, unchanged window every tick. It is safe from the
// loop's own goroutine (both flags are atomic). It is deliberately conservative
// about staying dark — appearance changes are only noticed inside Frame, so a
// gated loop keeps a slow heartbeat rather than trusting this alone.
func (a *App) NeedsPresent() bool {
	return atomic.LoadInt32(&a.presentWake) != 0 || atomic.LoadInt32(&a.presentBusy) != 0
}

// PresentImmediate reports that a background scene write is queued, so the next
// present must not be throttled — the new content has to reach the screen at
// once. A gated present loop checks it to override its spinner throttle. Safe
// from the loop's goroutine (the flag is atomic).
func (a *App) PresentImmediate() bool {
	return atomic.LoadInt32(&a.presentWake) != 0
}

// PresentThrottle reports that the only thing animating is the indeterminate
// loading spinner, whose clock is real-time, so a gated present loop may redraw
// it below its tick rate to spare the CPU a full-window blit on every tick. It
// is false the moment anything else needs the full cadence (a queued write, a
// playing GIF, a web-preview debounce) or nothing is animating at all. Safe from
// the loop's goroutine (the flag is atomic).
func (a *App) PresentThrottle() bool {
	return atomic.LoadInt32(&a.presentThrottle) != 0
}

// Refresh aggregates the active profile's subscriptions (concurrently,
// newest-first) and loads the merged items into the scene. It splits the
// failures: every provider that needs sign-in/configuration becomes a clickable
// in-feed prompt (de-duplicated, in subscription order), while any remaining
// non-auth failure is shown in the status line.
func (a *App) Refresh(ctx context.Context) []error {
	items, errs := a.reg.Aggregate(ctx, a.subs)
	a.vmDo(func() {
		a.vm.SetItems(items)
		a.vm.SetAuthPrompts(authPrompts(errs))
		a.vm.SetStatus(firstNonAuthError(errs))
	})
	a.post(a.PrefetchImages) // prefetch shown Usenet images in parallel (UI thread)
	a.post(a.PrefetchMedia)  // and every other source's remote thumbnails over HTTP
	return errs
}

// RefreshStreaming aggregates the active profile's subscriptions incrementally,
// updating the scene as each source returns instead of waiting for the slowest.
// It marks every source pending and sets the scene loading, then on each
// completed source refreshes the merged feed, clears that source's pending
// marker, updates the progress counters, and re-derives the auth prompts and
// status line (in stable subscription order, independent of completion order).
// When the last source lands it turns loading off. The window front-end uses it
// so the feed fills in live with a moving indicator; the CLI keeps the blocking
// [Refresh]. It returns the collected failures (subscription order).
func (a *App) RefreshStreaming(ctx context.Context) []error {
	subs := a.subs
	// A full refresh restarts pagination: drop every source's carried cursor so
	// the next LoadMore pages from the fresh first-page tokens this run records.
	a.moreMu.Lock()
	a.cursors = map[string]string{}
	a.moreMu.Unlock()
	a.vmDo(func() {
		a.vm.SetPending(subs)
		a.vm.SetLoad(true, 0, len(subs))
	})

	byIndex := make([]error, len(subs))
	a.reg.AggregateStream(ctx, subs, func(u source.StreamUpdate) {
		if u.Err != nil && u.Index >= 0 {
			byIndex[u.Index] = u.Err // byIndex is this call's local, touched only here
		}
		if u.Index >= 0 {
			// Record this source's next-page token (kept apart from the vmDo block so
			// moreMu and vmu are never held at once). "" means exhausted / failed.
			// cursors was reset to a fresh map above, so it is never nil here.
			a.moreMu.Lock()
			a.cursors[source.SubKey(u.Sub)] = u.Cursor
			a.moreMu.Unlock()
		}
		a.vmDo(func() {
			a.vm.SetItems(u.Items)
			if u.Index >= 0 {
				a.vm.ClearPending(u.Sub.Source, u.Sub.Channel)
			}
			errs := compactErrs(byIndex)
			a.vm.SetAuthPrompts(authPrompts(errs))
			a.vm.SetStatus(firstNonAuthError(errs))
			a.vm.SetLoad(u.Done < u.Total, u.Done, u.Total)
		})
		if a.deferScene {
			// Window path: fetch thumbnails as each source's items arrive, not
			// only once the whole aggregation finishes — with ~100 subscriptions
			// (some slow NNTP) that tail can be minutes away, so a single trailing
			// prefetch left every card on its placeholder for the whole load. The
			// prefetch is idempotent (HasThumb skips) and de-duplicated (claimMedia
			// / the downloader's inflight map), so posting it on every update just
			// pulls each thumbnail forward to the moment its item appears.
			// deferScene is set once at startup, before any refresh, never after.
			a.post(a.PrefetchImages)
			a.post(a.PrefetchMedia)
		}
	})
	a.post(a.PrefetchImages) // final sweep + the one-shot CLI path (SetMediaSync)
	a.post(a.PrefetchMedia)  // runs these once, after Refresh returns
	return compactErrs(byIndex)
}

// LoadMore fetches the next page of every subscription that still has a live
// cursor and appends the new, deduplicated items to the shown feed. It is the
// infinite-scroll seam the scene's OnReachBottom trigger drives. It is a no-op
// when infinite scroll is off, when a load-more is already in flight (the guard
// coalesces the burst of at-bottom wheel events), or when no source has an
// unexhausted cursor. Returns the collected per-subscription failures.
func (a *App) LoadMore(ctx context.Context) []error {
	if !a.set.InfiniteScrollEnabled() {
		return nil
	}
	// Claim the in-flight guard and snapshot the cursors under one lock; bail if a
	// load-more is already running or nothing is left to page.
	// Scope pagination to the tab in view: a single source when one is selected
	// (only that source pages, the per-tab lazy model), every source for "All".
	subs := a.subs
	if idx := a.scene.ActiveFilter(); idx >= 0 && idx < len(a.subs) {
		subs = []source.Subscription{a.subs[idx]}
	}
	a.moreMu.Lock()
	if a.loadingMore {
		a.moreMu.Unlock()
		return nil
	}
	snap := map[string]string{}
	has := false
	for _, sub := range subs {
		if v := a.cursors[source.SubKey(sub)]; v != "" {
			snap[source.SubKey(sub)] = v
			has = true
		}
	}
	if !has {
		a.moreMu.Unlock()
		return nil
	}
	a.loadingMore = true
	a.moreMu.Unlock()
	defer func() {
		a.moreMu.Lock()
		a.loadingMore = false
		a.moreMu.Unlock()
	}()

	items, next, errs := a.reg.AggregateMore(ctx, subs, snap)

	// Advance the carried cursors with the fresh tokens (exhausted sources come
	// back "" and stop paging). We only get here with a non-empty snapshot, which
	// means RefreshStreaming already initialized a.cursors.
	a.moreMu.Lock()
	for k, v := range next {
		a.cursors[k] = v
	}
	a.moreMu.Unlock()

	// Append the new page to the shown feed, deduped by (Source, ID) against what
	// is already displayed, and publish the merged list. Reading the current items
	// and setting the merged list both happen under vmDo so the view-model is only
	// touched by one goroutine at a time.
	a.vmDo(func() {
		cur := a.vm.Items.Slice()
		seen := make(map[string]bool, len(cur))
		for _, it := range cur {
			seen[itemKey(it)] = true
		}
		merged := cur
		for _, it := range items {
			if k := itemKey(it); !seen[k] {
				seen[k] = true
				merged = append(merged, it)
			}
		}
		a.vm.SetItems(merged)
	})
	return errs
}

// itemKey is a feed item's dedup identity: its source kind and stable ID joined
// by a NUL (which cannot occur in either), so the next page's items are appended
// without re-showing a post already on screen.
func itemKey(it source.Item) string { return string(it.Source) + "\x00" + it.ID }

// PullRefresh is the pull-to-refresh seam the scene's OnPullRefresh trigger
// drives. When the feed is filtered to a single subscription (the sidebar has
// one selected), it re-fetches ONLY that subscription — the one the user is
// looking at — instead of re-aggregating every source. With no single
// subscription selected ("All"), it falls back to a full streaming refresh.
func (a *App) PullRefresh(ctx context.Context) []error { return a.LoadActiveTab(ctx) }

// LoadActiveTab loads the tab the sidebar currently shows: a single subscription
// when one is selected (only that source is fetched — the per-tab lazy model), or
// a full streaming aggregate for the "All" filter. It backs both the initial load
// and pull-to-refresh, and always re-fetches (unlike the lazy first-visit load).
func (a *App) LoadActiveTab(ctx context.Context) []error {
	idx := a.scene.ActiveFilter()
	if idx < 0 || idx >= len(a.subs) {
		return a.RefreshStreaming(ctx) // "All" filter → refresh every source
	}
	return a.refreshOneSub(ctx, a.subs[idx])
}

// OpenDefaultTab selects the first source as the active tab so a launch loads
// only that source (the per-tab lazy model) rather than aggregating everything.
// A no-op with no subscriptions, leaving the "All" filter in place.
func (a *App) OpenDefaultTab() {
	if len(a.subs) > 0 {
		a.scene.SetActive(0)
	}
}

// loadTab lazily fetches what a just-selected tab needs the first time it is
// shown: a specific source that has never been fetched, or a full aggregate for
// "All" while any source is still unfetched. A tab already loaded shows its
// cached items untouched (a manual pull re-fetches).
func (a *App) loadTab(index int) {
	sub, one, all := a.tabToLoad(index)
	switch {
	case one:
		a.tabFetch(sub)
	case all:
		a.allFetch()
	}
}

// tabToLoad reports what the tab at index still needs: a specific sub to fetch
// (one), a full aggregate (all), or neither. "Loaded" is keyed on the cursor map
// — refreshOneSub / RefreshStreaming record a token (even "") for every source
// they fetch, so a present key means the source has been loaded this session.
func (a *App) tabToLoad(index int) (sub source.Subscription, one, all bool) {
	a.moreMu.Lock()
	defer a.moreMu.Unlock()
	if index < 0 || index >= len(a.subs) {
		for _, s := range a.subs {
			if _, ok := a.cursors[source.SubKey(s)]; !ok {
				return source.Subscription{}, false, true
			}
		}
		return source.Subscription{}, false, false
	}
	s := a.subs[index]
	if _, ok := a.cursors[source.SubKey(s)]; !ok {
		return s, true, false
	}
	return source.Subscription{}, false, false
}

// refreshOneSub re-fetches a single subscription and splices its fresh page into
// the shown feed: it replaces exactly that subscription's items (matched by
// source+channel), leaving every other source untouched, then re-sorts and
// republishes. It also resets that subscription's pagination cursor so a
// following infinite-scroll pages from the top. A fetch error leaves the feed as
// it was and surfaces on the status line / auth prompts.
func (a *App) refreshOneSub(ctx context.Context, sub source.Subscription) []error {
	a.vmDo(func() {
		a.vm.SetPending([]source.Subscription{sub})
		a.vm.SetLoad(true, 0, 1)
	})
	res, err := a.reg.Feed(ctx, sub.Source, source.Query{Channel: sub.Channel, Sort: sub.Sort, Limit: sub.Limit})
	if err != nil {
		errs := []error{&source.SubscriptionError{Sub: sub, Err: err}}
		a.vmDo(func() {
			a.vm.ClearPending(sub.Source, sub.Channel)
			a.vm.SetAuthPrompts(authPrompts(errs))
			a.vm.SetStatus(firstNonAuthError(errs))
			a.vm.SetLoad(false, 1, 1)
		})
		return errs
	}
	// A fresh page restarts this subscription's pagination.
	a.moreMu.Lock()
	if a.cursors == nil {
		a.cursors = map[string]string{}
	}
	a.cursors[source.SubKey(sub)] = res.Cursor
	a.moreMu.Unlock()

	a.vmDo(func() {
		cur := a.vm.Items.Slice()
		merged := make([]source.Item, 0, len(cur)+len(res.Items))
		for _, it := range cur {
			if !sub.Matches(it) { // keep every other subscription's items as-is
				merged = append(merged, it)
			}
		}
		merged = append(merged, res.Items...)
		source.SortItems(merged)
		a.vm.SetItems(merged)
		a.vm.ClearPending(sub.Source, sub.Channel)
		a.vm.SetLoad(false, 1, 1)
	})
	return nil
}

// compactErrs drops the nil entries from an index-keyed error slice, preserving
// subscription order.
func compactErrs(byIndex []error) []error {
	out := []error{}
	for _, e := range byIndex {
		if e != nil {
			out = append(out, e)
		}
	}
	return out
}

// authPrompts extracts a de-duplicated, subscription-ordered list of providers
// whose fetch failed because they need authentication or configuration.
func authPrompts(errs []error) []ui.AuthPrompt {
	var out []ui.AuthPrompt
	seen := map[source.Kind]bool{}
	for _, e := range errs {
		ae, ok := source.AsAuthError(e)
		if !ok || seen[ae.Kind] {
			continue
		}
		seen[ae.Kind] = true
		out = append(out, ui.AuthPrompt{Kind: ae.Kind, Reason: ae.Reason})
	}
	return out
}

// firstNonAuthError returns the message of the first failure that is not an
// auth/config prompt (those get the banner instead), or "" when there is none.
func firstNonAuthError(errs []error) string {
	for _, e := range errs {
		if _, ok := source.AsAuthError(e); !ok {
			return e.Error()
		}
	}
	return ""
}

// Items returns the currently loaded feed.
func (a *App) Items() []source.Item { return a.scene.Items }

// RenderPNG draws the current scene and encodes it as PNG.
func (a *App) RenderPNG() ([]byte, error) {
	s := a.scene
	buf := make([]byte, s.W*s.H*4)
	s.Draw(buf)
	img := &image.RGBA{Pix: buf, Stride: s.W * 4, Rect: image.Rect(0, 0, s.W, s.H)}
	var out bytes.Buffer
	if err := encodePNG(&out, img); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// encodePNG is a package var so tests can force the (otherwise-untriggerable)
// encode-error branch.
var encodePNG = png.Encode

// SetTheme reselects the palette (e.g. on a system light/dark change).
func (a *App) SetTheme(osName string, dark bool) {
	a.osName, a.dark = osName, dark
	a.applyTheme()
}

// reconstructor is the Usenet provider capability the reconstruction action
// needs; the registered provider satisfies it structurally.
type reconstructor interface {
	Reconstruct(ctx context.Context, req usenet.ReconstructRequest) (map[string][]byte, *par2.VerifyResult, error)
}

// writeFile saves a reconstructed file; a package var so tests exercise the
// success and error branches without touching the real filesystem.
var writeFile = os.WriteFile

// ReconstructGroup triggers reconstruction of the Usenet post group with the
// given release base. It is non-blocking: the default hook runs on a goroutine
// so the UI keeps presenting the loading indicator; tests substitute a
// synchronous hook.
func (a *App) ReconstructGroup(base string) { a.reconstruct(base) }

// doReconstruct performs the reconstruction synchronously: it collects the
// group's member articles from the scene, asks the Usenet provider to download
// and reassemble them (PAR2 verify/repair), drives the existing loading
// indicator with a "k/n parts" status, and on success writes the data files to
// the cache directory. Any failure is surfaced in the status line.
func (a *App) doReconstruct(ctx context.Context, base string) {
	parts, ok := a.scene.GroupParts(base)
	if !ok {
		a.vmStatus("Nothing to reconstruct for " + base)
		return
	}
	prov, ok := a.reg.Get(source.Usenet)
	if !ok {
		a.vmStatus("Usenet provider not configured")
		return
	}
	rc, ok := prov.(reconstructor)
	if !ok {
		a.vmStatus("Usenet provider cannot reconstruct")
		return
	}

	total := len(parts)
	a.vmLoad(true, 0, total)
	a.vmStatus(fmt.Sprintf("Reconstructing %s… (0/%d parts)", base, total))

	req := usenet.ReconstructRequest{
		Parts: toReconstructParts(parts),
		OnProgress: func(done, tot int) {
			a.vmLoad(true, done, tot)
			a.vmStatus(fmt.Sprintf("Reconstructing %s… (%d/%d parts)", base, done, tot))
		},
	}
	files, vr, err := rc.Reconstruct(ctx, req)
	a.vmLoad(false, total, total)
	if err != nil {
		a.vmStatus(fmt.Sprintf("Reconstruct %s failed: %v", base, err))
		return
	}
	if err := a.saveFiles(files); err != nil {
		a.vmStatus(fmt.Sprintf("Reconstruct %s: save failed: %v", base, err))
		return
	}
	a.vmStatus(fmt.Sprintf("Saved %s (%d files, %s)", base, len(files), verifyWord(vr)))
}

// toReconstructParts converts the ui-level part list to the provider request.
func toReconstructParts(parts []ui.ReconstructPart) []usenet.ReconstructPart {
	out := make([]usenet.ReconstructPart, len(parts))
	for i, p := range parts {
		out[i] = usenet.ReconstructPart{MessageID: p.MessageID, Filename: p.Filename}
	}
	return out
}

// saveFiles writes each reconstructed data file into the cache directory (the
// current working directory when no cache path is set).
func (a *App) saveFiles(files map[string][]byte) error {
	return a.saveFilesTo(files, a.scene.CachePath())
}

// saveFilesTo writes each reconstructed file into dir (captured by the caller so
// a background worker never reads the scene for the path).
func (a *App) saveFilesTo(files map[string][]byte, dir string) error {
	for name, data := range files {
		if err := writeFile(filepath.Join(dir, filepath.Base(name)), data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// verifyWord describes the PAR2 outcome for the saved status line.
func verifyWord(vr *par2.VerifyResult) string {
	switch {
	case vr == nil:
		return "no PAR2"
	case vr.Complete:
		return "verified"
	default:
		return "incomplete"
	}
}
