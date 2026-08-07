package ui

import (
	"image"
	"testing"

	"github.com/go-widgets/toolkit"

	"github.com/go-news-reader/reader/source"
)

func webTestItem() source.Item {
	return source.Item{ID: "h1", Source: source.HackerNews, Channel: "news", Title: "Title", Body: "summary text", Link: "https://example.com/a"}
}

func TestWebPreviewURL(t *testing.T) {
	if got := webPreviewURL(webTestItem()); got != "https://example.com/a" {
		t.Errorf("Link URL = %q", got)
	}
	// No Link → falls back to Permalink.
	if got := webPreviewURL(source.Item{Source: source.Reddit, Permalink: "https://p"}); got != "https://p" {
		t.Errorf("Permalink URL = %q", got)
	}
	// Usenet never web-renders.
	if got := webPreviewURL(source.Item{Source: source.Usenet, Link: "https://x"}); got != "" {
		t.Errorf("Usenet URL = %q, want empty", got)
	}
	// Non-http scheme → empty.
	if got := webPreviewURL(source.Item{Source: source.Reddit, Link: "ftp://x"}); got != "" {
		t.Errorf("ftp URL = %q, want empty", got)
	}
	// Exported wrapper.
	s := New(760, 460, ThemeFor(OSMac, false))
	if s.WebPreviewURL(webTestItem()) == "" {
		t.Error("WebPreviewURL wrapper returned empty")
	}
}

// deliverPage opens url in the scene's browser and delivers a canned page render
// so the web preview shows a page (as the app would after a render lands).
func deliverPage(s *Scene, url, title string, w, h int) {
	b := s.Browser()
	b.Open(url, title)
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	b.Deliver(url, img.Pix, w, h, w, nil, title)
}

func TestWebPreviewSelectionAndDraw(t *testing.T) {
	s := New(900, 560, ThemeFor(OSMac, false))
	buf := make([]byte, s.W*s.H*4)

	// A fresh scene: no web preview active, browser accessors are live.
	if s.WebPreviewActive() || s.webPreviewItem() {
		t.Fatal("fresh scene must not have a web preview")
	}
	if s.Browser() == nil || s.BrowserVM() == nil {
		t.Fatal("Browser()/BrowserVM() must be wired")
	}

	// Selecting a web-linked item activates the embedded browser preview.
	s.SelectPreview(webTestItem())
	if !s.WebPreviewActive() {
		t.Fatal("selecting a web-linked item should activate the web preview")
	}
	// Loading (before a render lands): draws the browser chrome + indeterminate bar.
	s.Browser().Open("https://example.com/a", "Title")
	if !s.Browser().Loading() || !s.Animating() {
		t.Fatal("an open (loading) browser tab should animate")
	}
	s.Draw(buf) // exercises drawWebPreview (header + browser chrome + progress)
	if b := s.Browser().Bounds(); b.W <= 0 || b.H <= 0 {
		t.Fatalf("layoutPreview should size the browser: %+v", b)
	}

	// Deliver a tall rendered page → loading clears, page draws.
	page := image.NewRGBA(image.Rect(0, 0, 400, 1600))
	s.Browser().Deliver("https://example.com/a", page.Pix, 400, 1600, 400, nil, "Title")
	if s.Browser().Loading() {
		t.Fatal("Deliver should clear the loading state")
	}
	s.Draw(buf) // exercises drawWebPreview with a delivered page (drawPage blit)
}

func TestWebPreviewHeaderLines(t *testing.T) {
	// A long title in a narrow pane wraps to more than two lines; the fixed header
	// caps it at two.
	s := New(700, 500, ThemeFor(OSMac, false))
	long := "This is a deliberately very long article title that will certainly wrap across several lines in the narrow preview pane header area"
	s.SelectPreview(source.Item{ID: "L", Source: source.HackerNews, Title: long, Link: "https://ex/long"})
	buf := make([]byte, s.W*s.H*4)
	s.Draw(buf)
	if got := len(s.previewHeaderLines()); got != 2 {
		t.Fatalf("header lines = %d, want capped at 2", got)
	}
	if s.previewHeaderH() <= 0 {
		t.Fatal("header height must be positive")
	}
}

func TestBrowserTabModeSetting(t *testing.T) {
	s := New(900, 560, ThemeFor(OSMac, false))
	if s.BrowserSingleTab() {
		t.Fatal("default tab mode is multi-tab")
	}
	// The snapshot persists the tab mode explicitly (a *bool), so the scene's
	// multi-tab default round-trips as an explicit false rather than nil.
	if snap := s.Settings().BrowserSingleTab; snap == nil || *snap {
		t.Fatal("default settings snapshot should report multi-tab")
	}
	s.SetBrowserSingleTab(true)
	if snap := s.Settings().BrowserSingleTab; !s.BrowserSingleTab() || snap == nil || !*snap {
		t.Fatal("SetBrowserSingleTab(true) should switch to single-tab and persist")
	}
	s.SetBrowserSingleTab(false)
	if s.BrowserSingleTab() {
		t.Fatal("SetBrowserSingleTab(false) should return to multi-tab")
	}

	// Browser chrome (toolbar/urlbar) visibility is off by default and round-trips.
	if s.BrowserChromeHidden() || s.Settings().HideBrowserChrome {
		t.Fatal("browser chrome is shown by default")
	}
	s.SetBrowserChromeHidden(true)
	if !s.BrowserChromeHidden() || !s.Settings().HideBrowserChrome {
		t.Fatal("SetBrowserChromeHidden(true) should hide the chrome and persist")
	}
	s.SetBrowserChromeHidden(false)
	if s.BrowserChromeHidden() {
		t.Fatal("SetBrowserChromeHidden(false) should show the chrome again")
	}
}

func TestForwardBrowserClick(t *testing.T) {
	s := New(900, 560, ThemeFor(OSMac, false))
	// Not a web preview: never consumed.
	if s.ForwardBrowserClick(10, 10) {
		t.Fatal("no web preview → click not forwarded")
	}

	s.SelectPreview(webTestItem())
	// Web active but the browser hasn't been laid out yet (bounds zero): the click
	// is not forwarded (and browser focus is cleared).
	if s.ForwardBrowserClick(10, 10) || s.BrowserFocused() {
		t.Fatal("un-laid-out browser must not consume a click")
	}
	deliverPage(s, "https://example.com/a", "Title", 400, 1200)
	buf := make([]byte, s.W*s.H*4)
	s.Draw(buf) // lay out the browser bounds

	b := s.Browser().Bounds()
	// A click outside the browser rect blurs focus and is not consumed.
	if s.ForwardBrowserClick(b.X-100, b.Y) || s.BrowserFocused() {
		t.Fatal("click outside the browser must not be consumed")
	}
	// A click inside is forwarded and focuses the browser.
	if !s.ForwardBrowserClick(b.X+b.W/2, b.Y+2) || !s.BrowserFocused() {
		t.Fatal("click inside the browser should be forwarded + focus it")
	}
	// A second inside click keeps focus (setBrowserFocused no-op branch).
	if !s.ForwardBrowserClick(b.X+b.W/2, b.Y+3) {
		t.Fatal("second inside click should still be forwarded")
	}
}

func TestForwardBrowserScroll(t *testing.T) {
	s := New(900, 560, ThemeFor(OSMac, false))
	if s.ForwardBrowserScroll(40) {
		t.Fatal("no web preview → scroll not forwarded")
	}
	s.SelectPreview(webTestItem())
	// Web active, browser not laid out (bounds zero) → not forwarded.
	if s.ForwardBrowserScroll(40) {
		t.Fatal("un-laid-out browser must not consume a wheel")
	}
	deliverPage(s, "https://example.com/a", "Title", 400, 2000)
	buf := make([]byte, s.W*s.H*4)
	s.Draw(buf)
	b := s.Browser().Bounds()

	// Pointer outside the browser → not forwarded.
	s.MouseMove(b.X-50, b.Y)
	if s.ForwardBrowserScroll(40) {
		t.Fatal("wheel with the pointer off the browser must not be consumed")
	}
	// Pointer over the browser.
	s.MouseMove(b.X+b.W/2, b.Y+b.H/2)
	if !s.ForwardBrowserScroll(80) { // large delta → several rows
		t.Fatal("wheel over the browser should be consumed (multi-row)")
	}
	if !s.ForwardBrowserScroll(5) { // small positive → floored to one row
		t.Fatal("small positive wheel should be consumed as one row")
	}
	if !s.ForwardBrowserScroll(-5) { // small negative → floored to minus one row
		t.Fatal("small negative wheel should be consumed as minus one row")
	}
	if s.ForwardBrowserScroll(0) { // zero delta → nothing to do
		t.Fatal("zero wheel delta must not be consumed")
	}
}

func TestForwardBrowserKey(t *testing.T) {
	s := New(900, 560, ThemeFor(OSMac, false))
	// Not web / not focused → never forwarded.
	if s.ForwardBrowserKey("Backspace", 0) {
		t.Fatal("no web preview → key not forwarded")
	}
	s.SelectPreview(webTestItem())
	deliverPage(s, "https://example.com/a", "Title", 400, 1200)
	buf := make([]byte, s.W*s.H*4)
	s.Draw(buf)
	// Web active but browser not focused → not forwarded.
	if s.ForwardBrowserKey("Backspace", 0) {
		t.Fatal("unfocused browser must not capture keys")
	}
	// Focus the browser's address field by clicking it, then keys route to it.
	b := s.Browser().Bounds()
	_, _, addr := browserAddrRect(s, b)
	s.ForwardBrowserClick(addr.X+addr.W/2, addr.Y+addr.H/2)
	if !s.BrowserFocused() {
		t.Fatal("address-field click should focus the browser")
	}
	if !s.ForwardBrowserKey("", 'q') {
		t.Fatal("printable rune should route to the browser")
	}
	if !s.ForwardBrowserKey("Backspace", 0) {
		t.Fatal("Backspace should route to the browser")
	}
	// A bare key name with no rune (e.g. an arrow) is not forwarded.
	if s.ForwardBrowserKey("Up", 0) {
		t.Fatal("a non-editing key must not be forwarded")
	}
	// Enter commits/navigates the address (routed to the browser).
	if !s.ForwardBrowserKey("Enter", 0) {
		t.Fatal("Enter should route to the browser")
	}
}

// browserAddrRect reproduces the toolkit browser's toolbar layout enough to find
// the address-field rect (right of the three buttons) in screen coords, so a
// test can click it. Falls back to the toolbar's right portion.
func browserAddrRect(s *Scene, b toolkit.Rect) (int, int, toolkit.Rect) {
	// The toolbar row sits just below any tab strip; with one tab there is no
	// strip, so it is the first BrowserToolbarH rows of the browser bounds. The
	// address field fills the right ~half — clicking the far right is safely on it.
	y := b.Y + toolkit.BrowserToolbarH/2
	addr := toolkit.Rect{X: b.X + b.W*3/4, Y: y, W: b.W / 5, H: toolkit.BrowserToolbarH / 2}
	return b.X, y, addr
}

func TestWebPreviewAnimationTick(t *testing.T) {
	s := New(900, 560, ThemeFor(OSMac, false))
	if s.Animating() {
		t.Fatal("idle scene must not animate")
	}
	s.SelectPreview(webTestItem())
	s.Browser().Open("https://example.com/a", "Title") // loading → animates
	if !s.Animating() {
		t.Fatal("a loading browser tab should animate")
	}
	before := s.Browser().Phase
	s.AdvanceAnim() // must advance the browser's loading-bar phase
	if s.Browser().Phase == before {
		t.Fatal("AdvanceAnim should tick the browser phase")
	}
	// A pending Usenet image preview also animates.
	u := New(760, 460, ThemeFor(OSMac, false))
	u.SetPreviewLoading(true)
	if !u.Animating() {
		t.Fatal("pending image preview should animate")
	}
}

// TestSceneBookmarkModelAndDraw covers the bookmark set + the address-bar
// leading (SSL) and trailing (bookmark) icon hooks, both branches each.
func TestSceneBookmarkModelAndDraw(t *testing.T) {
	s := New(1100, 700, ThemeFor(OSMac, false))
	s.SetBookmarks([]string{"https://a/", "https://b/"})
	if !s.IsBookmarked("https://a/") || s.IsBookmarked("https://z/") {
		t.Fatal("SetBookmarks/IsBookmarked")
	}
	s.SetBookmarked("https://c/", true)
	s.SetBookmarked("https://a/", false)
	if got := s.BookmarkedURLs(); len(got) != 2 || got[0] != "https://b/" || got[1] != "https://c/" {
		t.Fatalf("BookmarkedURLs = %v, want [https://b/ https://c/]", got)
	}
	// Lazy-alloc: SetBookmarked on a fresh (nil-map) scene.
	fresh := New(400, 300, ThemeFor(OSMac, false))
	fresh.SetBookmarked("https://x/", true)
	if !fresh.IsBookmarked("https://x/") {
		t.Fatal("SetBookmarked should lazily allocate the set")
	}
	if len(New(400, 300, ThemeFor(OSMac, false)).BookmarkedURLs()) != 0 {
		t.Fatal("a fresh scene has no bookmarks")
	}

	// Draw the address icons: https (lock) + bookmarked (accent star).
	s.SelectPreview(webTestItem())
	deliverPage(s, "https://ex/page", "P", 400, 900)
	s.SetBookmarks([]string{"https://ex/page"})
	s.SyncBookmarkStar()
	if !s.Browser().Bookmarked {
		t.Fatal("SyncBookmarkStar should mark the bookmarked page")
	}
	buf := make([]byte, s.W*s.H*4)
	s.Draw(buf) // LeadingIcon https→lock, BookmarkIcon on
	// Non-https page (lock-slash) + not bookmarked (plain star).
	deliverPage(s, "http://insecure/", "I", 400, 900)
	s.SyncBookmarkStar()
	if s.Browser().Bookmarked {
		t.Fatal("http page is not bookmarked → star off")
	}
	s.Draw(buf) // LeadingIcon http→lock-slash, BookmarkIcon off
}
