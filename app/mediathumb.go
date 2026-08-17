package app

import (
	"bytes"
	"context"
	"errors"
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
	reqs := a.claimMedia(a.scene.MediaPrefetch())
	if len(reqs) == 0 {
		return
	}
	a.mediaFetch(reqs)
}

// claimMedia keeps only the requests whose thumbnail is not already being
// fetched and marks the survivors in flight. RefreshStreaming posts PrefetchMedia
// on EVERY streaming update, so without this each still-downloading thumbnail
// would be re-requested on every one of the ~100 updates — a fetch storm.
// loadMediaThumbs releases each ID once its fetch finishes (success or failure),
// so a transient failure is retried on a later prefetch, exactly as the Usenet
// downloader's inflight map behaves.
func (a *App) claimMedia(reqs []ui.MediaRequest) []ui.MediaRequest {
	a.mediaMu.Lock()
	defer a.mediaMu.Unlock()
	if a.mediaInflight == nil {
		a.mediaInflight = map[string]bool{}
	}
	out := reqs[:0]
	for _, r := range reqs {
		if a.mediaInflight[r.ID] {
			continue
		}
		a.mediaInflight[r.ID] = true
		out = append(out, r)
	}
	return out
}

// releaseMedia clears one thumbnail's in-flight mark once its fetch has
// finished, so a later prefetch may request it again (only ever after a failure,
// since a success sets HasThumb, which MediaPrefetch skips).
func (a *App) releaseMedia(id string) {
	a.mediaMu.Lock()
	delete(a.mediaInflight, id)
	a.mediaMu.Unlock()
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
		a.releaseMedia(res.id) // fetch finished — let a later prefetch retry a failure
		if res.img == nil {
			continue
		}
		a.post(func() { a.scene.SetThumb(res.id, res.img) })
	}
}

// fetchThumb returns the card thumbnail for url, decoded and bounded to
// thumbMaxDim, or nil if it is not a usable image. It first consults the on-disk
// media cache: a hit reads the ORIGINAL media bytes straight from disk with NO
// network request (the whole point — media already seen is never re-downloaded,
// across restarts too) and derives the small card thumbnail from them, so the
// cached file the user browses in Finder is the real, correctly-typed media (a
// .gif animates, a .png keeps its alpha) while the card thumb is recomputed each
// time. On a miss it GETs url through the shared browser-fingerprint client (so
// media hosts that fingerprint the TLS handshake, pbs.twimg.com among them,
// serve it and the Network log records the fetch), then — only when the bytes
// are a usable image — caches the ORIGINAL for next time.
func (a *App) fetchThumb(ctx context.Context, url string) *image.RGBA {
	raw, cached := a.mediaCache.Get(url)
	if !cached {
		var derr error
		raw, derr = a.downloadMedia(ctx, url)
		if derr != nil {
			return nil
		}
	}
	img := thumbFromRaw(raw)
	if img == nil {
		return nil // a non-image (or undecodable) download is never cached
	}
	if !cached {
		a.mediaCache.Put(url, raw)
	}
	return img
}

// thumbFromRaw derives the bounded card thumbnail from raw original media bytes:
// usenet.Thumbnail decodes, bounds and re-encodes in one step (shared with the
// Usenet path; an animated GIF yields its first frame, which is all a static
// card needs), then the decodeImage seam re-decodes it to RGBA. nil when the
// bytes are not a usable image.
func thumbFromRaw(raw []byte) *image.RGBA {
	thumb, err := usenet.Thumbnail(raw, thumbMaxDim)
	if err != nil {
		return nil
	}
	img, _, err := decodeImage(bytes.NewReader(thumb))
	if err != nil {
		return nil
	}
	return toRGBA(img)
}

// errThumbStatus marks a non-200 media response, so downloadMedia reports a
// miss without caching anything.
var errThumbStatus = errors.New("media: non-200 response")

// downloadMedia GETs url and returns the RAW original bytes (bounded to
// maxThumbBytes), or an error for a transport failure or a non-200 response. The
// raw bytes — not a re-encode — are what the cache preserves, so Finder shows
// the real media.
func (a *App) downloadMedia(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := a.mediaClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errThumbStatus
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxThumbBytes))
	if err != nil {
		return nil, err
	}
	return raw, nil
}
