//go:build !darwin

package main

import "github.com/go-news-reader/reader/app"

// makeStatusItem is a no-op off macOS: there is no AppKit menu bar to put a
// status item in, so the reader simply runs without a tray.
func makeStatusItem(*app.App) *statusItem { return &statusItem{} }
