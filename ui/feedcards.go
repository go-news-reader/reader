package ui

// Feed migration (step 1): the unified feed is modelled as a live
// mvvm.ObservableList of source.Item rendered by a virtual.CardList — the
// toolkit's recycled card feed — instead of the hand-drawn card loop. This file
// owns the model, its anchor-preserving synchronisation, and the mapping from a
// display entry to one of the toolkit content-card types (ArticleCard /
// LinkCard / MediaCard). The CardList orders items oldest→newest (newest at the
// bottom, the chat-style feed) and opens at the bottom on a filter change.
//
// A Usenet multipart post (an itemGroup) collapses to a single summary
// ArticleCard here — its inline expand/collapse/download is deferred to a
// follow-up (a dedicated toolkit GroupCard), so this step draws only the summary.

import (
	"fmt"
	"strings"

	"github.com/go-widgets/mvvm"
	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
	"github.com/go-widgets/toolkit/virtual"

	"github.com/go-news-reader/reader/source"
)

// cardTitleMaxLines caps how many lines a wrapped card title occupies before its
// last shown line is ellipsised, so a pathologically long title can only grow a
// card so far. cardBodyMaxLines is the same cap for a card whose headline is its
// body (see cardText): it is higher because a short post's whole text IS the card
// — a tweet or toot should read in full rather than stop at an ellipsis three
// lines in. These feed the PostCard's MaxTitleLines (see feedPostCard).
const (
	cardTitleMaxLines = 3
	cardBodyMaxLines  = 5
)

// cardMaxLines is how many wrapped headline lines a card may show: titled items
// keep the historical three, a body fallback gets cardBodyMaxLines.
func cardMaxLines(it source.Item) int {
	if strings.TrimSpace(it.Title) != "" {
		return cardTitleMaxLines
	}
	return cardBodyMaxLines
}

// feedCards binds the feed's ObservableList model to a virtual.CardList and the
// parallel display entries that drive per-row card typing and selection. The
// model and display slice are always the same length and in the same
// (oldest→newest) order, rebuilt together on every sync, so a CardList row index
// maps 1:1 onto display[i].
type feedCards struct {
	model   *mvvm.ObservableList[source.Item]
	list    *virtual.CardList[source.Item]
	display []feedEntry
	width   int            // content width the row heights were last measured at
	sig     feedSig        // inputs the model was last built from
	synced  bool           // whether the model has been built at least once
	heights map[string]int // memoised row heights keyed by feedEntryKey, cleared on a width change
}

// feedEntryKey identifies a feed row's CONTENT for the height memo: same key ⇒
// same measured height at a fixed width. Group summaries fold their thread
// summary in so a group that gains members re-measures.
func feedEntryKey(e feedEntry) string {
	if e.group != nil {
		return "g\x00" + e.group.Base + "\x00" + groupThreadSummary(e.group)
	}
	return "i\x00" + feedItemKey(e.item)
}

// feedHeightKey is feedEntryKey with the row's thumbnail-presence folded in, the
// key the height memo actually stores under. A card reserves its thumbnail
// column from the moment its item declares media, so its height does not depend
// on whether the decoded image has landed yet — but folding thumbnail presence
// into the key anyway makes the memo self-correcting: when a thumbnail arrives
// the key changes, the stale entry is dropped (see invalidateFeedThumb), and the
// row re-measures. The two suffixes ("\x00T" present, "\x00-" absent) are the
// only two states, so invalidation can delete both without knowing which is
// cached. A group row folds its expand state in too (see groupExpandSuffix): an
// expanded group card is taller (it lists its members), so toggling must
// re-measure — the same self-correcting trick as the thumbnail suffix.
func (s *Scene) feedHeightKey(e feedEntry) string {
	k := feedEntryKey(e) + s.feedThumbSuffix(s.feedEntryItem(e).ID)
	if e.group != nil {
		k += groupExpandSuffix(s.GroupExpanded(e.group.Base))
	}
	return k
}

// groupExpandSuffix is the height-key suffix folding a group row's expand state
// in: "\x00x1" when expanded (members listed → taller), "\x00x0" when collapsed.
func groupExpandSuffix(expanded bool) string {
	if expanded {
		return "\x00x1"
	}
	return "\x00x0"
}

// dropHeightMemo deletes every cached height variant for display entry e — both
// thumbnail-presence suffixes, and (for a group) both expand-state suffixes — so
// a re-measure is forced without the caller knowing which variant is cached. It
// is a no-op when nothing has been measured yet.
func (s *Scene) dropHeightMemo(e feedEntry) {
	if s.feed.heights == nil {
		return
	}
	base := feedEntryKey(e)
	for _, t := range []string{"\x00T", "\x00-"} {
		if e.group != nil {
			delete(s.feed.heights, base+t+groupExpandSuffix(true))
			delete(s.feed.heights, base+t+groupExpandSuffix(false))
			continue
		}
		delete(s.feed.heights, base+t)
	}
}

// feedThumbSuffix reports the height-key suffix for an item id: "\x00T" once a
// decoded thumbnail is cached for it, "\x00-" while none has landed.
func (s *Scene) feedThumbSuffix(id string) string {
	if s.Thumbs != nil && s.Thumbs[id] != nil {
		return "\x00T"
	}
	return "\x00-"
}

// feedEntryItem is the source.Item a display entry renders as: the post itself,
// or the synthesised summary item standing in for a Usenet group.
func (s *Scene) feedEntryItem(e feedEntry) source.Item {
	if e.group != nil {
		return groupSummaryItem(e.group)
	}
	return e.item
}

// feedSig captures the cheap, scalar inputs that decide the feed's display list,
// so syncFeed rebuilds the model only when one of them actually changes rather
// than re-diffing (and re-measuring every card) on every idle layout. subsRev
// bumps on every SetItems / SetSubs, so a content change is caught without
// hashing the whole feed.
type feedSig struct {
	width    int
	active   int
	search   string
	itemsRev int
	loading  bool
}

// ensureFeed lazily builds the feed model + CardList on first use. The CardList
// subscribes to the model immediately (via NewCardList), so later syncs mutate
// the model in place and the list keeps its scroll anchor across them.
func (s *Scene) ensureFeed() {
	if s.feed != nil {
		return
	}
	fc := &feedCards{model: mvvm.NewObservableList[source.Item]()}
	fc.list = virtual.NewCardList(fc.model,
		func(i int) int { return s.feedRowHeight(i) },
		func(p painter.Painter, th *toolkit.Theme, r toolkit.Rect, i int, item source.Item, st virtual.CardState) {
			s.feedCardRender(p, th, r, i, item, st)
		})
	// CardList.OnReachTop loads OLDER posts (newest is at the bottom, so an
	// upward pull toward the top asks for what came before); OnReachBottom
	// refreshes (a downward pull past the newest re-aggregates). These reuse the
	// existing app seams, which the mission keeps: OnReachBottom == "load more
	// older", OnPullRefresh == "refresh".
	fc.list.OnReachTop = func() {
		// Loading OLDER posts is the infinite-scroll next page, gated by the setting;
		// pull-to-refresh (OnReachBottom below) is always on.
		if s.infiniteScroll && s.OnReachBottom != nil {
			s.OnReachBottom()
		}
	}
	fc.list.OnReachBottom = func() {
		if s.OnPullRefresh != nil {
			s.OnPullRefresh()
		}
	}
	fc.list.Dimmed = func(i int) bool { return s.feedDimmed(i) }
	fc.list.OnSelect = func(i int) { s.feedSelect(i) }
	fc.list.OnActivate = func(i int) { s.feedActivate(i) }
	s.feed = fc
}

// FeedCardList exposes the feed's virtual.CardList (front-ends/tests drive and
// inspect it: bounds, scroll, events, animation).
func (s *Scene) FeedCardList() *virtual.CardList[source.Item] {
	s.ensureFeed()
	return s.feed.list
}

// FeedModelItems returns the feed model's current items (oldest→newest), a
// stable snapshot for tests/inspection.
func (s *Scene) FeedModelItems() []source.Item {
	s.ensureFeed()
	return s.feed.model.Slice()
}

// feedDimmed reports whether feed row i is muted ("already read"): true once its
// item has been opened in the preview pane this session (tracked in s.viewed).
// The CardList veils a dimmed card; a fast path skips the lookup while nothing
// has been viewed.
func (s *Scene) feedDimmed(i int) bool {
	if len(s.viewed) == 0 {
		return false
	}
	it, ok := s.feedItemAt(i)
	return ok && s.viewed[it.ID]
}

// feedSelect handles a CardList selection (click or keyboard cursor move) at row
// i: it previews the row's item. It routes through the app seam when installed
// (so the app can fetch the item's image / web page / comments), falling back to
// the scene's own SelectPreview so a bare scene still tracks the selection.
func (s *Scene) feedSelect(i int) {
	it, ok := s.feedItemAt(i)
	if !ok {
		return
	}
	s.feedPreviewItem(it, s.feedSelectViaKeyboard)
}

// feedPreviewItem previews a feed item: through the app select hook when one is
// installed (so the app can fetch the item's image / web page / comments —
// viaKeyboard debounces a held-arrow web render), else through the scene's own
// preview so a bare scene still tracks the selection. It is shared by a CardList
// selection (feedSelect) and a direct member-row click (FeedClickAt).
func (s *Scene) feedPreviewItem(it source.Item, viaKeyboard bool) {
	if s.onSelectItem != nil {
		s.onSelectItem(it, viaKeyboard)
		return
	}
	s.SelectPreview(it)
}

// feedActivate handles a CardList activation (Enter) at row i: it opens the
// row's item in the reading view.
func (s *Scene) feedActivate(i int) {
	if it, ok := s.feedItemAt(i); ok {
		s.OpenDetail(it)
	}
}

// feedItemAt returns the source.Item backing feed row i (the real post, or the
// synthesised summary item for a Usenet group), and false when i is out of
// range.
func (s *Scene) feedItemAt(i int) (source.Item, bool) {
	s.ensureFeed()
	if i < 0 || i >= len(s.feed.display) {
		return source.Item{}, false
	}
	e := s.feed.display[i]
	if e.group != nil {
		return groupSummaryItem(e.group), true
	}
	return e.item, true
}

// feedRowHeight measures feed row i's card at the current content width so the
// CardList can lay out variable-height cards. Out-of-range rows measure to zero.
func (s *Scene) feedRowHeight(i int) int {
	s.ensureFeed()
	if i < 0 || i >= len(s.feed.display) {
		return 0
	}
	w := s.feed.width
	if w < 1 {
		w = 1
	}
	// Memoise the measured height: VirtualList's height index calls this for every
	// row on every model mutation, and PostCard.Measure runs full title shaping, so
	// without the memo an insert re-shapes the whole feed (O(n²) shaping — it froze
	// the app). The cache is cleared on a width change in syncFeed.
	e := s.feed.display[i]
	key := s.feedHeightKey(e)
	if h, ok := s.feed.heights[key]; ok {
		return h
	}
	h := s.feedRowCard(e).Measure(w)
	if s.feed.heights == nil {
		s.feed.heights = map[string]int{}
	}
	s.feed.heights[key] = h
	return h
}

// feedCard is the passive toolkit content card that measures, paints and yields
// selectable runs for one feed row — a PostCard or a GroupCard. It is a
// toolkit.Widget (so CollectRuns lifts its text out) plus Measure (so the
// CardList lays it out).
type feedCard interface {
	toolkit.Widget
	Measure(width int) int
}

// feedRowCard maps a display entry to the passive toolkit content card that
// renders and measures it: a GroupCard for a Usenet multipart post (its
// collapsible header + member list), a PostCard for every standalone item.
// Measure and Draw run against the same card, so a row's laid-out height and its
// painted content can never disagree.
func (s *Scene) feedRowCard(e feedEntry) feedCard {
	if e.group != nil {
		return s.feedGroupCard(e.group)
	}
	return s.feedPostCard(s.feedEntryItem(e))
}

// invalidateFeedThumb re-measures and re-lays-out the feed rows backing item id
// after its thumbnail landed. The height memo is keyed by thumbnail presence, so
// dropping both possible entries forces a fresh measure; re-setting the row in
// the model fires a ListReplace the CardList turns into a height-index rebuild,
// so a card mounted before its image arrived cannot keep a stale (too-short)
// height and clip the thumbnail. It is a no-op before the feed exists.
func (s *Scene) invalidateFeedThumb(id string) {
	if s.feed == nil {
		return
	}
	for i, e := range s.feed.display {
		if s.feedEntryItem(e).ID != id {
			continue
		}
		s.dropHeightMemo(e)
		if i < s.feed.model.Len() {
			s.feed.model.Set(i, s.feed.model.At(i)) // ListReplace → CardList rebuilds this row's height
		}
	}
}

// invalidateFeedGroup re-measures and re-lays-out the feed row backing the
// Usenet group with the given release base after its expand state flipped. The
// height memo is keyed by expand state (see groupExpandSuffix), so dropping the
// row's cached variants forces a fresh measure; re-setting the row in the model
// fires a ListReplace the CardList turns into a height-index rebuild, so an
// expanded group grows (and a collapsed one shrinks) in the laid-out feed. It is
// a no-op before the feed exists.
func (s *Scene) invalidateFeedGroup(base string) {
	if s.feed == nil {
		return
	}
	for i, e := range s.feed.display {
		if e.group == nil || e.group.Base != base {
			continue
		}
		s.dropHeightMemo(e)
		if i < s.feed.model.Len() {
			s.feed.model.Set(i, s.feed.model.At(i)) // ListReplace → CardList rebuilds this row's height
		}
	}
}

// feedCardRender draws feed row i's card into r, reproducing the reader's own
// composed card (rounded frame + badge/title/meta content column + optional
// thumbnail) via a toolkit.PostCard, and folds the card's selectable text runs
// (channel, title lines, meta) into this frame's cross-surface selection
// accumulator so a drag spans the sidebar, the cards and the preview. The
// CardList overlays the selection ring and the read veil on top, so this only
// paints — and collects the runs of — the card's own content. r is the card's
// exact on-screen span, so its runs need no further translation.
func (s *Scene) feedCardRender(p painter.Painter, th *toolkit.Theme, r toolkit.Rect, i int, _ source.Item, _ virtual.CardState) {
	if i < 0 || i >= len(s.feed.display) {
		return
	}
	card := s.feedRowCard(s.feed.display[i])
	card.SetBounds(r)
	card.Draw(p, th)
	s.addSelectableRuns(toolkit.CollectRuns(card), 0, 0)
}

// feedPostCard maps one feed item onto a toolkit.PostCard — the shared rich feed
// row (source pill + channel subtitle, wrapped headline, muted meta line, lead
// thumbnail) — reproducing the reader's own card without hand-drawing it.
//
// The card's four fonts are passed already in DEVICE pixels (ttFont at rpxOf, the
// same faces the reader draws every other run with), because PostCard does NOT
// auto-scale fonts. Its box METRICS (padding, gaps, corner radius, thumbnail
// column) are LOGICAL: PostCard routes them through the toolkit's global metric
// scale, which the reader pins to s.Scale (see SetScale + the call below), so the
// chrome around the text scales in lockstep with the device-pixel fonts. Passing
// the thumbnail dimensions as their logical bases (104×60, the same values
// computeMetrics feeds through rpx) rather than pre-scaled rpx values is what
// keeps the column from being scaled twice.
func (s *Scene) feedPostCard(it source.Item) *toolkit.PostCard {
	// Pin the toolkit's global metric scale to the reader's device scale before the
	// card measures or draws, so PostCard's internal metrics track the reader's rpx
	// exactly at every point of use (and defensively, whatever a prior surface left
	// the package global at). SetScale does the same on a density change (#165).
	toolkit.SetMetricScale(s.Scale)

	col := sourceColor(it.Source)
	pc := &toolkit.PostCard{
		Pill:          sourceLabel(it.Source),
		PillColor:     col,
		PillInk:       onAccentFor(col),
		Subtitle:      it.Channel,
		Title:         cardText(it),
		Meta:          metaLine(it),
		MaxTitleLines: cardMaxLines(it),
		// Logical bases (NOT rpx): PostCard scales them by the metric scale, so
		// passing rpxOf here would double-scale the thumbnail column.
		ThumbW: 104,
		ThumbH: 60,
		// Device-pixel faces: PostCard renders these verbatim (no font auto-scale).
		TitleFont:    ttFont(true, rpxOf(s, 15)),
		SubtitleFont: ttFont(false, rpxOf(s, 12)),
		MetaFont:     ttFont(false, rpxOf(s, 12)),
		PillFont:     ttFont(true, rpxOf(s, 10)),
	}
	if s.Thumbs != nil {
		pc.Thumbnail = s.Thumbs[it.ID]
	}
	// Reserve the thumbnail column as soon as the post HAS media — the historical
	// behaviour — not only once the image has decoded, so a card does not reflow
	// (grow a column) when the thumbnail lands. ThumbPlaceholder pins the column
	// while the image is still absent and draws the media kind ("image"/"video"/…)
	// centred on the muted SurfaceAlt ground, exactly the box the old card drew
	// while loading.
	if pc.Thumbnail == nil && len(it.Media) > 0 {
		pc.ThumbPlaceholder = string(it.Media[0].Kind)
	}
	return pc
}

// feedGroupCard maps one Usenet multipart post (an itemGroup) onto a
// toolkit.GroupCard — the shared collapsible group row that restores the
// reader's pre-migration card: a Usenet source pill, a green/red complete /
// incomplete status pill, the release base as the headline, the "N parts · M
// files · SIZE" meta, and — only when the post is complete (all files + parts
// present, so reassembly can run) — a download checkbox plus a "Reconstruct"
// pill. Expanding it lists each member part ("filename (p/P) size"). An
// incomplete post is not Actionable, so it exposes neither the checkbox nor the
// pill, exactly like the old card.
//
// Fonts are passed already in DEVICE pixels (ttFont at rpxOf, the same faces
// feedPostCard uses), because GroupCard does NOT auto-scale fonts; its box
// metrics ARE logical, routed through the toolkit's global metric scale, which
// the reader pins to s.Scale first — the same anti-double-scaling approach as
// feedPostCard.
func (s *Scene) feedGroupCard(g *itemGroup) *toolkit.GroupCard {
	// Pin the toolkit's global metric scale to the reader's device scale before the
	// card measures or draws, so GroupCard's internal metrics track the reader's rpx
	// exactly (and defensively, whatever a prior surface left the package global at).
	toolkit.SetMetricScale(s.Scale)

	th := s.theme
	complete := g.Complete()
	status, statusFill := "incomplete", errorColor(th)
	if complete {
		status, statusFill = "complete", successColor(th)
	}
	pillFill := sourceColor(source.Usenet)

	members := make([]string, len(g.Members))
	for i, mem := range g.Members {
		members[i] = memberLine(mem)
	}

	return &toolkit.GroupCard{
		Pill:        sourceLabel(source.Usenet),
		PillColor:   pillFill,
		PillInk:     onAccentFor(pillFill),
		Status:      status,
		StatusColor: statusFill,
		StatusInk:   onAccentFor(statusFill),
		Title:       g.Base,
		Meta:        groupMeta(g),
		Expanded:    s.GroupExpanded(g.Base),
		Members:     members,
		Actionable:  complete,
		Action:      "Reconstruct",
		Checked:     s.IsDownloadQueued(g.Base),
		// Device-pixel faces (GroupCard renders these verbatim); the box metrics scale
		// themselves through the pinned global scale, so passing rpxOf here is correct
		// and does NOT double-scale (fonts and metrics are separate axes).
		TitleFont: ttFont(true, rpxOf(s, 15)),
		MetaFont:  ttFont(false, rpxOf(s, 12)),
		PillFont:  ttFont(true, rpxOf(s, 10)),
	}
}

// groupThreadSummary is the collapsed body of a Usenet group's summary card:
// "<N>-part thread" plus the parts/files/size line the expanded card would show.
func groupThreadSummary(g *itemGroup) string {
	return fmt.Sprintf("%d-part thread · %s", len(g.Members), groupMeta(g))
}

// groupSummaryItem synthesises the source.Item that stands in for a Usenet group
// in the feed model + selection: the release base as id/title (so selecting it
// previews the post by base) and a declared image so the preview pane reserves
// the reconstructed-image box, mirroring GroupPreviewItem.
func groupSummaryItem(g *itemGroup) source.Item {
	it := source.Item{
		ID: g.Base, Source: source.Usenet, Title: g.Base,
		Body:  groupThreadSummary(g),
		Score: -1, Comments: -1,
		Media: []source.Media{{Kind: source.MediaImage}},
	}
	if len(g.Members) > 0 {
		it.Channel = g.Members[0].Item.Channel
	}
	return it
}

// feedDisplayEntries builds the feed's display list in the CardList's
// oldest→newest order: the filtered items (newest-first) are grouped, then the
// whole list is reversed so the newest post lands at the bottom.
func (s *Scene) feedDisplayEntries() []feedEntry {
	entries := groupItems(s.filtered())
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
	return entries
}

// feedItemKey is the identity used to diff the model across a sync: two items
// are the same feed row exactly when sameItem would treat them as one post
// (Source + ID + headline), so a re-sync with unchanged content is a no-op and a
// streamed insert is a minimal edit.
func feedItemKey(it source.Item) string {
	return string(it.Source) + "\x00" + it.ID + "\x00" + cardText(it)
}

// syncFeedModel reconciles the model to next (already oldest→newest) with the
// fewest mutations: it keeps the common prefix and suffix untouched and edits
// only the middle, so an insert of older posts at the top or newer posts at the
// bottom shifts the model without disturbing the CardList's scroll anchor.
func (s *Scene) syncFeedModel(next []source.Item) {
	m := s.feed.model
	cur := m.Slice()
	p := 0
	for p < len(cur) && p < len(next) && feedItemKey(cur[p]) == feedItemKey(next[p]) {
		p++
	}
	sfx := 0
	for sfx < len(cur)-p && sfx < len(next)-p &&
		feedItemKey(cur[len(cur)-1-sfx]) == feedItemKey(next[len(next)-1-sfx]) {
		sfx++
	}
	// A full replacement (no shared end) is a single Clear + Append — two model
	// events, so the CardList rebuilds its height index once (O(n)) rather than
	// once per inserted row (O(n²)). A partial change keeps a shared prefix/suffix
	// and edits only the middle in place, preserving the scroll anchor.
	if p == 0 && sfx == 0 {
		m.Clear()
		if len(next) > 0 {
			m.Append(next...)
		}
		return
	}
	for i := len(cur) - sfx - 1; i >= p; i-- {
		m.RemoveAt(i)
	}
	for i := p; i < len(next)-sfx; i++ {
		m.Insert(i, next[i])
	}
}

// syncFeed rebuilds the display entries, reconciles the model to them, positions
// the CardList in the feed rect at the current content width, mirrors the
// loading state onto the pull-to-fetch strip, and — after a filter/tab change —
// opens the feed at the bottom (the newest post). It is the feed's per-layout
// refresh point, called at the tail of layout().
func (s *Scene) syncFeed() {
	s.ensureFeed()
	feedX, feedW := s.feedGeom()
	cardW := s.feedCardW(feedW)
	if cardW < 1 {
		cardW = 1
	}

	// Rebuild the model only when a content-affecting input changed — otherwise an
	// idle re-layout (every Draw / HitTest calls layout) would re-diff and
	// re-measure the whole feed for nothing. The width is part of the signature so
	// a resize re-measures the variable-height cards.
	sig := feedSig{width: cardW, active: s.Active, search: s.searchEntry.Text, itemsRev: s.subsRev, loading: s.loading}
	if !s.feed.synced || s.feed.sig != sig {
		if cardW != s.feed.width {
			s.feed.heights = nil // width changed → measured heights are stale
		}
		s.feed.width = cardW
		entries := s.feedDisplayEntries()
		items := make([]source.Item, len(entries))
		for i, e := range entries {
			if e.group != nil {
				items[i] = groupSummaryItem(e.group)
			} else {
				items[i] = e.item
			}
		}
		// display must be in place before the model mutates: the model subscription
		// fires the CardList's onChange synchronously inside the mutation, and that
		// calls feedRowHeight, which reads display.
		s.feed.display = entries
		s.syncFeedModel(items)
		s.feed.sig = sig
		s.feed.synced = true
	}

	_ = feedX // width feeds the sig above; the list rect is placed below the banners
	s.feed.list.SetBounds(s.feedListRect())

	// A refresh in flight spins the bottom pull-to-fetch strip (newest lands at
	// the bottom). A separate "loading older" flag is deferred, so the top strip
	// stays idle for now.
	s.feed.list.FetchingBottom = s.loading
	if s.loading {
		s.feed.list.BottomLabel = "Loading…"
	} else {
		s.feed.list.BottomLabel = ""
	}

	if s.feedBottomPending {
		s.feedBottomPending = false
		s.feed.list.ScrollToBottom()
	}
}

// FeedTick advances the feed CardList's pull-to-fetch spinners by dt seconds
// through the toolkit animation tree, and reports whether the feed still wants
// frames. A present loop drives it while the feed is fetching.
func (s *Scene) FeedTick(dt float64) {
	s.ensureFeed()
	toolkit.TickTree(s.feed.list, dt)
}

// FeedAnimating reports whether the feed CardList's fetch strips are spinning
// (a fetch is in flight), so a gated present loop keeps ticking the feed.
func (s *Scene) FeedAnimating() bool {
	s.ensureFeed()
	return toolkit.TreeAnimating(s.feed.list)
}

// FeedScrollToBottom opens the feed at its newest (bottom) post — the app calls
// it after switching tabs / filters (it is also armed automatically by
// SetActive / SetItems).
func (s *Scene) FeedScrollToBottom() {
	s.ensureFeed()
	s.layout()
	s.feed.list.ScrollToBottom()
}

// SetFeedSelectHook installs the app callback fired when a feed card is selected
// (click or keyboard). Its bool argument is true when the selection came from a
// keyboard cursor move, so the app can debounce a web render. Passing nil
// restores the scene's own preview-only behaviour.
func (s *Scene) SetFeedSelectHook(f func(it source.Item, viaKeyboard bool)) { s.onSelectItem = f }

// FeedEvent routes one input event into the feed CardList (wheel scroll,
// selection keys, or a widget-local click), setting the keyboard-origin flag
// around it so the select hook can tell a cursor move from a click. localY is
// only read for a click (feed-relative). It is the input seam that replaces the
// hand-drawn feed's wheel / arrow / hit-test handling.
func (s *Scene) FeedEvent(ev toolkit.Event) {
	s.ensureFeed()
	s.feedSelectViaKeyboard = ev.Kind == toolkit.EventKeyDown
	s.feed.list.OnEvent(ev)
	s.feedSelectViaKeyboard = false
	s.touch()
}

// feedBannerH is the pinned pixel height of the "needs sign-in" banner stack
// that sits between the topbar and the feed CardList: one banner plus the
// inter-row gap per prompt, and zero with no prompts. The banners do not scroll
// with the cards, so their height is subtracted from the CardList's viewport.
func (s *Scene) feedBannerH() int {
	if len(s.authPrompts) == 0 {
		return 0
	}
	return len(s.authPrompts) * (s.m.bannerH + s.m.cardGap)
}

// feedViewportH is the height available to the feed CardList: the feed column
// between the topbar and the download panel / status bar, less the pinned auth
// banners. It never depends on the card width, so feedCardW can consult
// feedScrollbarShown (which uses it) without recursing.
func (s *Scene) feedViewportH() int {
	h := s.feedBottom() - s.m.topbarH - s.feedBannerH()
	if h < 0 {
		h = 0
	}
	return h
}

// feedListRect is the on-screen rectangle the feed CardList occupies: the feed
// column (scrollbar-gutter clamped), below the topbar and any pinned banners,
// down to the feed bottom. Draw, hit-testing and the a11y tree all read the
// list bounds this places.
func (s *Scene) feedListRect() toolkit.Rect {
	feedX, feedW := s.feedGeom()
	cardW := s.feedCardW(feedW)
	if cardW < 1 {
		cardW = 1
	}
	return toolkit.Rect{X: feedX, Y: s.m.topbarH + s.feedBannerH(), W: cardW, H: s.feedViewportH()}
}

// feedContentH is the total pixel height of every feed card at the current
// content width — the CardList's content extent. It sums the same per-row
// measure the CardList lays out with, so the external scrollbar (and the
// gutter-reserving feedScrollbarShown) tracks the list exactly.
func (s *Scene) feedContentH() int {
	s.ensureFeed()
	total := 0
	for i := range s.feed.display {
		total += s.feedRowHeight(i)
	}
	return total
}

// FeedScrollOffset exposes the feed CardList's current pixel scroll offset. The
// window-app layer, the scrollbar and the a11y tree read it to map feed content
// to screen space.
func (s *Scene) FeedScrollOffset() int {
	s.ensureFeed()
	return s.feed.list.ScrollOffset
}

// FeedWheel routes a device-pixel wheel delta into the feed CardList as a signed
// whole-row scroll (magnitude ≥ 1 for any non-zero nudge, so a small notch still
// advances a card). The CardList owns the scroll anchor and the infinite-scroll
// / pull-to-refresh accumulators, so this is the only feed-wheel path.
func (s *Scene) FeedWheel(dy int) {
	if dy == 0 {
		return
	}
	s.ensureFeed()
	s.layout()
	rows := dy / s.m.rowH
	if rows == 0 {
		if dy > 0 {
			rows = 1
		} else {
			rows = -1
		}
	}
	s.FeedEvent(toolkit.Event{Kind: toolkit.EventScroll, Delta: rows})
}

// FeedClickAt routes a screen-space click into the feed. A click over an
// EXPANDED group card's member row previews that member article directly (its
// own item, like the old member row) and stops there; every other click goes to
// the CardList (selecting the card under the pointer, translating to the list's
// widget-local coordinates). A click above the list origin (e.g. on a pinned
// banner) maps to a negative local Y the CardList treats as no card.
func (s *Scene) FeedClickAt(x, y int) {
	s.ensureFeed()
	if it, ok := s.feedGroupMemberAt(x, y); ok {
		s.feedPreviewItem(it, false)
		return
	}
	b := s.feed.list.Bounds()
	s.FeedEvent(toolkit.Event{Kind: toolkit.EventClick, X: x - b.X, Y: y - b.Y})
}

// feedRowAt maps a screen-space point to the feed display row under it: the row
// index, its display entry, and its exact on-screen rectangle (the same rect
// feedCardRender paints the card into). ok is false when the point is outside
// the list or over no card. It mirrors the CardList's own local-Y → row mapping
// so HitTest, member hit-testing and the a11y tree all agree with what a click
// selects.
func (s *Scene) feedRowAt(x, y int) (int, feedEntry, toolkit.Rect, bool) {
	s.ensureFeed()
	lr := s.feed.list.Bounds()
	if lr.W <= 0 || lr.H <= 0 || !inRect(lr, x, y) {
		return 0, feedEntry{}, toolkit.Rect{}, false
	}
	off := s.FeedScrollOffset()
	contentY := (y - lr.Y) + off
	acc := 0
	for i := range s.feed.display {
		h := s.feedRowHeight(i)
		if contentY >= acc && contentY < acc+h {
			return i, s.feed.display[i], toolkit.Rect{X: lr.X, Y: lr.Y - off + acc, W: lr.W, H: h}, true
		}
		acc += h
	}
	return 0, feedEntry{}, toolkit.Rect{}, false
}

// feedItemAtPoint maps a screen-space point to the feed item under it (a real
// post or a group summary), false when the point is outside the list or over no
// card.
func (s *Scene) feedItemAtPoint(x, y int) (source.Item, bool) {
	i, _, _, ok := s.feedRowAt(x, y)
	if !ok {
		return source.Item{}, false
	}
	return s.feedItemAt(i)
}

// feedGroupHit reports the group-card affordance under a screen point: the
// disclosure chevron (toggle expand/collapse), or — only when the post is
// complete — the download checkbox (queue/cancel) or the "Reconstruct" pill. It
// mirrors the OLD hitGroup: an incomplete post is not Actionable, so its card
// draws neither the checkbox nor the pill and their rects are empty, hence a
// click there falls through to the plain card body (preview). ok is false for a
// non-group row or a click that missed every affordance.
func (s *Scene) feedGroupHit(x, y int) (Hit, bool) {
	_, e, rect, ok := s.feedRowAt(x, y)
	if !ok || e.group == nil {
		return Hit{}, false
	}
	gc := s.feedGroupCard(e.group)
	gc.SetBounds(rect)
	if inRect(gc.ChevronRect(), x, y) {
		return Hit{Kind: HitToggleGroup, Value: e.group.Base}, true
	}
	// CheckRect / ActionRect are empty unless the card is Actionable (complete), so
	// these never fire for an incomplete post — the old "cannot be rebuilt" rule.
	if inRect(gc.CheckRect(), x, y) {
		return Hit{Kind: HitToggleDownload, Value: e.group.Base}, true
	}
	if inRect(gc.ActionRect(), x, y) {
		return Hit{Kind: HitReconstruct, Value: e.group.Base}, true
	}
	return Hit{}, false
}

// feedGroupMemberAt returns the member article under a screen point when that
// point falls on an EXPANDED group card's member row, false otherwise. It lets a
// click on a listed part preview that specific article (the old member-row
// behaviour) instead of previewing the whole post.
func (s *Scene) feedGroupMemberAt(x, y int) (source.Item, bool) {
	_, e, rect, ok := s.feedRowAt(x, y)
	if !ok || e.group == nil || !s.GroupExpanded(e.group.Base) {
		return source.Item{}, false
	}
	gc := s.feedGroupCard(e.group)
	gc.SetBounds(rect)
	for i, mem := range e.group.Members {
		if inRect(gc.MemberRect(i), x, y) {
			return mem.Item, true
		}
	}
	return source.Item{}, false
}
