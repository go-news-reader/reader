package ui

// The in-canvas Reddit search / discover view (ModeSearch). Reddit does not let
// its subreddits be enumerated, so a keyword query against its search endpoint is
// the only way to discover them; this view runs that search and, on a second tab,
// searches posts by keyword. Each subreddit result carries a Subscribe affordance
// that adds an r/<name> subscription; a post keyword search can be saved as a
// live "search:<query>" subscription so fresh matches keep flowing into the feed.
// A client-side regexp field narrows the subreddit results locally. It is drawn
// with the same painter + anti-aliased text and toolkit widgets as the rest of
// the app (following browse_view.go), so its layout / hit-testing is
// snapshot-verifiable under native `go test`.

import (
	"fmt"
	"image"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"

	"github.com/go-news-reader/reader/source"
)

// searchTab selects which kind of result the search view shows.
type searchTab int

const (
	searchTabSubreddits searchTab = iota // discover subreddits (subscribe as r/<name>)
	searchTabPosts                       // search posts by keyword (save as search:<query>)
)

// searchRowLayout positions one visible result row: its top offset below the
// list viewport origin, plus either the subreddit or the post it renders and the
// per-row Subscribe button rect (zero for a post row, which is preview-only).
type searchRowLayout struct {
	top       int
	sub       *source.SubredditResult
	post      *source.Item
	subscribe toolkit.Rect
}

// searchState holds the Reddit search view's own state so the Scene struct stays
// lean (only a single embedded field). The query/regex widgets are real
// toolkit.SearchEntry widgets (mvvm-bindable and self-caret-drawing), lazily
// built so a bare Scene need not allocate them.
type searchState struct {
	tab         searchTab
	queryEntry  *toolkit.SearchEntry
	regexEntry  *toolkit.SearchEntry
	queryFocus  bool
	regexFocus  bool
	loading     bool
	status      string
	subResults  []source.SubredditResult
	postResults []source.Item
	scroll      panelScroll

	// Laid-out rects (screen coords), filled by layoutSearch and read by drawSearch
	// / searchHitTest so a click maps to exactly what was drawn.
	backR     toolkit.Rect
	runR      toolkit.Rect
	queryR    toolkit.Rect
	tabSubR   toolkit.Rect
	tabPostR  toolkit.Rect
	regexR    toolkit.Rect // subreddits tab: the regexp filter field
	saveR     toolkit.Rect // posts tab: the "save this search" button
	listTop   int          // screen Y where the scrolling result list begins
	rows      []searchRowLayout
	filterN   int    // number of subreddit rows after the regexp filter
	filterMsg string // an inline hint for a bad regexp ("" when the filter is valid)
}

// ensureWidgets lazily builds the query + regexp SearchEntry widgets (with the
// shared magnifier prefix icon), so OpenSearch, typing and layout can all rely on
// them being present.
func (ss *searchState) ensureWidgets() {
	if ss.queryEntry == nil {
		ss.queryEntry = toolkit.NewSearchEntry("")
		ss.queryEntry.Icon = drawSearchIcon
	}
	if ss.regexEntry == nil {
		ss.regexEntry = toolkit.NewSearchEntry("")
		ss.regexEntry.Icon = drawSearchIcon
	}
}

// typeRune routes a printable rune into whichever search field has focus, feeding
// it through the widget itself (so a future mvvm OnChange fires like a real
// keypress) and resetting the results scroll for the regexp filter.
func (ss *searchState) typeRune(s *Scene, r rune) {
	ss.ensureWidgets()
	switch {
	case ss.queryFocus:
		ss.queryEntry.OnEvent(toolkit.Event{Kind: toolkit.EventChar, Code: string(r)})
		s.touch()
	case ss.regexFocus:
		ss.regexEntry.OnEvent(toolkit.Event{Kind: toolkit.EventChar, Code: string(r)})
		ss.scroll.offset = 0
		s.touch()
	}
}

// backspace removes the last rune of the focused search field.
func (ss *searchState) backspace(s *Scene) {
	ss.ensureWidgets()
	switch {
	case ss.queryFocus && ss.queryEntry.Text != "":
		ss.queryEntry.OnEvent(toolkit.Event{Kind: toolkit.EventKeyDown, Code: "Backspace"})
		s.touch()
	case ss.regexFocus && ss.regexEntry.Text != "":
		ss.regexEntry.OnEvent(toolkit.Event{Kind: toolkit.EventKeyDown, Code: "Backspace"})
		ss.scroll.offset = 0
		s.touch()
	}
}

// OpenSearch enters the Reddit search view, resetting the scroll and focus.
func (s *Scene) OpenSearch() {
	s.search.ensureWidgets()
	s.mode = ModeSearch
	s.search.scroll.offset = 0
	s.search.queryFocus = false
	s.search.regexFocus = false
	s.touch()
}

// CloseSearch returns to the feed view.
func (s *Scene) CloseSearch() { s.mode = ModeFeed; s.touch() }

// SetSearchTab selects the subreddit-discovery or post-search results tab and
// resets the result scroll.
func (s *Scene) SetSearchTab(posts bool) {
	if posts {
		s.search.tab = searchTabPosts
	} else {
		s.search.tab = searchTabSubreddits
	}
	s.search.scroll.offset = 0
	s.touch()
}

// SearchTabPosts reports whether the post-search tab is active (else subreddits).
func (s *Scene) SearchTabPosts() bool { return s.search.tab == searchTabPosts }

// SearchQuery returns the trimmed query text.
func (s *Scene) SearchQuery() string {
	s.search.ensureWidgets()
	return strings.TrimSpace(s.search.queryEntry.Text)
}

// SearchRegex returns the trimmed subreddit-results regexp filter text.
func (s *Scene) SearchRegex() string {
	s.search.ensureWidgets()
	return strings.TrimSpace(s.search.regexEntry.Text)
}

// FocusSearchQuery gives (or removes) keyboard focus to the query field. Focusing
// the query blurs the regexp field so keystrokes never land in both.
func (s *Scene) FocusSearchQuery(v bool) {
	s.search.queryFocus = v
	if v {
		s.search.regexFocus = false
	}
	s.touch()
}

// FocusSearchRegex gives (or removes) keyboard focus to the regexp filter field.
func (s *Scene) FocusSearchRegex(v bool) {
	s.search.regexFocus = v
	if v {
		s.search.queryFocus = false
	}
	s.touch()
}

// SearchQueryFocused / SearchRegexFocused report which field holds focus.
func (s *Scene) SearchQueryFocused() bool { return s.search.queryFocus }
func (s *Scene) SearchRegexFocused() bool { return s.search.regexFocus }

// SetSearchLoading marks whether a Reddit search request is in flight (folded
// into Animating so the spinner keeps ticking) and clears the status while busy.
func (s *Scene) SetSearchLoading(v bool) {
	s.search.loading = v
	if v {
		s.search.status = ""
	}
	s.touch()
}

// SearchLoading reports whether a search request is in flight.
func (s *Scene) SearchLoading() bool { return s.search.loading }

// SetSearchStatus sets an informational/error line for the search view (e.g. "No
// results" or a provider message) and clears the loading flag.
func (s *Scene) SetSearchStatus(v string) {
	s.search.status = v
	s.search.loading = false
	s.touch()
}

// SearchStatus returns the current search status line.
func (s *Scene) SearchStatus() string { return s.search.status }

// SetSubredditResults delivers subreddit-search results, switches to the
// subreddits tab, clears loading and resets the scroll.
func (s *Scene) SetSubredditResults(rs []source.SubredditResult) {
	s.search.subResults = rs
	s.search.tab = searchTabSubreddits
	s.search.loading = false
	s.search.scroll.offset = 0
	if len(rs) == 0 {
		s.search.status = "No subreddits found"
	} else {
		s.search.status = ""
	}
	s.touch()
}

// SetPostResults delivers post-search results, switches to the posts tab, clears
// loading and resets the scroll.
func (s *Scene) SetPostResults(items []source.Item) {
	s.search.postResults = items
	s.search.tab = searchTabPosts
	s.search.loading = false
	s.search.scroll.offset = 0
	if len(items) == 0 {
		s.search.status = "No posts found"
	} else {
		s.search.status = ""
	}
	s.touch()
}

// SubredditResults / PostResults expose the delivered results (front-ends/tests).
func (s *Scene) SubredditResults() []source.SubredditResult { return s.search.subResults }
func (s *Scene) PostResults() []source.Item                 { return s.search.postResults }

// filteredSubResults applies the client-side regexp filter to the subreddit
// results, matching (case-insensitively) against the name, title or description.
// An empty filter returns everything; an invalid pattern records an inline hint
// and, rather than crashing or hiding everything, returns everything so a partial
// pattern being typed never blanks the list.
func (ss *searchState) filteredSubResults() []source.SubredditResult {
	ss.filterMsg = ""
	filter := ""
	if ss.regexEntry != nil {
		filter = strings.TrimSpace(ss.regexEntry.Text)
	}
	if filter == "" {
		return ss.subResults
	}
	re, err := regexp.Compile("(?i)" + filter)
	if err != nil {
		ss.filterMsg = "bad regexp: " + err.Error()
		return ss.subResults
	}
	out := make([]source.SubredditResult, 0, len(ss.subResults))
	for _, r := range ss.subResults {
		if re.MatchString(r.Name) || re.MatchString(r.Title) || re.MatchString(r.Description) {
			out = append(out, r)
		}
	}
	return out
}

// searchRowH is the fixed height of one result row (two text lines plus padding),
// shared by subreddit and post rows so scroll math and the offscreen-skip work.
func (s *Scene) searchRowH() int {
	m := s.m
	return m.title.height + m.meta.height + rpxOf(s, 20)
}

// searchSubscribeRect is the right-aligned Subscribe pill for a row at screen y.
func (s *Scene) searchSubscribeRect(y, w int) toolkit.Rect {
	m := s.m
	bw := m.tab.width("Subscribe") + rpxOf(s, 20)
	h := m.btnH
	return toolkit.Rect{X: w - m.pad - bw, Y: y + (s.searchRowH()-h)/2, W: bw, H: h}
}

// layoutSearch computes the topbar buttons, the query/regex fields, the tab
// pills and the flattened result rows, applying the vertical scroll via row tops.
func (s *Scene) layoutSearch() {
	s.search.ensureWidgets()
	s.m = s.computeMetrics()
	m := s.m
	ss := &s.search
	pad := m.pad
	btnH := m.btnH

	// Topbar band: "‹ Back" (left), "Search" run button (right).
	bw := m.tab.width("‹ Back") + rpxOf(s, 20)
	ss.backR = toolkit.Rect{X: pad, Y: (m.topbarH - btnH) / 2, W: bw, H: btnH}
	rw := m.tab.width("Search") + rpxOf(s, 24)
	ss.runR = toolkit.Rect{X: s.W - pad - rw, Y: (m.topbarH - btnH) / 2, W: rw, H: btnH}

	// Query field below the topbar.
	fy := m.topbarH + pad
	ss.queryR = toolkit.Rect{X: pad, Y: fy, W: s.W - 2*pad, H: btnH}

	// Tab toggle row: two equal pills.
	ty := ss.queryR.Y + ss.queryR.H + rpxOf(s, 8)
	half := (s.W - 2*pad - rpxOf(s, 8)) / 2
	ss.tabSubR = toolkit.Rect{X: pad, Y: ty, W: half, H: btnH}
	ss.tabPostR = toolkit.Rect{X: pad + half + rpxOf(s, 8), Y: ty, W: half, H: btnH}

	// Third control row depends on the tab: the regexp filter (subreddits) or the
	// "save this search" button (posts).
	cy := ty + btnH + rpxOf(s, 8)
	ss.regexR = toolkit.Rect{}
	ss.saveR = toolkit.Rect{}
	switch ss.tab {
	case searchTabSubreddits:
		ss.regexR = toolkit.Rect{X: pad, Y: cy, W: s.W - 2*pad, H: btnH}
	case searchTabPosts:
		if s.SearchQuery() != "" {
			sw := m.tab.width("Save this search") + rpxOf(s, 24)
			ss.saveR = toolkit.Rect{X: pad, Y: cy, W: sw, H: btnH}
		}
	}

	// Status / count line, then the scrolling list.
	countY := cy + btnH + rpxOf(s, 6)
	ss.listTop = countY + m.side.height + rpxOf(s, 8)

	// Materialise the visible rows for the active tab.
	ss.rows = ss.rows[:0]
	rowH := s.searchRowH()
	top := 0
	switch ss.tab {
	case searchTabSubreddits:
		fs := ss.filteredSubResults()
		ss.filterN = len(fs)
		for i := range fs {
			ss.rows = append(ss.rows, searchRowLayout{top: top, sub: &fs[i], subscribe: s.searchSubscribeRect(ss.listTop+top, s.W)})
			top += rowH
		}
	case searchTabPosts:
		for i := range ss.postResults {
			ss.rows = append(ss.rows, searchRowLayout{top: top, post: &ss.postResults[i]})
			top += rowH
		}
	}
	ss.scroll.refresh((ss.listTop-m.topbarH)+top, s.H-m.topbarH)
}

// drawSearch paints the Reddit search view.
func (s *Scene) drawSearch(buf []byte) {
	s.layoutSearch()
	m := s.m
	ss := &s.search
	p := painter.NewPixelPainter(buf, s.W, s.H)
	img := &image.RGBA{Pix: buf, Stride: s.W * 4, Rect: image.Rect(0, 0, s.W, s.H)}
	th := s.theme
	onAccent := themeOnAccent(th)
	muteS := mute(th.OnSurface, th.Surface)

	p.FillRect(painter.Rect{X: 0, Y: 0, W: s.W, H: s.H}, th.Background)

	s.drawSearchControls(p, img, muteS)
	s.drawSearchRows(p, img, muteS)

	// Scrollbar when the result list overflows.
	s.drawVScrollbar(p, toolkit.Rect{X: 0, Y: m.topbarH, W: s.W, H: s.H - m.topbarH}, 0, ss.scroll.contentH, ss.scroll.offset)

	// Topbar (accent) with Back, title, and the Search run button, over any overflow.
	p.FillRect(painter.Rect{X: 0, Y: 0, W: s.W, H: m.topbarH}, th.Accent)
	p.FillRoundRect(painter.Rect(ss.backR), rpxOf(s, 6), onAccent)
	m.tab.draw(img, ss.backR.X+rpxOf(s, 10), ss.backR.Y+(ss.backR.H-m.tab.height)/2, "‹ Back", th.Accent)
	title := "Search Reddit"
	tx := ss.backR.X + ss.backR.W + m.pad
	m.title.draw(img, tx, (m.topbarH-m.title.height)/2, title, onAccent)
	p.FillRoundRect(painter.Rect(ss.runR), rpxOf(s, 6), onAccent)
	m.tab.draw(img, ss.runR.X+(ss.runR.W-m.tab.width("Search"))/2, ss.runR.Y+(ss.runR.H-m.tab.height)/2, "Search", th.Accent)
}

// drawSearchControls paints the query field, the tab pills, the third control row
// (regexp filter or save button) and the status/count line.
func (s *Scene) drawSearchControls(p *painter.PixelPainter, img *image.RGBA, muteS toolkit.RGBA) {
	m := s.m
	ss := &s.search
	th := s.theme

	// Query field.
	drawEntry := func(se *toolkit.SearchEntry, r toolkit.Rect, focused bool) {
		se.SetBounds(r)
		se.Font = ttFont(false, rpxOf(s, 13))
		se.SetFocused(focused)
		p.FillRoundRect(painter.Rect(r), rpxOf(s, 6), th.Surface)
		se.Draw(p, th)
		if focused {
			p.StrokeRoundRect(painter.Rect(r), rpxOf(s, 6), th.Accent, rpxOf(s, 2))
		}
	}
	drawEntry(ss.queryEntry, ss.queryR, ss.queryFocus)

	// Tab pills: the active one is filled with the accent, the other outlined.
	drawTab := func(r toolkit.Rect, label string, active bool) {
		if active {
			p.FillRoundRect(painter.Rect(r), rpxOf(s, 6), th.Accent)
			m.tab.draw(img, r.X+(r.W-m.tab.width(label))/2, r.Y+(r.H-m.tab.height)/2, label, themeOnAccent(th))
		} else {
			p.FillRoundRect(painter.Rect(r), rpxOf(s, 6), th.Surface)
			p.StrokeRoundRect(painter.Rect(r), rpxOf(s, 6), th.Border, rpxOf(s, 1))
			m.tab.draw(img, r.X+(r.W-m.tab.width(label))/2, r.Y+(r.H-m.tab.height)/2, label, th.OnSurface)
		}
	}
	drawTab(ss.tabSubR, "Subreddits", ss.tab == searchTabSubreddits)
	drawTab(ss.tabPostR, "Posts", ss.tab == searchTabPosts)

	// Third control row.
	switch ss.tab {
	case searchTabSubreddits:
		drawEntry(ss.regexEntry, ss.regexR, ss.regexFocus)
		if ss.regexEntry.Text == "" && !ss.regexFocus {
			// A faint hint of the field's purpose when empty and unfocused.
			m.meta.draw(img, ss.regexR.X+rpxOf(s, 30), ss.regexR.Y+(ss.regexR.H-m.meta.height)/2, "regexp filter", muteS)
		}
	case searchTabPosts:
		if ss.saveR.W > 0 {
			p.FillRoundRect(painter.Rect(ss.saveR), rpxOf(s, 6), th.Accent)
			lbl := "Save this search"
			m.tab.draw(img, ss.saveR.X+(ss.saveR.W-m.tab.width(lbl))/2, ss.saveR.Y+(ss.saveR.H-m.tab.height)/2, lbl, themeOnAccent(th))
		}
	}

	// Status / count line.
	countY := ss.listTop - m.side.height - rpxOf(s, 8)
	msg, col := s.searchStatusLine(muteS)
	m.side.draw(img, m.pad, countY, msg, col)
}

// searchStatusLine returns the count/status text and its colour: a bad-regexp
// hint (error), an explicit status, a live "Searching…" while loading, else the
// result count for the active tab.
func (s *Scene) searchStatusLine(muteS toolkit.RGBA) (string, toolkit.RGBA) {
	ss := &s.search
	th := s.theme
	switch {
	case ss.filterMsg != "":
		return ss.filterMsg, errorColor(th)
	case ss.loading:
		return "Searching…", muteS
	case ss.status != "":
		return ss.status, muteS
	case ss.tab == searchTabSubreddits:
		return fmt.Sprintf("%d subreddits", ss.filterN), muteS
	default:
		return fmt.Sprintf("%d posts", len(ss.postResults)), muteS
	}
}

// drawSearchRows paints the scrolling result rows for the active tab, clipped to
// the list viewport (so a row scrolled under the controls never paints over them).
func (s *Scene) drawSearchRows(p *painter.PixelPainter, img *image.RGBA, muteS toolkit.RGBA) {
	m := s.m
	ss := &s.search
	th := s.theme
	rowH := s.searchRowH()
	shown := s.scrollbarNeeded(ss.scroll.contentH, s.H-m.topbarH)
	rowW := s.scrollClampRight(s.W, s.W, 0, shown)
	for _, r := range ss.rows {
		y := ss.listTop + r.top - ss.scroll.offset
		if y+rowH < ss.listTop || y >= s.H {
			continue
		}
		switch {
		case r.sub != nil:
			s.drawSubredditRow(p, img, *r.sub, r.subscribe, m.pad, y, rowW, muteS)
		case r.post != nil:
			s.drawPostRow(p, img, *r.post, m.pad, y, rowW, muteS)
		}
		p.FillRect(painter.Rect{X: 0, Y: y + rowH - 1, W: rowW, H: 1}, th.Border)
	}
}

// drawSubredditRow paints one subreddit result: r/<name> + subscriber count on
// the first line, the description on the second, and a Subscribe (or subscribed
// ✓) affordance on the right.
func (s *Scene) drawSubredditRow(p *painter.PixelPainter, img *image.RGBA, sr source.SubredditResult, btn toolkit.Rect, x, y, w int, muteS toolkit.RGBA) {
	m := s.m
	th := s.theme
	subscribed := s.IsSubscribed(source.Reddit, "r/"+sr.Name)
	if subscribed {
		drawCheckIcon(p, toolkit.Rect{X: btn.X + btn.W - m.navIcon - rpxOf(s, 4), Y: btn.Y + (btn.H-m.navIcon)/2, W: m.navIcon, H: m.navIcon}, successColor(th), s.iconStroke())
	} else {
		p.FillRoundRect(painter.Rect(btn), rpxOf(s, 6), th.Accent)
		m.tab.draw(img, btn.X+(btn.W-m.tab.width("Subscribe"))/2, btn.Y+(btn.H-m.tab.height)/2, "Subscribe", themeOnAccent(th))
	}
	right := btn.X - rpxOf(s, 8)

	name := "r/" + sr.Name
	if sr.NSFW {
		name += "  (NSFW)"
	}
	m.title.draw(img, x, y+rpxOf(s, 8), m.title.clipRight(name, right-x), th.OnSurface)
	subs := formatSubscribers(sr.Subscribers)
	if subs != "" {
		m.meta.drawRight(img, right, y+rpxOf(s, 10), subs, muteS)
	}
	desc := strings.TrimSpace(sr.Description)
	if desc == "" {
		desc = strings.TrimSpace(sr.Title)
	}
	if desc != "" {
		dy := y + rpxOf(s, 8) + m.title.height + rpxOf(s, 2)
		m.meta.draw(img, x, dy, m.meta.clipRight(desc, right-x), muteS)
	}
}

// drawPostRow paints one post result: its title on the first line and a compact
// meta line (subreddit · score · comments) on the second. Post rows are preview
// only — the whole keyword search is saved as one subscription via the Save
// button, not per row.
func (s *Scene) drawPostRow(p *painter.PixelPainter, img *image.RGBA, it source.Item, x, y, w int, muteS toolkit.RGBA) {
	m := s.m
	th := s.theme
	right := w - m.pad
	m.title.draw(img, x, y+rpxOf(s, 8), m.title.clipRight(it.Title, right-x), th.OnSurface)
	meta := "r/" + it.Channel
	meta += fmt.Sprintf(" · %d pts · %d comments", it.Score, it.Comments)
	dy := y + rpxOf(s, 8) + m.title.height + rpxOf(s, 2)
	m.meta.draw(img, x, dy, m.meta.clipRight(meta, right-x), muteS)
}

// formatSubscribers renders a subscriber count compactly (e.g. "1.2M members",
// "12.3k members"), or "" for a non-positive count.
func formatSubscribers(n int64) string {
	switch {
	case n <= 0:
		return ""
	case n >= 1_000_000:
		return strconv.FormatFloat(float64(n)/1_000_000, 'f', 1, 64) + "M members"
	case n >= 1_000:
		return strconv.FormatFloat(float64(n)/1_000, 'f', 1, 64) + "k members"
	default:
		return strconv.FormatInt(n, 10) + " members"
	}
}

// searchHitTest maps a click in the Reddit search view to an action.
func (s *Scene) searchHitTest(x, y int) Hit {
	s.layoutSearch()
	ss := &s.search
	if inRect(ss.backR, x, y) {
		return Hit{Kind: HitCloseSearch}
	}
	if inRect(ss.runR, x, y) {
		return Hit{Kind: HitRunSearch}
	}
	if inRect(ss.queryR, x, y) {
		return Hit{Kind: HitFocusSearchQuery}
	}
	if inRect(ss.tabSubR, x, y) {
		return Hit{Kind: HitSearchTab, Value: "subreddits"}
	}
	if inRect(ss.tabPostR, x, y) {
		return Hit{Kind: HitSearchTab, Value: "posts"}
	}
	if ss.regexR.W > 0 && inRect(ss.regexR, x, y) {
		return Hit{Kind: HitFocusSearchRegex}
	}
	if ss.saveR.W > 0 && inRect(ss.saveR, x, y) {
		return Hit{Kind: HitSubscribePostSearch, Value: s.SearchQuery()}
	}
	// A click above the list viewport (in the control chrome) hits no row.
	if y < ss.listTop {
		return Hit{Kind: HitNone}
	}
	rowH := s.searchRowH()
	for _, r := range ss.rows {
		top := ss.listTop + r.top - ss.scroll.offset
		if top+rowH < ss.listTop || top >= s.H {
			continue
		}
		if y < top || y >= top+rowH {
			continue
		}
		// A subreddit row's Subscribe button subscribes to r/<name> (unless already
		// subscribed). Post rows are preview only.
		if r.sub != nil && inRect(r.subscribe, x, y) && !s.IsSubscribed(source.Reddit, "r/"+r.sub.Name) {
			return Hit{Kind: HitSubscribeSubreddit, Value: r.sub.Name}
		}
		return Hit{Kind: HitNone}
	}
	return Hit{Kind: HitNone}
}
