package main

import (
	"bytes"
	"errors"
	"testing"

	"github.com/go-news-reader/reader/app"
	"github.com/go-news-reader/reader/internal/window"
	"github.com/go-news-reader/reader/source"
)

var errWindowProbe = errors.New("probe: window unavailable")

func TestStatusItemClose(t *testing.T) {
	// nil receiver and empty item are both no-ops (emitWindow defers Close before
	// onReady may have assigned one).
	var nilItem *statusItem
	nilItem.Close()
	(&statusItem{}).Close()

	// A real close func is invoked exactly once.
	closed := 0
	(&statusItem{closeFn: func() { closed++ }}).Close()
	if closed != 1 {
		t.Fatalf("closeFn called %d times, want 1", closed)
	}
}

// TestEmitWindowShowsTrayThenActivates proves the ordering the feature exists
// for: the tray goes up and the vault-reading activation runs only AFTER a frame
// is on screen, via onReady on the second frame — and the tray is closed when the
// window closes.
func TestEmitWindowShowsTrayThenActivates(t *testing.T) {
	a := app.New(app.Config{Registry: source.NewRegistry(), Width: 400, Height: 300})
	a.SetRefreshHook(func() {}) // refreshFeed must not reach the network

	// Stub the AppKit tray seam so the test touches no menu bar, and record it.
	made, closed := false, false
	origTray := newStatusItem
	newStatusItem = func(*app.App) *statusItem {
		made = true
		return &statusItem{closeFn: func() { closed = true }}
	}
	t.Cleanup(func() { newStatusItem = origTray })

	// Stub the window so it renders two frames — enough to fire onReady — then
	// returns as a closed window would.
	origOpen := openWindow
	var firedBeforeSecondFrame bool
	openWindow = func(_ window.Config, h window.Handler) error {
		h.Frame() // frame 1: onReady must NOT have run yet
		firedBeforeSecondFrame = made
		h.Frame() // frame 2: onReady runs (tray up, activation)
		return nil
	}
	t.Cleanup(func() { openWindow = origOpen })

	var out, errb bytes.Buffer
	if code := emitWindow(a, config{w: 400, h: 300}, &out, &errb); code != 0 {
		t.Fatalf("emitWindow code=%d err=%s", code, errb.String())
	}
	if firedBeforeSecondFrame {
		t.Fatal("the tray/activation ran before the first frame was shown")
	}
	if !made {
		t.Fatal("the status item was never created")
	}
	if !closed {
		t.Fatal("the status item was not closed when the window closed")
	}
}

// TestEmitWindowReportsOpenError covers the failure path: a window that cannot
// open is reported and the tray (never created) is closed harmlessly.
func TestEmitWindowReportsOpenError(t *testing.T) {
	a := app.New(app.Config{Registry: source.NewRegistry(), Width: 400, Height: 300})
	a.SetRefreshHook(func() {})
	origOpen := openWindow
	openWindow = func(window.Config, window.Handler) error { return errWindowProbe }
	t.Cleanup(func() { openWindow = origOpen })

	var out, errb bytes.Buffer
	if code := emitWindow(a, config{w: 400, h: 300}, &out, &errb); code != 1 {
		t.Fatalf("emitWindow code=%d, want 1 on an open failure", code)
	}
}
