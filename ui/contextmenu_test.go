package ui

import (
	"testing"

	"github.com/go-widgets/toolkit"

	"github.com/go-news-reader/reader/source"
)

// TestContextMenuOverlayLifecycle drives a menu through the scene's glue: open,
// draw as an overlay, navigate + fire an item by keyboard, and confirm it closed
// and dropped. The toolkit widget owns the behaviour; this checks the scene holds
// and forwards to it correctly.
func TestContextMenuOverlayLifecycle(t *testing.T) {
	s := New(1000, 700, ThemeFor(OSMac, false))
	s.SetSubs([]Subscription{{Source: source.HackerNews, Channel: "top"}})

	if _, ok := s.SubAt(-1); ok {
		t.Error("SubAt(-1) should report not-ok")
	}
	if _, ok := s.SubAt(99); ok {
		t.Error("SubAt(out-of-range) should report not-ok")
	}
	sub, ok := s.SubAt(0)
	if !ok || sub.Channel != "top" {
		t.Fatalf("SubAt(0) = %+v, %v", sub, ok)
	}

	if s.ContextMenuActive() {
		t.Fatal("no menu is open yet")
	}
	// Forwarding with no menu open is a no-op (must not panic).
	s.ContextMenuEvent(toolkit.Event{Kind: toolkit.EventKeyDown, Code: "Escape"})

	fired := 0
	m := toolkit.NewContextMenu(toolkit.NewMenu([]toolkit.MenuItem{
		{Label: "Do it", Action: func() { fired++ }},
	}))
	s.OpenContextMenu(m, 120, 140)
	if !s.ContextMenuActive() {
		t.Fatal("menu should be active after OpenContextMenu")
	}

	// The overlay paints on top of the frame without panicking.
	s.Draw(make([]byte, s.W*s.H*4))

	// Keyboard: highlight the first row, then fire it.
	s.ContextMenuEvent(toolkit.Event{Kind: toolkit.EventKeyDown, Code: "ArrowDown"})
	s.ContextMenuEvent(toolkit.Event{Kind: toolkit.EventKeyDown, Code: "Enter"})
	if fired != 1 {
		t.Errorf("item action fired %d times, want 1", fired)
	}
	if s.ContextMenuActive() {
		t.Error("menu should close once an item fires")
	}
}

// TestContextMenuDismissByEscape checks Escape closes the menu without firing an
// item.
func TestContextMenuDismissByEscape(t *testing.T) {
	s := New(800, 600, ThemeFor(OSMac, false))
	ran := false
	m := toolkit.NewContextMenu(toolkit.NewMenu([]toolkit.MenuItem{
		{Label: "X", Action: func() { ran = true }},
	}))
	s.OpenContextMenu(m, 400, 300)
	if !s.ContextMenuActive() {
		t.Fatal("menu should be open")
	}
	s.ContextMenuEvent(toolkit.Event{Kind: toolkit.EventKeyDown, Code: "Escape"})
	if s.ContextMenuActive() {
		t.Error("Escape should dismiss the menu")
	}
	if ran {
		t.Error("Escape should not fire an item")
	}
}
