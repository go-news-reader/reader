// Windowing: the reader opens its native window through go-widgets/application,
// which owns the run loop (over go-widgets/window's toolkit.Surface: Cocoa /
// win32 / X11) and composes a go-widgets/tray menu-bar item alongside it. This
// is the sole shipped path.

package main

import (
	"github.com/go-news-reader/reader/app"
	"github.com/go-news-reader/reader/internal/appicon"
	"github.com/go-news-reader/reader/internal/windowapp"
	"github.com/go-widgets/application"
	"github.com/go-widgets/tray"
)

// windowTitle is the native window's title.
const windowTitle = "News Reader"

// presentWindow opens the reader's native window through go-widgets/application,
// which owns the run loop, puts a menu-bar tray up beside it, and fires onReady
// once the first frame is on screen. Goes through the openWindow seam so tests
// substitute it.
func presentWindow(a *app.App, cfg config, onReady func()) error {
	spec := application.Spec{
		Name:       windowTitle,
		Identifier: "com.gonewsreader.reader",
		Icon:       appicon.Tray,
		Tray: func() *tray.Menu {
			return tray.NewMenu().Add(
				tray.Item("Quit News Reader", func() { a.PersistSettings(); osExit(0) }),
			)
		},
	}
	return openWindow(spec, application.Config{
		Title:  windowTitle,
		Width:  float64(cfg.w),
		Height: float64(cfg.h),
	}, windowapp.New(a), onReady)
}
