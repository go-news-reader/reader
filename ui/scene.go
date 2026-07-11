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
)

// Mode selects which view the scene renders.
type Mode int

const (
	ModeFeed   Mode = iota // the topbar + sidebar + unified feed
	ModeDetail             // a single item's full detail / reading view
)

// Hit is the result of [Scene.HitTest].
type Hit struct {
	Kind HitKind
	Item source.Item // HitItem
	Sub  int         // HitSub: index into Subs, or AllFilter
}

// Scene is the mutable aggregator UI state.
type Scene struct {
	W, H int

	theme  *toolkit.Theme
	Items  []source.Item // the unified feed (merge newest-first before setting)
	Subs   []Subscription
	Active int // selected subscription index, or AllFilter
	Status string
	ScrollY int
	Scale   float64 // display scale (zoom × devicePixelRatio); 0 => 1

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

	m       metrics
	subs    []subHit
	searchR toolkit.Rect
	rows    []rowLayout
	contentH int

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
	s := &Scene{W: w, H: h, theme: theme, Active: AllFilter, Scale: 1}
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

// TypeRune appends r to the search text when it is focused.
func (s *Scene) TypeRune(r rune) {
	if s.searchFocused {
		s.search += string(r)
		s.touch()
	}
}

// Backspace removes the last search rune when focused.
func (s *Scene) Backspace() {
	if s.searchFocused && s.search != "" {
		r := []rune(s.search)
		s.search = string(r[:len(r)-1])
		s.touch()
	}
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
