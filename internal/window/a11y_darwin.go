// NSAccessibility bridge: it turns the handler's description of what it is
// showing into accessibility elements VoiceOver can read.
//
// The window presents ONE view holding a rasterised UI. To the accessibility
// system that is a single opaque rectangle: there are no child NSViews to
// inspect, so without this the entire application is one unlabelled image and a
// screen-reader user cannot find a single control. Every custom-drawn
// application has to answer this question, and AppKit's answer is
// NSAccessibilityElement — a concrete class for exactly this case, elements that
// exist for the accessibility tree and nowhere else.
//
// The view therefore reports itself as a group whose children are built on
// demand from Handler.A11yElements. On demand matters: the tree is rebuilt each
// time the accessibility client asks, so it always describes the frame on screen
// rather than a snapshot taken when the window opened.
//
// # Verified end to end
//
// Measured with an external accessibility client (AXUIElementCopyAttributeValue,
// the API VoiceOver itself uses), validated against Finder first so the
// instrument was known good. The running app exposes:
//
//	AXWindow "News Reader"
//	  AXGroup "News"
//	    AXButton    "Toggle sidebar"
//	    AXTextField "Search"
//	    AXButton    "Home"        active
//	    AXButton    "HN"          0/25
//	    AXSheet     "reddit needs sign-in"  log into Reddit in your browser…
//
// Two non-obvious things had to be true, both found by measurement and both
// documented at the code below: the view must report itself AS an accessibility
// element, and the children must be PUSHED with the setter rather than returned
// from an -accessibilityChildren override.
//
//go:build darwin

package window

import (
	"sync"

	objc "github.com/go-macos/objc"
)

var (
	selSetAccessibilityFrame    = objc.RegisterName("setAccessibilityFrame:")
	selSetAccessibilityParent   = objc.RegisterName("setAccessibilityParent:")
	selSetAccessibilityIdent    = objc.RegisterName("setAccessibilityIdentifier:")
	selAccessibilityIdent       = objc.RegisterName("accessibilityIdentifier")
	selSetAccessibilityValue    = objc.RegisterName("setAccessibilityValue:")
	selArray                    = objc.RegisterName("array")
	selAddObject                = objc.RegisterName("addObject:")
	selConvertRectToView        = objc.RegisterName("convertRect:toView:")
	selConvertRectToScreen      = objc.RegisterName("convertRectToScreen:")
	selSetAccessibilityChildren = objc.RegisterName("setAccessibilityChildren:")
	selSetAccessibilityRole     = objc.RegisterName("setAccessibilityRole:")
	selSetAccessibilityLabel    = objc.RegisterName("setAccessibilityLabel:")
	selSetAccessibilityElement  = objc.RegisterName("setAccessibilityElement:")
)

// viewIsAccessibilityElement reports YES.
//
// The instinct is NO — the view is a container, not a control — and that is what
// this returned first. Measured result: the view was pruned from the tree
// ENTIRELY, taking its children with it, and the window reported no accessible
// children at all. AppKit does not lift a non-element's synthetic children into
// its parent. Reporting YES with a group role puts the container in the tree and
// the elements beneath it, which is the shape a screen reader wants anyway.
func viewIsAccessibilityElement(_ objc.ID, _ objc.SEL) bool { return true }

// viewAccessibilityRole reports the view as a group, the role AppKit uses for
// something whose meaning is its contents.
func viewAccessibilityRole(_ objc.ID, _ objc.SEL) objc.ID { return nsString("AXGroup") }

// viewAccessibilityLabel names the window's content area.
func viewAccessibilityLabel(_ objc.ID, _ objc.SEL) objc.ID { return nsString("News") }

// viewAccessibilityChildren builds the element tree from the handler.
//
// Frames are converted here rather than by the caller because the conversion
// needs live Cocoa state: the element rects arrive in device pixels with a
// top-left origin (the space the framebuffer and the mouse events use), and
// NSAccessibilityElement wants screen points with a bottom-left origin. The trip
// is pixels -> view points (axViewRect) -> window -> screen, using the view's
// own conversions so a moved, resized or Retina window needs no special case.
func buildA11yChildren(self objc.ID) objc.ID {
	acc, ok := handler.(Accessible)
	if !ok {
		return 0
	}
	mu.Lock()
	scale := curScale
	mu.Unlock()

	// Collect into an NSMutableArray one object at a time. The obvious
	// +arrayWithObjects:count: takes a C array, which means handing Objective-C a
	// pointer into Go memory — a hazard worth removing on its own terms, though
	// measurement showed it was not what is wrong here (see the KNOWN GAP above).
	// Adding them one by one keeps every pointer on the Objective-C side.
	arr := objc.ID(objc.GetClass("NSMutableArray")).Send(selArray)
	if arr == 0 {
		return 0
	}
	count := 0

	elems := acc.A11yElements()
	for _, e := range elems {
		if axSkip(e) {
			continue
		}
		// The scene opens its tree with a document node covering the whole
		// surface. The container view already plays that part here, so emitting it
		// again would give VoiceOver two nested groups with the same name and
		// nothing between them.
		if e.Role == "document" {
			continue
		}
		x, y, w, h := axViewRect(e, scale)
		frame := nsRect{Origin: nsPoint{X: x, Y: y}, Size: nsSize{W: w, H: h}}
		// View -> window -> screen. Passing a nil view to convertRect:toView:
		// means "the window's base coordinate space", which also undoes the
		// flipped view's top-left origin for us.
		inWindow := objc.Send[nsRect](self, selConvertRectToView, frame, objc.ID(0))
		win := self.Send(selWindow)
		if win == 0 {
			continue
		}
		onScreen := objc.Send[nsRect](win, selConvertRectToScreen, inWindow)

		// Build with individual setters rather than
		// +accessibilityElementWithRole:frame:label:parent:. That factory takes an
		// NSRect BETWEEN two object pointers, and passing a by-value struct in the
		// middle of an argument list through purego shifts everything after it:
		// the elements appeared in the tree with no role and no label at all.
		// One-argument setters keep every value in a register of its own.
		el := objc.ID(a11yElementClass).Send(selAlloc).Send(selInit)
		if el == 0 {
			continue
		}
		el.Send(selSetAccessibilityRole, nsString(axRole(e.Role)))
		el.Send(selSetAccessibilityLabel, nsString(e.Name))
		el.Send(selSetAccessibilityFrame, onScreen)
		el.Send(selSetAccessibilityParent, self)
		// Carry the element's own centre, in the device pixels the input path
		// speaks, so a press can be replayed as an ordinary click. Stashing it on
		// the element beats holding a Go-side table keyed by index: the tree is
		// rebuilt on every damaged frame, and an index would go stale the moment
		// the feed scrolled under a VoiceOver user's cursor.
		el.Send(selSetAccessibilityIdent, nsString(pressPoint(e)))
		if e.Value != "" {
			el.Send(selSetAccessibilityValue, nsString(e.Value))
		}
		arr.Send(selAddObject, el)
		count++
	}
	if count == 0 {
		return 0
	}
	return arr
}

// a11yMethods are the accessibility overrides added to the view class. Kept
// separate from the presentation methods so registerClasses reads as two
// distinct jobs: showing pixels, and describing them.
func a11yMethods() []objc.MethodDef {
	return []objc.MethodDef{
		{Cmd: objc.RegisterName("isAccessibilityElement"), Fn: viewIsAccessibilityElement},
		{Cmd: objc.RegisterName("accessibilityRole"), Fn: viewAccessibilityRole},
		{Cmd: objc.RegisterName("accessibilityLabel"), Fn: viewAccessibilityLabel},
	}
}

// refreshA11y republishes the accessibility tree for the current frame.
//
// The children are PUSHED with -setAccessibilityChildren: rather than left for
// AppKit to pull through an -accessibilityChildren override. Overriding the
// getter is the documented shape and it is genuinely called — measurement
// confirmed AppKit invoking it repeatedly — but the array it returned never
// reached the tree: the window showed no children at all. The setter works. That
// difference cost a debugging session and is the reason this is not written the
// obvious way.
func refreshA11y(v objc.ID) {
	if v == 0 {
		return
	}
	if err := registerA11yElementClass(); err != nil {
		return // no pressable element class: describe nothing rather than half of it
	}
	if _, ok := handler.(Accessible); !ok {
		return
	}
	v.Send(selSetAccessibilityElement, true)
	v.Send(selSetAccessibilityRole, nsString("AXGroup"))
	v.Send(selSetAccessibilityLabel, nsString("News"))
	if arr := buildA11yChildren(v); arr != 0 {
		v.Send(selSetAccessibilityChildren, arr)
	}
}

// a11yElementClass is a subclass of NSAccessibilityElement that can be pressed.
// A plain NSAccessibilityElement is readable and inert: VoiceOver announces
// "Settings, button" and then has no way to press it, which is half an interface.
var a11yElementClass objc.Class

// registerA11yElementClass builds that subclass once.
func registerA11yElementClass() error {
	var err error
	a11yElementOnce.Do(func() {
		a11yElementClass, err = objc.RegisterClass(
			"GoNewsReaderA11yElement", objc.GetClass("NSAccessibilityElement"),
			[]objc.MethodDef{
				{Cmd: objc.RegisterName("accessibilityPerformPress"), Fn: elementPerformPress},
				{Cmd: objc.RegisterName("isAccessibilityEnabled"), Fn: elementIsEnabled},
			})
	})
	return err
}

var a11yElementOnce sync.Once

// elementIsEnabled reports the element as enabled; a disabled one is announced
// as unavailable and cannot be pressed.
func elementIsEnabled(_ objc.ID, _ objc.SEL) bool { return true }

// elementPerformPress replays the press as a click at the element's centre,
// through the SAME MouseDown/MouseUp path a real click takes.
//
// Routing it through the input path rather than into some parallel "activate"
// API is what keeps the two in step: every behaviour a click has — opening an
// article, toggling the sidebar, focusing the search field — is had by a press,
// for free and forever, with no second implementation to drift.
//
// It is safe because of an invariant the ui package already tests: every element
// carrying a rect came from a hit-testable region, so its centre resolves to
// something.
func elementPerformPress(self objc.ID, _ objc.SEL) bool {
	return performPressAt(goString(self.Send(selAccessibilityIdent)))
}

// performPressAt is the press itself, split from the Objective-C entry point so
// it can be tested: instantiating an AppKit object off the main thread aborts
// the process (see the note at the top of the test file), so the only way to
// exercise this is to call it with the identifier a real element would carry.
func performPressAt(ident string) bool {
	if handler == nil {
		return false
	}
	x, y, ok := parsePressPoint(ident)
	if !ok {
		return false
	}
	handler.MouseDown(x, y)
	handler.MouseUp(x, y)
	if view != 0 {
		view.Send(selSetNeedsDisplay, true)
	}
	return true
}
