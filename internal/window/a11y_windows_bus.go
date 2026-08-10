// Bringing the UI Automation bridge up and keeping the published tree current.
//
//go:build windows

package window

import "unsafe"

var procClientToScreen = user32.NewProc("ClientToScreen")

// The client area's screen position, recorded by the UI thread.
var axOriginX, axOriginY int

// axHandler is the Handler the win32 back-end is driving. UI Automation calls
// arrive on its own RPC threads, which have no path back to the back-end's
// local state, so Run publishes it here once.
var axHandler Handler

// setA11yHandlerWindows is called by the back-end as it starts.
func setA11yHandlerWindows(h Handler, hwnd uintptr) {
	axHandler = h
	axmu.Lock()
	axHWND = hwnd
	axmu.Unlock()
	noteWindowOrigin(hwnd)
}

// axWindowOrigin is the client area's top-left corner in screen pixels. UI
// Automation reports every rectangle in screen coordinates while the handler
// describes its elements relative to the window, so nothing lines up without
// this — and the error is invisible until a user actually points at something.
//
// It reads a cached value rather than asking the window. UI Automation calls
// arrive on its own threads, and querying the window from one of them made
// every child element's rectangle unreadable to a client while the root's — the
// one path that did not do it — came back correctly. The UI thread records the
// origin whenever the window moves or resizes, which is the only time it can
// change.
func axWindowOrigin() (int, int) {
	axmu.Lock()
	defer axmu.Unlock()
	return axOriginX, axOriginY
}

// noteWindowOrigin is called from the UI thread when the window is created,
// moved or resized.
func noteWindowOrigin(hwnd uintptr) {
	var pt winPoint
	if r, _, _ := procClientToScreen.Call(hwnd, uintptr(unsafe.Pointer(&pt))); r == 0 {
		return
	}
	axmu.Lock()
	axOriginX, axOriginY = int(pt.x), int(pt.y)
	axmu.Unlock()
}

// refreshA11yWindows republishes the tree for the frame just drawn. It is
// called from the same place that decides a repaint is needed, so the
// description never lags the pixels.
func refreshA11yWindows() {
	acc, ok := axHandler.(Accessible)
	if !ok {
		return
	}
	// Drop what cannot be announced before publishing: an unnamed or zero-area
	// element is a stop a screen-reader user has to skip past for nothing. The
	// document element goes too — the fragment root already represents the
	// window, and publishing both makes the window appear inside itself.
	var keep []A11yElement
	for _, e := range acc.A11yElements() {
		if axSkip(e) || e.Role == "document" {
			continue
		}
		keep = append(keep, e)
	}
	axmu.Lock()
	axElems = keep
	axmu.Unlock()
}

// a11yGetObject answers WM_GETOBJECT.
//
// A client asks for several object ids on the same message; only
// UiaRootObjectId means "give me your UI Automation tree". Answering any other
// id with this provider would hand a UI Automation object to a caller expecting
// an MSAA one.
//
// The returned value is not an HRESULT and not the provider: it is the LRESULT
// that UiaReturnRawElementProvider marshals, and returning anything else makes
// the window silently unreadable.
func a11yGetObject(hwnd, wparam, lparam uintptr) (uintptr, bool) {
	if int32(lparam) != uiaRootObjectID {
		return 0, false
	}
	if _, ok := axHandler.(Accessible); !ok {
		return 0, false
	}
	axVtblOnce.Do(buildVtables)
	axmu.Lock()
	if axHWND == 0 {
		axHWND = hwnd
	}
	axmu.Unlock()

	root := axProvider(rootIdx)
	ret, _, _ := procUiaRet.Call(hwnd, wparam, lparam,
		uintptr(unsafe.Pointer(root))+offSimple)
	return ret, true
}

// axDisconnect lets a client know the tree is gone when the window closes.
// Without it UI Automation can keep calling into providers after the window has
// been destroyed.
func axDisconnect() {
	if proc := uiaCore.NewProc("UiaDisconnectAllProviders"); proc.Find() == nil {
		proc.Call()
	}
}
