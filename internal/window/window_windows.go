// Native Windows (win32) back-end: it opens a top-level window and blits the
// caller's RGBA framebuffer into it via GDI, feeding native mouse/wheel/key
// input back to the [Handler]. Like the macOS back-end it is a pure presenter
// that never imports the widget layer.
//
// Everything goes through user32.dll / gdi32.dll resolved lazily with
// golang.org/x/sys/windows (NewLazySystemDLL + NewProc + Call) — no cgo — so
// the app builds and links with CGO_ENABLED=0. A ~16 ms WM_TIMER damage-gates
// repaints (InvalidateRect only when the frame changed); WM_PAINT converts the
// current buffer RGBA->BGRA and StretchDIBits-es it, scaled to fill the client
// area, from a top-down BI_RGB DIB.
//
//go:build windows

package window

import (
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32 = windows.NewLazySystemDLL("user32.dll")
	gdi32  = windows.NewLazySystemDLL("gdi32.dll")

	procRegisterClassExW   = user32.NewProc("RegisterClassExW")
	procCreateWindowExW    = user32.NewProc("CreateWindowExW")
	procDefWindowProcW     = user32.NewProc("DefWindowProcW")
	procShowWindow         = user32.NewProc("ShowWindow")
	procUpdateWindow       = user32.NewProc("UpdateWindow")
	procGetMessageW        = user32.NewProc("GetMessageW")
	procTranslateMessage   = user32.NewProc("TranslateMessage")
	procDispatchMessageW   = user32.NewProc("DispatchMessageW")
	procPostQuitMessage    = user32.NewProc("PostQuitMessage")
	procBeginPaint         = user32.NewProc("BeginPaint")
	procEndPaint           = user32.NewProc("EndPaint")
	procGetClientRect      = user32.NewProc("GetClientRect")
	procInvalidateRect     = user32.NewProc("InvalidateRect")
	procSetTimer           = user32.NewProc("SetTimer")
	procLoadCursorW        = user32.NewProc("LoadCursorW")
	procGetDpiForWindow    = user32.NewProc("GetDpiForWindow")
	procSetProcessDpiAware = user32.NewProc("SetProcessDPIAware")

	procStretchDIBits = gdi32.NewProc("StretchDIBits")

	kernel32             = windows.NewLazySystemDLL("kernel32.dll")
	procGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
)

// win32 message and style constants.
const (
	wsOverlappedWindow = 0x00CF0000
	cwUseDefault       = 0x80000000 // as an int32 this is CW_USEDEFAULT
	swShow             = 5

	wmDestroy     = 0x0002
	wmSize        = 0x0005
	wmPaint       = 0x000F
	wmKeyDown     = 0x0100
	wmChar        = 0x0102
	wmLButtonDown = 0x0201
	wmMouseWheel  = 0x020A
	wmTimer       = 0x0113

	idcArrow    = 32512
	csHRedraw   = 0x0002
	csVRedraw   = 0x0001
	biRGB       = 0
	diBRGBColor = 0
	srcCopy     = 0x00CC0020
	timerID     = 1
	timerMillis = 16
)

type wndClassExW struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       uintptr
}

type winMsg struct {
	hwnd     uintptr
	message  uint32
	wParam   uintptr
	lParam   uintptr
	time     uint32
	pt       winPoint
	lPrivate uint32
}

type winPoint struct{ x, y int32 }

type winRect struct{ left, top, right, bottom int32 }

type paintStruct struct {
	hdc         uintptr
	fErase      int32
	rcPaint     winRect
	fRestore    int32
	fIncUpdate  int32
	rgbReserved [32]byte
}

type bitmapInfoHeader struct {
	biSize          uint32
	biWidth         int32
	biHeight        int32
	biPlanes        uint16
	biBitCount      uint16
	biCompression   uint32
	biSizeImage     uint32
	biXPelsPerMeter int32
	biYPelsPerMeter int32
	biClrUsed       uint32
	biClrImportant  uint32
}

// Single-window process state, shared between the timer tick and WM_PAINT.
var (
	wmu      sync.Mutex
	wHandler Handler
	wBuf     []byte // latest framebuffer (RGBA)
	wScratch []byte // RGBA->BGRA conversion target, reused across paints
	wW, wH   int
	wScale   = 1.0
	wHwnd    uintptr
)

// present pulls the latest frame; it stores and reports true only when changed.
func present() bool {
	if wHandler == nil {
		return false
	}
	buf, w, h, changed := wHandler.Frame()
	if !changed {
		return false
	}
	wmu.Lock()
	wBuf, wW, wH = buf, w, h
	wmu.Unlock()
	return true
}

// paint blits the current buffer into hdc, scaled to fill the client rect.
func paint(hdc uintptr) {
	wmu.Lock()
	buf, w, h := wBuf, wW, wH
	if len(buf) == 0 || w == 0 || h == 0 {
		wmu.Unlock()
		return
	}
	if cap(wScratch) < len(buf) {
		wScratch = make([]byte, len(buf))
	}
	scratch := wScratch[:len(buf)]
	rgbaToBGRA(scratch, buf)
	wmu.Unlock()

	var rc winRect
	procGetClientRect.Call(wHwnd, uintptr(unsafe.Pointer(&rc)))
	dstW := int(rc.right - rc.left)
	dstH := int(rc.bottom - rc.top)
	if dstW <= 0 || dstH <= 0 {
		dstW, dstH = w, h
	}

	bi := bitmapInfoHeader{
		biSize:        uint32(unsafe.Sizeof(bitmapInfoHeader{})),
		biWidth:       int32(w),
		biHeight:      -int32(h), // negative => top-down rows
		biPlanes:      1,
		biBitCount:    32,
		biCompression: biRGB,
	}
	procStretchDIBits.Call(
		hdc,
		0, 0, uintptr(dstW), uintptr(dstH), // dest x,y,w,h
		0, 0, uintptr(w), uintptr(h), // src x,y,w,h
		uintptr(unsafe.Pointer(&scratch[0])),
		uintptr(unsafe.Pointer(&bi)),
		diBRGBColor, srcCopy,
	)
}

// wndProc is the window procedure; it maps native messages to Handler calls.
func wndProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	switch uint32(msg) {
	case wmPaint:
		var ps paintStruct
		hdc, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		paint(hdc)
		procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		return 0
	case wmTimer:
		if present() {
			procInvalidateRect.Call(hwnd, 0, 0)
		}
		return 0
	case wmSize:
		w, h := winSize(uint32(lparam))
		scale := dpiScale(hwnd)
		wmu.Lock()
		wScale = scale
		wmu.Unlock()
		if wHandler != nil {
			wHandler.Resize(int(float64(w)*scale), int(float64(h)*scale), scale)
			if present() {
				procInvalidateRect.Call(hwnd, 0, 0)
			}
		}
		return 0
	case wmLButtonDown:
		if wHandler != nil {
			x, y := winMouseCoords(uint32(lparam))
			wmu.Lock()
			scale := wScale
			wmu.Unlock()
			wHandler.MouseDown(int(float64(x)*scale), int(float64(y)*scale))
			if present() {
				procInvalidateRect.Call(hwnd, 0, 0)
			}
		}
		return 0
	case wmMouseWheel:
		if wHandler != nil {
			wmu.Lock()
			scale := wScale
			wmu.Unlock()
			wHandler.Scroll(int(float64(winWheelScroll(winWheelDelta(uint32(wparam)))) * scale))
			if present() {
				procInvalidateRect.Call(hwnd, 0, 0)
			}
		}
		return 0
	case wmKeyDown:
		if wHandler != nil {
			if name := winKeyName(uint32(wparam)); name != "" {
				wHandler.Key(name, 0)
				if present() {
					procInvalidateRect.Call(hwnd, 0, 0)
				}
			}
		}
		return 0
	case wmChar:
		if wHandler != nil {
			if r := winCharRune(uint16(wparam)); r != 0 {
				wHandler.Key("", r)
				if present() {
					procInvalidateRect.Call(hwnd, 0, 0)
				}
			}
		}
		return 0
	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	}
	ret, _, _ := procDefWindowProcW.Call(hwnd, msg, wparam, lparam)
	return ret
}

// dpiScale returns the window's DPI scale factor (device pixels per point),
// falling back to 1.0 when GetDpiForWindow is unavailable or reports zero.
func dpiScale(hwnd uintptr) float64 {
	dpi, _, _ := procGetDpiForWindow.Call(hwnd)
	if dpi == 0 {
		return 1.0
	}
	return float64(dpi) / 96.0
}

// Run opens the window and pumps the win32 message loop until WM_QUIT. It must
// run on the main OS thread (the caller does runtime.LockOSThread).
func Run(cfg Config, h Handler) error {
	if cfg.Width == 0 {
		cfg.Width = 1000
	}
	if cfg.Height == 0 {
		cfg.Height = 700
	}
	wHandler = h
	procSetProcessDpiAware.Call() // best-effort: crisp pixels on hi-DPI

	hInstance, _, _ := procGetModuleHandleW.Call(0)
	className, err := windows.UTF16PtrFromString("GoNewsReaderWindow")
	if err != nil {
		return err
	}
	title, err := windows.UTF16PtrFromString(cfg.Title)
	if err != nil {
		return err
	}
	cursor, _, _ := procLoadCursorW.Call(0, idcArrow)

	wc := wndClassExW{
		cbSize:        uint32(unsafe.Sizeof(wndClassExW{})),
		style:         csHRedraw | csVRedraw,
		lpfnWndProc:   windows.NewCallback(wndProc),
		hInstance:     hInstance,
		hCursor:       cursor,
		lpszClassName: className,
	}
	if ret, _, callErr := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); ret == 0 {
		return callErr
	}

	hwnd, _, callErr := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(title)),
		wsOverlappedWindow,
		cwUseDefault, cwUseDefault,
		uintptr(int32(cfg.Width)), uintptr(int32(cfg.Height)),
		0, 0, hInstance, 0,
	)
	if hwnd == 0 {
		return callErr
	}
	wHwnd = hwnd

	// Seed the scene from the actual client size and backing scale.
	scale := dpiScale(hwnd)
	var rc winRect
	procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
	cw, ch := int(rc.right-rc.left), int(rc.bottom-rc.top)
	if cw == 0 || ch == 0 {
		cw, ch = int(cfg.Width), int(cfg.Height)
	}
	wmu.Lock()
	wScale = scale
	wmu.Unlock()
	h.Resize(int(float64(cw)*scale), int(float64(ch)*scale), scale)
	present()

	procShowWindow.Call(hwnd, swShow)
	procUpdateWindow.Call(hwnd)
	procSetTimer.Call(hwnd, timerID, timerMillis, 0)

	var msg winMsg
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(ret) <= 0 { // 0 => WM_QUIT, -1 => error
			return nil
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
}
