// Pure, platform-independent helpers shared by the win32 and X11 back-ends.
//
// These carry the byte-order conversion and event-mapping logic that would
// otherwise be buried inside the launch-verified Run implementations. Keeping
// them here — with no build constraint — means they compile and are unit-tested
// on every OS (including the macOS dev host), so the tricky pixel and input
// arithmetic is provable without a real window. The per-OS Run functions are
// the only untestable boundary; everything they can delegate lives here.

package window

// wheelPixelsPerNotch is how many device pixels one mouse-wheel notch scrolls.
// Win32 reports wheel motion in 120-unit (WHEEL_DELTA) steps; X11 reports one
// button-press per notch. Both funnel through this constant so the two
// back-ends scroll at the same rate.
const wheelPixelsPerNotch = 40

// putImageHeaderBytes is the fixed size, in bytes, of the X11 PutImage core
// request that precedes the pixel data (6 four-byte fields).
const putImageHeaderBytes = 24

// rgbaToBGRA converts straight-alpha, row-major RGBA into the BGRA byte order
// that both a win32 32-bit BI_RGB DIB and a little-endian X11 ZPixmap expect,
// writing into dst (which must be at least as long as src). It swaps the red
// and blue channels and leaves green and alpha in place. Trailing bytes that do
// not form a whole pixel are ignored.
func rgbaToBGRA(dst, src []byte) {
	n := len(src) &^ 3 // whole pixels only
	for i := 0; i < n; i += 4 {
		dst[i+0] = src[i+2] // B <- R-position source's blue
		dst[i+1] = src[i+1] // G
		dst[i+2] = src[i+0] // R
		dst[i+3] = src[i+3] // A
	}
}

// winMouseCoords extracts the signed x/y client coordinates packed into the low
// and high 16 bits of a WM_LBUTTONDOWN lParam (GET_X_LPARAM / GET_Y_LPARAM).
func winMouseCoords(lparam uint32) (x, y int) {
	return int(int16(lparam & 0xffff)), int(int16(lparam >> 16))
}

// winSize extracts the client width and height packed into the low and high
// 16 bits of a WM_SIZE lParam (LOWORD / HIWORD).
func winSize(lparam uint32) (w, h int) {
	return int(lparam & 0xffff), int(lparam >> 16)
}

// mkLButton is the WM_MOUSEMOVE wParam bit set while the left button is held.
const mkLButton = 0x0001

// winLeftButtonHeld reports whether a WM_MOUSEMOVE wParam indicates the left
// button is down, so pointer motion is forwarded as a drag rather than an idle
// hover.
func winLeftButtonHeld(wparam uint32) bool { return wparam&mkLButton != 0 }

// winWheelDelta extracts the signed wheel delta from the high word of a
// WM_MOUSEWHEEL wParam (GET_WHEEL_DELTA_WPARAM).
func winWheelDelta(wparam uint32) int {
	return int(int16(wparam >> 16))
}

// winWheelScroll turns a raw WM_MOUSEWHEEL delta into a device-pixel Scroll
// amount. Win32 reports positive when the wheel rotates away from the user
// (scroll up); we negate so a downward wheel increases ScrollY, matching the
// browser/Scene convention.
func winWheelScroll(delta int) int {
	return -delta * wheelPixelsPerNotch / 120
}

// win32 virtual-key codes for the editing keys we surface by name.
const (
	vkBack   = 0x08
	vkReturn = 0x0D
	vkEscape = 0x1B
)

// winKeyName maps a WM_KEYDOWN virtual-key code to a symbolic editing-key name,
// or "" for keys that should instead arrive as a printable rune via WM_CHAR.
func winKeyName(vk uint32) string {
	switch vk {
	case vkBack:
		return "Backspace"
	case vkEscape:
		return "Escape"
	case vkReturn:
		return "Enter"
	}
	return ""
}

// winCharRune maps a WM_CHAR code unit to a printable rune, or 0 for control
// characters (which are handled as named keys via WM_KEYDOWN instead).
func winCharRune(c uint16) rune {
	if c >= 0x20 && c != 0x7f {
		return rune(c)
	}
	return 0
}

// x11ButtonScroll maps an X11 pointer button to a wheel Scroll delta: button 4
// is a notch up, button 5 a notch down. ok is false for non-wheel buttons.
func x11ButtonScroll(button byte) (dy int, ok bool) {
	switch button {
	case 4:
		return -wheelPixelsPerNotch, true
	case 5:
		return wheelPixelsPerNotch, true
	}
	return 0, false
}

// X11 keysyms for the editing keys and the Unicode-keysym range.
const (
	ksBackSpace   = 0xff08
	ksReturn      = 0xff0d
	ksKPEnter     = 0xff8d
	ksEscape      = 0xff1b
	ksUnicodeBase = 0x01000000
	ksUnicodeMin  = 0x01000020
	ksUnicodeMax  = 0x0110ffff
)

// x11KeyDecode maps an X11 keysym to a symbolic editing-key name or a printable
// rune. Latin-1 keysyms (0x20..0x7e) are their own code points; keysyms in the
// 0x01000000 Unicode block map by subtracting the base.
func x11KeyDecode(ks uint32) (name string, r rune) {
	switch ks {
	case ksBackSpace:
		return "Backspace", 0
	case ksEscape:
		return "Escape", 0
	case ksReturn, ksKPEnter:
		return "Enter", 0
	}
	if ks >= 0x20 && ks <= 0x7e {
		return "", rune(ks)
	}
	if ks >= ksUnicodeMin && ks <= ksUnicodeMax {
		return "", rune(ks - ksUnicodeBase)
	}
	return "", 0
}

// putImageRows returns how many scanline rows of a stride-byte-wide image fit
// in a single X11 PutImage request, given the server's maximum request length
// (in four-byte units) and accounting for the request header. It never returns
// fewer than one row, so a single over-wide scanline is still attempted rather
// than dividing by zero.
func putImageRows(maxReqUnits uint32, stride int) int {
	if stride <= 0 {
		return 1
	}
	budget := int(maxReqUnits)*4 - putImageHeaderBytes
	rows := budget / stride
	if rows < 1 {
		rows = 1
	}
	return rows
}
