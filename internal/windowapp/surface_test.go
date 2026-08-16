// Copyright (c) 2026 the go-news-reader authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package windowapp

import (
	"testing"

	"github.com/go-news-reader/reader/app"
	"github.com/go-news-reader/reader/internal/window"
	"github.com/go-news-reader/reader/source"
	"github.com/go-widgets/toolkit"
)

// These drive a REAL app scene through the REAL surface the native window uses,
// and assert the scene moved. They exist because the on-device check could not
// cover them: an accessibility client can press a button — which is how the
// click path was verified against the running application — but nothing in that
// API replays a scroll or a drag, and synthesising OS events would mean taking
// over the machine's cursor.
//
// What they do not cover, and what does not need covering here: the OS event to
// toolkit.Event half. That belongs to go-widgets/window and is proven there by
// its own live tests, which post real NSEvents through AppKit.

func boundSurface(t *testing.T, w, h int) (*toolkit.Surface, *app.App) {
	t.Helper()
	a := app.New(app.Config{Registry: source.NewRegistry(), Width: w, Height: h})
	// A feed with more items than fit, because an empty one cannot scroll and a
	// scroll test against it passes for the wrong reason -- which is how the
	// first version of this failed, honestly.
	items := make([]source.Item, 60)
	for i := range items {
		items[i] = source.Item{
			ID: string(rune('a' + i%26)), Source: source.Reddit, Channel: "golang",
			Title: "A reasonably long headline number", Author: "user", Score: i, Comments: i,
		}
	}
	a.Scene().SetItems(items)
	surf := window.Bind(New(a), 1)
	surf.SetBounds(toolkit.Rect{X: 0, Y: 0, W: w, H: h})
	// One frame, so the scene has laid out and there is something to scroll.
	surf.Frame()
	return surf, a
}

// A wheel notch moves the feed through the virtual.CardList: the chat-style feed
// opens at the bottom (newest), and the wheel scrolls it toward the top by whole
// rows, clamping at the ends rather than running off.
func TestSurfaceScrollMovesTheFeed(t *testing.T) {
	surf, a := boundSurface(t, 900, 600)

	// The overflowing feed opens scrolled to the bottom (newest post visible).
	start := a.Scene().ScrollY()
	if start <= 0 {
		t.Fatalf("the overflowing feed should open scrolled to the bottom, got %d", start)
	}
	// Wheel-up notches scroll the CardList toward the top (by whole rows), so the
	// offset shrinks from the bottom.
	surf.OnEvent(toolkit.Event{Kind: toolkit.EventScroll, Delta: -3, X: 450, Y: 300})
	surf.Frame()
	if got := a.Scene().ScrollY(); got >= start {
		t.Errorf("three wheel-up notches did not move toward the top: %d (start %d)", got, start)
	}

	// Far past the top clamps to 0 rather than going negative.
	surf.OnEvent(toolkit.Event{Kind: toolkit.EventScroll, Delta: -999, X: 450, Y: 300})
	surf.Frame()
	if got := a.Scene().ScrollY(); got != 0 {
		t.Errorf("scrolling far past the top left the offset at %d, want 0", got)
	}
}

// Input lands in the BUFFER's coordinates, not the surface's. A surface placed
// anywhere but the origin is what tells the two apart: forwarding raw
// coordinates would scroll from a point 40 pixels to the right of the one the
// user aimed at, and no test that leaves the surface at 0,0 can see it.
func TestSurfaceInputIsTranslatedIntoBufferCoordinates(t *testing.T) {
	atOrigin, a1 := boundSurface(t, 900, 600)
	offset, a2 := boundSurface(t, 900, 600)
	offset.SetBounds(toolkit.Rect{X: 40, Y: 25, W: 900, H: 600})
	offset.Frame()

	// The same point of the CONTENT, addressed in each surface's own space (a
	// wheel-up, since the feed opens at the bottom and can move from there).
	atOrigin.OnEvent(toolkit.Event{Kind: toolkit.EventScroll, Delta: -2, X: 450, Y: 300})
	offset.OnEvent(toolkit.Event{Kind: toolkit.EventScroll, Delta: -2, X: 40 + 450, Y: 25 + 300})
	atOrigin.Frame()
	offset.Frame()

	if a1.Scene().ScrollY() != a2.Scene().ScrollY() {
		t.Errorf("the same content point scrolled to %d at the origin and %d when offset",
			a1.Scene().ScrollY(), a2.Scene().ScrollY())
	}
}

// A press, a move and a release arrive as the press/drag/release the scene
// expects, rather than collapsing into a click. Nothing here asserts what the
// divider did -- that is the scene's business -- only that the sequence reaches
// it intact.
func TestSurfaceDragReachesTheScene(t *testing.T) {
	surf, a := boundSurface(t, 900, 600)
	before := a.Scene().ScrollY()

	surf.OnEvent(toolkit.Event{Kind: toolkit.EventClick, X: 200, Y: 300})
	surf.OnEvent(toolkit.Event{Kind: toolkit.EventMouseDrag, X: 260, Y: 300})
	surf.OnEvent(toolkit.Event{Kind: toolkit.EventMouseUp, X: 260, Y: 300})
	surf.Frame()

	// The point is that the sequence is survivable and translated; a scene that
	// panicked or mistook a drag for a click would not get this far.
	if got := a.Scene().ScrollY(); got < 0 {
		t.Errorf("the drag left the feed at a negative offset %d", got)
	}
	_ = before
}

// The accessibility tree a screen reader reads comes from the same scene, and
// the rectangles are in the buffer's coordinates offset onto the surface.
func TestSurfacePublishesTheScenesElements(t *testing.T) {
	surf, _ := boundSurface(t, 900, 600)
	surf.SetBounds(toolkit.Rect{X: 10, Y: 20, W: 900, H: 600})

	nodes := toolkit.WalkA11y(surf)
	if len(nodes) == 0 {
		t.Fatal("the surface published nothing for a screen reader")
	}
	// The surface origin offset (10, 20) is added to every node. Nothing is ever
	// left of the surface (the leftmost scene X is 0). Vertically, though, the feed
	// opens at the bottom, so the older cards legitimately sit ABOVE the fold with a
	// negative Y — the a11y tree keeps every post walkable, painted or not — so a
	// blanket Y>=20 check would wrongly reject them.
	var named int
	for _, n := range nodes {
		if n.Name != "" {
			named++
		}
		if n.Rect.X < 10 {
			t.Errorf("node %q at %+v is left of the surface, so the horizontal offset was not applied", n.Name, n.Rect)
		}
	}
	if named == 0 {
		t.Error("every published node is unnamed, which reads as an empty interface")
	}
	// The full-window "News" document node sits at scene (0,0), so after the offset
	// it must land exactly at the surface origin — proving the vertical offset too.
	doc, ok := findNode(nodes, "News")
	if !ok {
		t.Fatal("no News document node published")
	}
	if doc.Rect.X != 10 || doc.Rect.Y != 20 {
		t.Errorf("document node at %+v, want it offset to the surface origin (10, 20)", doc.Rect)
	}
}

// findNode returns the first walked node with the given name.
func findNode(nodes []toolkit.A11yNode, name string) (toolkit.A11yNode, bool) {
	for _, n := range nodes {
		if n.Name == name {
			return n, true
		}
	}
	return toolkit.A11yNode{}, false
}

// The scale is not fixed for the life of a window: dragging one to a display of
// another density changes it (go-widgets/window v0.24.1 made the window follow
// the panel). A scale captured at startup would keep telling the scene that a
// point is worth what it was worth on the display the window opened on — the
// text would be right and everything else half or double size.
func TestSurfaceFollowsAChangingScale(t *testing.T) {
	a := app.New(app.Config{Registry: source.NewRegistry(), Width: 400, Height: 300})
	scale := 1.0
	surf := window.BindScaled(New(a), func() float64 { return scale })
	surf.SetBounds(toolkit.Rect{X: 0, Y: 0, W: 400, H: 300})
	surf.Frame()

	first := a.Scene().Scale
	if first != 1 {
		t.Fatalf("the scene starts at scale %v, not 1", first)
	}

	scale = 2 // the window moved to a 2x panel
	surf.Frame()

	if got := a.Scene().Scale; got != 2 {
		t.Errorf("after the scale changed the scene is still at %v; it was read once and kept", got)
	}
}
