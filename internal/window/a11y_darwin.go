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
	objc "github.com/go-macos/objc"
)

var (
	selSetAccessibilityFrame    = objc.RegisterName("setAccessibilityFrame:")
	selSetAccessibilityParent   = objc.RegisterName("setAccessibilityParent:")
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
		el := objc.ID(objc.GetClass("NSAccessibilityElement")).Send(selAlloc).Send(selInit)
		if el == 0 {
			continue
		}
		el.Send(selSetAccessibilityRole, nsString(axRole(e.Role)))
		el.Send(selSetAccessibilityLabel, nsString(e.Name))
		el.Send(selSetAccessibilityFrame, onScreen)
		el.Send(selSetAccessibilityParent, self)
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
