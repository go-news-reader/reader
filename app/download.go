package app

import (
	"bytes"
	"context"
	"image"
	"sync"

	"github.com/go-news-reader/reader/provider/usenet"
	"github.com/go-news-reader/reader/source"
)

// imagePrefetcher is the Usenet-provider capability the download manager needs:
// download a batch of image posts in parallel over a connection pool.
type imagePrefetcher interface {
	PrefetchImages(ctx context.Context, reqs []usenet.ImageRequest, workers, maxDim int, onResult func(usenet.ImageResult))
}

// defaultImageWorkers is how many NNTP connections the manager downloads over in
// parallel — a handful, which real servers (incl. Free) tolerate.
const defaultImageWorkers = 4

// downloader is the app's parallel image download manager. It batches image
// requests, de-duplicates them against in-flight and already-cached ids, and
// downloads each batch concurrently over the provider's connection pool,
// delivering every decoded thumbnail to the scene (keyed by id) as it lands. One
// background run drains the queue; requests enqueued while it runs fold into the
// next batch. enqueue must be called on the UI thread (its HasThumb read is
// UI-thread scene state); results are marshalled back through App.post.
type downloader struct {
	a       *App
	workers int
	maxDim  int

	mu       sync.Mutex
	pending  []usenet.ImageRequest
	inflight map[string]bool
	running  bool
	async    bool // run the drain on its own goroutine (false in tests for determinism)
}

func newDownloader(a *App) *downloader {
	return &downloader{a: a, workers: defaultImageWorkers, maxDim: previewMaxDim, inflight: map[string]bool{}, async: true}
}

// enqueue adds requests (skipping ids already cached or in flight) and starts a
// background drain when one is not already running.
func (d *downloader) enqueue(reqs ...usenet.ImageRequest) {
	d.mu.Lock()
	for _, r := range reqs {
		if r.ID == "" || d.inflight[r.ID] || d.a.scene.HasThumb(r.ID) {
			continue
		}
		d.inflight[r.ID] = true
		d.pending = append(d.pending, r)
	}
	start := !d.running && len(d.pending) > 0
	if start {
		d.running = true
	}
	d.mu.Unlock()
	if start {
		if d.async {
			go d.run()
		} else {
			d.run()
		}
	}
}

func (d *downloader) run() {
	prov, _ := d.a.reg.Get(source.Usenet)
	pf, ok := prov.(imagePrefetcher)
	for {
		d.mu.Lock()
		batch := d.pending
		d.pending = nil
		if len(batch) == 0 {
			d.running = false
			d.mu.Unlock()
			return
		}
		d.mu.Unlock()

		if ok {
			pf.PrefetchImages(context.Background(), batch, d.workers, d.maxDim, func(res usenet.ImageResult) {
				var img *image.RGBA
				if res.Err == nil {
					img = decodeThumb(res.JPEG)
				}
				id := res.ID
				d.a.post(func() { d.a.scene.FinishPreviewImage(id, img) })
			})
		}
		// Clear the whole batch from inflight (delivered or not) so a later enqueue
		// can retry anything a cancelled/absent provider dropped.
		d.mu.Lock()
		for _, r := range batch {
			delete(d.inflight, r.ID)
		}
		d.mu.Unlock()
	}
}

// decodeThumb decodes a Thumbnail JPEG into an *image.RGBA (nil on failure).
func decodeThumb(jpg []byte) *image.RGBA {
	img, _, err := decodeImage(bytes.NewReader(jpg))
	if err != nil {
		return nil
	}
	return toRGBA(img)
}

// PrefetchImages enqueues every shown Usenet image post for parallel background
// download, so their thumbnails stream into the feed cards and preview pane. Safe
// to call repeatedly (it de-duplicates); must run on the UI thread.
func (a *App) PrefetchImages() {
	reqs := a.scene.ImagePrefetch()
	if len(reqs) == 0 {
		return
	}
	out := make([]usenet.ImageRequest, len(reqs))
	for i, r := range reqs {
		out[i] = usenet.ImageRequest{ID: r.ID, Parts: toReconstructParts(r.Parts)}
	}
	a.dl.enqueue(out...)
}
