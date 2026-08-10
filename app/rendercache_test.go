package app

import (
	"testing"

	"github.com/go-news-reader/reader/internal/webrender"
)

// entry builds a renderEntry holding n pixel bytes (n>0) for the size-bounded
// eviction tests.
func entry(n int) *renderEntry { return &renderEntry{pix: make([]byte, n), iw: 1, ih: n} }

func TestRenderCachePutGetAndStats(t *testing.T) {
	c := newRenderCache(1000)
	k := renderKey{"https://a/", 800}

	// Miss on an empty cache.
	if _, ok := c.get(k); ok {
		t.Fatal("empty cache should miss")
	}
	// An empty entry is never stored.
	c.put(k, &renderEntry{})
	c.put(k, nil)
	if pages, bytes := c.stats(); pages != 0 || bytes != 0 {
		t.Fatalf("empty/nil entry cached: pages=%d bytes=%d", pages, bytes)
	}
	// A real entry stores and hits, with links preserved.
	e := &renderEntry{pix: make([]byte, 100), iw: 10, ih: 10, links: []webrender.Link{{Href: "https://x"}}}
	c.put(k, e)
	got, ok := c.get(k)
	if !ok || got != e {
		t.Fatalf("get after put = %v ok=%v", got, ok)
	}
	if pages, bytes := c.stats(); pages != 1 || bytes != 100 {
		t.Fatalf("stats = %d pages %d bytes", pages, bytes)
	}
	// Replacing the same key adjusts the byte tally and keeps one page.
	c.put(k, entry(200))
	if pages, bytes := c.stats(); pages != 1 || bytes != 200 {
		t.Fatalf("after replace = %d pages %d bytes", pages, bytes)
	}
}

func TestRenderCacheLRUEviction(t *testing.T) {
	c := newRenderCache(250) // holds ~2 of the 100-byte entries below
	ka := renderKey{"https://a/", 800}
	kb := renderKey{"https://b/", 800}
	kd := renderKey{"https://d/", 800}
	c.put(ka, entry(100))
	c.put(kb, entry(100))
	// Touch a so b becomes least-recently-used.
	if _, ok := c.get(ka); !ok {
		t.Fatal("a should still be cached")
	}
	// Inserting d (100) pushes the total to 300 > 250 → evict the LRU (b).
	c.put(kd, entry(100))
	if _, ok := c.get(kb); ok {
		t.Fatal("b should have been evicted as least-recently-used")
	}
	if _, ok := c.get(ka); !ok {
		t.Fatal("a was touched and must survive")
	}
	if _, ok := c.get(kd); !ok {
		t.Fatal("the newly inserted d must be present")
	}
	if pages, bytes := c.stats(); pages != 2 || bytes != 200 {
		t.Fatalf("after eviction = %d pages %d bytes", pages, bytes)
	}
}

func TestRenderCacheOversizedEntryKept(t *testing.T) {
	c := newRenderCache(50)
	k := renderKey{"https://big/", 800}
	// A single entry larger than the whole budget still caches (Len()>1 guard
	// stops eviction from dropping the only entry).
	c.put(k, entry(500))
	if _, ok := c.get(k); !ok {
		t.Fatal("an oversized sole entry should still be cached")
	}
}

func TestRenderCacheClearAndInflight(t *testing.T) {
	c := newRenderCache(1000)
	k := renderKey{"https://a/", 800}

	// begin marks in-flight; a second begin (and one for a cached key) is refused.
	if !c.begin(k) {
		t.Fatal("first begin should win")
	}
	if c.begin(k) {
		t.Fatal("a second begin for an in-flight key must be refused")
	}
	c.done(k)
	if !c.begin(k) {
		t.Fatal("begin after done should win again")
	}
	c.done(k)

	c.put(k, entry(100))
	if c.begin(k) {
		t.Fatal("begin for an already-cached key must be refused")
	}

	c.clear()
	if pages, bytes := c.stats(); pages != 0 || bytes != 0 {
		t.Fatalf("clear should empty the cache: %d pages %d bytes", pages, bytes)
	}
	if _, ok := c.get(k); ok {
		t.Fatal("clear should drop entries")
	}
}
