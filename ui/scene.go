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
	HitNone   HitKind = iota
	HitItem           // a feed item (open its permalink) — Item set
	HitSub            // a sidebar subscription (filter the feed) — Sub set (AllFilter = All)
	HitSearch         // the topbar search field (focus it)
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

	m       metrics
	subs    []subHit
	searchR toolkit.Rect
	rows    []rowLayout
	contentH int
}

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
	}
}

// SetItems replaces the feed (caller merges/sorts newest-first).
func (s *Scene) SetItems(items []source.Item) { s.Items = items; s.ScrollY = 0 }

// SetSubs replaces the sidebar subscriptions.
func (s *Scene) SetSubs(subs []Subscription) { s.Subs = subs }

// Resize updates the surface size, clamped to the minimum.
func (s *Scene) Resize(w, h int) { s.W, s.H = w, h; s.clampSize() }

// SetScale sets the display scale, clamped to [MinZoom, MaxZoom].
func (s *Scene) SetScale(f float64) {
	if f < MinZoom {
		f = MinZoom
	}
	if f > MaxZoom {
		f = MaxZoom
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
func (s *Scene) SetSearch(v string) { s.search = v }

// SearchFocused reports whether the search field has keyboard focus.
func (s *Scene) SearchFocused() bool { return s.searchFocused }

// FocusSearch gives (or removes) keyboard focus to the search field.
func (s *Scene) FocusSearch(v bool) { s.searchFocused = v }

// TypeRune appends r to the search text when it is focused.
func (s *Scene) TypeRune(r rune) {
	if s.searchFocused {
		s.search += string(r)
	}
}

// Backspace removes the last search rune when focused.
func (s *Scene) Backspace() {
	if s.searchFocused && s.search != "" {
		r := []rune(s.search)
		s.search = string(r[:len(r)-1])
	}
}

// Scroll adjusts the vertical scroll, clamped to the content height.
func (s *Scene) Scroll(dy int) {
	s.ScrollY += dy
	s.layout()
	max := s.contentH - (s.H - s.m.topbarH)
	if max < 0 {
		max = 0
	}
	if s.ScrollY > max {
		s.ScrollY = max
	}
	if s.ScrollY < 0 {
		s.ScrollY = 0
	}
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
