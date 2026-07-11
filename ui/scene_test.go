package ui

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"

	"github.com/go-widgets/toolkit"

	"github.com/go-news-reader/reader/source"
)

func sampleItems() []source.Item {
	return []source.Item{
		{ID: "1", Source: source.Reddit, Channel: "golang", Title: "Go 2 is here", Author: "gopher", Score: 42, Comments: 7, Media: []source.Media{{Kind: source.MediaImage, URL: "x"}}},
		{ID: "2", Source: source.HackerNews, Channel: "", Title: "Show HN: a thing", Author: "pg", Score: 100, Comments: -1},
		{ID: "3", Source: source.Mastodon, Channel: "@a@m", Title: "toot toot", Author: "a@m", Score: -1, Comments: -1},
	}
}

func sampleSubs() []Subscription {
	return []Subscription{
		{Source: source.Reddit, Channel: "golang", Label: "r/golang"},
		{Source: source.HackerNews},
	}
}

func newScene() *Scene {
	s := New(900, 600, ThemeFor(OSLinux, false)) // adwaita has Extra["OnAccent"]
	s.SetItems(sampleItems())
	s.SetSubs(sampleSubs())
	return s
}

func px(buf []byte, w, x, y int) color.RGBA {
	i := (y*w + x) * 4
	return color.RGBA{buf[i], buf[i+1], buf[i+2], buf[i+3]}
}

func TestNewClampAndScale(t *testing.T) {
	s := New(10, 10, nil) // below minimums; nil theme -> default
	if s.W != MinW || s.H != MinH {
		t.Fatalf("clamp = %dx%d", s.W, s.H)
	}
	if s.theme == nil {
		t.Fatal("nil theme not defaulted")
	}
	// clampSize Scale==0 branch via a zero-value scene.
	z := &Scene{W: 500, H: 400}
	z.clampSize()
	if z.Scale != 1 {
		t.Fatalf("scale not defaulted: %v", z.Scale)
	}
	s.SetScale(10)
	if s.Scale != MaxZoom {
		t.Fatalf("scale hi = %v", s.Scale)
	}
	s.SetScale(0.1)
	if s.Scale != MinZoom {
		t.Fatalf("scale lo = %v", s.Scale)
	}
	s.SetScale(1.5)
	if s.Scale != 1.5 {
		t.Fatalf("scale mid = %v", s.Scale)
	}
}

func TestSetters(t *testing.T) {
	s := newScene()
	s.ScrollY = 50
	s.SetItems(nil)
	if s.ScrollY != 0 {
		t.Fatal("SetItems should reset scroll")
	}
	s.SetTheme(nil) // no-op
	if s.theme == nil {
		t.Fatal("SetTheme(nil) cleared theme")
	}
	dark := ThemeFor(OSMac, true)
	s.SetTheme(dark)
	if s.theme != dark {
		t.Fatal("SetTheme not applied")
	}
	s.Resize(1000, 700)
	if s.W != 1000 || s.H != 700 {
		t.Fatalf("resize = %dx%d", s.W, s.H)
	}
}

func TestSearchEditing(t *testing.T) {
	s := newScene()
	if s.SearchFocused() {
		t.Fatal("focused by default")
	}
	s.TypeRune('a') // ignored, not focused
	if s.Search() != "" {
		t.Fatal("typed while unfocused")
	}
	s.FocusSearch(true)
	s.TypeRune('g')
	s.TypeRune('o')
	if s.Search() != "go" {
		t.Fatalf("search = %q", s.Search())
	}
	s.Backspace()
	if s.Search() != "g" {
		t.Fatalf("after bs = %q", s.Search())
	}
	s.SetSearch("")
	s.Backspace() // focused but empty
	if s.Search() != "" {
		t.Fatal("bs on empty")
	}
	s.FocusSearch(false)
	s.Backspace() // unfocused
	s.SetSearch("x")
	s.Backspace()
	if s.Search() != "x" {
		t.Fatal("bs while unfocused changed text")
	}
}

func TestScrollClamp(t *testing.T) {
	s := newScene()
	// Short content: max < 0, scroll pinned to 0.
	s.SetItems(sampleItems()[:1])
	s.Scroll(100)
	if s.ScrollY != 0 {
		t.Fatalf("short content scroll = %d", s.ScrollY)
	}
	// Tall content: many items so contentH exceeds viewport.
	many := make([]source.Item, 40)
	for i := range many {
		many[i] = source.Item{ID: string(rune('a' + i%26)), Source: source.Reddit, Title: "t", Score: -1, Comments: -1}
	}
	s.SetItems(many)
	s.Scroll(100000) // clamp to max
	if s.ScrollY <= 0 {
		t.Fatalf("expected positive clamped scroll, got %d", s.ScrollY)
	}
	max := s.ScrollY
	s.Scroll(-100000) // clamp to 0
	if s.ScrollY != 0 {
		t.Fatalf("neg clamp = %d", s.ScrollY)
	}
	s.Scroll(max / 2) // within range
	if s.ScrollY != max/2 {
		t.Fatalf("mid scroll = %d want %d", s.ScrollY, max/2)
	}
}

func TestSubscriptionName(t *testing.T) {
	if (Subscription{Label: "L", Channel: "C", Source: source.Reddit}).name() != "L" {
		t.Fatal("label")
	}
	if (Subscription{Channel: "C", Source: source.Reddit}).name() != "C" {
		t.Fatal("channel")
	}
	if (Subscription{Source: source.Bluesky}).name() != "Bluesky" {
		t.Fatal("source fallback")
	}
}

func TestFiltered(t *testing.T) {
	s := newScene()
	// All + no search => all 3.
	if got := len(s.filtered()); got != 3 {
		t.Fatalf("all = %d", got)
	}
	// Sub 0 (reddit/golang) => 1.
	s.Active = 0
	if got := s.filtered(); len(got) != 1 || got[0].ID != "1" {
		t.Fatalf("reddit filter = %v", got)
	}
	// Sub source match but channel mismatch => 0.
	s.Subs = []Subscription{{Source: source.Reddit, Channel: "rust"}}
	s.Active = 0
	if got := len(s.filtered()); got != 0 {
		t.Fatalf("channel-mismatch = %d", got)
	}
	// Search substring on title (All).
	s.Active = AllFilter
	s.SetSearch("show")
	if got := s.filtered(); len(got) != 1 || got[0].ID != "2" {
		t.Fatalf("search = %v", got)
	}
}

func renderPNG(t *testing.T, s *Scene, name string) []byte {
	t.Helper()
	buf := make([]byte, s.W*s.H*4)
	s.Draw(buf)
	img := &image.RGBA{Pix: buf, Stride: s.W * 4, Rect: image.Rect(0, 0, s.W, s.H)}
	if dir := os.Getenv("UI_SNAPSHOT_DIR"); dir != "" {
		f, err := os.Create(dir + "/" + name + ".png")
		if err == nil {
			png.Encode(f, img)
			f.Close()
		}
	}
	return buf
}

func TestDrawSmoke(t *testing.T) {
	s := newScene()
	buf := renderPNG(t, s, "feed")
	// Topbar is an accent fill; sample a strip clear of title/search text.
	acc := s.theme.Accent
	got := px(buf, s.W, s.m.sidebarW-6, 4)
	if got.R != acc.R || got.G != acc.G || got.B != acc.B {
		t.Fatalf("topbar pixel = %v, want accent %v", got, acc)
	}
	// A hit on the first card returns its item.
	h := s.HitTest(s.m.sidebarW+40, s.m.topbarH+s.m.pad+s.m.rowH/2)
	if h.Kind != HitItem || h.Item.ID != "1" {
		t.Fatalf("card hit = %+v", h)
	}
}

func TestDrawThumbnailsAndScroll(t *testing.T) {
	s := newScene()
	// Provide a decoded thumbnail for item 1; item's Media triggers the thumb slot.
	th := image.NewRGBA(image.Rect(0, 0, 200, 200)) // larger than slot -> blit clips
	for i := range th.Pix {
		th.Pix[i] = 0x80
	}
	s.Thumbs = map[string]*image.RGBA{"1": th}
	s.Scroll(5) // exercise the scrolled branch
	renderPNG(t, s, "thumbs")
	// Media present but no Thumbs entry -> placeholder label path.
	s.Thumbs = map[string]*image.RGBA{}
	renderPNG(t, s, "placeholder")

	// Tall feed scrolled to the bottom: early rows fall above the viewport and
	// are skipped (the offscreen-continue branch).
	tall := New(700, 400, ThemeFor(OSMac, false))
	many := make([]source.Item, 40)
	for i := range many {
		many[i] = source.Item{ID: string(rune('a' + i%26)), Source: source.Reddit, Title: "t", Score: -1, Comments: -1}
	}
	tall.SetItems(many)
	tall.Scroll(1 << 20) // to max
	renderPNG(t, tall, "scrolled")
}

func TestDrawNoItemsAndStatus(t *testing.T) {
	s := New(700, 500, ThemeFor(OSMac, false)) // WhiteSur: no Extra OnAccent (absent branch)
	s.Status = "Loading…"
	buf := renderPNG(t, s, "empty")
	if len(buf) == 0 {
		t.Fatal("no buffer")
	}
}

func TestDrawActiveAndSearchStates(t *testing.T) {
	s := newScene()
	s.Active = 0 // a subscription selected -> sub highlight branch
	renderPNG(t, s, "active-sub")
	s.Active = AllFilter // All highlighted
	s.FocusSearch(true)
	s.SetSearch("go")
	renderPNG(t, s, "search-focused")
}

func TestHitTest(t *testing.T) {
	s := newScene()
	m := s.computeMetrics()
	// Search field.
	if s.HitTest(m.sidebarW+m.pad+5, m.topbarH/2).Kind != HitSearch {
		t.Fatal("search hit")
	}
	// Topbar, not on search (over the title area).
	if s.HitTest(5, m.topbarH/2).Kind != HitNone {
		t.Fatal("topbar none")
	}
	// Sidebar "All".
	if h := s.HitTest(10, m.topbarH+m.sideItemH/2); h.Kind != HitSub || h.Sub != AllFilter {
		t.Fatalf("all hit = %+v", h)
	}
	// Sidebar first subscription.
	if h := s.HitTest(10, m.topbarH+m.sideItemH+m.sideItemH/2); h.Kind != HitSub || h.Sub != 0 {
		t.Fatalf("sub hit = %+v", h)
	}
	// The ⚙ Settings entry is pinned to the bottom of the sidebar.
	if s.HitTest(10, s.H-2).Kind != HitSettings {
		t.Fatal("bottom sidebar should be Settings")
	}
	// Empty sidebar gap between the last sub and the Settings entry -> none.
	if s.HitTest(10, m.topbarH+4*m.sideItemH).Kind != HitNone {
		t.Fatal("sidebar gap miss")
	}
	// Feed gap between cards -> none.
	gapY := m.topbarH + m.pad + m.rowH + m.cardGap/2
	if s.HitTest(m.sidebarW+40, gapY).Kind != HitNone {
		t.Fatal("feed gap none")
	}
}

func TestRevAndActive(t *testing.T) {
	s := newScene()
	r0 := s.Rev()
	s.Scroll(1)
	s.SetSearch("q")
	s.FocusSearch(true)
	s.SetActive(1)
	if s.Active != 1 {
		t.Fatal("SetActive did not set")
	}
	if s.Rev() <= r0 {
		t.Fatalf("rev did not advance: %d -> %d", r0, s.Rev())
	}
	// A no-op scale (same value) must not advance rev.
	before := s.Rev()
	s.SetScale(s.Scale)
	if s.Rev() != before {
		t.Fatal("no-op SetScale advanced rev")
	}
}

func TestChromeCacheReuse(t *testing.T) {
	s := newScene()
	buf := make([]byte, s.W*s.H*4)
	s.Draw(buf)
	sb, tb := s.sidebarSpr, s.topbarSpr
	s.Scroll(3)
	s.Draw(buf) // scroll must not re-render chrome
	if s.sidebarSpr != sb || s.topbarSpr != tb {
		t.Fatal("chrome sprites re-rendered on scroll")
	}
	// Changing the active filter re-renders the sidebar but not the topbar.
	s.SetActive(0)
	s.Draw(buf)
	if s.sidebarSpr == sb {
		t.Fatal("sidebar not re-rendered after SetActive")
	}
	if s.topbarSpr != tb {
		t.Fatal("topbar needlessly re-rendered")
	}
	// Typing re-renders the topbar.
	s.FocusSearch(true)
	s.TypeRune('x')
	s.Draw(buf)
	if s.topbarSpr == tb {
		t.Fatal("topbar not re-rendered after typing")
	}
}

func TestSetThumb(t *testing.T) {
	s := newScene()
	buf := make([]byte, s.W*s.H*4)
	s.Draw(buf) // warm the cache
	th := image.NewRGBA(image.Rect(0, 0, 10, 10))
	s.SetThumb("1", th) // invalidates + attaches
	if s.Thumbs["1"] != th {
		t.Fatal("thumb not stored")
	}
	if s.cardCache != nil {
		t.Fatal("cache not invalidated by SetThumb")
	}
	// SetThumb on a fresh scene allocates the map.
	s2 := New(400, 300, nil)
	s2.SetThumb("x", th)
	if s2.Thumbs["x"] != th {
		t.Fatal("thumb map not allocated")
	}
}

func TestCardCacheReuse(t *testing.T) {
	s := newScene()
	buf := make([]byte, s.W*s.H*4)
	s.Draw(buf)
	n := len(s.cardCache)
	s.Scroll(3)
	s.Draw(buf) // same items -> cache hit, no new sprites
	if len(s.cardCache) != n {
		t.Fatalf("cache grew on scroll: %d -> %d", n, len(s.cardCache))
	}
}

func TestBlitAt(t *testing.T) {
	dst := image.NewRGBA(image.Rect(0, 0, 20, 20))
	src := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for i := range src.Pix {
		src.Pix[i] = 0xFF
	}
	blitAt(dst, src, 5, 5) // fully inside
	if dst.RGBAAt(5, 5).R != 0xFF {
		t.Fatal("inside blit")
	}
	// Top/bottom clip and left/right clip: place partly off each edge.
	blitAt(dst, src, -3, -3) // top-left off-screen
	if dst.RGBAAt(0, 0).R != 0xFF {
		t.Fatal("top-left clip visible portion")
	}
	blitAt(dst, src, 16, 16) // bottom-right off-screen (right + bottom clip)
	if dst.RGBAAt(18, 18).R != 0xFF {
		t.Fatal("bottom-right clip visible portion")
	}
	blitAt(dst, src, 100, 100) // wholly off-screen -> no-op, no panic
	blitAt(dst, src, 20, 5)    // x at right edge: in-bounds row but wpix<=0
}

func BenchmarkScrollCached(b *testing.B) {
	s := New(1000, 700, ThemeFor(OSMac, false))
	items := make([]source.Item, 60)
	for i := range items {
		items[i] = source.Item{ID: string(rune('a' + i%26)), Source: source.Reddit, Channel: "golang", Title: "A reasonably long headline number", Author: "user", Score: i, Comments: i}
	}
	s.SetItems(items)
	buf := make([]byte, s.W*s.H*4)
	s.Draw(buf) // warm cache
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.ScrollY = i % 400
		s.Draw(buf)
	}
}

func BenchmarkScrollUncached(b *testing.B) {
	s := New(1000, 700, ThemeFor(OSMac, false))
	items := make([]source.Item, 60)
	for i := range items {
		items[i] = source.Item{ID: string(rune('a' + i%26)), Source: source.Reddit, Channel: "golang", Title: "A reasonably long headline number", Author: "user", Score: i, Comments: i}
	}
	s.SetItems(items)
	buf := make([]byte, s.W*s.H*4)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.invalidateCards() // force full re-rasterisation every frame
		s.ScrollY = i % 400
		s.Draw(buf)
	}
}

func TestMetaLine(t *testing.T) {
	if metaLine(source.Item{Author: "a", Score: 5, Comments: 2}) != "a · 5 pts · 2 comments" {
		t.Fatal("full")
	}
	if metaLine(source.Item{Score: -1, Comments: -1}) != "" {
		t.Fatal("empty")
	}
	if metaLine(source.Item{Author: "a", Score: -1, Comments: 3}) != "a · 3 comments" {
		t.Fatal("partial")
	}
	// Created adds a timestamp (UTC, deterministic).
	if got := metaLine(source.Item{Author: "pg", Score: -1, Comments: -1, Created: 1700000000}); got != "pg · 14 Nov 2023 22:13" {
		t.Fatalf("timestamp = %q", got)
	}
}

func TestTruncate(t *testing.T) {
	f := getFace(14, false)
	if truncate(f, "hi", 0) != "hi" {
		t.Fatal("maxW<=0 returns as-is")
	}
	if truncate(f, "hi", 10000) != "hi" {
		t.Fatal("fits")
	}
	long := "this is a very long title that will not fit"
	got := truncate(f, long, 60)
	if got == long || got[len(got)-3:] != "…" {
		t.Fatalf("truncated = %q", got)
	}
	if truncate(f, long, 1) != "…" {
		t.Fatal("tiny -> ellipsis")
	}
}

func TestBlit(t *testing.T) {
	dst := image.NewRGBA(image.Rect(0, 0, 20, 20))
	src := image.NewRGBA(image.Rect(0, 0, 10, 10))
	for i := range src.Pix {
		src.Pix[i] = 0xFF
	}
	blit(dst, src, 2, 2, 5, 4) // clip to 5x4
	if dst.RGBAAt(2, 2).R != 0xFF {
		t.Fatal("blit top-left")
	}
	if dst.RGBAAt(8, 2).R == 0xFF {
		t.Fatal("blit exceeded maxW")
	}
	if dst.RGBAAt(2, 7).R == 0xFF {
		t.Fatal("blit exceeded maxH")
	}
}

func TestMuteAndRpx(t *testing.T) {
	m := mute(toolkit.RGBA{R: 0, G: 0, B: 0}, toolkit.RGBA{R: 100, G: 100, B: 100})
	if m.R != 45 {
		t.Fatalf("mute = %v", m)
	}
	if rpxOf(&Scene{Scale: 0.5}, 0) != 1 {
		t.Fatal("rpx clamp to 1")
	}
	if rpxOf(&Scene{Scale: 2}, 10) != 20 {
		t.Fatal("rpx scale")
	}
}

func TestSourceLabelAndColor(t *testing.T) {
	kinds := []source.Kind{source.Reddit, source.HackerNews, source.Syndication, source.Usenet,
		source.Mastodon, source.Lemmy, source.Bluesky, source.Twitter, source.Instagram, source.TikTok, source.Kind("weird")}
	for _, k := range kinds {
		if sourceLabel(k) == "" {
			t.Fatalf("empty label for %q", k)
		}
		c := sourceColor(k)
		if c.A != 0xFF {
			t.Fatalf("color alpha for %q = %d", k, c.A)
		}
	}
	if sourceLabel(source.Kind("weird")) != "weird" {
		t.Fatal("default label")
	}
}

func TestTextFace(t *testing.T) {
	if getFace(0, false).height < 1 {
		t.Fatal("px<1 clamp") // clamps to 1px
	}
	a := getFace(14, true)
	b := getFace(14, true) // cache hit
	if a.face != b.face {
		t.Fatal("face not cached")
	}
	if a.width("hello") <= 0 {
		t.Fatal("width")
	}
	img := image.NewRGBA(image.Rect(0, 0, 60, 20))
	a.drawRight(img, 55, 2, "hi", toolkit.RGBA{R: 0, G: 0, B: 0, A: 0xFF})
}

func TestTheme(t *testing.T) {
	for _, os := range []string{OSMac, OSLinux, OSWindows, "other"} {
		for _, dark := range []bool{false, true} {
			if ThemeFor(os, dark) == nil {
				t.Fatalf("nil theme for %s dark=%v", os, dark)
			}
		}
	}
	if ResolveTheme("light", OSMac, true).Background != ThemeFor(OSMac, false).Background {
		t.Fatal("resolve light")
	}
	if ResolveTheme("dark", OSMac, false).Background != ThemeFor(OSMac, true).Background {
		t.Fatal("resolve dark")
	}
	if ResolveTheme("system", OSMac, true).Background != ThemeFor(OSMac, true).Background {
		t.Fatal("resolve system")
	}
	// withOnAccent on a theme that already has an Extra map (non-nil branch).
	th := &toolkit.Theme{Extra: map[string]toolkit.RGBA{"X": rgb(0x010203)}}
	withOnAccent(th, rgb(0x040506))
	if th.Extra["OnAccent"] != rgb(0x040506) || th.Extra["X"] != rgb(0x010203) {
		t.Fatal("withOnAccent preserve + set")
	}
}
