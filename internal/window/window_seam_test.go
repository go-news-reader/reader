package window

import "testing"

func TestRGBAToBGRA(t *testing.T) {
	// Two pixels plus three trailing bytes that must be ignored.
	src := []byte{
		0x10, 0x20, 0x30, 0x40,
		0x01, 0x02, 0x03, 0x04,
		0xaa, 0xbb, 0xcc, // partial pixel: ignored
	}
	dst := make([]byte, len(src))
	// pre-fill so we can prove the trailing bytes are left untouched.
	for i := range dst {
		dst[i] = 0xee
	}
	rgbaToBGRA(dst, src)
	want := []byte{
		0x30, 0x20, 0x10, 0x40,
		0x03, 0x02, 0x01, 0x04,
		0xee, 0xee, 0xee, // untouched
	}
	for i := range want {
		if dst[i] != want[i] {
			t.Fatalf("byte %d = %#02x, want %#02x", i, dst[i], want[i])
		}
	}
}

func TestRGBAToBGRAEmpty(t *testing.T) {
	rgbaToBGRA(nil, nil) // must not panic on an empty framebuffer
}

func TestWinMouseCoords(t *testing.T) {
	// x=100, y=200 packed low/high.
	if x, y := winMouseCoords(200<<16 | 100); x != 100 || y != 200 {
		t.Fatalf("winMouseCoords = %d,%d want 100,200", x, y)
	}
	// Negative coordinates (click dragged above/left of the client area).
	nx, ny := int16(-5), int16(-9)
	neg := uint32(uint16(nx)) | uint32(uint16(ny))<<16
	if x, y := winMouseCoords(neg); x != -5 || y != -9 {
		t.Fatalf("winMouseCoords negative = %d,%d want -5,-9", x, y)
	}
}

func TestWinSize(t *testing.T) {
	if w, h := winSize(700<<16 | 1000); w != 1000 || h != 700 {
		t.Fatalf("winSize = %d,%d want 1000,700", w, h)
	}
}

func TestWinWheel(t *testing.T) {
	// One notch up in win32 = +120; must scroll up (negative device pixels).
	if got := winWheelScroll(winWheelDelta(120 << 16)); got != -wheelPixelsPerNotch {
		t.Fatalf("wheel up = %d want %d", got, -wheelPixelsPerNotch)
	}
	// One notch down = -120; must scroll down (positive).
	nd := int16(-120)
	down := uint32(uint16(nd)) << 16
	if got := winWheelScroll(winWheelDelta(down)); got != wheelPixelsPerNotch {
		t.Fatalf("wheel down = %d want %d", got, wheelPixelsPerNotch)
	}
}

func TestWinKeyName(t *testing.T) {
	cases := map[uint32]string{
		vkBack:   "Backspace",
		vkEscape: "Escape",
		vkReturn: "Enter",
		0x41:     "", // 'A' arrives via WM_CHAR instead
	}
	for vk, want := range cases {
		if got := winKeyName(vk); got != want {
			t.Fatalf("winKeyName(%#x) = %q want %q", vk, got, want)
		}
	}
}

func TestWinCharRune(t *testing.T) {
	if r := winCharRune('A'); r != 'A' {
		t.Fatalf("winCharRune('A') = %q want 'A'", r)
	}
	if r := winCharRune(0x08); r != 0 { // backspace control char
		t.Fatalf("winCharRune(0x08) = %q want 0", r)
	}
	if r := winCharRune(0x7f); r != 0 { // DEL
		t.Fatalf("winCharRune(0x7f) = %q want 0", r)
	}
}

func TestX11ButtonScroll(t *testing.T) {
	if dy, ok := x11ButtonScroll(4); !ok || dy != -wheelPixelsPerNotch {
		t.Fatalf("button 4 = %d,%v want %d,true", dy, ok, -wheelPixelsPerNotch)
	}
	if dy, ok := x11ButtonScroll(5); !ok || dy != wheelPixelsPerNotch {
		t.Fatalf("button 5 = %d,%v want %d,true", dy, ok, wheelPixelsPerNotch)
	}
	if _, ok := x11ButtonScroll(1); ok {
		t.Fatalf("button 1 reported as scroll")
	}
}

func TestX11KeyDecode(t *testing.T) {
	type want struct {
		name string
		r    rune
	}
	cases := map[uint32]want{
		ksBackSpace: {"Backspace", 0},
		ksEscape:    {"Escape", 0},
		ksReturn:    {"Enter", 0},
		ksKPEnter:   {"Enter", 0},
		'a':         {"", 'a'},         // Latin-1
		0x010001f4:  {"", rune(0x1f4)}, // Unicode-block keysym
		0xffff:      {"", 0},           // unmapped function key
	}
	for ks, w := range cases {
		name, r := x11KeyDecode(ks)
		if name != w.name || r != w.r {
			t.Fatalf("x11KeyDecode(%#x) = %q,%q want %q,%q", ks, name, r, w.name, w.r)
		}
	}
}

func TestPutImageRows(t *testing.T) {
	// stride 0 is a degenerate guard.
	if got := putImageRows(1000, 0); got != 1 {
		t.Fatalf("putImageRows stride 0 = %d want 1", got)
	}
	// A generous budget yields many rows.
	// budget = 1000*4 - 24 = 3976 bytes; stride 400 -> 9 rows.
	if got := putImageRows(1000, 400); got != (1000*4-putImageHeaderBytes)/400 {
		t.Fatalf("putImageRows = %d", got)
	}
	// A tiny budget still yields at least one row.
	if got := putImageRows(1, 4096); got != 1 {
		t.Fatalf("putImageRows tiny = %d want 1", got)
	}
}
