// Package mediacacheplugin lets the news reader use an external process as its
// media-cache backend over gRPC (via hashicorp/go-plugin), so a shared/network
// cache can be plugged in without recompiling the reader. It reuses the same
// plugin framework as package sourceplugin, applied to mediacache.Cache instead
// of source.Provider.
//
// It has two faces:
//
//   - Plugin authors implement mediacache.Cache and call [Serve] from their
//     main(). That is the whole SDK: a cache plugin is a normal executable that
//     serves one cache.
//   - The host calls [Load] on a plugin binary path; it launches the executable,
//     wraps it in an adapter implementing mediacache.Cache, and returns the cache
//     plus a Close function that stops the subprocess.
//
// A minimal cache plugin looks like:
//
//	package main
//
//	import (
//		"github.com/go-news-reader/reader/mediacache"
//		"github.com/go-news-reader/reader/mediacacheplugin"
//	)
//
//	type myCache struct{ /* … */ }
//
//	func (myCache) Get(url string) ([]byte, bool) { /* … */ return nil, false }
//	func (myCache) Put(url string, data []byte)   { /* … */ }
//
//	func main() { mediacacheplugin.Serve(myCache{}) }
//
// Build it and point the reader's CacheBackend setting at the binary; the reader
// launches it at start and routes every media Get/Put through it.
//
// The thin go-plugin/gRPC transport and the generated protobuf code live under
// internal/mediacacheplugin (excluded from the coverage gate, being generated and
// subprocess-bound); the host's launch/adapter wiring kept here is ordinary logic
// held to 100% coverage.
package mediacacheplugin

import (
	goplugin "github.com/hashicorp/go-plugin"

	"github.com/go-news-reader/reader/internal/mediacacheplugin/transport"
	"github.com/go-news-reader/reader/mediacache"
)

// pluginServe is the go-plugin server entry point, injected as a package var so
// [Serve] can be unit-tested without blocking on a live server.
var pluginServe = goplugin.Serve

// Serve runs c as a plugin: it serves the MediaCache gRPC service backed by c
// over hashicorp/go-plugin and blocks until the host disconnects. A cache
// plugin author's main() calls this and nothing else.
func Serve(c mediacache.Cache) {
	pluginServe(transport.ServeConfig(c))
}

// launcher starts one plugin binary and returns a cache backed by it plus a
// function that kills the subprocess. It matches transport.Launch and is a seam
// so Load's wiring can be tested without spawning a real process.
type launcher func(path string) (mediacache.Cache, func() error, error)

// Load launches the media-cache plugin binary at path and returns a
// mediacache.Cache backed by the subprocess together with a Close function that
// stops it. A handshake/dial failure (path is missing or not one of our cache
// plugins) is returned as an error, so the host can fall back to the built-in
// cache rather than run without one.
func Load(path string) (mediacache.Cache, func() error, error) {
	return load(path, transport.Launch)
}

// load is Load with an injectable launcher, for testing.
func load(path string, launch launcher) (mediacache.Cache, func() error, error) {
	return launch(path)
}
