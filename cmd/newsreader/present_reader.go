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
// It goes through the openWindow seam so cmd tests can substitute it.
func presentWindow(a *app.App, cfg config) error {
	return openWindow(window.Config{
		Title:  windowTitle,
		Width:  float64(cfg.w),
		Height: float64(cfg.h),
	}, windowapp.New(a))
}
