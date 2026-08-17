// Copyright (c) 2026 the go-news-reader authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package window

import (
	"image/color"
	"testing"

	"github.com/go-widgets/toolkit"
	gw "github.com/go-widgets/window"
)

// recorder is a Handler that writes down what it was told, so a test can assert
// the translation rather than the window.
type recorder struct {
	calls     []string
	elements  []A11yElement
	appear    []SystemAppearance
	clipboard SystemClipboard
	resizes   [][3]float64
}

func (r *recorder) Frame() ([]byte, int, int, bool) { return nil, 0, 0, false }
func (r *recorder) Resize(w, h int, scale float64) {
	r.resizes = append(r.resizes, [3]float64{float64(w), float64(h), scale})
}
func (r *recorder) MouseDown(x, y int) { r.log("down", x, y) }
func (r *recorder) MouseMove(x, y int) { r.log("move", x, y) }
func (r *recorder) MouseUp(x, y int)   { r.log("up", x, y) }
func (r *recorder) Scroll(dy int)      { r.log("scroll", dy, 0) }
func (r *recorder) Key(name string, ru rune) {
	r.calls = append(r.calls, "key:"+name+":"+string(ru))
}
func (r *recorder) Shortcut(ru rune, ctrl, meta bool) {
	s := "shortcut:" + string(ru)
	if ctrl {
		s += ":ctrl"
	}
	if meta {
		s += ":meta"
	}
	r.calls = append(r.calls, s)
}
func (r *recorder) A11yElements() []A11yElement          { return r.elements }
func (r *recorder) SystemAppearance(a SystemAppearance)  { r.appear = append(r.appear, a) }
func (r *recorder) SetSystemClipboard(c SystemClipboard) { r.clipboard = c }

func (r *recorder) log(kind string, a, b int) {
	r.calls = append(r.calls, kind+":"+itoa(a)+","+itoa(b))
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var b []byte
	for v > 0 {
		b = append([]byte{byte('0' + v%10)}, b...)
		v /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}

// The arrows are the divergence this whole file exists for: the toolkit says
// "ArrowUp", this application has always said "Up", and nothing errors when
// they disagree — the key just stops working.
func TestKeyNameTranslatesTheArrows(t *testing.T) {
	for code, want := range map[string]string{
		"ArrowUp":    "Up",
		"ArrowDown":  "Down",
		"ArrowLeft":  "Left",
		"ArrowRight": "Right",
		"Enter":      "Enter",
		"Escape":     "Escape",
		"Backspace":  "Backspace",
		"Tab":        "Tab",      // preview-focus toggle
		"PageUp":     "PageUp",   // preview page-scroll
		"PageDown":   "PageDown", // preview page-scroll
		"F7":         "",         // not part of this application's vocabulary
		"a":          "",         // a character arrives as EventChar, not by name
		"":           "",
	} {
		if got := keyName(code); got != want {
			t.Errorf("keyName(%q) = %q, want %q", code, got, want)
		}
	}
}

// A wheel notch is a ROW in the toolkit and DEVICE PIXELS here.
func TestScrollConvertsRowsToPixels(t *testing.T) {
	r := &recorder{}
	route(r, toolkit.Event{Kind: toolkit.EventScroll, X: 200, Y: 50, Delta: 3})
	route(r, toolkit.Event{Kind: toolkit.EventScroll, X: 200, Y: 50, Delta: -1})
	// Each wheel notch first applies its pointer position (so the scroll routes to
	// the pane under the cursor), then scrolls.
	want := []string{"move:200,50", "scroll:120,0", "move:200,50", "scroll:-40,0"}
	assertCalls(t, r, want)
}

// TestWheelAppliesItsPointerPosition guards the fix for scrolling the wrong pane:
// the wheel's own coordinates must reach the handler as a pointer move BEFORE the
// scroll, so routing sees the cursor over the preview even when no hover move
// arrived between notches.
func TestWheelAppliesItsPointerPosition(t *testing.T) {
	r := &recorder{}
	route(r, toolkit.Event{Kind: toolkit.EventScroll, X: 640, Y: 300, Delta: 1})
	want := []string{"move:640,300", "scroll:40,0"}
	assertCalls(t, r, want)
}

func TestRoutePointerKinds(t *testing.T) {
	r := &recorder{}
	route(r, toolkit.Event{Kind: toolkit.EventClick, X: 3, Y: 4})
	route(r, toolkit.Event{Kind: toolkit.EventMouseDrag, X: 5, Y: 6})
	route(r, toolkit.Event{Kind: toolkit.EventMouseMove, X: 7, Y: 8})
	route(r, toolkit.Event{Kind: toolkit.EventMouseUp, X: 9, Y: 10})
	// A kind this application has no use for is dropped, not mistranslated.
	route(r, toolkit.Event{Kind: toolkit.EventDrop, X: 11, Y: 12})
	assertCalls(t, r, []string{"down:3,4", "move:5,6", "move:7,8", "up:9,10"})
}

// A command chord carries a rune Key has no room for, so it goes to Shortcut.
func TestRouteSendsChordsToTheShortcutSink(t *testing.T) {
	r := &recorder{}
	route(r, toolkit.Event{Kind: toolkit.EventKeyDown, Code: "=", Ctrl: true, Meta: true})
	route(r, toolkit.Event{Kind: toolkit.EventKeyDown, Code: "c", Ctrl: true})
	// A NAMED key with a modifier is not a shortcut: it stays a key.
	route(r, toolkit.Event{Kind: toolkit.EventKeyDown, Code: "ArrowDown", Ctrl: true})
	// And without a modifier, a named key is just a key.
	route(r, toolkit.Event{Kind: toolkit.EventKeyDown, Code: "Escape"})
	assertCalls(t, r, []string{"shortcut:=:ctrl:meta", "shortcut:c:ctrl", "key:Down:\x00", "key:Escape:\x00"})
}

// Printable input arrives as EventChar and reaches Key as a rune.
func TestRouteCharacters(t *testing.T) {
	r := &recorder{}
	route(r, toolkit.Event{Kind: toolkit.EventChar, Code: "é"})
	route(r, toolkit.Event{Kind: toolkit.EventChar, Code: ""}) // nothing to send
	assertCalls(t, r, []string{"key::é"})
}

// A handler with no shortcut sink still gets the key rather than nothing.
func TestRouteWithoutAShortcutSink(t *testing.T) {
	h := &plainHandler{}
	route(h, toolkit.Event{Kind: toolkit.EventKeyDown, Code: "Escape", Meta: true})
	if h.keys != 1 {
		t.Errorf("a modified named key reached Key %d times, want 1", h.keys)
	}
}

type plainHandler struct{ keys int }

func (p *plainHandler) Frame() ([]byte, int, int, bool) { return nil, 0, 0, false }
func (p *plainHandler) Resize(int, int, float64)        {}
func (p *plainHandler) MouseDown(int, int)              {}
func (p *plainHandler) MouseMove(int, int)              {}
func (p *plainHandler) MouseUp(int, int)                {}
func (p *plainHandler) Scroll(int)                      {}
func (p *plainHandler) Key(string, rune)                { p.keys++ }

func TestElementsMapIntoTheToolkitShape(t *testing.T) {
	r := &recorder{elements: []A11yElement{
		{Role: "button", Name: "Refresh", Value: "v", X: 1, Y: 2, W: 3, H: 4},
	}}
	got := elements(r)
	if len(got) != 1 {
		t.Fatalf("mapped %d elements, want 1", len(got))
	}
	if got[0].Role != toolkit.Role("button") || got[0].Name != "Refresh" || got[0].Value != "v" {
		t.Errorf("element = %+v, want the role, name and value carried over", got[0])
	}
	if got[0].X != 1 || got[0].Y != 2 || got[0].W != 3 || got[0].H != 4 {
		t.Errorf("element rect = %+v, want 1,2 3x4", got[0])
	}
	if elements(&recorder{}) != nil {
		t.Error("nothing to describe should map to nothing, not an empty slice")
	}
}

// fakeAppearance is a window that answers appearance questions, so the pump can
// be tested without opening anything.
type fakeAppearance struct {
	gw.Backend
	ap   gw.Appearance
	ttf  []byte
	err  error
	read int
}

func (f *fakeAppearance) Appearance() gw.Appearance      { f.read++; return f.ap }
func (f *fakeAppearance) SystemFontTTF() ([]byte, error) { return f.ttf, f.err }

// The window answers questions; the handler wants to be told. The pump pushes
// once at start WITH the font, then only when the look changes and never with
// the font again.
func TestAppearancePumpPushesOnlyOnChange(t *testing.T) {
	win := &fakeAppearance{ap: gw.Appearance{Dark: true}, ttf: []byte("FONT")}
	h := &recorder{}
	p := newAppearancePump(win, h)

	p.start()
	if len(h.appear) != 1 || !h.appear[0].Dark || string(h.appear[0].FontTTF) != "FONT" {
		t.Fatalf("first push = %+v, want dark with the font", h.appear)
	}

	p.poll() // unchanged
	if len(h.appear) != 1 {
		t.Errorf("an unchanged look pushed again: %d pushes", len(h.appear))
	}

	win.ap = gw.Appearance{Dark: false, Accent: color.RGBA{R: 1, A: 255}, HasAccent: true}
	p.poll()
	if len(h.appear) != 2 {
		t.Fatalf("a changed look did not push: %d pushes", len(h.appear))
	}
	if h.appear[1].Dark || !h.appear[1].HasAccent {
		t.Errorf("second push = %+v, want the new look", h.appear[1])
	}
	if len(h.appear[1].FontTTF) != 0 {
		t.Error("the font was re-sent on a look change; it is 8 MB and never changes")
	}
}

// A font the platform cannot supply is not fatal: the look still arrives.
func TestAppearancePumpSurvivesAMissingFont(t *testing.T) {
	win := &fakeAppearance{ap: gw.Appearance{Dark: true}, err: errNoFont{}}
	h := &recorder{}
	newAppearancePump(win, h).start()
	if len(h.appear) != 1 || len(h.appear[0].FontTTF) != 0 {
		t.Errorf("pushes = %+v, want one with no font", h.appear)
	}
}

type errNoFont struct{}

func (errNoFont) Error() string { return "no system font" }

// A window that cannot report the look, or a handler that does not want it,
// leaves the pump inert rather than panicking on a nil.
func TestAppearancePumpIsInertWithoutBothEnds(t *testing.T) {
	h := &recorder{}
	p := newAppearancePump(nopBackend{}, h) // window cannot report
	p.start()
	p.poll()

	q := newAppearancePump(&fakeAppearance{}, &plainHandler{}) // handler does not want it
	q.start()
	q.poll()

	if len(h.appear) != 0 {
		t.Errorf("an inert pump pushed %d times", len(h.appear))
	}
}

type nopBackend struct{ gw.Backend }

func assertCalls(t *testing.T, r *recorder, want []string) {
	t.Helper()
	if len(r.calls) != len(want) {
		t.Fatalf("calls = %v, want %v", r.calls, want)
	}
	for i := range want {
		if r.calls[i] != want[i] {
			t.Errorf("call %d = %q, want %q", i, r.calls[i], want[i])
		}
	}
}

// menuRecorder is a recorder that also plays a context-menu host, to drive the
// secondary-click and modal-menu branches of route.
type menuRecorder struct {
	recorder
	menuActive bool
	secondary  [][2]int
	menuEvents []toolkit.EventKind
}

func (m *menuRecorder) SecondaryClick(x, y int) { m.secondary = append(m.secondary, [2]int{x, y}) }
func (m *menuRecorder) ContextMenuActive() bool { return m.menuActive }
func (m *menuRecorder) ContextMenuEvent(ev toolkit.Event) {
	m.menuEvents = append(m.menuEvents, ev.Kind)
}

func TestRouteSecondaryClickAndModalMenu(t *testing.T) {
	m := &menuRecorder{}
	// Closed menu: a secondary press reaches SecondaryClick.
	route(m, toolkit.Event{Kind: toolkit.EventSecondaryClick, X: 12, Y: 34})
	if len(m.secondary) != 1 || m.secondary[0] != [2]int{12, 34} {
		t.Fatalf("secondary click not routed to SecondaryClick: %v", m.secondary)
	}
	// Open menu: every event is forwarded to the menu and nothing reaches the scene.
	m.menuActive = true
	route(m, toolkit.Event{Kind: toolkit.EventClick, X: 1, Y: 2})
	route(m, toolkit.Event{Kind: toolkit.EventKeyDown, Code: "Escape"})
	if len(m.menuEvents) != 2 {
		t.Fatalf("menu received %d events, want 2 (%v)", len(m.menuEvents), m.menuEvents)
	}
	if len(m.calls) != 0 {
		t.Fatalf("events leaked past the modal menu: %v", m.calls)
	}

	// A handler that is not a SecondaryClicker simply ignores the secondary press.
	r := &recorder{}
	route(r, toolkit.Event{Kind: toolkit.EventSecondaryClick, X: 5, Y: 6})
	if len(r.calls) != 0 {
		t.Fatalf("a plain handler should ignore a secondary click: %v", r.calls)
	}
}
