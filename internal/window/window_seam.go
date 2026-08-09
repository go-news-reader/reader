// Pure, platform-independent helpers shared by the win32 and X11 back-ends.
//
// These carry the byte-order conversion and event-mapping logic that would
// otherwise be buried inside the launch-verified Run implementations. Keeping
// them here — with no build constraint — means they compile and are unit-tested
// on every OS (including the macOS dev host), so the tricky pixel and input
// arithmetic is provable without a real window. The per-OS Run functions are
// the only untestable boundary; everything they can delegate lives here.

package window

import (
	"strconv"
	"strings"
	"unicode/utf16"
)

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
	vkLeft   = 0x25
	vkUp     = 0x26
	vkRight  = 0x27
	vkDown   = 0x28
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
	case vkUp:
		return "Up"
	case vkDown:
		return "Down"
	case vkLeft:
		return "Left"
	case vkRight:
		return "Right"
	}
	return ""
}

// win32 OEM virtual-key codes for the punctuation keys usable as a shortcut base
// (the US-layout '=' and '-' keys). Digits and letters use their ASCII VK codes.
const (
	vkOemPlus  = 0xBB // '=' / '+'
	vkOemMinus = 0xBD // '-' / '_'
)

// winShortcutRune maps a WM_KEYDOWN virtual-key code to the base (unmodified,
// US-layout) printable rune used to match a configurable shortcut: the OEM
// plus/minus keys and the ASCII digit/letter VKs (which equal their rune, with
// letters normalised to lower case so they match a typed character). It returns
// 0 for keys that are not shortcut bases (so the caller ignores the chord).
func winShortcutRune(vk uint32) rune {
	switch vk {
	case vkOemPlus:
		return '='
	case vkOemMinus:
		return '-'
	}
	if vk >= '0' && vk <= '9' {
		return rune(vk)
	}
	if vk >= 'A' && vk <= 'Z' {
		return rune(vk - 'A' + 'a')
	}
	return 0
}

// winKeyDown reports whether a GetKeyState return value marks the key as
// currently held (its high-order bit set).
func winKeyDown(state uint16) bool { return state&0x8000 != 0 }

// winCharRune maps a WM_CHAR code unit to a printable rune, or 0 for control
// characters (handled as named keys via WM_KEYDOWN) and for surrogate halves
// (0xD800..0xDFFF), which are UTF-16 fragments that only form a rune in pairs —
// see [utf16Assembler]. Casting a lone surrogate to a rune would insert an
// invalid code point.
func winCharRune(c uint16) rune {
	if c >= 0xD800 && c <= 0xDFFF {
		return 0
	}
	if c >= 0x20 && c != 0x7f {
		return rune(c)
	}
	return 0
}

// utf16Assembler combines the UTF-16 code units WM_CHAR delivers into whole
// runes. A BMP unit yields its rune at once; a high surrogate is held until the
// following low surrogate arrives, when the pair is decoded into one astral
// rune (e.g. an emoji). Control units and unpaired surrogates yield 0. The
// win32 Run loop keeps one of these across WM_CHAR messages.
type utf16Assembler struct{ hi uint16 }

func (a *utf16Assembler) next(c uint16) rune {
	switch {
	case c >= 0xD800 && c <= 0xDBFF: // high surrogate: hold for its low half
		a.hi = c
		return 0
	case c >= 0xDC00 && c <= 0xDFFF: // low surrogate: complete the pair
		hi := a.hi
		a.hi = 0
		if hi == 0 {
			return 0 // unpaired low surrogate
		}
		return utf16.DecodeRune(rune(hi), rune(c))
	}
	a.hi = 0 // a non-surrogate cancels any dangling high surrogate
	return winCharRune(c)
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
	ksLeft        = 0xff51
	ksUp          = 0xff52
	ksRight       = 0xff53
	ksDown        = 0xff54
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
	case ksUp:
		return "Up", 0
	case ksDown:
		return "Down", 0
	case ksLeft:
		return "Left", 0
	case ksRight:
		return "Right", 0
	}
	if ks >= 0x20 && ks <= 0x7e {
		return "", rune(ks)
	}
	if ks >= ksUnicodeMin && ks <= ksUnicodeMax {
		return "", rune(ks - ksUnicodeBase)
	}
	return "", 0
}

// X11 modifier-state mask bits (the state field of an XKeyEvent).
const (
	x11Control = 1 << 2 // Control
	x11Mod1    = 1 << 3 // Alt / Meta
	x11Mod4    = 1 << 6 // Super
)

// x11KeyDecodeState is [x11KeyDecode] with modifier awareness: when a
// command-style modifier (Control, Alt or Super) is held, a printable keysym is
// NOT surfaced as a typed rune — it is a shortcut (Ctrl+V, Alt+F …), not text —
// which is what win32's WM_CHAR collapses for free. Named editing keys still
// resolve. Shift alone is not command-style and is left to produce its rune.
func x11KeyDecodeState(ks, state uint32) (name string, r rune) {
	name, r = x11KeyDecode(ks)
	if r != 0 && state&(x11Control|x11Mod1|x11Mod4) != 0 {
		return "", 0
	}
	return name, r
}

// x11Shortcut decodes a command-style shortcut from an X11 keysym + modifier
// state: r is the base printable rune, ctrl reports Control, meta reports Super
// (Mod4) — the Linux/X11 command-style modifiers. ok is true only when a base
// rune resolves (not a named editing key) AND at least one command modifier is
// held, so an ordinary keystroke stays on the [x11KeyDecodeState] text path. Alt
// (Mod1) is not a shortcut modifier here; it keeps its suppressed-rune behaviour.
func x11Shortcut(ks, state uint32) (r rune, ctrl, meta, ok bool) {
	name, base := x11KeyDecode(ks)
	if name != "" || base == 0 {
		return 0, false, false, false
	}
	ctrl = state&x11Control != 0
	meta = state&x11Mod4 != 0
	return base, ctrl, meta, ctrl || meta
}

// macOS NSEvent modifier-flag bits (the relevant device-independent flags).
const (
	nsControl = 1 << 18
	nsOption  = 1 << 19
	nsCommand = 1 << 20
)

// cocoaSuppressesRune reports whether a printable character produced by
// -charactersIgnoringModifiers should be dropped rather than typed, because a
// command-style modifier (Command, Control or Option) is held. Command/Control
// are always shortcuts; Option composes accented input on some layouts, but with
// no input-method plumbing here it only yields shortcut-style symbols, so it is
// suppressed too. Shift is not command-style and does not suppress.
func cocoaSuppressesRune(modFlags uint64) bool {
	return modFlags&(nsCommand|nsControl|nsOption) != 0
}

// cocoaShortcut decodes the command-style modifiers of an NSEvent flag mask for
// the shortcut path: ctrl for Control, meta for Command. ok is true when either
// is held, so a printable key becomes a shortcut rather than text. Option is not
// treated as a shortcut modifier here (it composes symbols), matching how
// cocoaSuppressesRune keeps it on the suppressed-text path.
func cocoaShortcut(mod uint64) (ctrl, meta, ok bool) {
	ctrl = mod&nsControl != 0
	meta = mod&nsCommand != 0
	return ctrl, meta, ctrl || meta
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

// axRole maps a neutral role name (see A11yElement) to the macOS
// NSAccessibility role constant that matches it.
//
// The names on the left are the toolkit's ARIA-flavoured vocabulary; the ones on
// the right are AppKit's. Nothing maps automatically between them, and guessing
// wrong is worse than being vague: VoiceOver announces the role, so a button
// described as static text is silently unclickable to someone using it.
// Anything unrecognised becomes a group, which is AppKit's own "a thing that
// contains things" and the honest answer for a role this table does not know.
func axRole(neutral string) string {
	switch neutral {
	case "button":
		return "AXButton"
	case "text":
		return "AXStaticText"
	case "textbox", "searchbox":
		return "AXTextField"
	case "checkbox":
		return "AXCheckBox"
	case "radio":
		return "AXRadioButton"
	case "slider":
		return "AXSlider"
	case "img":
		return "AXImage"
	case "list", "listbox":
		return "AXList"
	case "grid":
		return "AXTable"
	case "toolbar":
		return "AXToolbar"
	case "menu":
		return "AXMenu"
	case "menubar":
		return "AXMenuBar"
	case "progressbar":
		return "AXProgressIndicator"
	case "document":
		return "AXGroup"
	case "alert", "dialog":
		return "AXSheet"
	case "status":
		return "AXStaticText"
	case "navigation", "banner", "group", "tablist", "tree", "presentation":
		return "AXGroup"
	default:
		return "AXGroup"
	}
}

// axSkip reports whether an element should be left out of the accessibility
// tree entirely. An element with no name says nothing a reader could announce,
// and a zero-area one cannot be pointed at; either would land in VoiceOver's
// rotor as an unlabelled stop the user has to skip past.
func axSkip(e A11yElement) bool {
	return e.Name == "" || e.W <= 0 || e.H <= 0
}

// axViewRect converts an element's device-pixel rect (top-left origin) into the
// view's own point coordinates, still top-left because the view is flipped.
//
// It clamps nothing: an element scrolled above or below the viewport keeps its
// true off-screen position, so the accessibility client can tell that it exists
// and where it would be, rather than seeing a pile of elements collapsed onto
// the top edge.
func axViewRect(e A11yElement, scale float64) (x, y, w, h float64) {
	if scale <= 0 {
		scale = 1
	}
	return float64(e.X) / scale, float64(e.Y) / scale,
		float64(e.W) / scale, float64(e.H) / scale
}

// pressPoint encodes an element's centre, in device pixels, for the platform to
// carry back when the element is activated.
func pressPoint(e A11yElement) string {
	return strconv.Itoa(e.X+e.W/2) + "," + strconv.Itoa(e.Y+e.H/2)
}

// parsePressPoint reads back what pressPoint wrote. Anything malformed reports
// not-ok rather than clicking at (0,0), which is a real control.
func parsePressPoint(s string) (x, y int, ok bool) {
	comma := strings.IndexByte(s, ',')
	if comma <= 0 || comma == len(s)-1 {
		return 0, 0, false
	}
	x, err := strconv.Atoi(s[:comma])
	if err != nil {
		return 0, 0, false
	}
	y, err = strconv.Atoi(s[comma+1:])
	if err != nil {
		return 0, 0, false
	}
	return x, y, true
}
