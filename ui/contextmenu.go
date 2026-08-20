// Copyright (c) the go-news-reader authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package ui

import (
	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
)

// SubAt returns the active-profile subscription at index, or ok=false when the
// index is out of range (e.g. AllFilter). The Handler uses it to name the source
// a sidebar right-click landed on.
func (s *Scene) SubAt(index int) (Subscription, bool) {
	if index < 0 || index >= len(s.Subs) {
		return Subscription{}, false
	}
	return s.Subs[index], true
}

// OpenContextMenu pops the given toolkit ContextMenu up at (x, y). The menu is
// bounded to the whole surface so it clamps itself on screen and treats a click
// outside its body as a dismissal. The scene owns none of the menu's behaviour —
// it only holds it so Draw can paint it and events can reach it.
func (s *Scene) OpenContextMenu(m *toolkit.ContextMenu, x, y int) {
	// Scale the menu to the display so its rows/gutters match the rest of the UI
	// on HiDPI, and give it the app's own shaped typeface (at a matching size)
	// instead of the toolkit's bare fallback face.
	m.Menu.Scale = s.Scale
	f := ttFont(false, rpxOf(s, 13))
	m.Font, m.Menu.Font = f, f
	s.ctxMenu = m
	m.SetBounds(s.surfaceRect())
	m.Popup(x, y)
	s.touch()
}

// ContextMenuActive reports whether a context menu is currently popped up, so the
// input path can route events to it instead of the scene beneath.
func (s *Scene) ContextMenuActive() bool { return s.ctxMenu != nil && s.ctxMenu.Open().Get() }

// ContextMenuEvent forwards one input event to the open context menu and drops
// the menu once it has closed itself (an item fired, a click landed outside, or
// Escape was pressed). It is a no-op when no menu is open.
func (s *Scene) ContextMenuEvent(ev toolkit.Event) {
	if !s.ContextMenuActive() {
		return
	}
	s.ctxMenu.SetBounds(s.surfaceRect())
	s.ctxMenu.OnEvent(ev)
	if !s.ctxMenu.Open().Get() {
		s.ctxMenu = nil
	}
	s.touch()
}

// drawContextMenuOverlay paints the open context menu on top of the scene. The
// toolkit widget does the drawing; this only supplies a painter over the frame
// buffer and the surface bounds it clamps within.
func (s *Scene) drawContextMenuOverlay(buf []byte) {
	if !s.ContextMenuActive() {
		return
	}
	p := painter.NewPixelPainter(buf, s.W, s.H)
	s.ctxMenu.SetBounds(s.surfaceRect())
	s.ctxMenu.Draw(p, s.theme)
}

// surfaceRect is the whole scene rectangle, the bounds a full-surface overlay
// clamps within.
func (s *Scene) surfaceRect() toolkit.Rect {
	return toolkit.Rect{X: 0, Y: 0, W: s.W, H: s.H}
}
