// Package sourceplugin lets the news reader load feed sources as external
// plugin binaries over gRPC (via hashicorp/go-plugin), so a new source can be
// added without recompiling the reader.
//
// It has two faces:
//
//   - Plugin authors implement source.Provider and call [Serve] from their
//     main(). That is the whole SDK: a plugin is a normal executable that serves
//     one provider.
//   - The host calls [Load] on a directory of such executables; it launches each
//     one, wraps it in an adapter implementing source.Provider, and returns the
//     providers plus a Close function that stops every subprocess.
//
// A minimal plugin looks like:
//
//	package main
//
//	import (
//		"context"
//
//		"github.com/go-news-reader/reader/source"
//		"github.com/go-news-reader/reader/sourceplugin"
//	)
//
//	type myProvider struct{}
//
//	func (myProvider) Kind() source.Kind { return "example" }
//	func (myProvider) Feed(context.Context, source.Query) (source.Result, error) {
//		return source.Result{Items: []source.Item{{ID: "1", Title: "hello"}}}, nil
//	}
//
//	func main() { sourceplugin.Serve(myProvider{}) }
//
// Build it and drop the binary into the reader's plugins directory
// (see settings.Settings.PluginsDirOrDefault); the reader discovers it at start.
//
// The thin go-plugin/gRPC transport and the generated protobuf code live under
// internal/sourceplugin (excluded from the coverage gate, being generated and
// subprocess-bound); the discovery, adapter wiring and mapping kept here and in
// internal/sourceplugin/protoconv are ordinary logic held to 100% coverage.
package sourceplugin

import (
	"errors"
	"os"
	"path/filepath"

	goplugin "github.com/hashicorp/go-plugin"

	"github.com/go-news-reader/reader/internal/sourceplugin/transport"
	"github.com/go-news-reader/reader/source"
)

// pluginServe is the go-plugin server entry point, injected as a package var so
// [Serve] can be unit-tested without blocking on a live server.
var pluginServe = goplugin.Serve

// Serve runs p as a plugin: it serves the SourceProvider gRPC service backed by
// p over hashicorp/go-plugin and blocks until the host disconnects. A plugin
// author's main() calls this and nothing else.
func Serve(p source.Provider) {
	pluginServe(transport.ServeConfig(p))
}

// launcher starts one plugin binary and returns a provider backed by it plus a
// function that kills the subprocess. It matches transport.Launch and is a seam
// so Load's discovery logic can be tested without spawning real processes.
type launcher func(path string) (source.Provider, func() error, error)

// Load scans dir for plugin executables, launches each one, and returns the
// discovered providers together with a Close function that stops every plugin
// subprocess. Files that are not launchable source plugins are skipped, and a
// missing directory is not an error (no plugins installed is a no-op): in both
// cases Load returns whatever it could load and a Close that is always safe to
// call. Load only errors when dir exists but cannot be read as a directory.
func Load(dir string) ([]source.Provider, func() error, error) {
	return load(dir, transport.Launch)
}

// load is Load with an injectable launcher, for testing.
func load(dir string, launch launcher) ([]source.Provider, func() error, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// No plugins directory yet: a clean no-op, with a Close that does nothing.
			return nil, func() error { return nil }, nil
		}
		return nil, nil, err
	}

	var providers []source.Provider
	var closers []func() error
	closeAll := func() error {
		var errs []error
		for _, c := range closers {
			if e := c(); e != nil {
				errs = append(errs, e)
			}
		}
		return errors.Join(errs...)
	}

	for _, e := range entries {
		// Only plain files are plugin candidates; a subdirectory or symlink is
		// skipped. Non-plugin files fail the handshake in launch and are skipped too.
		if !e.Type().IsRegular() {
			continue
		}
		prov, kill, lerr := launch(filepath.Join(dir, e.Name()))
		if lerr != nil {
			continue
		}
		providers = append(providers, prov)
		closers = append(closers, kill)
	}
	return providers, closeAll, nil
}
