// Fallback for platforms without a native back-end. Run reports the window is
// unavailable so the caller falls back to a browser or a static render. The
// three desktop OSes each have a real back-end (window_darwin.go /
// window_windows.go / window_linux.go); this is only the true fallback (e.g.
// GOOS=js, *bsd).
//
//go:build !darwin && !windows && !linux

package window

// Run is unavailable here and always returns [ErrUnsupported].
func Run(cfg Config, h Handler) error { return ErrUnsupported }
