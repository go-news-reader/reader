// Package window opens a native OS window and blits the aggregator's own RGBA
// framebuffer into it — no WebKit, no wasm, no HTTP. We draw every widget with
// our framework (ui.Scene), so the window is just a bitmap surface plus a native
// event source: macOS Cocoa, Windows win32, and Linux X11 back-ends all present
// the same framebuffer and feed native input into the Scene. Everything is
// CGO_ENABLED=0.
//
// Opening a real OS window and pumping its event loop is inherently a
// launch-verified boundary, so the per-OS Run implementations are excluded from
// the coverage gate (like the wasm glue); this contract file and the adapters
// that drive it are unit-tested.
package window

import (
	"errors"
	"image/color"
)

// ErrUnsupported is returned by [Run] on a platform without a native back-end.
var ErrUnsupported = errors.New("window: no native window back-end on this platform")

// SystemAppearance carries look-and-feel harvested from the host UI so the
// renderer can match the live system look rather than a fixed palette.
type SystemAppearance struct {
	// Dark is the effective dark/light mode (macOS effectiveAppearance).
	Dark bool
	// Accent is the user's accent colour (macOS controlAccentColor); only
	// meaningful when HasAccent is set.
	Accent    color.RGBA
	HasAccent bool
	// FontTTF is the raw system font (e.g. macOS SFNS.ttf). Empty on a poll that
	// only refreshes colours, so the already-installed font is kept.
	FontTTF []byte
}

// AppearanceSink is an optional [Handler] capability. A back-end that can read
// the host appearance (currently the macOS Cocoa back-end) pushes it so the UI
// adopts the native dark/light mode, accent colour, and system font.
type AppearanceSink interface {
	SystemAppearance(SystemAppearance)
}

// Handler is the presenter's data source and input sink. The window calls Frame
// each tick (and after each event) and blits the returned buffer only when it
// reports changed. Input coordinates are device pixels (points × backing scale)
// with a top-left origin, matching the framebuffer.
type Handler interface {
	// Frame returns the current RGBA framebuffer (w*h*4 bytes) and whether it
	// changed since the last call (damage gate).
	Frame() (buf []byte, w, h int, changed bool)
	// Resize maps the new logical size to device pixels; scale is the backing
	// scale factor (device pixels per point).
	Resize(w, h int, scale float64)
	// MouseDown reports a left button press at device-pixel coordinates.
	MouseDown(x, y int)
	// MouseMove reports pointer motion at device-pixel coordinates. It fires
	// continuously during a left-button drag (back-ends need not emit idle
	// hovers) so the handler can drive interactions like a divider resize.
	MouseMove(x, y int)
	// MouseUp reports a left button release at device-pixel coordinates.
	MouseUp(x, y int)
	// Scroll reports a wheel delta in device pixels.
	Scroll(dy int)
	// Key reports a key press: name is a symbolic label for editing keys
	// ("Backspace"/"Escape"/"Enter"), r the rune for a printable character.
	Key(name string, r rune)
}

// Config controls the window.
type Config struct {
	Title         string
	Width, Height float64
}
