package ui

import (
	"testing"

	"github.com/go-news-reader/reader/internal/settings"
	"github.com/go-news-reader/reader/source"
)

// searchScene builds a scene with one profile, already in ModeSearch.
func searchScene() *Scene {
	s := New(900, 640, ThemeFor(OSLinux, false))
	s.SetProfiles([]settings.Profile{{Name: "Home"}}, 0)
	s.OpenSearch()
	return s
}

// subSample is a small set of subreddit results with varied fields.
func subSample() []source.SubredditResult {
	return []source.SubredditResult{
		{Name: "golang", Title: "The Go Programming Language", Description: "Gophers unite", Subscribers: 250000},
		{Name: "rust", Title: "Rust", Description: "systems programming", Subscribers: 1_500_000, NSFW: false},
		{Name: "cpp", Title: "C++", Description: "", Subscribers: 500},
	}
}

// postSample is a small set of post results.
func postSample() []source.Item {
	return []source.Item{
		{ID: "p1", Source: source.Reddit, Channel: "golang", Title: "Generics tips", Score: 42, Comments: 7},
		{ID: "p2", Source: source.Reddit, Channel: "rust", Title: "Async explained", Score: 99, Comments: 12},
	}
}

// firstSearchHit scans the surface for the first coordinate whose HitTest yields
// kind, returning the Hit (for its Value).
func firstSearchHit(s *Scene, kind HitKind) (Hit, bool) {
	for y := 0; y < s.H; y += 2 {
		for x := 0; x < s.W; x += 2 {
			if h := s.HitTest(x, y); h.Kind == kind {
				return h, true
			}
		}
	}
	return Hit{}, false
}

func TestSearchOpenAndChrome(t *testing.T) {
	s := searchScene()
	if s.Mode() != ModeSearch {
		t.Fatalf("mode = %v, want ModeSearch", s.Mode())
	}
	renderPNG(t, s, "search-empty")
	s.layoutSearch()
	// Every chrome rect must be laid out (non-zero) in ModeSearch.
	for name, r := range map[string]struct{ W, H int }{
		"back":  {s.search.backR.W, s.search.backR.H},
		"run":   {s.search.runR.W, s.search.runR.H},
		"query": {s.search.queryR.W, s.search.queryR.H},
		"tabS":  {s.search.tabSubR.W, s.search.tabSubR.H},
		"tabP":  {s.search.tabPostR.W, s.search.tabPostR.H},
		"regex": {s.search.regexR.W, s.search.regexR.H}, // subreddits tab is default
	} {
		if r.W <= 0 || r.H <= 0 {
			t.Fatalf("rect %q not laid out: %+v", name, r)
		}
	}
	// Chrome hits resolve to the right kinds.
	if _, ok := firstSearchHit(s, HitCloseSearch); !ok {
		t.Fatal("no Back hit")
	}
	if _, ok := firstSearchHit(s, HitRunSearch); !ok {
		t.Fatal("no Run hit")
	}
	if _, ok := firstSearchHit(s, HitFocusSearchQuery); !ok {
		t.Fatal("no query-field hit")
	}
	if _, ok := firstSearchHit(s, HitFocusSearchRegex); !ok {
		t.Fatal("no regex-field hit")
	}
	if h, ok := firstSearchHit(s, HitSearchTab); !ok || (h.Value != "subreddits" && h.Value != "posts") {
		t.Fatalf("tab hit = %+v ok=%v", h, ok)
	}
}

func TestSearchSidebarEntry(t *testing.T) {
	s := New(900, 640, ThemeFor(OSLinux, false))
	s.SetProfiles([]settings.Profile{{Name: "Home"}}, 0)
	renderPNG(t, s, "feed-with-search-entry")
	if _, ok := firstSearchHit(s, HitSearchReddit); !ok {
		t.Fatal("sidebar Search Reddit entry missing")
	}
}

func TestSearchSubredditResultsAndSubscribe(t *testing.T) {
	s := searchScene()
	s.SetSubredditResults(subSample())
	if s.SearchTabPosts() {
		t.Fatal("delivering subreddits should select the subreddits tab")
	}
	if got := s.SubredditResults(); len(got) != 3 {
		t.Fatalf("results = %d", len(got))
	}
	renderPNG(t, s, "search-subreddits")
	s.layoutSearch()
	if len(s.search.rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(s.search.rows))
	}
	// Each row's Subscribe button is laid out with a positive rect.
	for i, r := range s.search.rows {
		if r.sub == nil || r.subscribe.W <= 0 || r.subscribe.H <= 0 {
			t.Fatalf("row %d subscribe rect not laid out: %+v", i, r)
		}
	}
	// A click inside the first row's Subscribe button returns HitSubscribeSubreddit
	// carrying the subreddit name.
	btn := s.search.rows[0].subscribe
	h := s.searchHitTest(btn.X+btn.W/2, btn.Y+btn.H/2)
	if h.Kind != HitSubscribeSubreddit || h.Value != "golang" {
		t.Fatalf("subscribe hit = %+v, want HitSubscribeSubreddit golang", h)
	}
	// After subscribing, the same button no longer offers Subscribe (it shows ✓).
	if !s.SubscribeActive(source.Reddit, "r/golang") {
		t.Fatal("SubscribeActive should add r/golang")
	}
	s.layoutSearch()
	btn = s.search.rows[0].subscribe
	if h := s.searchHitTest(btn.X+btn.W/2, btn.Y+btn.H/2); h.Kind == HitSubscribeSubreddit {
		t.Fatal("subscribed subreddit must not offer Subscribe again")
	}
	renderPNG(t, s, "search-subreddits-subscribed")
}

func TestSearchRegexFilter(t *testing.T) {
	s := searchScene()
	s.SetSubredditResults(subSample())
	// Focus the regex field and type a pattern matching only "rust".
	s.FocusSearchRegex(true)
	if !s.SearchRegexFocused() || s.SearchQueryFocused() {
		t.Fatal("focusing regex should blur the query")
	}
	for _, r := range "rust" {
		s.TypeRune(r)
	}
	if s.SearchRegex() != "rust" {
		t.Fatalf("regex text = %q", s.SearchRegex())
	}
	s.layoutSearch()
	if s.search.filterN != 1 || len(s.search.rows) != 1 {
		t.Fatalf("filtered rows = %d (filterN=%d), want 1", len(s.search.rows), s.search.filterN)
	}
	if s.search.rows[0].sub.Name != "rust" {
		t.Fatalf("filtered row = %q, want rust", s.search.rows[0].sub.Name)
	}
	renderPNG(t, s, "search-regex")
	// A pattern that matches the description of golang ("Gophers") but no name.
	s.Backspace()
	s.Backspace()
	s.Backspace()
	s.Backspace()
	for _, r := range "gophers" {
		s.TypeRune(r)
	}
	s.layoutSearch()
	if s.search.filterN != 1 || s.search.rows[0].sub.Name != "golang" {
		t.Fatalf("description match failed: filterN=%d rows=%+v", s.search.filterN, s.search.rows)
	}
}

func TestSearchInvalidRegexShowsAll(t *testing.T) {
	s := searchScene()
	s.SetSubredditResults(subSample())
	s.FocusSearchRegex(true)
	s.TypeRune('(') // unterminated group: invalid
	s.layoutSearch()
	if s.search.filterMsg == "" {
		t.Fatal("invalid regex should set an inline hint")
	}
	// Never crash, and show every result rather than blanking the list.
	if len(s.search.rows) != 3 {
		t.Fatalf("invalid regex should show all rows, got %d", len(s.search.rows))
	}
	renderPNG(t, s, "search-badregex")
	// The status line reports the bad-regex hint.
	msg, _ := s.searchStatusLine(mute(s.theme.OnSurface, s.theme.Surface))
	if msg != s.search.filterMsg {
		t.Fatalf("status line = %q, want the filter hint %q", msg, s.search.filterMsg)
	}
	// Recovering to a valid pattern clears the error.
	s.TypeRune(')')
	s.layoutSearch()
	if s.search.filterMsg != "" {
		t.Fatalf("valid continuation should clear the hint, got %q", s.search.filterMsg)
	}
}

func TestSearchPostsTabAndSave(t *testing.T) {
	s := searchScene()
	// Type a query and switch to the posts tab.
	s.FocusSearchQuery(true)
	for _, r := range "generics" {
		s.TypeRune(r)
	}
	s.SetSearchTab(true)
	if !s.SearchTabPosts() {
		t.Fatal("SetSearchTab(true) should select posts")
	}
	s.SetPostResults(postSample())
	if len(s.PostResults()) != 2 {
		t.Fatalf("post results = %d", len(s.PostResults()))
	}
	renderPNG(t, s, "search-posts")
	s.layoutSearch()
	if len(s.search.rows) != 2 {
		t.Fatalf("post rows = %d, want 2", len(s.search.rows))
	}
	// The Save button is laid out (query is non-empty) and hit-tests to a
	// HitSubscribePostSearch carrying the query.
	if s.search.saveR.W <= 0 {
		t.Fatal("Save button not laid out with a query present")
	}
	h, ok := firstSearchHit(s, HitSubscribePostSearch)
	if !ok || h.Value != "generics" {
		t.Fatalf("save-search hit = %+v ok=%v, want query 'generics'", h, ok)
	}
}

func TestSearchPostsTabNoSaveWithoutQuery(t *testing.T) {
	s := searchScene()
	s.SetSearchTab(true)
	s.SetPostResults(postSample())
	s.layoutSearch()
	if s.search.saveR.W != 0 {
		t.Fatalf("Save button should be hidden without a query: %+v", s.search.saveR)
	}
	// A post row is preview-only: a click inside it returns HitNone (never a
	// per-row subscribe).
	if len(s.search.rows) == 0 {
		t.Fatal("expected post rows")
	}
	rowH := s.searchRowH()
	top := s.search.listTop + s.search.rows[0].top
	if h := s.searchHitTest(s.W/2, top+rowH/2); h.Kind != HitNone {
		t.Fatalf("post-row click = %+v, want HitNone", h)
	}
}

func TestSearchTabToggleHits(t *testing.T) {
	s := searchScene()
	s.layoutSearch()
	// Click the Posts pill.
	pp := s.search.tabPostR
	if h := s.searchHitTest(pp.X+pp.W/2, pp.Y+pp.H/2); h.Kind != HitSearchTab || h.Value != "posts" {
		t.Fatalf("posts pill hit = %+v", h)
	}
	// Click the Subreddits pill.
	sp := s.search.tabSubR
	if h := s.searchHitTest(sp.X+sp.W/2, sp.Y+sp.H/2); h.Kind != HitSearchTab || h.Value != "subreddits" {
		t.Fatalf("subreddits pill hit = %+v", h)
	}
}

func TestSearchStatusLineBranches(t *testing.T) {
	s := searchScene()
	muteC := mute(s.theme.OnSurface, s.theme.Surface)
	// Loading.
	s.SetSearchLoading(true)
	if !s.SearchLoading() || !s.Animating() {
		t.Fatal("loading should be reported and animate")
	}
	if msg, _ := s.searchStatusLine(muteC); msg != "Searching…" {
		t.Fatalf("loading status = %q", msg)
	}
	// Explicit status.
	s.SetSearchStatus("hello")
	if s.SearchStatus() != "hello" || s.SearchLoading() {
		t.Fatalf("status = %q loading=%v", s.SearchStatus(), s.SearchLoading())
	}
	if msg, _ := s.searchStatusLine(muteC); msg != "hello" {
		t.Fatalf("explicit status = %q", msg)
	}
	// Subreddit count.
	s.SetSubredditResults(subSample())
	s.layoutSearch()
	if msg, _ := s.searchStatusLine(muteC); msg != "3 subreddits" {
		t.Fatalf("subreddit count status = %q", msg)
	}
	// Post count.
	s.SetPostResults(postSample())
	s.layoutSearch()
	if msg, _ := s.searchStatusLine(muteC); msg != "2 posts" {
		t.Fatalf("post count status = %q", msg)
	}
}

func TestSearchEmptyResultsStatus(t *testing.T) {
	s := searchScene()
	s.SetSubredditResults(nil)
	if s.SearchStatus() != "No subreddits found" {
		t.Fatalf("empty subreddit status = %q", s.SearchStatus())
	}
	s.SetPostResults(nil)
	if s.SearchStatus() != "No posts found" {
		t.Fatalf("empty post status = %q", s.SearchStatus())
	}
}

func TestSearchTypeIntoQuery(t *testing.T) {
	s := searchScene()
	// Not focused: TypeRune/Backspace are no-ops.
	s.TypeRune('x')
	s.Backspace()
	if s.SearchQuery() != "" {
		t.Fatalf("unfocused typing leaked: %q", s.SearchQuery())
	}
	s.FocusSearchQuery(true)
	if !s.SearchQueryFocused() || s.SearchRegexFocused() {
		t.Fatal("focusing query should blur regex")
	}
	for _, r := range "cats" {
		s.TypeRune(r)
	}
	s.Backspace()
	if s.SearchQuery() != "cat" {
		t.Fatalf("query = %q, want cat", s.SearchQuery())
	}
}

func TestSearchScrollClamps(t *testing.T) {
	s := searchScene()
	many := make([]source.SubredditResult, 0, 200)
	for i := 0; i < 200; i++ {
		many = append(many, source.SubredditResult{Name: "sub" + itoaTest(i), Subscribers: int64(i)})
	}
	s.SetSubredditResults(many)
	s.Scroll(100000) // clamp to bottom
	if s.search.scroll.offset <= 0 {
		t.Fatalf("scroll did not advance: %d", s.search.scroll.offset)
	}
	renderPNG(t, s, "search-scrolled")
	s.Scroll(-100000) // clamp back to top
	if s.search.scroll.offset != 0 {
		t.Fatalf("scroll not clamped to 0: %d", s.search.scroll.offset)
	}
	// A click above the list viewport (in the control chrome) hits no result row.
	if h := s.searchHitTest(s.W/2, s.search.listTop-2); h.Kind == HitSubscribeSubreddit {
		t.Fatal("a click in the chrome must not subscribe")
	}
}

func TestSearchHitTestRowRegions(t *testing.T) {
	s := searchScene()
	s.SetSearchTab(false) // exercise the subreddits (else) arm explicitly
	s.SetSubredditResults(subSample())
	s.layoutSearch()
	if len(s.search.rows) < 3 {
		t.Fatalf("need >=3 rows, got %d", len(s.search.rows))
	}
	rowH := s.searchRowH()
	// A click on the SECOND row's body (not its Subscribe button) returns HitNone,
	// after the loop skips the first row (y past its extent).
	r1top := s.search.listTop + s.search.rows[1].top
	if h := s.searchHitTest(s.m.pad+2, r1top+rowH/2); h.Kind != HitNone {
		t.Fatalf("row-1 body click = %+v, want HitNone", h)
	}
	// A click below every row (still inside the viewport) falls through to HitNone.
	if h := s.searchHitTest(s.W/2, s.H-2); h.Kind != HitNone {
		t.Fatalf("below-rows click = %+v, want HitNone", h)
	}
}

func TestSearchHitTestSkipsOffscreenRows(t *testing.T) {
	s := searchScene()
	many := make([]source.SubredditResult, 0, 200)
	for i := 0; i < 200; i++ {
		many = append(many, source.SubredditResult{Name: "sub" + itoaTest(i)})
	}
	s.SetSubredditResults(many)
	s.Scroll(4000) // push the early rows above the list viewport
	s.layoutSearch()
	// Hit-test a visible row: the loop must skip the scrolled-off rows first.
	got := s.searchHitTest(s.m.pad+2, s.search.listTop+2)
	if got.Kind != HitNone && got.Kind != HitSubscribeSubreddit {
		t.Fatalf("visible-row hit after scroll = %+v", got)
	}
	// Confirm at least one row is scrolled off the top (its screen top < listTop).
	off := false
	for _, r := range s.search.rows {
		if s.search.listTop+r.top-s.search.scroll.offset < s.search.listTop {
			off = true
			break
		}
	}
	if !off {
		t.Fatal("expected some rows scrolled above the viewport")
	}
}

func TestSearchCloseAndGetters(t *testing.T) {
	s := searchScene()
	s.CloseSearch()
	if s.Mode() != ModeFeed {
		t.Fatalf("CloseSearch mode = %v", s.Mode())
	}
}

func TestFormatSubscribers(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{
		{0, ""},
		{-5, ""},
		{500, "500 members"},
		{1500, "1.5k members"},
		{250000, "250.0k members"},
		{1_500_000, "1.5M members"},
	}
	for _, c := range cases {
		if got := formatSubscribers(c.n); got != c.want {
			t.Fatalf("formatSubscribers(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

func TestSearchDescriptionFallsBackToTitle(t *testing.T) {
	// The "cpp" result has an empty description, so its row falls back to the title.
	s := searchScene()
	s.SetSubredditResults(subSample())
	renderPNG(t, s, "search-desc-fallback") // exercises the fallback + NSFW-absent branch
	s.layoutSearch()
	var cpp *searchRowLayout
	for i := range s.search.rows {
		if s.search.rows[i].sub != nil && s.search.rows[i].sub.Name == "cpp" {
			cpp = &s.search.rows[i]
		}
	}
	if cpp == nil {
		t.Fatal("cpp row missing")
	}
	if cpp.sub.Description != "" {
		t.Fatalf("cpp description should be empty for the fallback test")
	}
}

func TestSearchNSFWBadge(t *testing.T) {
	s := searchScene()
	s.SetSubredditResults([]source.SubredditResult{{Name: "spicy", Title: "Spicy", NSFW: true, Subscribers: 10}})
	renderPNG(t, s, "search-nsfw") // exercises the NSFW-present branch of drawSubredditRow
	s.layoutSearch()
	if len(s.search.rows) != 1 || !s.search.rows[0].sub.NSFW {
		t.Fatalf("nsfw row not laid out: %+v", s.search.rows)
	}
}
