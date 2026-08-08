package ui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/go-widgets/toolkit"

	"github.com/go-news-reader/reader/source"
)

func TestArticlePlainText(t *testing.T) {
	// Title + HTML body + a web link: body is de-HTML-ified, parts blank-line joined.
	it := source.Item{
		Source: source.HackerNews, Title: "Welcome to Nepal",
		Body: "<p>Line one</p><p>Line two &amp; more</p>", Link: "https://ex.com/a",
	}
	got := articlePlainText(it)
	want := "Welcome to Nepal\n\nLine one\n\nLine two & more\n\nhttps://ex.com/a"
	if got != want {
		t.Fatalf("articlePlainText =\n%q\nwant\n%q", got, want)
	}

	// Usenet never web-renders (webPreviewURL empty) → falls back to the raw Link.
	un := source.Item{Source: source.Usenet, Title: "Post", Link: "news://server/grp"}
	if got := articlePlainText(un); got != "Post\n\nnews://server/grp" {
		t.Fatalf("usenet link fallback = %q", got)
	}

	// All-empty item yields an empty string (no stray separators).
	if got := articlePlainText(source.Item{}); got != "" {
		t.Fatalf("empty item = %q, want empty", got)
	}
	// Title only.
	if got := articlePlainText(source.Item{Title: "  Solo  "}); got != "Solo" {
		t.Fatalf("title-only = %q", got)
	}
}

func TestCopyToClipboard(t *testing.T) {
	toolkit.SetClipboard(nil) // fresh in-process clipboard
	s := New(800, 600, ThemeFor(OSMac, false))
	// Blank text is not written and reports false.
	if s.copyToClipboard("   \n ") {
		t.Fatal("blank text should not be copied")
	}
	if toolkit.ClipboardText() != "" {
		t.Fatalf("blank copy wrote %q", toolkit.ClipboardText())
	}
	// Real text is written to the toolkit clipboard and reported.
	if !s.copyToClipboard("hello") || toolkit.ClipboardText() != "hello" {
		t.Fatalf("copy wrote %q", toolkit.ClipboardText())
	}
}

func TestCopySearchField(t *testing.T) {
	toolkit.SetClipboard(nil)
	s := New(1000, 600, ThemeFor(OSMac, false))
	s.SetScale(1)

	// Not focused → the search field is not the copy target.
	s.SetSearch("golang")
	if s.searchCopied {
		t.Fatal("searchCopied should start false")
	}

	// Focused with text → Cmd/Ctrl+C copies the query + arms the highlight.
	s.FocusSearch(true)
	if !s.Copy() {
		t.Fatal("copy from the focused search field should report true")
	}
	if toolkit.ClipboardText() != "golang" {
		t.Fatalf("search copy = %q", toolkit.ClipboardText())
	}
	if !s.searchCopied {
		t.Fatal("copy should arm the search select-all highlight")
	}
	// Drawing paints the highlight (covers the topbar highlight branch). Prove it
	// actually changes pixels in the search band: render with the highlight, then
	// without, and require the search-box rows to differ.
	band := func() []byte {
		buf := make([]byte, s.W*s.H*4)
		s.Draw(buf)
		r := s.searchR
		out := make([]byte, 0, r.W*r.H*4)
		for y := r.Y; y < r.Y+r.H; y++ {
			o := (y*s.W + r.X) * 4
			out = append(out, buf[o:o+r.W*4]...)
		}
		return out
	}
	withHL := band()
	s.searchCopied = false // same text/focus, highlight off
	withoutHL := band()
	if bytes.Equal(withHL, withoutHL) {
		t.Fatal("the copied-highlight did not change any pixels in the search band")
	}
	s.searchCopied = true // restore for the clear-path checks below

	// Typing dismisses the highlight.
	s.TypeRune('x')
	if s.searchCopied {
		t.Fatal("typing should dismiss the search highlight")
	}
	// Re-copy, then Backspace dismisses it.
	s.Copy()
	s.Backspace()
	if s.searchCopied {
		t.Fatal("backspace should dismiss the search highlight")
	}
	// Re-copy, then a focus change dismisses it.
	s.Copy()
	s.FocusSearch(false)
	if s.searchCopied {
		t.Fatal("blur should dismiss the search highlight")
	}

	// Focused but empty → not the copy target (falls through).
	s.SetSearch("")
	s.FocusSearch(true)
	toolkit.SetClipboard(nil)
	if s.Copy() {
		t.Fatal("empty focused search should not be the copy target")
	}
}

func TestCopyAddressBarPrecedence(t *testing.T) {
	toolkit.SetClipboard(nil)
	s := New(900, 560, ThemeFor(OSMac, false))
	s.SelectPreview(webTestItem()) // web preview active (Link https://example.com/a)
	deliverPage(s, "https://example.com/a", "Title", 400, 1200)
	buf := make([]byte, s.W*s.H*4)
	s.Draw(buf)
	b := s.Browser().Bounds()
	_, _, addr := browserAddrRect(s, b)

	// Focus the address field, then Cmd/Ctrl+C copies the URL (not the article).
	s.ForwardBrowserClick(addr.X+addr.W/2, addr.Y+addr.H/2)
	if !s.Browser().AddressFocused() {
		t.Fatal("clicking the address field should focus it")
	}
	if !s.Copy() {
		t.Fatal("copy from the focused address bar should report true")
	}
	if got := toolkit.ClipboardText(); got != "https://example.com/a" {
		t.Fatalf("address-bar copy = %q, want the URL", got)
	}

	// Click into the page body (blurs the address): Copy now falls back to the
	// whole article instead of the URL-only copy.
	s.ForwardBrowserClick(b.X+b.W/2, b.Y+b.H-4)
	if s.Browser().AddressFocused() {
		t.Fatal("a content click should blur the address field")
	}
	if !s.Copy() {
		t.Fatal("copy should fall back to the article")
	}
}

func TestSceneCopyCurrentArticle(t *testing.T) {
	toolkit.SetClipboard(nil) // fresh in-process clipboard
	s := New(900, 560, ThemeFor(OSMac, false))

	// Nothing selected → nothing to copy.
	if s.Copy() {
		t.Fatal("copy with no article should report false")
	}

	// Preview an item: Copy grabs the preview item.
	s.SelectPreview(source.Item{Source: source.HackerNews, Title: "Prev", Body: "body", Link: "https://ex.com/p"})
	if !s.Copy() {
		t.Fatal("copy with a preview item should report true")
	}
	if got := toolkit.ClipboardText(); !strings.Contains(got, "Prev") || !strings.Contains(got, "https://ex.com/p") {
		t.Fatalf("preview copy = %q", got)
	}

	// The detail (reading) view takes precedence over the preview pane.
	s.OpenDetail(source.Item{Source: source.Reddit, Title: "Detail", Permalink: "https://ex.com/d"})
	if !s.Copy() || !strings.Contains(toolkit.ClipboardText(), "Detail") {
		t.Fatalf("detail copy = %q", toolkit.ClipboardText())
	}
}
