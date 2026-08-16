package ui

import (
	"image"
	"strings"
	"testing"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
	"github.com/go-widgets/toolkit/virtual"

	"github.com/go-news-reader/reader/source"
)

// feedTheme is the palette every feed-card test renders against.
func feedTheme() *toolkit.Theme { return ThemeFor(OSLinux, false) }

// usenetGroupItems returns two same-base Usenet part items that groupItems
// collapses into one multipart group (so the feed shows a group summary card).
func usenetGroupItems() []source.Item {
	return []source.Item{
		{ID: "a1", Source: source.Usenet, Channel: "alt.bin", Created: 1000,
			Title: `[1/2] - "rel.part1.rar" yEnc (1/1) 100`},
		{ID: "a2", Source: source.Usenet, Channel: "alt.bin", Created: 2000,
			Title: `[2/2] - "rel.part2.rar" yEnc (1/1) 100`},
	}
}

// TestFeedModelOldestToNewest checks the feed model is the filtered feed
// reversed (newest last), and that the CardList row count matches.
func TestFeedModelOldestToNewest(t *testing.T) {
	s := New(900, 600, feedTheme())
	s.SetItems([]source.Item{
		{ID: "new", Source: source.Reddit, Title: "newest", Score: -1, Comments: -1},
		{ID: "old", Source: source.Reddit, Title: "oldest", Score: -1, Comments: -1},
	})
	s.layout()
	got := s.FeedModelItems()
	if len(got) != 2 {
		t.Fatalf("model len = %d, want 2", len(got))
	}
	if got[0].ID != "old" || got[1].ID != "new" {
		t.Fatalf("order = %q,%q; want old,new (oldest→newest)", got[0].ID, got[1].ID)
	}
	if n := s.FeedCardList().Model.Len(); n != 2 {
		t.Fatalf("cardlist model len = %d, want 2", n)
	}
}

// TestFeedEntryItem maps a display entry to the source.Item it renders as: the
// post itself for a standalone entry, the synthesised summary item for a group.
func TestFeedEntryItem(t *testing.T) {
	s := New(900, 600, feedTheme())
	it := source.Item{ID: "p", Source: source.Reddit, Title: "post"}
	if got := s.feedEntryItem(feedEntry{item: it}); got.ID != "p" {
		t.Fatalf("item entry mapped to %+v, want p", got)
	}
	g := newGroup("rel", usenetGroupItems())
	if got := s.feedEntryItem(feedEntry{group: g}); got.Source != source.Usenet || got.ID != "rel" {
		t.Fatalf("group entry mapped to %+v, want the rel summary", got)
	}
}

// TestFeedPostCardMapping proves feedPostCard maps a source.Item onto the
// toolkit.PostCard fields the reader's card is built from: the source pill (label
// + colour + contrasting ink), the channel subtitle, the headline, the meta line,
// the per-element device fonts, the logical thumbnail bases, and the wrapped-line
// cap. It also covers cardMaxLines' two branches (a titled item vs a body-only
// post) and the thumbnail wiring (absent, then present).
func TestFeedPostCardMapping(t *testing.T) {
	s := New(900, 600, feedTheme())
	s.SetScale(1)
	s.layout()

	it := source.Item{ID: "p", Source: source.Reddit, Channel: "r/go",
		Title: "a headline", Author: "amy", Score: 5, Comments: 2}
	pc := s.feedPostCard(it)

	if pc.Pill != sourceLabel(source.Reddit) {
		t.Fatalf("Pill = %q, want %q", pc.Pill, sourceLabel(source.Reddit))
	}
	if pc.PillColor != sourceColor(source.Reddit) {
		t.Fatalf("PillColor = %v, want the source colour", pc.PillColor)
	}
	if pc.PillInk != onAccentFor(sourceColor(source.Reddit)) {
		t.Fatalf("PillInk = %v, want the on-accent ink", pc.PillInk)
	}
	if pc.Subtitle != "r/go" {
		t.Fatalf("Subtitle = %q, want the channel", pc.Subtitle)
	}
	if pc.Title != cardText(it) {
		t.Fatalf("Title = %q, want cardText %q", pc.Title, cardText(it))
	}
	if pc.Meta != metaLine(it) {
		t.Fatalf("Meta = %q, want metaLine %q", pc.Meta, metaLine(it))
	}
	// Thumbnail bases are the LOGICAL values (PostCard scales them itself); passing
	// rpx here would double-scale the column.
	if pc.ThumbW != 104 || pc.ThumbH != 60 {
		t.Fatalf("Thumb bases = %d×%d, want the logical 104×60", pc.ThumbW, pc.ThumbH)
	}
	// A titled item keeps the historical three-line cap.
	if pc.MaxTitleLines != cardTitleMaxLines {
		t.Fatalf("titled MaxTitleLines = %d, want %d", pc.MaxTitleLines, cardTitleMaxLines)
	}
	// Fonts are the device-pixel faces (ttFont at rpx), not left nil for PostCard to
	// scale: the height a face reports must match the reader's rpx face.
	if pc.TitleFont == nil || pc.TitleFont.Height() != ttFont(true, rpxOf(s, 15)).Height() {
		t.Fatal("TitleFont should be the device bold 15px face")
	}
	if pc.SubtitleFont == nil || pc.MetaFont == nil || pc.PillFont == nil {
		t.Fatal("subtitle/meta/pill fonts must all be set to device faces")
	}
	// No thumbnail cached yet (Thumbs nil) → no image.
	if pc.Thumbnail != nil {
		t.Fatal("Thumbnail should be nil before one is cached")
	}

	// A body-only post (no title) gets the higher body cap (cardMaxLines' other
	// branch).
	body := source.Item{ID: "b", Source: source.Mastodon, Body: "just a toot"}
	if bp := s.feedPostCard(body); bp.MaxTitleLines != cardBodyMaxLines {
		t.Fatalf("body-only MaxTitleLines = %d, want %d", bp.MaxTitleLines, cardBodyMaxLines)
	}

	// Once a thumbnail is cached for the id, feedPostCard wires it in.
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	s.SetThumb("p", img)
	if pc2 := s.feedPostCard(it); pc2.Thumbnail != img {
		t.Fatal("a cached thumbnail should be wired onto the PostCard")
	}
}

// postCardExpectedH re-derives a PostCard's exact height at the current global
// metric scale straight from the toolkit's public geometry — the SAME logical
// metrics PostCard scales (CardPadX/Y, CardGapX/Y, CardLineSpacing, BadgePadY,
// the 104×60 thumbnail bases) routed once through toolkit.Scaled, over the SAME
// device faces feedPostCard passes. It is the independent yardstick the
// anti-double-scaling test measures feedPostCard against: metrics scaled once,
// fonts already at device size. It assumes a single wrapped title line (the test
// feeds short titles), matching PostCard's per-line slotting.
func postCardExpectedH(s *Scene, it source.Item, hasThumb bool) int {
	sc := toolkit.Scaled
	pill := ttFont(true, rpxOf(s, 10))
	sub := ttFont(false, rpxOf(s, 12))
	title := ttFont(true, rpxOf(s, 15))
	meta := ttFont(false, rpxOf(s, 12))

	badgeRowH := 0
	if pc := pill.Height() + 2*sc(toolkit.BadgePadY); it.Channel != "" || pc > 0 {
		badgeRowH = pc
	}
	if it.Channel != "" && sub.Height() > badgeRowH {
		badgeRowH = sub.Height()
	}
	titleSlot := title.Height() + sc(toolkit.CardLineSpacing)
	metaH := 0
	if metaLine(it) != "" {
		metaH = meta.Height()
	}
	// One title line for a short headline.
	content := badgeRowH + titleSlot + sc(toolkit.CardGapY) + metaH
	if hasThumb {
		if th := sc(60); th > content {
			content = th
		}
	}
	return 2*sc(toolkit.CardPadY) + content
}

// TestFeedPostCardHeightNoDoubleScaling is the anti-double-scaling proof: at
// scale 1 AND scale 2 (with SetMetricScale mirroring the scene scale), a card's
// measured height equals the height independently derived from the SAME device
// fonts and the SAME logical metrics scaled EXACTLY ONCE (postCardExpectedH). A
// double-scaled metric (e.g. a thumbnail column passed in rpx, or a padding
// counted twice) would push the scale-2 height well past this yardstick. The
// no-thumb and with-thumb cases both hold, and the scale-2 height lands near 2×
// the scale-1 height — the signature of a single, linear scaling.
func TestFeedPostCardHeightNoDoubleScaling(t *testing.T) {
	s := New(900, 600, feedTheme())
	const w = 420

	// A short, single-line headline with a channel and a meta line, with and
	// without a cached thumbnail.
	noThumb := source.Item{ID: "n", Source: source.Reddit, Channel: "r/go",
		Title: "short title", Author: "amy", Score: 3, Comments: 1}
	withThumb := noThumb
	withThumb.ID = "y"
	s.SetThumb("y", image.NewRGBA(image.Rect(0, 0, 8, 8)))

	measure := func(scale float64, it source.Item) (got, want int) {
		s.SetScale(scale)
		s.layout()
		pc := s.feedPostCard(it) // pins the metric scale to s.Scale
		return pc.Measure(w), postCardExpectedH(s, it, pc.Thumbnail != nil)
	}

	for _, tc := range []struct {
		name string
		it   source.Item
	}{{"no-thumb", noThumb}, {"with-thumb", withThumb}} {
		got1, want1 := measure(1, tc.it)
		if got1 != want1 {
			t.Fatalf("%s scale 1: Measure=%d, derived=%d", tc.name, got1, want1)
		}
		got2, want2 := measure(2, tc.it)
		if got2 != want2 {
			t.Fatalf("%s scale 2: Measure=%d, derived=%d (double-scaling?)", tc.name, got2, want2)
		}
		// Scale-2 is ~2× scale-1: a single linear scaling, not a squared one. Fonts
		// round independently of the metrics, so allow a small slack.
		if lo, hi := got1*2-6, got1*2+6; got2 < lo || got2 > hi {
			t.Fatalf("%s: scale-2 height %d not ≈ 2×%d (=%d±6); double-scaled?", tc.name, got2, got1, got1*2)
		}
	}
}

// TestFeedThumbInvalidation proves a thumbnail landing after the row was measured
// re-keys the height memo and re-sets the row in the model, so the CardList
// re-measures the card at the height that reserves (and now fills) the thumbnail.
func TestFeedThumbInvalidation(t *testing.T) {
	s := New(900, 600, feedTheme())
	s.SetScale(1)
	// A no-op before the feed exists.
	s.invalidateFeedThumb("nope")

	s.SetItems([]source.Item{{ID: "pic", Source: source.Reddit, Title: "pic",
		Media: []source.Media{{Kind: source.MediaImage}}, Score: -1, Comments: -1}})
	s.layout()

	e := s.feed.display[0]
	before := s.feedHeightKey(e)
	if !strings.HasSuffix(before, "\x00-") {
		t.Fatalf("pre-thumb key %q should mark the thumbnail absent", before)
	}
	// Prime the memo, then land the thumbnail.
	_ = s.feedRowHeight(0)
	s.SetThumb("pic", image.NewRGBA(image.Rect(0, 0, 4, 4)))
	after := s.feedHeightKey(s.feed.display[0])
	if !strings.HasSuffix(after, "\x00T") {
		t.Fatalf("post-thumb key %q should mark the thumbnail present", after)
	}
	if _, stale := s.feed.heights[before]; stale {
		t.Fatal("the pre-thumb height entry should have been dropped")
	}
	// An id backing no row is a harmless no-op.
	s.invalidateFeedThumb("absent-id")
}

// TestFeedThumbnailAppearsOnDraw drives the whole feed-draw pipeline: a card
// declaring media renders a placeholder before its image lands, and once SetThumb
// delivers a decoded image the very next Draw blits it into the card — proving
// the thumbnail-invalidation path re-lays-out and re-paints the card with the
// image rather than keeping the placeholder.
func TestFeedThumbnailAppearsOnDraw(t *testing.T) {
	s := New(1000, 700, ThemeFor(OSMac, false))
	s.SetScale(1)
	s.SetSubs(nil)
	s.SetItems([]source.Item{{ID: "pic", Source: source.Reddit, Channel: "chan",
		Title: "with an image", Media: []source.Media{{Kind: source.MediaImage}},
		Score: -1, Comments: -1}})

	// The distinctive colour the thumbnail is filled with — chosen so it cannot be
	// confused with any theme surface / text ink.
	const cr, cg, cb = 0x11, 0xF0, 0x22
	has := func(buf []byte) bool {
		for i := 0; i+3 < len(buf); i += 4 {
			if buf[i] == cr && buf[i+1] == cg && buf[i+2] == cb {
				return true
			}
		}
		return false
	}

	buf0 := make([]byte, s.W*s.H*4)
	s.Draw(buf0) // placeholder: no thumbnail yet
	if has(buf0) {
		t.Fatal("the thumbnail colour must not appear before SetThumb")
	}

	thumb := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for i := 0; i < len(thumb.Pix); i += 4 {
		thumb.Pix[i], thumb.Pix[i+1], thumb.Pix[i+2], thumb.Pix[i+3] = cr, cg, cb, 0xFF
	}
	s.SetThumb("pic", thumb)

	buf1 := make([]byte, s.W*s.H*4)
	s.Draw(buf1)
	if !has(buf1) {
		t.Fatal("the decoded thumbnail should be blitted into the card after SetThumb")
	}
}

func TestGroupSummaryItem(t *testing.T) {
	g := newGroup("rel", usenetGroupItems())
	it := groupSummaryItem(g)
	if it.ID != "rel" || it.Source != source.Usenet || it.Channel != "alt.bin" {
		t.Fatalf("summary item = %+v", it)
	}
	if len(it.Media) != 1 || it.Media[0].Kind != source.MediaImage {
		t.Fatal("summary item should declare an image for the preview box")
	}
	// A memberless group synthesises no channel (guard branch).
	if em := groupSummaryItem(&itemGroup{Base: "y"}); em.Channel != "" {
		t.Fatalf("memberless summary channel = %q", em.Channel)
	}
}

func TestFeedRowHeightAndRender(t *testing.T) {
	s := New(900, 600, feedTheme())
	s.SetItems([]source.Item{{ID: "1", Source: source.Reddit, Title: "hi", Score: -1, Comments: -1}})
	s.layout()

	if h := s.feedRowHeight(0); h <= 0 {
		t.Fatalf("row height = %d, want > 0", h)
	}
	// Out-of-range rows measure to zero.
	if h := s.feedRowHeight(-1); h != 0 {
		t.Fatalf("oob row height = %d, want 0", h)
	}
	if h := s.feedRowHeight(99); h != 0 {
		t.Fatalf("oob row height = %d, want 0", h)
	}
	// A zero width clamps to 1 rather than measuring negative.
	s.feed.width = 0
	if h := s.feedRowHeight(0); h <= 0 {
		t.Fatalf("row height at width 0 = %d, want > 0", h)
	}

	// feedCardRender: the valid path paints; an out-of-range index is a no-op.
	buf := make([]byte, 300*200*4)
	p := painter.NewPixelPainter(buf, 300, 200)
	s.feedCardRender(p, s.theme, toolkit.Rect{X: 0, Y: 0, W: 300, H: 90}, 0, source.Item{}, virtual.CardState{})
	s.feedCardRender(p, s.theme, toolkit.Rect{}, -1, source.Item{}, virtual.CardState{}) // early return

	// Drawing the CardList itself exercises its CardRender wrapper (→ feedCardRender).
	list := s.FeedCardList()
	list.SetBounds(toolkit.Rect{X: 0, Y: 0, W: 300, H: 180})
	list.Draw(p, s.theme)
}

func TestFeedItemAtAndDimmed(t *testing.T) {
	s := New(900, 600, feedTheme())
	s.SetItems(append(usenetGroupItems(),
		source.Item{ID: "z", Source: source.Reddit, Title: "plain", Score: -1, Comments: -1}))
	s.layout()

	// Input order [group-parts, z] reverses to [z, group]: the reddit post is row
	// 0, the synthesised group summary is the last row.
	if it, ok := s.feedItemAt(0); !ok || it.ID != "z" || it.Source != source.Reddit {
		t.Fatalf("row 0 = %+v ok=%v, want reddit z", it, ok)
	}
	last := s.FeedCardList().Model.Len() - 1
	if it, ok := s.feedItemAt(last); !ok || it.Source != source.Usenet || it.ID == "" {
		t.Fatalf("last row = %+v ok=%v, want a group summary", it, ok)
	}
	if _, ok := s.feedItemAt(-1); ok {
		t.Fatal("oob feedItemAt should be !ok")
	}
	// No read state yet: nothing is dimmed.
	if s.feedDimmed(0) {
		t.Fatal("feedDimmed should be false")
	}
}

func TestFeedSelectAndActivate(t *testing.T) {
	s := New(900, 600, feedTheme())
	items := []source.Item{
		{ID: "0", Source: source.Reddit, Title: "a", Score: -1, Comments: -1},
		{ID: "1", Source: source.Reddit, Title: "b", Score: -1, Comments: -1},
	}
	s.SetItems(items)
	s.layout()

	// Input [item0,item1] reverses to [item1,item0]: row 0 is "1", row 1 is "0".
	// Without a hook, selection updates the scene preview directly.
	s.feedSelect(0)
	if it, ok := s.PreviewItem(); !ok || it.ID != "1" {
		t.Fatalf("preview after select = %+v ok=%v, want 1", it, ok)
	}
	// Out-of-range select is a no-op.
	s.feedSelect(99)

	// With a hook installed, selection routes to it (carrying the keyboard flag).
	var gotID string
	var gotKb bool
	s.SetFeedSelectHook(func(it source.Item, kb bool) { gotID, gotKb = it.ID, kb })
	s.feedSelectViaKeyboard = true
	s.feedSelect(1)
	s.feedSelectViaKeyboard = false
	if gotID != "0" || !gotKb {
		t.Fatalf("hook got %q kb=%v, want 0 true", gotID, gotKb)
	}

	// Activate opens the reading view.
	s.feedActivate(0)
	if s.Mode() != ModeDetail {
		t.Fatalf("mode after activate = %v, want ModeDetail", s.Mode())
	}
	s.feedActivate(99) // no-op, no panic
}

func TestFeedEventRouting(t *testing.T) {
	s := New(900, 600, feedTheme())
	items := make([]source.Item, 40)
	for i := range items {
		items[i] = source.Item{ID: string(rune('a' + i%26)), Source: source.Reddit, Title: "row", Score: -1, Comments: -1}
	}
	s.SetItems(items)
	s.layout()

	var kb bool
	var selected bool
	s.SetFeedSelectHook(func(_ source.Item, viaKeyboard bool) { selected, kb = true, viaKeyboard })

	// A keyboard selection key routes through with the keyboard flag set.
	s.FeedEvent(toolkit.Event{Kind: toolkit.EventKeyDown, Code: "ArrowDown"})
	if !selected || !kb {
		t.Fatalf("keydown routing: selected=%v kb=%v, want true true", selected, kb)
	}

	// A click routes through with the keyboard flag clear.
	selected, kb = false, false
	s.FeedEvent(toolkit.Event{Kind: toolkit.EventClick, Y: 5})
	if !selected || kb {
		t.Fatalf("click routing: selected=%v kb=%v, want true false", selected, kb)
	}

	// A wheel scroll moves the CardList offset (the prior arrow/click left it at
	// the top, so scroll DOWN to have room to move).
	before := s.FeedCardList().ScrollOffset
	s.FeedEvent(toolkit.Event{Kind: toolkit.EventScroll, Delta: 5})
	if s.FeedCardList().ScrollOffset == before {
		t.Fatal("wheel scroll should change the offset")
	}

	// Enter activates the selected card (→ OnActivate → OpenDetail).
	s.FeedEvent(toolkit.Event{Kind: toolkit.EventKeyDown, Code: "Enter"})
	if s.Mode() != ModeDetail {
		t.Fatalf("Enter should open the reading view, mode = %v", s.Mode())
	}
}

func TestFeedReachSeamsAndFetching(t *testing.T) {
	s := New(900, 600, feedTheme())
	s.SetItems([]source.Item{{ID: "1", Source: source.Reddit, Title: "x", Score: -1, Comments: -1}})
	s.layout()
	list := s.FeedCardList()

	// Nil-seam guards: firing the reach callbacks without hooks must not panic.
	s.SetInfiniteScroll(true)
	list.OnReachTop()
	list.OnReachBottom()

	// Load-older (OnReachTop) is gated by infinite scroll: off → silent even with a
	// hook installed; refresh (OnReachBottom) is always on.
	older, refresh := false, false
	s.OnReachBottom = func() { older = true }
	s.OnPullRefresh = func() { refresh = true }
	s.SetInfiniteScroll(false)
	list.OnReachTop()
	if older {
		t.Fatal("load-older fired with infinite scroll off")
	}
	// Installed seams with infinite scroll on: OnReachTop loads older (OnReachBottom
	// seam), OnReachBottom refreshes (OnPullRefresh seam).
	s.SetInfiniteScroll(true)
	list.OnReachTop()
	list.OnReachBottom()
	if !older || !refresh {
		t.Fatalf("reach seams: older=%v refresh=%v, want true true", older, refresh)
	}
	// Dimmed callback wired.
	if list.Dimmed(0) {
		t.Fatal("dimmed callback should report false")
	}

	// Loading mirrors onto the bottom pull-to-fetch strip + label.
	s.SetLoading(true, 0, 2)
	s.layout()
	if !list.FetchingBottom || list.BottomLabel == "" {
		t.Fatalf("loading strip = %v %q, want fetching + label", list.FetchingBottom, list.BottomLabel)
	}
	s.SetLoading(false, 2, 2)
	s.layout()
	if list.FetchingBottom || list.BottomLabel != "" {
		t.Fatal("cleared loading should idle the strip")
	}
}

func TestFeedAnimationAndScrollToBottom(t *testing.T) {
	s := New(900, 600, feedTheme())
	items := make([]source.Item, 50)
	for i := range items {
		items[i] = source.Item{ID: string(rune('0' + i%10)), Source: source.Reddit, Title: "t", Score: -1, Comments: -1}
	}
	s.SetItems(items)
	s.layout()
	list := s.FeedCardList()

	// Idle: no animation.
	if s.FeedAnimating() {
		t.Fatal("idle feed should not animate")
	}
	s.FeedTick(0.1) // safe no-op tick

	// A fetch in flight animates and a tick advances the spinner.
	s.SetLoading(true, 0, 1)
	s.layout()
	if !s.FeedAnimating() {
		t.Fatal("fetching feed should animate")
	}
	s.FeedTick(0.5)

	// ScrollToBottom / FeedScrollToBottom open at the newest post.
	list.ScrollTo(0)
	s.FeedScrollToBottom()
	if list.ScrollOffset == 0 {
		t.Fatal("FeedScrollToBottom should move off the top on an overflowing feed")
	}
}

func TestSyncFeedModelEdits(t *testing.T) {
	s := New(900, 600, feedTheme())
	s.ensureFeed()
	mk := func(id string) source.Item {
		return source.Item{ID: id, Source: source.Reddit, Title: id, Score: -1, Comments: -1}
	}
	ids := func() []string {
		var out []string
		for _, it := range s.feed.model.Slice() {
			out = append(out, it.ID)
		}
		return out
	}
	eq := func(want ...string) {
		t.Helper()
		got := ids()
		if len(got) != len(want) {
			t.Fatalf("model = %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("model = %v, want %v", got, want)
			}
		}
	}

	// Seed the model (full replace via Clear+Append: empty → non-empty).
	s.syncFeedModel([]source.Item{mk("A"), mk("B"), mk("C")})
	eq("A", "B", "C")

	// Partial edit: shared prefix A and suffix C, middle B → X,Y (RemoveAt + Insert).
	s.syncFeedModel([]source.Item{mk("A"), mk("X"), mk("Y"), mk("C")})
	eq("A", "X", "Y", "C")

	// Shared prefix only: append at the end grows in place.
	s.syncFeedModel([]source.Item{mk("A"), mk("X"), mk("Y"), mk("C"), mk("Z")})
	eq("A", "X", "Y", "C", "Z")

	// Full replace (no shared end) → Clear+Append.
	s.syncFeedModel([]source.Item{mk("P"), mk("Q")})
	eq("P", "Q")

	// Empty next → Clear only (no Append branch).
	s.syncFeedModel(nil)
	if s.feed.model.Len() != 0 {
		t.Fatalf("model should be empty, got %v", ids())
	}
}

// TestFeedClickAt proves a screen-space click routes into the CardList (via the
// list's widget-local coordinates), selecting the card under the pointer and
// firing the feed select hook with the keyboard flag clear.
func TestFeedClickAt(t *testing.T) {
	s := New(900, 600, feedTheme())
	items := make([]source.Item, 6)
	for i := range items {
		items[i] = source.Item{ID: string(rune('a' + i)), Source: source.Reddit, Title: "row", Score: -1, Comments: -1}
	}
	s.SetItems(items)
	s.layout()

	var gotID string
	var kb bool
	sawSelect := false
	s.SetFeedSelectHook(func(it source.Item, viaKeyboard bool) { gotID, kb, sawSelect = it.ID, viaKeyboard, true })

	// Click the centre of the first display row (through screen coords).
	lr := s.FeedCardList().Bounds()
	want, _ := s.feedItemAt(0)
	s.FeedClickAt(lr.X+lr.W/2, lr.Y+s.feedRowHeight(0)/2)
	if !sawSelect || kb {
		t.Fatalf("click hook: selected=%v kb=%v, want true false", sawSelect, kb)
	}
	if gotID != want.ID {
		t.Fatalf("click selected %q, want the first row %q", gotID, want.ID)
	}
	if s.FeedCardList().Selected != 0 {
		t.Fatalf("CardList.Selected = %d, want 0", s.FeedCardList().Selected)
	}
	// A click above the list origin (a pinned-banner row) maps to a negative local
	// Y the CardList treats as no card — the selection does not move.
	s.FeedClickAt(lr.X+lr.W/2, lr.Y-100)
	if s.FeedCardList().Selected != 0 {
		t.Fatalf("out-of-list click moved the selection to %d", s.FeedCardList().Selected)
	}
}

// TestFeedWheelDirections covers FeedWheel's small-nudge min-magnitude branches
// in both directions and the zero-delta no-op.
func TestFeedWheelDirections(t *testing.T) {
	s := New(400, 300, feedTheme())
	items := make([]source.Item, 60)
	for i := range items {
		items[i] = source.Item{ID: string(rune('a'+i%26)) + itoa(i), Source: source.Reddit, Title: "t", Score: -1, Comments: -1}
	}
	s.SetItems(items)
	s.layout()

	// Scroll to a middle position first (away from both ends).
	s.FeedCardList().ScrollTo(200)
	at := s.FeedScrollOffset()
	// A one-pixel-up nudge advances one row toward the top (the rows==0 → -1 branch).
	s.FeedWheel(-1)
	if s.FeedScrollOffset() >= at {
		t.Fatalf("small up nudge did not move toward the top: %d -> %d", at, s.FeedScrollOffset())
	}
	// A one-pixel-down nudge advances one row toward the bottom (the +1 branch).
	at = s.FeedScrollOffset()
	s.FeedWheel(1)
	if s.FeedScrollOffset() <= at {
		t.Fatalf("small down nudge did not move toward the bottom: %d -> %d", at, s.FeedScrollOffset())
	}
	// A zero delta is a no-op.
	at = s.FeedScrollOffset()
	s.FeedWheel(0)
	if s.FeedScrollOffset() != at {
		t.Fatalf("zero wheel changed the offset: %d -> %d", at, s.FeedScrollOffset())
	}
}

func TestEnsureFeedIdempotent(t *testing.T) {
	s := New(900, 600, feedTheme())
	s.ensureFeed()
	first := s.feed
	s.ensureFeed()
	if s.feed != first {
		t.Fatal("ensureFeed must not rebuild the feed")
	}
}
