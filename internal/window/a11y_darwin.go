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
//go:build darwin

package window

import (
	objc "github.com/go-macos/objc"
)

var (
	selAccessibilityElementWithRole = objc.RegisterName("accessibilityElementWithRole:frame:label:parent:")
	selSetAccessibilityValue        = objc.RegisterName("setAccessibilityValue:")
	selArrayWithObjects             = objc.RegisterName("arrayWithObjects:count:")
	selConvertRectToView            = objc.RegisterName("convertRect:toView:")
	selConvertRectToScreen          = objc.RegisterName("convertRectToScreen:")
)

// viewIsAccessibilityElement reports NO: the view is a container, not a control.
// Saying YES would make VoiceOver treat the whole window as one element and stop
// descending into the children below.
func viewIsAccessibilityElement(_ objc.ID, _ objc.SEL) bool { return false }

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
func viewAccessibilityChildren(self objc.ID, _ objc.SEL) objc.ID {
	acc, ok := handler.(Accessible)
	if !ok {
		return 0
	}
	mu.Lock()
	scale := curScale
	mu.Unlock()

	elems := acc.A11yElements()
	ids := make([]objc.ID, 0, len(elems))
	for _, e := range elems {
		if axSkip(e) {
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

		el := objc.ID(objc.GetClass("NSAccessibilityElement")).Send(
			selAccessibilityElementWithRole,
			nsString(axRole(e.Role)), onScreen, nsString(e.Name), self)
		if el == 0 {
			continue
		}
		if e.Value != "" {
			el.Send(selSetAccessibilityValue, nsString(e.Value))
		}
		ids = append(ids, el)
	}
	if len(ids) == 0 {
		return 0
	}
	return objc.ID(objc.GetClass("NSArray")).Send(selArrayWithObjects, &ids[0], uint(len(ids)))
}

// a11yMethods are the accessibility overrides added to the view class. Kept
// separate from the presentation methods so registerClasses reads as two
// distinct jobs: showing pixels, and describing them.
func a11yMethods() []objc.MethodDef {
	return []objc.MethodDef{
		{Cmd: objc.RegisterName("isAccessibilityElement"), Fn: viewIsAccessibilityElement},
		{Cmd: objc.RegisterName("accessibilityRole"), Fn: viewAccessibilityRole},
		{Cmd: objc.RegisterName("accessibilityLabel"), Fn: viewAccessibilityLabel},
		{Cmd: objc.RegisterName("accessibilityChildren"), Fn: viewAccessibilityChildren},
	}
}
