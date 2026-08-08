package app

import (
	"bytes"
	"context"
	"image"
	"io"
	"net/http"
	"sync"

	"github.com/go-news-reader/reader/provider/usenet"
	"github.com/go-news-reader/reader/ui"
)

// thumbMaxDim caps a decoded feed thumbnail's longest side. The card's thumb
// slot is ~104x60 logical pixels, so 512 stays generous even at a 3x display
// scale while keeping a 4000px press photo from costing tens of megabytes of
// RGBA per card.
const thumbMaxDim = 512

// maxThumbBytes bounds how much of a remote image is read before giving up, so
// a mislabelled video or a hostile host cannot exhaust memory.
const maxThumbBytes = 16 << 20

// mediaThumbWorkers bounds how many thumbnails download at once: enough to fill
// a screenful quickly, few enough not to look like a scraper to the media host.
const mediaThumbWorkers = 6

// PrefetchMedia downloads the remote thumbnails the shown feed is missing and
// hands each decoded image to the scene. Runs on the UI thread (posted after a
// refresh), the same thread that drains SetThumb, so the HasThumb reads inside
// MediaPrefetch are race-free; the downloads themselves are asynchronous.
func (a *App) PrefetchMedia() {
	reqs := a.scene.MediaPrefetch()
	if len(reqs) == 0 {
		return
	}
	a.mediaFetch(reqs)
}

// SetMediaFetchHook overrides the async thumbnail fetch (tests use a
// synchronous, network-free variant for determinism).
func (a *App) SetMediaFetchHook(f func(reqs []ui.MediaRequest)) { a.mediaFetch = f }

// SetMediaSync makes PrefetchMedia block until every thumbnail has been fetched.
// The one-shot CLI paths need it: they render (or serialise) the feed the moment
// Refresh returns, so an asynchronous fetch would always lose the race and emit
// placeholder-only images. The interactive window keeps the async default, where
// thumbnails filling in a frame or two later is exactly the wanted behaviour.
func (a *App) SetMediaSync() {
	a.mediaFetch = func(reqs []ui.MediaRequest) { a.loadMediaThumbs(context.Background(), reqs) }
}

// thumbResult carries one worker's decoded image back to the delivering
// goroutine; img is nil when the fetch produced nothing usable.
type thumbResult struct {
	id  string
	img *image.RGBA
}

// loadMediaThumbs fetches every request with bounded concurrency and hands each
// decoded thumbnail to the scene. A request that fails, times out or does not
// decode is simply skipped: the card keeps its placeholder rather than the feed
// failing.
//
// The DOWNLOADS fan out; the DELIVERY deliberately does not. App.post applies a
// scene write inline unless the front-end asked for it to be queued, so calling
// it from several workers at once would mutate the scene concurrently — the same
// single-writer contract the Usenet prefetch honours by posting from its one
// callback goroutine. Workers therefore hand their result down a channel, and
// this goroutine alone posts.
func (a *App) loadMediaThumbs(ctx context.Context, reqs []ui.MediaRequest) {
	out := make(chan thumbResult, len(reqs))
	sem := make(chan struct{}, mediaThumbWorkers)
	var wg sync.WaitGroup
	for _, r := range reqs {
		wg.Add(1)
		go func(r ui.MediaRequest) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			out <- thumbResult{id: r.ID, img: a.fetchThumb(ctx, r.URL)}
		}(r)
	}
	go func() { wg.Wait(); close(out) }()

	for res := range out {
		if res.img == nil {
			continue
		}
		a.post(func() { a.scene.SetThumb(res.id, res.img) })
	}
}

// fetchThumb GETs url and returns the image decoded and bounded to thumbMaxDim,
// or nil if anything about it is not a usable image. It goes through the shared
// browser-fingerprint client, so media hosts that fingerprint the TLS handshake
// (pbs.twimg.com among them) serve it, and the Network log records the fetch.
func (a *App) fetchThumb(ctx context.Context, url string) *image.RGBA {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil
	}
	resp, err := a.mediaClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxThumbBytes))
	if err != nil {
		return nil
	}
	// Thumbnail decodes, bounds and re-encodes in one step (shared with the
	// Usenet path); a non-image body simply errors here and is skipped.
	jpg, err := usenet.Thumbnail(raw, thumbMaxDim)
	if err != nil {
		return nil
	}
	img, _, err := decodeImage(bytes.NewReader(jpg))
	if err != nil {
		return nil
	}
	return toRGBA(img)
}
