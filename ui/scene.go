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

// HitKind classifies what a click landed on.
type HitKind int

const (
	HitNone         HitKind = iota
	HitItem                 // a feed item (open it in the detail view) — Item set
	HitSub                  // a sidebar subscription (filter the feed) — Sub set (AllFilter = All)
	HitSearch               // the topbar search field (focus it)
	HitBack                 // the detail view's back button (return to the feed)
	HitOpenExternal         // the detail view's "open original" button — Item set
	HitProfile              // a sidebar profile tab (switch active profile) — Profile set
	HitSettings             // the sidebar ⚙ Settings entry (open preferences)

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
)

// Mode selects which view the scene renders.
type Mode int

const (
	ModeFeed     Mode = iota // the topbar + sidebar + unified feed
	ModeDetail               // a single item's full detail / reading view
	ModeSettings             // the in-canvas preferences editor
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

	theme   *toolkit.Theme
	Items   []source.Item // the unified feed (merge newest-first before setting)
	Subs    []Subscription
	Active  int // selected subscription index, or AllFilter
	Status  string
	ScrollY int
	Scale   float64 // display scale (zoom × devicePixelRatio); 0 => 1

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

	// Optional decoded thumbnails keyed by Item.ID (blitted when present).
	Thumbs map[string]*image.RGBA

	// Topbar search/filter.
	search        string
	searchFocused bool

	// Detail (reading) view: ModeDetail shows a single opened item in-app.
	mode           Mode
	detail         source.Item
	detailScrollY  int
	detailContentH int
	backR, openR   toolkit.Rect

	m         metrics
	subs      []subHit
	profTabs  []profTabHit
	settingsR toolkit.Rect
	searchR   toolkit.Rect
	rows      []rowLayout
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
		themeName: settings.ThemeSystem, newKind: source.Reddit}
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

// Search returns the current filter text.
func (s *Scene) Search() string { return s.search }

// SetSearch replaces the filter text.
func (s *Scene) SetSearch(v string) { s.search = v; s.touch() }

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
	if s.searchFocused {
		s.search += string(r)
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
	if s.searchFocused && s.search != "" {
		s.search = trimLastRune(s.search)
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

// Scroll adjusts the vertical scroll of whichever view is showing, clamped to
// its content height.
func (s *Scene) Scroll(dy int) {
	if s.mode == ModeSettings {
		return // the settings editor fits the surface; nothing to scroll
	}
	if s.mode == ModeDetail {
		s.detailScrollY += dy
		s.layoutDetail()
		s.detailScrollY = clampScroll(s.detailScrollY, s.detailContentH-(s.H-s.m.topbarH))
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
	q := strings.ToLower(strings.TrimSpace(s.search))
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
