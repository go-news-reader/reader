// Fallback for platforms without a native back-end yet. Run reports the window
// is unavailable so the caller falls back to a browser or a static render. As
// win32 and X11 back-ends land, this build constraint gains && !windows / && !linux.
//
//go:build !darwin

package window

// Run is unavailable here and always returns [ErrUnsupported].
func Run(cfg Config, h Handler) error { return ErrUnsupported }
