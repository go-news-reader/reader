// Windowing: the reader's own internal/window backend, an adapter over
// go-widgets/window's toolkit.Surface (Cocoa / win32 / X11). This is the sole
// shipped path.

package main

import (
	"github.com/go-news-reader/reader/app"
	"github.com/go-news-reader/reader/internal/window"
	"github.com/go-news-reader/reader/internal/windowapp"
)

// windowTitle is the native window's title.
const windowTitle = "News Reader"

// presentWindow opens the reader's own native window and blocks until it closes.
// It goes through the openWindow seam so cmd tests can substitute it. onReady
// runs once after the first frame is on screen (see [windowapp.Handler.SetOnReady]),
// which is where the caller defers the vault-reading startup so the window — and
// its Dock and status-item presence — is visible before the keychain prompt.
func presentWindow(a *app.App, cfg config, onReady func()) error {
	h := windowapp.New(a)
	h.SetOnReady(onReady)
	return openWindow(window.Config{
		Title:  windowTitle,
		Width:  float64(cfg.w),
		Height: float64(cfg.h),
	}, h)
}
