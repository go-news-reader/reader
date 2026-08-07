package ui

import (
	"strings"
	"testing"

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
	s := New(800, 600, ThemeFor(OSMac, false))
	// No writer installed → no-op, reports false.
	if s.copyToClipboard("x") {
		t.Fatal("copy with no writer should report false")
	}
	var got string
	s.SetClipboardWriter(func(text string) { got = text })
	// Blank text is not written.
	if s.copyToClipboard("   \n ") {
		t.Fatal("blank text should not be copied")
	}
	if got != "" {
		t.Fatalf("blank copy wrote %q", got)
	}
	// Real text is written and reported.
	if !s.copyToClipboard("hello") || got != "hello" {
		t.Fatalf("copy wrote %q, ok?", got)
	}
}

func TestSceneCopyCurrentArticle(t *testing.T) {
	s := New(900, 560, ThemeFor(OSMac, false))
	var got string
	s.SetClipboardWriter(func(text string) { got = text })

	// Nothing selected → nothing to copy.
	if s.Copy() {
		t.Fatal("copy with no article should report false")
	}

	// Preview an item: Copy grabs the preview item.
	s.SelectPreview(source.Item{Source: source.HackerNews, Title: "Prev", Body: "body", Link: "https://ex.com/p"})
	if !s.Copy() {
		t.Fatal("copy with a preview item should report true")
	}
	if !strings.Contains(got, "Prev") || !strings.Contains(got, "https://ex.com/p") {
		t.Fatalf("preview copy = %q", got)
	}

	// The detail (reading) view takes precedence over the preview pane.
	s.OpenDetail(source.Item{Source: source.Reddit, Title: "Detail", Permalink: "https://ex.com/d"})
	got = ""
	if !s.Copy() || !strings.Contains(got, "Detail") {
		t.Fatalf("detail copy = %q", got)
	}
}
