package usenet

import (
	"context"
	"errors"
	"sort"
	"sync"

	"github.com/go-newsgroups/yenc"
)

// ImageRequest names one image post to prefetch: an id the caller keys results
// by (e.g. a feed item id) and the member articles to fetch, yEnc-decode and
// reassemble into the image file.
type ImageRequest struct {
	ID    string
	Parts []ReconstructPart
}

// ImageResult carries the outcome for one ImageRequest: a bounded JPEG thumbnail
// (Thumbnail-encoded) on success, or Err.
type ImageResult struct {
	ID   string
	JPEG []byte
	Err  error
}

// ErrNoImage is returned when none of a post's reassembled files decode as an
// image.
var ErrNoImage = errors.New("usenet: no decodable image in post")

// PrefetchImages downloads the images for reqs concurrently over up to `workers`
// NNTP connections — each opened once and REUSED across many requests, so a batch
// of posts (from one or several groups) streams in parallel like a download
// manager rather than one-connection-per-post. Every request's outcome is
// delivered through onResult, which is invoked from worker goroutines and so must
// be safe for concurrent use. Thumbnails are bounded to maxDim. workers is
// clamped to [1, len(reqs)]; empty reqs (or a nil callback) is a no-op. It blocks
// until every fed request has been delivered. A cancelled ctx stops feeding new
// requests; those already dispatched still complete. A ctx cancelled before the
// call dispatches nothing at all, so onResult is never invoked.
func (p *Provider) PrefetchImages(ctx context.Context, reqs []ImageRequest, workers, maxDim int, onResult func(ImageResult)) {
	if len(reqs) == 0 || onResult == nil {
		return
	}
	if workers < 1 {
		workers = 1
	}
	if workers > len(reqs) {
		workers = len(reqs)
	}
	tasks := make(chan ImageRequest)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// One connection per worker, reused for every request it pulls. A worker
			// that cannot connect still drains its tasks, reporting the dial error, so
			// the feeder never blocks.
			c, cerr := p.connect(ctx)
			if c != nil {
				defer c.Close()
			}
			for req := range tasks {
				if cerr != nil {
					onResult(ImageResult{ID: req.ID, Err: mapErr(cerr)})
					continue
				}
				jpg, err := imageFromParts(c, req.Parts, maxDim)
				onResult(ImageResult{ID: req.ID, JPEG: jpg, Err: err})
			}
		}()
	}
feed:
	for _, r := range reqs {
		// A context that is ALREADY done wins outright, before the offer is made.
		//
		// The select below cannot give it priority: when a worker is ready to
		// receive and the context is done, both cases are ready and Go picks
		// uniformly at random. So a prefetch cancelled before the feeder got to its
		// first offer dispatched work anyway -- about one round in two hundred with
		// two workers, and in every hundred rounds with eight, which is what made
		// TestPrefetchImagesCtxCancel fail on a loaded CI runner rather than on an
		// idle laptop.
		if ctx.Err() != nil {
			break feed
		}
		select {
		case tasks <- r:
		case <-ctx.Done():
			break feed
		}
	}
	close(tasks)
	wg.Wait()
}

// imageFromParts fetches every part on c, yEnc-decodes and reassembles by
// filename, and returns a bounded JPEG thumbnail of the first file (by name) that
// decodes as an image. It does not run PAR2 — an image post is normally a single
// intact file and a preview needs no repair.
func imageFromParts(c conn, parts []ReconstructPart, maxDim int) ([]byte, error) {
	if len(parts) == 0 {
		return nil, ErrNoImage
	}
	files := map[string][]byte{}
	for _, part := range parts {
		art, err := c.Article(bracketID(part.MessageID))
		if err != nil {
			return nil, err
		}
		dec, err := yenc.Decode([]byte(art.Body))
		if err != nil {
			return nil, err
		}
		name := part.Filename
		if name == "" {
			name = dec.Name
		}
		files[name] = placePart(files[name], dec)
	}
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if jpg, err := Thumbnail(files[n], maxDim); err == nil {
			return jpg, nil
		}
	}
	return nil, ErrNoImage
}
