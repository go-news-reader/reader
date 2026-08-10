// The COM half of the UI Automation bridge: the vtables, the interface methods
// behind them, and the WM_GETOBJECT answer that hands the whole thing to a
// client.
//
// # The one method Go cannot express
//
// IRawElementProviderFragmentRoot::ElementProviderFromPoint takes two DOUBLES.
// windows.NewCallback builds a C-callable function from a Go func, but it only
// carries integer-sized arguments: it never reads the floating-point registers
// the coordinates arrive in. There is no way to see them from Go, so the method
// declines rather than hit-testing against coordinates it cannot trust — a
// wrong answer would point a screen reader at the wrong element and nothing
// would look broken. Clients that hit-test from the published rectangles
// instead are unaffected.
//
// Everything else, including get_BoundingRectangle, uses the ordinary COM
// out-parameter convention and is expressible directly.
//
//go:build windows

package window

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// The interface identities a client asks for by GUID.
var (
	iidIUnknown = windows.GUID{Data1: 0x00000000, Data2: 0x0000, Data3: 0x0000,
		Data4: [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}
	iidSimple = windows.GUID{Data1: 0xD6DD68D1, Data2: 0x86FD, Data3: 0x4332,
		Data4: [8]byte{0x86, 0x66, 0x9A, 0xBE, 0xDE, 0xA2, 0xD2, 0x4C}}
	iidFragment = windows.GUID{Data1: 0xF7063DA8, Data2: 0x8359, Data3: 0x439C,
		Data4: [8]byte{0x92, 0x97, 0xBB, 0xC5, 0x29, 0x9A, 0x7D, 0x87}}
	iidFragmentRoot = windows.GUID{Data1: 0x620CE2A5, Data2: 0xAB8F, Data3: 0x40A9,
		Data4: [8]byte{0x86, 0xCB, 0xDE, 0x3C, 0x75, 0x59, 0x9B, 0x58}}
	iidInvoke = windows.GUID{Data1: 0x54FCB24B, Data2: 0xE18E, Data3: 0x47A2,
		Data4: [8]byte{0xB4, 0xD3, 0xEC, 0xCB, 0xE7, 0x75, 0x99, 0xA2}}
)

func guidEqual(a *windows.GUID, b *windows.GUID) bool {
	return a.Data1 == b.Data1 && a.Data2 == b.Data2 && a.Data3 == b.Data3 && a.Data4 == b.Data4
}

// queryInterface hands back the embedded vtable pointer for the interface the
// client named. Handing back the wrong one is not detectable by the client: it
// would call through a vtable of a different shape and jump into the middle of
// another method, so the offsets here are the whole contract.
func (p *provider) queryInterface(riid, ppv uintptr) uintptr {
	if ppv == 0 {
		return ePointer
	}
	out := (*uintptr)(ptr(ppv))
	id := (*windows.GUID)(ptr(riid))
	base := uintptr(unsafe.Pointer(p))
	switch {
	case guidEqual(id, &iidIUnknown), guidEqual(id, &iidSimple):
		*out = base + offSimple
	case guidEqual(id, &iidFragment):
		*out = base + offFragment
	case guidEqual(id, &iidFragmentRoot):
		// Only the root is a fragment root. A child claiming to be one would
		// make the client treat the whole subtree as a separate window.
		if p.idx != rootIdx {
			*out = 0
			return eNoIface
		}
		*out = base + offRoot
	case guidEqual(id, &iidInvoke):
		if !p.invokable() {
			*out = 0
			return eNoIface
		}
		*out = base + offInvoke
	default:
		*out = 0
		return eNoIface
	}
	p.refs++
	return sOK
}

// invokable reports whether this element can be activated. Only elements the
// handler describes as buttons are, which is also what decides whether
// IInvokeProvider is offered at all.
func (p *provider) invokable() bool {
	e, ok := p.element()
	return ok && e.Role == "button"
}

// unknownMethods builds the three IUnknown entries for one embedded vtable.
// Each interface needs its own set because `this` arrives pointing at that
// interface's slot, and only a matching offset recovers the provider.
func unknownMethods(off uintptr) (qi, addref, release uintptr) {
	qi = windows.NewCallback(func(this, riid, ppv uintptr) uintptr {
		return (*provider)(ptr(this-off)).queryInterface(riid, ppv)
	})
	addref = windows.NewCallback(func(this uintptr) uintptr {
		p := (*provider)(ptr(this - off))
		p.refs++
		return uintptr(p.refs)
	})
	release = windows.NewCallback(func(this uintptr) uintptr {
		// Providers outlive every client reference on purpose; see the file
		// comment in a11y_windows.go.
		p := (*provider)(ptr(this - off))
		if p.refs > 0 {
			p.refs--
		}
		return uintptr(p.refs)
	})
	return
}

// buildVtables creates the four shared vtables. It runs once: NewCallback draws
// from a process-wide table, so one set per element would exhaust it.
func buildVtables() {
	// IRawElementProviderSimple
	simple := new([16]uintptr)
	simple[0], simple[1], simple[2] = unknownMethods(offSimple)
	simple[3] = windows.NewCallback(func(this, pRetVal uintptr) uintptr {
		if pRetVal == 0 {
			return ePointer
		}
		*(*int32)(ptr(pRetVal)) = providerOptionsServerSide
		return sOK
	})
	simple[4] = windows.NewCallback(func(this, patternID, ppRetVal uintptr) uintptr {
		if ppRetVal == 0 {
			return ePointer
		}
		out := (*uintptr)(ptr(ppRetVal))
		*out = 0
		p := fromSimple(this)
		if int32(patternID) == patternInvoke && p.invokable() {
			*out = uintptr(unsafe.Pointer(p)) + offInvoke
			p.refs++
		}
		return sOK
	})
	simple[5] = windows.NewCallback(func(this, propertyID, pRetVal uintptr) uintptr {
		if pRetVal == 0 {
			return ePointer
		}
		return fromSimple(this).propertyValue(int32(propertyID), (*variant)(ptr(pRetVal)))
	})
	simple[6] = windows.NewCallback(func(this, ppRetVal uintptr) uintptr {
		if ppRetVal == 0 {
			return ePointer
		}
		out := (*uintptr)(ptr(ppRetVal))
		*out = 0
		// Only the root has a host: it is the HWND provider that supplies the
		// window-level properties (bounds, focus, the native window handle) that
		// a drawn element has no way to answer.
		if fromSimple(this).idx == rootIdx {
			procUiaHost.Call(axHWND, ppRetVal)
		}
		return sOK
	})

	// IRawElementProviderFragment
	frag := new([16]uintptr)
	frag[0], frag[1], frag[2] = unknownMethods(offFragment)
	frag[3] = windows.NewCallback(func(this, direction, ppRetVal uintptr) uintptr {
		if ppRetVal == 0 {
			return ePointer
		}
		return fromFragment(this).navigate(int32(direction), (*uintptr)(ptr(ppRetVal)))
	})
	frag[4] = windows.NewCallback(func(this, ppRetVal uintptr) uintptr {
		if ppRetVal == 0 {
			return ePointer
		}
		return fromFragment(this).runtimeID((*uintptr)(ptr(ppRetVal)))
	})
	frag[5] = boundingRectangleMethod()
	frag[6] = windows.NewCallback(func(this, ppRetVal uintptr) uintptr {
		if ppRetVal != 0 {
			*(*uintptr)(ptr(ppRetVal)) = 0
		}
		return sOK
	})
	frag[7] = windows.NewCallback(func(this uintptr) uintptr { return sOK })
	frag[8] = windows.NewCallback(func(this, ppRetVal uintptr) uintptr {
		if ppRetVal == 0 {
			return ePointer
		}
		root := axProvider(rootIdx)
		*(*uintptr)(ptr(ppRetVal)) = uintptr(unsafe.Pointer(root)) + offRoot
		root.refs++
		return sOK
	})

	// IRawElementProviderFragmentRoot
	root := new([16]uintptr)
	root[0], root[1], root[2] = unknownMethods(offRoot)
	// ElementProviderFromPoint takes two doubles, which a Go callback cannot
	// receive at all — they arrive in floating-point registers NewCallback never
	// reads. Returning "no element here" is the truthful answer; hit-testing is
	// answered by the tree's rectangles instead, which is how a client finds an
	// element under the pointer when a root declines.
	root[3] = windows.NewCallback(func(this, x, y, ppRetVal uintptr) uintptr {
		if ppRetVal != 0 {
			*(*uintptr)(ptr(ppRetVal)) = 0
		}
		return sOK
	})
	root[4] = windows.NewCallback(func(this, ppRetVal uintptr) uintptr {
		if ppRetVal != 0 {
			*(*uintptr)(ptr(ppRetVal)) = 0
		}
		return sOK
	})

	// IInvokeProvider
	inv := new([16]uintptr)
	inv[0], inv[1], inv[2] = unknownMethods(offInvoke)
	inv[3] = windows.NewCallback(func(this uintptr) uintptr {
		return fromInvoke(this).invoke()
	})

	axVtabls.simple, axVtabls.fragment, axVtabls.root, axVtabls.invoke = simple, frag, root, inv
}

// propertyValue answers the properties a client reads to announce an element.
func (p *provider) propertyValue(id int32, v *variant) uintptr {
	*v = variant{}
	if p.idx == rootIdx {
		switch id {
		// ControlType is deliberately NOT answered: the root stands for the
		// window itself, and leaving it empty lets the HWND host provider
		// supply the real one. Answering it made every client report the
		// application as a Pane.
		case propIsControlElement, propIsContentElement, propIsEnabled:
			v.setBool(true)
		case propIsOffscreen:
			v.setBool(false)
		}
		return sOK
	}
	e, ok := p.element()
	if !ok {
		return sOK
	}
	switch id {
	case propName:
		v.setBSTR(e.Name)
	case propControlType:
		v.setI4(uiaControlType(e.Role))
	case propValueValue:
		if e.Value != "" {
			v.setBSTR(e.Value)
		}
	case propAutomationID:
		v.setBSTR(e.Role)
	case propBoundingRect:
		// The rect the handler publishes is window-relative device pixels; UI
		// Automation wants screen pixels, so the window's own origin is added.
		ox, oy := axWindowOrigin()
		return boundingRectVariant(v,
			float64(e.X+ox), float64(e.Y+oy), float64(e.W), float64(e.H))
	case propIsControlElement, propIsContentElement, propIsEnabled:
		// Both must be true or the element is invisible to the control and
		// content views — which is every view a screen reader actually walks.
		v.setBool(true)
	case propIsKeyboardFocus:
		v.setBool(false)
	case propIsOffscreen:
		v.setBool(false)
	case propNativeWindow:
		// A drawn element has no window of its own. Saying so is not the same
		// as staying silent: a client asks this first, to decide whether it can
		// go to the window for what the provider cannot supply.
		v.setI4(0)
	}
	return sOK
}

// navigate walks the flat tree: the root holds every element as a direct child,
// the same shape the macOS and AT-SPI bridges publish.
func (p *provider) navigate(direction int32, out *uintptr) uintptr {
	*out = 0
	n := int32(axCount())
	set := func(idx int32) {
		q := axProvider(idx)
		*out = uintptr(unsafe.Pointer(q)) + offFragment
		q.refs++
	}
	if p.idx == rootIdx {
		switch direction {
		case navFirstChild:
			if n > 0 {
				set(0)
			}
		case navLastChild:
			if n > 0 {
				set(n - 1)
			}
		}
		return sOK
	}
	switch direction {
	case navParent:
		root := axProvider(rootIdx)
		*out = uintptr(unsafe.Pointer(root)) + offFragment
		root.refs++
	case navNextSibling:
		if p.idx+1 < n {
			set(p.idx + 1)
		}
	case navPrevSibling:
		if p.idx > 0 {
			set(p.idx - 1)
		}
	}
	return sOK
}

// runtimeID gives each element an identity a client can compare across calls.
// The root returns nothing, so UI Automation derives its identity from the
// window handle, which is what makes the window and the fragment root the same
// object to a client.
func (p *provider) runtimeID(out *uintptr) uintptr {
	*out = 0
	if p.idx == rootIdx {
		return sOK
	}
	sa, _, _ := procSafeArrayCreateVec.Call(vtI4, 0, 2)
	if sa == 0 {
		return eFail
	}
	vals := [2]int32{uiaAppendRuntimeID, p.idx}
	for i := int32(0); i < 2; i++ {
		idx := i
		procSafeArrayPutElement.Call(sa,
			uintptr(unsafe.Pointer(&idx)), uintptr(unsafe.Pointer(&vals[i])))
	}
	*out = sa
	return sOK
}

// invoke activates the element, which for this UI means a click at its centre:
// the handler has no notion of "press this element", only of pointer events, so
// the bridge synthesises the gesture the element was drawn to receive.
func (p *provider) invoke() uintptr {
	e, ok := p.element()
	if !ok {
		return eFail
	}
	h, _ := axHandler.(Handler)
	if h == nil {
		return eFail
	}
	x, y := e.X+e.W/2, e.Y+e.H/2
	h.MouseDown(x, y)
	h.MouseUp(x, y)
	if axHWND != 0 {
		procInvalidateRect.Call(axHWND, 0, 0)
	}
	return sOK
}
