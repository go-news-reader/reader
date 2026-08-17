package app

import (
	"github.com/go-news-reader/reader/internal/settings"
	"github.com/go-news-reader/reader/internal/webrender"
	"github.com/go-news-reader/reader/mediacache"
	"github.com/go-news-reader/reader/mediacacheplugin"
)

// mediaCacheDir resolves the on-disk media cache directory. A package var (not a
// direct call to mediacache.DefaultDir) so tests can point the reader's built-in
// DiskCache at a throwaway dir, mirroring groupCacheDir. initMediaCache wires it
// into the DiskCache it constructs.
var mediaCacheDir = mediacache.DefaultDir

// loadMediaCachePlugin launches a cache-plugin binary and returns it as a
// mediacache.Cache plus a Close. A seam (default mediacacheplugin.Load) so tests
// select the plugin backend without spawning a real subprocess.
var loadMediaCachePlugin = mediacacheplugin.Load

// newDiskCache builds the built-in on-disk cache, pointed at the (test-override-
// able) mediaCacheDir and bounded to the settings' media-cache byte budget.
func newDiskCache(set *settings.Settings) *mediacache.DiskCache {
	dc := mediacache.NewDiskCache(set.MediaCacheBytes())
	dc.Dir = mediaCacheDir
	return dc
}

// initMediaCache selects the media-cache backend from the settings: an empty or
// "local" CacheBackend uses the built-in on-disk DiskCache, while a filesystem
// path names a cache-plugin binary to launch and drive over gRPC. A plugin that
// fails to load never crashes the reader: it falls back to the DiskCache and
// records a status line so the user sees why the shared cache is not in effect.
func (a *App) initMediaCache(set *settings.Settings) {
	if !settings.IsCachePlugin(set.CacheBackend) {
		a.mediaCache = newDiskCache(set)
		return
	}
	cache, closeFn, err := loadMediaCachePlugin(set.CacheBackend)
	if err != nil {
		a.mediaCache = newDiskCache(set)
		a.vmStatus("Cache plugin failed to load; using local disk cache")
		a.wireImageCache()
		return
	}
	a.mediaCache = cache
	a.mediaCacheClose = closeFn
	a.wireImageCache()
}

// wireImageCache hands the selected media cache to the web renderer so the
// engine serves page images from it (the on-disk cache, or a shared plugin
// backend) instead of re-downloading them on every render. It is a no-op for a
// renderer that lacks the capability (a test/CLI stub), mirroring SetBackdrop.
func (a *App) wireImageCache() {
	if ic, ok := a.webRender.(interface {
		SetImageCache(webrender.ImageCache)
	}); ok {
		ic.SetImageCache(a.mediaCache)
	}
}

// cacheMedia stores the ORIGINAL media bytes for url in the selected media
// cache, stamping the on-disk file's modification time with the POST's creation
// time (created, Unix seconds UTC) when it is known (>0) so the cache mirrors
// post chronology rather than download order. A backend that cannot honour a
// timed write (a plugin whose store has no per-entry mtime) falls back to a
// plain Put; either way the cache key is unchanged, so a later cache hit still
// finds the same entry.
func (a *App) cacheMedia(url string, data []byte, created int64) {
	if tc, ok := a.mediaCache.(mediacache.TimedCache); ok {
		tc.PutTimed(url, data, created)
		return
	}
	a.mediaCache.Put(url, data)
}

// applyMediaCacheBudget re-applies the settings' media-cache byte budget to the
// built-in DiskCache after a live settings edit. It is a no-op for a plugin
// backend, which manages its own capacity out of process.
func (a *App) applyMediaCacheBudget(set *settings.Settings) {
	if dc, ok := a.mediaCache.(*mediacache.DiskCache); ok {
		dc.Budget = set.MediaCacheBytes()
	}
}
