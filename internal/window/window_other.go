// On non-macOS platforms there is no native NSWindow presenter; Run reports
// that the window is unavailable so the caller can fall back to a browser or a
// static render.
//
//go:build !darwin

package window

import "errors"

// ErrUnsupported is returned by [Run] on platforms without a native window.
var ErrUnsupported = errors.New("window: native window is only available on macOS")

// Handler is the presenter's data source and input sink. Its shape mirrors the
// darwin build so callers compile unchanged across platforms.
type Handler interface {
	Frame() (buf []byte, w, h int, changed bool)
	Resize(w, h int, scale float64)
	MouseDown(x, y int)
	Scroll(dy int)
	Key(name string, r rune)
}

// Config controls the window. Fields mirror the darwin build.
type Config struct {
	Title         string
	Width, Height float64
}

// Run is unavailable off macOS and always returns [ErrUnsupported].
func Run(cfg Config, h Handler) error { return ErrUnsupported }
