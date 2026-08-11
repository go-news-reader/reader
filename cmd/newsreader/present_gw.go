// Phase-1 adoption control: present the reader through the shared, org-owned
// github.com/go-widgets/window backend instead of the reader's own internal/window
// copy, selected with the `gwwindow` build tag:
//
//	go build -tags gwwindow ./cmd/newsreader && ./newsreader -window ...
//
// Everything else — the app, the scene, the ui rendering, the windowapp.Handler
// input/accessibility routing — is unchanged; only the OS window, its event loop
// and the platform accessibility bridge come from go-widgets/window (via the
// internal/windowgw adapter) rather than from internal/window. The default build
// (present_reader.go) is untouched, so this is a control that runs side by side
// with the shipped path, not a migration.
//
// This file is the launch boundary (gwwindow.Open + Backend.Run): it opens a real
// window and cannot be exercised headless, exactly like internal/window.Run and
// the default present_reader.go path. It is not compiled into the default build,
// so it is outside the coverage gate; the adapter logic it drives is unit-tested
// to 100% in internal/windowgw.

//go:build gwwindow

package main

import (
	gwwindow "github.com/go-widgets/window"

	"github.com/go-news-reader/reader/app"
	"github.com/go-news-reader/reader/internal/windowapp"
	"github.com/go-news-reader/reader/internal/windowgw"
)

// windowTitle is the native window's title, shared by both backends.
const windowTitle = "News Reader"

// presentWindow opens the shared go-widgets/window backend and blocks until it
// closes. The reader composes its own pixels, so it asks for a native-resolution
// framebuffer (crisp on Retina) and wires its framebuffer, input, accessibility,
// host look and OS clipboard through the internal/windowgw adapter.
func presentWindow(a *app.App, cfg config) error {
	h := windowapp.New(a)
	be, err := gwwindow.Open(gwwindow.Config{
		Title:       windowTitle,
		Width:       cfg.w,
		Height:      cfg.h,
		RenderScale: gwwindow.NativeScale,
	})
	if err != nil {
		return err
	}
	defer be.Close()

	windowgw.WireCapabilities(be, h)
	return be.Run(windowgw.Surface(h, windowgw.BackingScale(be, cfg.w)))
}
