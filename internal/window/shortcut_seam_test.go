package window

import "testing"

// TestCocoaShortcut checks the Cocoa modifier decode: Control and Command each
// mark a chord (ok), Option and Shift alone do not.
func TestCocoaShortcut(t *testing.T) {
	if ctrl, meta, ok := cocoaShortcut(nsControl); !ok || !ctrl || meta {
		t.Fatalf("Control: ctrl=%v meta=%v ok=%v", ctrl, meta, ok)
	}
	if ctrl, meta, ok := cocoaShortcut(nsCommand); !ok || ctrl || !meta {
		t.Fatalf("Command: ctrl=%v meta=%v ok=%v", ctrl, meta, ok)
	}
	if ctrl, meta, ok := cocoaShortcut(nsControl | nsCommand); !ok || !ctrl || !meta {
		t.Fatalf("Control+Command: ctrl=%v meta=%v ok=%v", ctrl, meta, ok)
	}
	if _, _, ok := cocoaShortcut(nsOption); ok {
		t.Fatal("Option alone should not be a command-style shortcut")
	}
	if _, _, ok := cocoaShortcut(0); ok {
		t.Fatal("no modifier should not be a shortcut")
	}
}

// TestX11Shortcut checks a printable keysym with Control/Super is a shortcut,
// while a named key, a bare key, or an Alt-only chord is not.
func TestX11Shortcut(t *testing.T) {
	if r, ctrl, meta, ok := x11Shortcut('=', x11Control); !ok || r != '=' || !ctrl || meta {
		t.Fatalf("Ctrl+=: r=%q ctrl=%v meta=%v ok=%v", r, ctrl, meta, ok)
	}
	if r, ctrl, meta, ok := x11Shortcut('-', x11Mod4); !ok || r != '-' || ctrl || !meta {
		t.Fatalf("Super+-: r=%q ctrl=%v meta=%v ok=%v", r, ctrl, meta, ok)
	}
	// A named editing key is never a shortcut base here.
	if _, _, _, ok := x11Shortcut(ksReturn, x11Control); ok {
		t.Fatal("Ctrl+Return should not be a shortcut base")
	}
	// A bare printable key (no command modifier) is not a shortcut.
	if _, _, _, ok := x11Shortcut('a', 0); ok {
		t.Fatal("bare 'a' should not be a shortcut")
	}
	// Alt (Mod1) alone is not a command-style shortcut modifier.
	if _, _, _, ok := x11Shortcut('a', x11Mod1); ok {
		t.Fatal("Alt+a should not be a command-style shortcut")
	}
}

// TestWinShortcutRune maps the OEM plus/minus keys and ASCII digit/letter VKs,
// and returns 0 for non-base keys.
func TestWinShortcutRune(t *testing.T) {
	cases := []struct {
		vk   uint32
		want rune
	}{
		{vkOemPlus, '='},
		{vkOemMinus, '-'},
		{'0', '0'},
		{'9', '9'},
		{'A', 'a'}, // letters normalise to lower case
		{'Z', 'z'},
		{vkReturn, 0}, // not a shortcut base
	}
	for _, c := range cases {
		if got := winShortcutRune(c.vk); got != c.want {
			t.Fatalf("winShortcutRune(%#x) = %q, want %q", c.vk, got, c.want)
		}
	}
}

// TestWinKeyDown checks the GetKeyState high-bit test.
func TestWinKeyDown(t *testing.T) {
	if !winKeyDown(0x8000) || !winKeyDown(0x8001) {
		t.Fatal("high bit set should report down")
	}
	if winKeyDown(0) || winKeyDown(0x0001) {
		t.Fatal("high bit clear should report up")
	}
}
