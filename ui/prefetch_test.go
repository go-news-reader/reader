package ui

import (
	"testing"

	"github.com/go-news-reader/reader/source"
)

func webIt(id, link string) source.Item {
	return source.Item{ID: id, Source: source.HackerNews, Title: "t" + id, Link: link}
}

// TestPrefetchWebURLs covers neighbour selection for the background pre-render:
// the n<=0 and unknown-item guards, skipping non-web neighbours, de-duplication,
// out-of-range offsets, and the n-limit.
func TestPrefetchWebURLs(t *testing.T) {
	s := New(900, 600, ThemeFor(OSLinux, false))

	// n<=0 yields nothing.
	if got := s.PrefetchWebURLs(webIt("b", "https://b/"), 0); got != nil {
		t.Fatalf("n<=0 = %v, want nil", got)
	}
	// Empty feed (no rows) → item not found → nil.
	if got := s.PrefetchWebURLs(webIt("b", "https://b/"), 2); got != nil {
		t.Fatalf("empty feed = %v, want nil", got)
	}

	// Feed with a non-web (Usenet) neighbour that must be skipped.
	feed := []source.Item{
		webIt("a", "https://a/"),
		{ID: "u", Source: source.Usenet, Title: "post"}, // no web URL
		webIt("b", "https://b/"),
		webIt("c", "https://c/"),
	}
	s.SetItems(feed)
	s.layout()

	// Unknown item → nil.
	if got := s.PrefetchWebURLs(webIt("zz", "https://zz/"), 2); got != nil {
		t.Fatalf("unknown item = %v, want nil", got)
	}
	// From b (index 2): prev = u (skipped, no URL), next = c, then -2 = a; n=2.
	got := s.PrefetchWebURLs(feed[2], 2)
	if len(got) != 2 || got[0] != "https://c/" || got[1] != "https://a/" {
		t.Fatalf("neighbours of b = %v, want [https://c/ https://a/]", got)
	}

	// De-duplication + out-of-range offsets: a centre whose prev and next share a
	// URL, with no items two steps out.
	dup := []source.Item{
		webIt("x", "https://same/"),
		webIt("y", "https://mid/"),
		webIt("z", "https://same/"),
	}
	s.SetItems(dup)
	s.layout()
	got = s.PrefetchWebURLs(dup[1], 3)
	if len(got) != 1 || got[0] != "https://same/" {
		t.Fatalf("dedup neighbours = %v, want [https://same/]", got)
	}
}
