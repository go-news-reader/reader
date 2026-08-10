package app

import (
	"container/list"
	"sync"

	"github.com/go-news-reader/reader/internal/webrender"
)

// renderCacheMaxBytes bounds the total pixel bytes the web-preview render cache
// holds. A full-page render at the browser's content width is a few MB (a
// 900px-wide page a few thousand pixels tall is ~10–15 MB), so ~96 MB keeps a
// handful of recently viewed and pre-rendered pages resident — enough to make
// re-selecting them instant — without growing without bound.
const renderCacheMaxBytes = 96 << 20

// prefetchWorkers bounds how many background pre-renders run at once, and
// prefetchCount how many neighbours a single selection warms — kept small so
// prefetch saves time without hammering the network.
const (
	prefetchWorkers = 2
	prefetchCount   = 2
)

// renderKey identifies a cached render: the page URL at the pixel width it was
// rendered for. A different width relays the page out, so it is a distinct
// entry (and a distinct cache line).
type renderKey struct {
	url   string
	width int
}

// renderEntry is one cached final render: the pixels (an owned copy — the cache
// never aliases a renderer's frame buffer), the image dimensions, the clickable
// link map, and the post-redirect final URL the frame reported.
type renderEntry struct {
	pix      []byte
	iw, ih   int
	links    []webrender.Link
	finalURL string
}

// renderCache is a bounded, LRU, concurrency-safe store of final web-preview
// renders keyed by (url, width). loadPreviewPage runs on a fresh goroutine per
// navigation and the prefetch workers run concurrently, so every access is
// guarded by mu. Eviction is by total pixel bytes: inserting past the bound
// drops least-recently-used entries until it fits again.
//
// inflight records the keys a render is currently in progress for (a real
// navigation or a prefetch), so prefetch can skip a page that is already being
// produced rather than rendering it twice.
type renderCache struct {
	mu       sync.Mutex
	max      int
	bytes    int
	ll       *list.List // front = most-recently-used; Value is *renderCacheItem
	items    map[renderKey]*list.Element
	inflight map[renderKey]struct{}
}

// renderCacheItem is the list node payload: the key (so eviction can delete the
// map entry) and the cached render.
type renderCacheItem struct {
	key   renderKey
	entry *renderEntry
}

// newRenderCache builds an empty cache bounded to max total pixel bytes.
func newRenderCache(max int) *renderCache {
	return &renderCache{
		max:      max,
		ll:       list.New(),
		items:    make(map[renderKey]*list.Element),
		inflight: make(map[renderKey]struct{}),
	}
}

// get returns the cached entry for k (marking it most-recently-used) and true,
// or nil/false on a miss.
func (c *renderCache) get(k renderKey) (*renderEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[k]
	if !ok {
		return nil, false
	}
	c.ll.MoveToFront(el)
	return el.Value.(*renderCacheItem).entry, true
}

// put stores e under k as most-recently-used and evicts least-recently-used
// entries until the total pixel budget is respected. An empty entry (no pixels)
// is never stored — an error/blank delivery must not be cached.
func (c *renderCache) put(k renderKey, e *renderEntry) {
	if e == nil || len(e.pix) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[k]; ok {
		// Replace an existing entry (e.g. a re-render): adjust the byte tally and
		// promote it to most-recently-used.
		it := el.Value.(*renderCacheItem)
		c.bytes += len(e.pix) - len(it.entry.pix)
		it.entry = e
		c.ll.MoveToFront(el)
	} else {
		c.items[k] = c.ll.PushFront(&renderCacheItem{key: k, entry: e})
		c.bytes += len(e.pix)
	}
	// Evict from the back until within budget, always keeping at least the entry
	// just inserted (so a single oversized page still caches rather than thrashing).
	// Len() > 1 guarantees Back() is non-nil.
	for c.bytes > c.max && c.ll.Len() > 1 {
		el := c.ll.Back()
		it := el.Value.(*renderCacheItem)
		c.ll.Remove(el)
		delete(c.items, it.key)
		c.bytes -= len(it.entry.pix)
	}
}

// begin marks k as in-flight and returns true when the caller should render it:
// false if it is already cached or another render for the same key is already
// running. A true result must be paired with a done(k).
func (c *renderCache) begin(k renderKey) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.items[k]; ok {
		return false
	}
	if _, ok := c.inflight[k]; ok {
		return false
	}
	c.inflight[k] = struct{}{}
	return true
}

// done clears the in-flight mark for k (paired with a true begin).
func (c *renderCache) done(k renderKey) {
	c.mu.Lock()
	delete(c.inflight, k)
	c.mu.Unlock()
}

// clear empties the cache (its entries and byte tally); in-flight marks are left
// alone so a render already running still records its own completion.
func (c *renderCache) clear() {
	c.mu.Lock()
	c.ll.Init()
	c.items = make(map[renderKey]*list.Element)
	c.bytes = 0
	c.mu.Unlock()
}

// stats reports the number of cached pages and their total pixel bytes.
func (c *renderCache) stats() (pages, bytes int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ll.Len(), c.bytes
}
