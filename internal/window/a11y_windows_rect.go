// Where an element is on screen.
//
// UI Automation asks for this twice, by two different routes, and a client may
// use either: the BoundingRectangle PROPERTY on IRawElementProviderSimple, and
// get_BoundingRectangle on IRawElementProviderFragment. Measured against the
// probe, the fragment method is the one that decides whether a client shows a
// rectangle at all — answering only the property left every element reported as
// "(no rect)". So both are implemented and both are fed from screenRect below.
//
// # get_BoundingRectangle is NOT answered, on purpose
//
// The fragment method declines. That is not an oversight and not laziness: it
// is the best of the states that were actually measured, on a live client
// validated against Notepad first.
//
//   - Declining leaves the root with the correct rectangle, because UI
//     Automation falls back to the HWND host provider for it. Children report
//     no rectangle, which is true and which a client handles.
//   - ANSWERING with the ordinary out-parameter shape publishes zeros: the
//     values written never reach the client, and the zeros then OVERRIDE the
//     host's correct rectangle, so the whole window loses its bounds too. That
//     is strictly worse for a user than declining.
//
// What is established, all by measurement rather than by reading the header:
// the method IS called, once per element (a counter in the callback proved it);
// screenRect below computes the RIGHT values for both the root and the children
// (published through AutomationId and read back through the client:
// hwnd=0x1b0340, rootok=true, rootrect=20,78,780x520, child 28,57,48x48); and
// yet the client reads zeros. So the fault is purely in how the result is
// handed back. A trampoline treating UiaRect as a by-value homogeneous
// floating-point aggregate returned in d0-d3 did not deliver either, and the
// two readings of the signature contradict each other in a way this
// application is too coarse an instrument to settle.
//
// The next step is a MINIMAL standalone UI Automation provider — one window,
// one element, no feed — where a hypothesis costs seconds instead of a
// cross-build, a deploy and two scheduled tasks.
//
// The rectangle the handler publishes is window-relative device pixels; UI
// Automation wants screen pixels. The window's own origin is what converts
// between them, and forgetting it is invisible until a user actually points at
// something and lands somewhere else entirely.
//
//go:build windows

package window

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var procGetWindowRect = user32.NewProc("GetWindowRect")

// boundingRectangleMethod is the vtable entry for
// IRawElementProviderFragment::get_BoundingRectangle. See the file comment for
// why it declines.
func boundingRectangleMethod() uintptr {
	return windows.NewCallback(func(this, pRetVal uintptr) uintptr { return eNotImpl })
}

// screenRect is the element's rectangle in screen pixels.
//
// The fragment root answers with the WINDOW's rectangle rather than a drawn
// element's: it stands for the window itself, and a root that reported a
// zero rect would make a client place the whole application at the origin.
func (p *provider) screenRect() (left, top, width, height float64, ok bool) {
	if p.idx == rootIdx {
		axmu.Lock()
		hwnd := axHWND
		axmu.Unlock()
		if hwnd == 0 {
			return 0, 0, 0, 0, false
		}
		var rc winRect
		if r, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&rc))); r == 0 {
			return 0, 0, 0, 0, false
		}
		return float64(rc.left), float64(rc.top),
			float64(rc.right - rc.left), float64(rc.bottom - rc.top), true
	}
	e, found := p.element()
	if !found {
		return 0, 0, 0, 0, false
	}
	ox, oy := axWindowOrigin()
	return float64(e.X + ox), float64(e.Y + oy), float64(e.W), float64(e.H), true
}

const (
	vtR8    = 5
	vtArray = 0x2000
)

// boundingRectVariant answers the same question through the property route, as
// a four-double SAFEARRAY: left, top, width, height, in that order.
func boundingRectVariant(v *variant, left, top, width, height float64) uintptr {
	sa, _, _ := procSafeArrayCreateVec.Call(vtR8, 0, 4)
	if sa == 0 {
		return eFail
	}
	vals := [4]float64{left, top, width, height}
	for i := int32(0); i < 4; i++ {
		idx := i
		procSafeArrayPutElement.Call(sa,
			uintptr(unsafe.Pointer(&idx)), uintptr(unsafe.Pointer(&vals[i])))
	}
	v.vt = vtR8 | vtArray
	v.val[0] = sa
	return sOK
}
