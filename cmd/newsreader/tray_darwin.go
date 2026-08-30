package main

import (
	"os"

	"github.com/go-macos/statusitem"
	"github.com/go-news-reader/reader/app"
)

// makeStatusItem puts the app in the macOS menu bar with a Quit action, through
// go-macos/statusitem (pure Go, CGO_ENABLED=0). It rides the NSApplication run
// loop that go-widgets/window owns, so it is called once that loop is running
// (from the window's onReady), never before. A failure to create it is
// non-fatal — the reader runs fine without a tray — so the error yields an empty
// (no-op Close) handle rather than aborting a launch over a menu-bar nicety.
func makeStatusItem(a *app.App) *statusItem {
	item, err := statusitem.New("📰", []statusitem.MenuItem{
		{Title: "Quit News Reader", Key: "q", Do: func() {
			a.PersistSettings()
			os.Exit(0)
		}},
	})
	if err != nil {
		return &statusItem{}
	}
	return &statusItem{closeFn: func() { _ = item.Close() }}
}
