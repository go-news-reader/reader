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

func TestWinLeftButtonHeld(t *testing.T) {
	if !winLeftButtonHeld(mkLButton) {
		t.Fatal("MK_LBUTTON set should report held")
	}
	if !winLeftButtonHeld(mkLButton | 0x0010) { // left + other bits
		t.Fatal("left held among other modifiers should report held")
	}
	if winLeftButtonHeld(0x0010) { // some other bit, no left button
		t.Fatal("no MK_LBUTTON should report not held")
	}
	if winLeftButtonHeld(0) {
		t.Fatal("zero wparam should report not held")
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
		vkUp:     "Up",
		vkDown:   "Down",
		vkLeft:   "Left",
		vkRight:  "Right",
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
	// D3: a lone surrogate half must not become a rune.
	if r := winCharRune(0xD83D); r != 0 {
		t.Fatalf("winCharRune(high surrogate) = %q want 0", r)
	}
	if r := winCharRune(0xDE00); r != 0 {
		t.Fatalf("winCharRune(low surrogate) = %q want 0", r)
	}
}

// D3: the assembler joins a surrogate pair into one astral rune and passes BMP
// units straight through; unpaired halves and control units yield 0.
func TestUTF16Assembler(t *testing.T) {
	var a utf16Assembler
	if r := a.next('x'); r != 'x' {
		t.Fatalf("BMP unit = %q want 'x'", r)
	}
	// 😀 U+1F600 = high D83D, low DE00.
	if r := a.next(0xD83D); r != 0 {
		t.Fatalf("high surrogate should buffer (0), got %q", r)
	}
	if r := a.next(0xDE00); r != 0x1F600 {
		t.Fatalf("pair = %#x want U+1F600", r)
	}
	// Unpaired low surrogate yields nothing.
	if r := a.next(0xDE00); r != 0 {
		t.Fatalf("unpaired low surrogate = %q want 0", r)
	}
	// A high surrogate followed by a non-surrogate cancels cleanly.
	a.next(0xD83D)
	if r := a.next('y'); r != 'y' {
		t.Fatalf("dangling-high then BMP = %q want 'y'", r)
	}
}

// D2: a printable keysym under a command-style modifier is suppressed (a
// shortcut, not text); Shift alone still types; named keys always resolve.
func TestX11KeyDecodeState(t *testing.T) {
	if _, r := x11KeyDecodeState('v', x11Control); r != 0 {
		t.Fatalf("Ctrl+v leaked rune %q", r)
	}
	if _, r := x11KeyDecodeState('f', x11Mod1); r != 0 {
		t.Fatalf("Alt+f leaked rune %q", r)
	}
	if _, r := x11KeyDecodeState('a', 1<<0 /*Shift*/); r != 'a' {
		t.Fatalf("Shift+a = %q want 'a'", r)
	}
	if _, r := x11KeyDecodeState('a', 0); r != 'a' {
		t.Fatalf("plain a = %q want 'a'", r)
	}
	if name, _ := x11KeyDecodeState(ksReturn, x11Control); name != "Enter" {
		t.Fatalf("Ctrl+Enter name = %q want Enter", name)
	}
}

// D2: the Cocoa decoder suppresses a rune under Command/Control/Option but not
// under Shift or no modifier.
func TestCocoaSuppressesRune(t *testing.T) {
	for _, m := range []uint64{nsCommand, nsControl, nsOption} {
		if !cocoaSuppressesRune(m) {
			t.Fatalf("modifier %#x should suppress", m)
		}
	}
	if cocoaSuppressesRune(0) {
		t.Fatal("no modifier should not suppress")
	}
	if cocoaSuppressesRune(1 << 17 /*shift*/) {
		t.Fatal("Shift should not suppress")
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
		ksUp:        {"Up", 0},
		ksDown:      {"Down", 0},
		ksLeft:      {"Left", 0},
		ksRight:     {"Right", 0},
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

// TestAXRoleMapsTheToolkitVocabulary checks the translation VoiceOver depends on.
// It announces the role, so a button described as static text is silently
// unclickable to someone using it — the mapping is not cosmetic.
func TestAXRoleMapsTheToolkitVocabulary(t *testing.T) {
	cases := map[string]string{
		"button":      "AXButton",
		"text":        "AXStaticText",
		"textbox":     "AXTextField",
		"searchbox":   "AXTextField",
		"checkbox":    "AXCheckBox",
		"radio":       "AXRadioButton",
		"slider":      "AXSlider",
		"img":         "AXImage",
		"list":        "AXList",
		"listbox":     "AXList",
		"grid":        "AXTable",
		"toolbar":     "AXToolbar",
		"menu":        "AXMenu",
		"menubar":     "AXMenuBar",
		"progressbar": "AXProgressIndicator",
		"document":    "AXGroup",
		"alert":       "AXSheet",
		"dialog":      "AXSheet",
		"status":      "AXStaticText",
		"navigation":  "AXGroup",
		"banner":      "AXGroup",
		"group":       "AXGroup",
		"tablist":     "AXGroup",
		"tree":        "AXGroup",
		// Presentational elements are filtered out before they reach here, but a
		// role must never be left without an answer.
		"presentation": "AXGroup",
	}
	for neutral, want := range cases {
		if got := axRole(neutral); got != want {
			t.Errorf("axRole(%q) = %q, want %q", neutral, got, want)
		}
	}
	// An unknown role becomes a group: vague, but true, and never a wrong promise.
	if got := axRole("something-new"); got != "AXGroup" {
		t.Errorf("unknown role = %q, want AXGroup", got)
	}
}

// TestAXSkipDropsWhatCannotBeAnnounced checks the filter: an unnamed or
// zero-area element would land in VoiceOver's rotor as a stop the user has to
// skip past, saying nothing.
func TestAXSkipDropsWhatCannotBeAnnounced(t *testing.T) {
	good := A11yElement{Role: "button", Name: "Settings", W: 40, H: 20}
	if axSkip(good) {
		t.Fatal("a named element with area must be exposed")
	}
	for name, e := range map[string]A11yElement{
		"no name":     {Role: "button", W: 40, H: 20},
		"zero width":  {Role: "button", Name: "x", W: 0, H: 20},
		"zero height": {Role: "button", Name: "x", W: 40, H: 0},
		"negative":    {Role: "button", Name: "x", W: -3, H: 20},
	} {
		if !axSkip(e) {
			t.Errorf("%s should be skipped", name)
		}
	}
}

// TestAXViewRectUndoesTheBackingScale checks the conversion from the buffer's
// device pixels to the view's points — the step that makes the bridge correct on
// a Retina display rather than pointing at coordinates twice too far out.
func TestAXViewRectUndoesTheBackingScale(t *testing.T) {
	e := A11yElement{X: 200, Y: 100, W: 400, H: 60}
	x, y, w, h := axViewRect(e, 2)
	if x != 100 || y != 50 || w != 200 || h != 30 {
		t.Fatalf("at scale 2 got (%v,%v %vx%v), want (100,50 200x30)", x, y, w, h)
	}
	// Scale 1 is the identity.
	if x, y, w, h := axViewRect(e, 1); x != 200 || y != 100 || w != 400 || h != 60 {
		t.Fatalf("at scale 1 got (%v,%v %vx%v), want the input", x, y, w, h)
	}
	// A zero or negative scale would divide the layout into nonsense; treat it
	// as 1 rather than producing infinities.
	if x, _, _, _ := axViewRect(e, 0); x != 200 {
		t.Fatalf("scale 0 got x=%v, want the unscaled 200", x)
	}
	if x, _, _, _ := axViewRect(e, -1); x != 200 {
		t.Fatalf("negative scale got x=%v, want the unscaled 200", x)
	}
	// An element scrolled above the viewport keeps its true negative position:
	// collapsing it to zero would pile every off-screen element on the top edge.
	up := A11yElement{X: 0, Y: -300, W: 100, H: 50}
	if _, y, _, _ := axViewRect(up, 1); y != -300 {
		t.Fatalf("off-screen y = %v, want it preserved", y)
	}
}
