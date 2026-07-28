package app

import (
	"context"
	"sync"

	"github.com/go-news-reader/reader/provider/usenet"
	"github.com/go-news-reader/reader/source"
	"github.com/go-news-reader/reader/ui"
)

// defaultDownloadWorkers bounds how many complete posts are reconstructed +
// saved in parallel (each over its own NNTP connection).
const defaultDownloadWorkers = 4

// fileDownloader reconstructs the complete binary posts the user ticks and saves
// them to the cache dir, in parallel over a bounded worker pool, publishing
// progress to the download panel. It owns the authoritative item list; the scene
// only displays snapshots pushed through App.post.
type fileDownloader struct {
	a     *App
	sem   chan struct{}
	async bool // false in tests for determinism

	mu    sync.Mutex
	items map[string]*ui.DownloadItem
	order []string
}

func newFileDownloader(a *App) *fileDownloader {
	return &fileDownloader{
		a:     a,
		sem:   make(chan struct{}, defaultDownloadWorkers),
		async: true,
		items: map[string]*ui.DownloadItem{},
	}
}

// toggle queues base for download, or cancels it if it is still queued (not yet
// started). An already-active/finished post is left alone. parts + cachePath are
// captured by the caller on the UI thread.
func (d *fileDownloader) toggle(base, name string, parts []usenet.ReconstructPart, cachePath string) {
	d.mu.Lock()
	if it, ok := d.items[base]; ok {
		if it.Status == ui.DLQueued { // cancel a not-yet-started queue entry
			delete(d.items, base)
			d.removeOrderLocked(base)
			d.mu.Unlock()
			d.publish()
			return
		}
		d.mu.Unlock() // active/done → ignore the toggle
		return
	}
	d.items[base] = &ui.DownloadItem{ID: base, Name: name, Status: ui.DLQueued, Total: len(parts)}
	d.order = append(d.order, base)
	d.mu.Unlock()
	d.publish()
	if d.async {
		go d.run(base, parts, cachePath)
	} else {
		d.run(base, parts, cachePath)
	}
}

// run reconstructs + saves one post, honouring the worker-pool semaphore.
func (d *fileDownloader) run(base string, parts []usenet.ReconstructPart, cachePath string) {
	d.sem <- struct{}{}
	defer func() { <-d.sem }()

	d.setStatus(base, ui.DLActive)
	prov, ok := d.a.reg.Get(source.Usenet)
	rc, okrc := prov.(reconstructor)
	if !ok || !okrc {
		d.setStatus(base, ui.DLFailed)
		return
	}
	req := usenet.ReconstructRequest{
		Parts:      parts,
		OnProgress: func(done, total int) { d.setProgress(base, done, total) },
	}
	files, _, err := rc.Reconstruct(context.Background(), req)
	if err != nil {
		d.setStatus(base, ui.DLFailed)
		return
	}
	if err := d.a.saveFilesTo(files, cachePath); err != nil {
		d.setStatus(base, ui.DLFailed)
		return
	}
	d.setStatus(base, ui.DLDone)
}

func (d *fileDownloader) setStatus(base string, st ui.DownloadStatus) {
	d.mu.Lock()
	if it, ok := d.items[base]; ok {
		it.Status = st
	}
	d.mu.Unlock()
	d.publish()
}

func (d *fileDownloader) setProgress(base string, done, total int) {
	d.mu.Lock()
	if it, ok := d.items[base]; ok {
		it.Done, it.Total, it.Status = done, total, ui.DLActive
	}
	d.mu.Unlock()
	d.publish()
}

// clear drops finished (done/failed) items.
func (d *fileDownloader) clear() {
	d.mu.Lock()
	kept := make([]string, 0, len(d.order))
	for _, b := range d.order {
		if it := d.items[b]; it.Status == ui.DLDone || it.Status == ui.DLFailed {
			delete(d.items, b)
		} else {
			kept = append(kept, b)
		}
	}
	d.order = kept
	d.mu.Unlock()
	d.publish()
}

func (d *fileDownloader) removeOrderLocked(base string) {
	out := d.order[:0]
	for _, b := range d.order {
		if b != base {
			out = append(out, b)
		}
	}
	d.order = out
}

// publish snapshots the item list (in order) onto the scene via the render-thread
// marshaler.
func (d *fileDownloader) publish() {
	d.mu.Lock()
	snap := make([]ui.DownloadItem, 0, len(d.order))
	for _, b := range d.order {
		if it, ok := d.items[b]; ok {
			snap = append(snap, *it)
		}
	}
	d.mu.Unlock()
	d.a.post(func() { d.a.scene.SetDownloads(snap) })
}

// ToggleDownload queues (or cancels) the download of the complete Usenet post
// with the given release base. Parts + the cache dir are captured here on the UI
// thread so the worker never reads the scene.
func (a *App) ToggleDownload(base string) {
	parts, ok := a.scene.GroupParts(base)
	if !ok {
		return
	}
	a.fdl.toggle(base, base, toReconstructParts(parts), a.scene.CachePath())
}

// ClearDownloads removes finished rows from the download panel.
func (a *App) ClearDownloads() { a.fdl.clear() }
