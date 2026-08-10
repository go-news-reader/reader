// UI Automation bridge: it publishes the handler's description of what it is
// showing to Narrator, NVDA and every other Windows assistive technology.
//
// The window presents ONE HWND holding a rasterised UI. To UI Automation that
// is an opaque rectangle with no structure — measured, before this file
// existed, with a probe validated against Notepad first:
//
//	=== top-level window: "News Reader" ===
//	Window "News Reader" (20,80 780x520)          <- and no children at all
//
// The same problem the macOS and AT-SPI bridges solve, in a third shape. macOS
// subclasses a view; Linux exports D-Bus objects; Windows answers WM_GETOBJECT
// with a COM object implementing a small family of UI Automation interfaces.
//
// # COM objects without cgo
//
// A COM object is a pointer to a pointer to an array of function pointers. Go
// can build exactly that: windows.NewCallback turns a Go func into a C-callable
// pointer, and a [4]uintptr header at the front of a struct gives one embedded
// vtable pointer per interface the object exposes. A method then recovers its
// provider by subtracting its own vtable's offset from `this` — which is why
// the offsets below are named constants rather than literals.
//
// The vtables are built ONCE and shared by every provider: NewCallback holds a
// process-wide table with a hard limit, so building them per element would
// exhaust it on a long feed.
//
// # Why providers are never freed
//
// AddRef/Release keep a count that nothing acts on, and providers stay in a Go
// slice for the life of the process. UI Automation may hold a reference across
// frames, and a client that comes back to an element after the tree has changed
// must find a live object rather than freed memory. The pool is bounded by the
// number of elements on screen, so it costs a few kilobytes.
//
//go:build windows

package window

import (
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	uiaCore                 = windows.NewLazySystemDLL("UIAutomationCore.dll")
	oleaut32                = windows.NewLazySystemDLL("oleaut32.dll")
	ole32                   = windows.NewLazySystemDLL("ole32.dll")
	procUiaRet              = uiaCore.NewProc("UiaReturnRawElementProvider")
	procUiaHost             = uiaCore.NewProc("UiaHostProviderFromHwnd")
	procSysAllocString      = oleaut32.NewProc("SysAllocString")
	procSafeArrayCreateVec  = oleaut32.NewProc("SafeArrayCreateVector")
	procSafeArrayPutElement = oleaut32.NewProc("SafeArrayPutElement")
	procCoTaskMemAlloc      = ole32.NewProc("CoTaskMemAlloc")
)

// win32 and UI Automation constants.
const (
	wmGetObject = 0x003D
	// UiaRootObjectId is negative; WM_GETOBJECT delivers it in lParam as an
	// unsigned word, so the comparison below sign-extends before testing.
	uiaRootObjectID = -25

	sOK       = 0
	eNoIface  = 0x80004002 // E_NOINTERFACE
	ePointer  = 0x80004003 // E_POINTER
	eNotImpl  = 0x80004001 // E_NOTIMPL
	eFail     = 0x80004005 // E_FAIL
	vtEmpty   = 0
	vtI4      = 3
	vtBSTR    = 8
	vtBool    = 11
	variantTr = 0xFFFF // VARIANT_TRUE is -1 as an int16

	providerOptionsServerSide = 1

	// Property ids a client asks for by number. These are the DOCUMENTED values,
	// checked against what a live client actually requests; an earlier set had
	// IsControlElement at 30010 — which is IsEnabled — and IsEnabled at 30019,
	// which is IsPassword. A provider answering those is not merely unhelpful,
	// it tells the client something else entirely.
	propRuntimeID        = 30000
	propBoundingRect     = 30001
	propControlType      = 30003
	propName             = 30005
	propIsKeyboardFocus  = 30009
	propIsEnabled        = 30010
	propAutomationID     = 30011
	propIsControlElement = 30016
	propIsContentElement = 30017
	propNativeWindow     = 30020
	propIsOffscreen      = 30022
	propValueValue       = 30045

	patternInvoke = 10000
	patternValue  = 10002

	navParent      = 0
	navNextSibling = 1
	navPrevSibling = 2
	navFirstChild  = 3
	navLastChild   = 4

	uiaAppendRuntimeID = 3
)

// UI Automation control types. A wrong number renames every element for the
// user, so these are the documented values rather than a guess.
const (
	ctButton   = 50000
	ctEdit     = 50004
	ctImage    = 50006
	ctListItem = 50007
	ctList     = 50008
	ctToolBar  = 50021
	ctText     = 50020
	ctCustom   = 50025
	ctGroup    = 50026
	ctDocument = 50030
	ctPane     = 50033
)

// uiaControlType maps a neutral role name (see [A11yElement]) to its UI
// Automation control type. Anything unrecognised becomes a group — UIA's
// "a thing containing things", the honest answer for a role not in this table.
func uiaControlType(neutral string) int32 {
	switch neutral {
	case "button":
		return ctButton
	case "text", "status":
		return ctText
	case "textbox", "searchbox":
		return ctEdit
	case "img":
		return ctImage
	case "listitem":
		return ctListItem
	case "list", "listbox":
		return ctList
	case "toolbar":
		return ctToolBar
	case "document":
		return ctDocument
	case "group", "navigation", "banner", "presentation":
		return ctGroup
	default:
		return ctGroup
	}
}

// uiaRect is UI Automation's rectangle: left, top, WIDTH, HEIGHT in screen
// pixels — not right/bottom, which is the easy mistake to make from the name.
type uiaRect struct {
	left, top, width, height float64
}

// variant is the 24-byte VARIANT of the 64-bit ABI: the tag, three reserved
// words, and a 16-byte union.
type variant struct {
	vt         uint16
	r1, r2, r3 uint16
	val        [2]uintptr
}

func (v *variant) setBSTR(s string) {
	p, err := windows.UTF16PtrFromString(s)
	if err != nil {
		v.vt = vtEmpty
		return
	}
	b, _, _ := procSysAllocString.Call(uintptr(unsafe.Pointer(p)))
	v.vt = vtBSTR
	v.val[0] = b
}

func (v *variant) setI4(n int32) {
	v.vt = vtI4
	v.val[0] = uintptr(uint32(n))
}

func (v *variant) setBool(b bool) {
	v.vt = vtBool
	if b {
		v.val[0] = variantTr
	} else {
		v.val[0] = 0
	}
}

// Offsets of the embedded vtable pointers inside a provider. A method entered
// through one interface subtracts its own offset from `this` to get back to the
// provider — the standard multi-interface COM layout.
const (
	offSimple   = 0
	offFragment = 8
	offRoot     = 16
	offInvoke   = 24
)

// provider is one accessible element: the fragment root when idx is rootIdx,
// otherwise the element at that index of the published snapshot.
//
// The four vtable pointers MUST stay first and in this order: their offsets are
// the constants above, and COM identifies an interface purely by which of them
// the client was handed.
type provider struct {
	vtblSimple   uintptr
	vtblFragment uintptr
	vtblRoot     uintptr
	vtblInvoke   uintptr

	refs int32
	idx  int32
}

const rootIdx = -1

var (
	axmu     sync.Mutex
	axElems  []A11yElement // the published snapshot
	axHWND   uintptr
	axProvs  = map[int32]*provider{} // idx -> provider, kept alive for the process
	axKeep   []*provider             // ditto, so nothing is ever collected
	axVtabls struct {
		simple, fragment, root, invoke *[16]uintptr
	}
	axVtblOnce sync.Once
)

// axProvider returns the (single, stable) provider for an element index.
func axProvider(idx int32) *provider {
	axmu.Lock()
	defer axmu.Unlock()
	return axProviderLocked(idx)
}

func axProviderLocked(idx int32) *provider {
	if p, ok := axProvs[idx]; ok {
		return p
	}
	p := &provider{
		vtblSimple:   uintptr(unsafe.Pointer(axVtabls.simple)),
		vtblFragment: uintptr(unsafe.Pointer(axVtabls.fragment)),
		vtblRoot:     uintptr(unsafe.Pointer(axVtabls.root)),
		vtblInvoke:   uintptr(unsafe.Pointer(axVtabls.invoke)),
		idx:          idx,
	}
	axProvs[idx] = p
	axKeep = append(axKeep, p)
	return p
}

// element returns the snapshot entry this provider describes.
func (p *provider) element() (A11yElement, bool) {
	axmu.Lock()
	defer axmu.Unlock()
	if p.idx < 0 || int(p.idx) >= len(axElems) {
		return A11yElement{}, false
	}
	return axElems[p.idx], true
}

func axCount() int {
	axmu.Lock()
	defer axmu.Unlock()
	return len(axElems)
}

// ptr converts a pointer that arrived from OUTSIDE Go — a COM `this`, or an
// out-parameter the caller allocated — back into one Go can dereference.
//
// go vet flags every uintptr-to-unsafe.Pointer conversion, and it is right to:
// if the integer named a Go heap object, the collector could move or free it
// while only an integer referred to it. Neither hazard applies on this path.
// Out-parameters point at memory the CALLER owns, outside the Go heap entirely.
// And the only Go objects whose addresses cross into COM are the providers,
// which are rooted in axKeep for the life of the process precisely so their
// addresses stay valid for as long as a client holds them.
func ptr(u uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&u))
}

// fromSimple and friends recover the provider from an interface pointer.
func fromSimple(this uintptr) *provider   { return (*provider)(ptr(this - offSimple)) }
func fromFragment(this uintptr) *provider { return (*provider)(ptr(this - offFragment)) }
func fromRoot(this uintptr) *provider     { return (*provider)(ptr(this - offRoot)) }
func fromInvoke(this uintptr) *provider   { return (*provider)(ptr(this - offInvoke)) }
