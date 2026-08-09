//go:build darwin

package window

import (
	"testing"

	objc "github.com/go-macos/objc"
)

// The tests below never instantiate an NSView. AppKit is main-thread-only and a
// Go test goroutine is not the main thread, so -initWithFrame: forwards into
// nothing and aborts the process — which is exactly what the first draft of this
// file did. Everything here is therefore either a class-level runtime query or a
// direct call into the Go implementations, both of which are safe off the main
// thread and test the same two things: that the methods are installed, and that
// they answer correctly.

// TestAccessibilityMethodsAreRegistered checks the bridge is actually installed.
// Registering an Objective-C method fails SILENTLY in every way that matters: a
// signature the runtime cannot encode, or an IMP it cannot build, leaves a
// program that builds, runs and draws perfectly while being invisible to
// VoiceOver. The only symptom is a screen reader saying nothing.
//
// objc.RegisterClass returns an error when ANY method in the list fails to
// install — those two failure modes exactly — so a successful registration is
// the proof that every method reached the class. This pins the other half: that
// the accessibility methods are in the list being registered, with the selectors
// AppKit actually looks for.
//
// It deliberately does not message the class or an instance. AppKit is
// main-thread-only and a Go test goroutine is not the main thread, so touching
// an NSView here aborts the process — which is what the first draft of this file
// did.
func TestAccessibilityMethodsAreRegistered(t *testing.T) {
	if _, _, err := registerClasses(); err != nil {
		t.Fatalf("registerClasses: %v — a method failed to install", err)
	}

	want := map[string]bool{
		"isAccessibilityElement": false,
		"accessibilityRole":      false,
		"accessibilityLabel":     false,
	}
	defs := a11yMethods()
	if len(defs) != len(want) {
		t.Fatalf("a11yMethods returned %d methods, want %d", len(defs), len(want))
	}
	for name := range want {
		for _, d := range defs {
			if d.Cmd == objc.RegisterName(name) {
				want[name] = true
			}
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("-%s is not registered; VoiceOver would not see it", name)
		}
	}
	for _, d := range defs {
		if d.Fn == nil {
			t.Error("a registered accessibility method has no implementation")
		}
	}
}

// TestViewDescribesItselfAsAContainer checks the answers that decide whether the
// view appears in the tree at all. It must report itself AS an element: measured
// against the running app, a view answering NO was pruned ENTIRELY, taking its
// children with it, and the window exposed nothing.
func TestViewDescribesItselfAsAContainer(t *testing.T) {
	if !viewIsAccessibilityElement(0, 0) {
		t.Error("the view must report itself as an accessibility element or AppKit prunes it with its children")
	}
	if role := goString(viewAccessibilityRole(0, 0)); role != "AXGroup" {
		t.Errorf("role = %q, want AXGroup", role)
	}
	if label := goString(viewAccessibilityLabel(0, 0)); label == "" {
		t.Error("the content area has no accessible name")
	}
}

// pixelsOnlyHandler presents a framebuffer and nothing else — the shape a
// back-end must keep supporting.
type pixelsOnlyHandler struct{}

func (pixelsOnlyHandler) Frame() ([]byte, int, int, bool) { return nil, 0, 0, false }
func (pixelsOnlyHandler) Resize(int, int, float64)        {}
func (pixelsOnlyHandler) MouseDown(int, int)              {}
func (pixelsOnlyHandler) MouseMove(int, int)              {}
func (pixelsOnlyHandler) MouseUp(int, int)                {}
func (pixelsOnlyHandler) Scroll(int)                      {}
func (pixelsOnlyHandler) Key(string, rune)                {}

// describingHandler also describes what it is showing.
type describingHandler struct {
	pixelsOnlyHandler
	elems []A11yElement
}

func (d *describingHandler) A11yElements() []A11yElement { return d.elems }

// TestAccessibilityChildrenNeedsAWindow pins the branch reachable without one:
// an element cannot be placed on screen until the view is IN a window, so the
// bridge yields nothing rather than reporting elements at meaningless
// coordinates.
func TestAccessibilityChildrenNeedsAWindow(t *testing.T) {
	old := handler
	defer func() { handler = old }()
	handler = &describingHandler{elems: []A11yElement{
		{Role: "button", Name: "Settings", W: 40, H: 20},
	}}
	if got := buildA11yChildren(0); got != 0 {
		t.Fatalf("children = %v, want none for a view with no window", got)
	}
}

// TestAccessibilityChildrenWithoutADescription checks the optional interface
// stays optional: a handler that only presents pixels is not forced to describe
// them, and must not crash the accessibility client either.
func TestAccessibilityChildrenWithoutADescription(t *testing.T) {
	old := handler
	defer func() { handler = old }()
	handler = pixelsOnlyHandler{}
	if got := buildA11yChildren(0); got != 0 {
		t.Fatalf("children = %v, want none from a handler that cannot describe itself", got)
	}
}

// TestAccessibilityChildrenSkipsUnannounceableElements checks the filter runs
// before any Cocoa work: an unnamed or zero-area element is dropped rather than
// becoming a silent stop in VoiceOver's rotor.
func TestAccessibilityChildrenSkipsUnannounceableElements(t *testing.T) {
	old := handler
	defer func() { handler = old }()
	handler = &describingHandler{elems: []A11yElement{
		{Role: "button", Name: "", W: 40, H: 20},   // unnamed
		{Role: "button", Name: "Zero", W: 0, H: 0}, // no area
		{Role: "list", Name: "Feed", W: 0, H: 0},   // a container carries no rect
	}}
	if got := buildA11yChildren(0); got != 0 {
		t.Fatalf("children = %v, want none once every element is filtered", got)
	}
}

// recordingHandler captures the clicks the accessibility layer replays.
type recordingHandler struct {
	pixelsOnlyHandler
	downs [][2]int
	ups   [][2]int
}

func (r *recordingHandler) MouseDown(x, y int)          { r.downs = append(r.downs, [2]int{x, y}) }
func (r *recordingHandler) MouseUp(x, y int)            { r.ups = append(r.ups, [2]int{x, y}) }
func (r *recordingHandler) A11yElements() []A11yElement { return nil }

// TestPressReplaysARealClick is the point of making elements pressable: a
// VoiceOver press must go through the SAME input path a mouse click takes, so
// every behaviour a click has is had by a press with no second implementation to
// drift out of step.
func TestPressReplaysARealClick(t *testing.T) {
	if err := registerA11yElementClass(); err != nil {
		t.Fatalf("registerA11yElementClass: %v", err)
	}
	rec := &recordingHandler{}
	old := handler
	defer func() { handler = old }()
	handler = rec

	if !performPressAt("120,210") {
		t.Fatal("press reported failure")
	}
	if len(rec.downs) != 1 || rec.downs[0] != [2]int{120, 210} {
		t.Fatalf("MouseDown = %v, want one click at the element's centre", rec.downs)
	}
	if len(rec.ups) != 1 || rec.ups[0] != [2]int{120, 210} {
		t.Fatalf("MouseUp = %v, want the matching release", rec.ups)
	}
}

// TestPressWithoutACoordinateDoesNothing checks the refusal path: an element
// carrying no usable point must not fall back to (0,0), which is a real control.
func TestPressWithoutACoordinateDoesNothing(t *testing.T) {
	if err := registerA11yElementClass(); err != nil {
		t.Fatalf("registerA11yElementClass: %v", err)
	}
	rec := &recordingHandler{}
	old := handler
	defer func() { handler = old }()
	handler = rec

	if performPressAt("not-a-point") {
		t.Error("a malformed point reported a successful press")
	}
	if len(rec.downs) != 0 {
		t.Fatalf("it clicked anyway: %v", rec.downs)
	}
}

// TestPressWithoutAHandlerIsSafe checks the accessibility client cannot crash a
// window that has no handler yet.
func TestPressWithoutAHandlerIsSafe(t *testing.T) {
	old := handler
	defer func() { handler = old }()
	handler = nil
	if performPressAt("10,20") {
		t.Error("press succeeded with no handler")
	}
}

// TestElementsAreEnabled checks elements are announced as available; a disabled
// element is read as unavailable and cannot be pressed at all.
func TestElementsAreEnabled(t *testing.T) {
	if !elementIsEnabled(0, 0) {
		t.Error("elements must report themselves enabled")
	}
}
