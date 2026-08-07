package ui

import (
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
