// Package windowgw presents the reader through the shared, org-owned
// github.com/go-widgets/window backend instead of the reader's own per-OS
// windowing code in internal/window.
//
// It is the "phase 1 / control" side of de-duplicating the reader's ~4.6k lines
// of native windowing (Cocoa, win32+UIA, AT-SPI) against go-widgets/window,
// which already provides all of it. Rather than migrate every backend and delete
// the copy in one step, this package adopts go-widgets/window for a SINGLE path
// (the macOS Cocoa backend, testable on the developer's own Mac) and proves it
// renders the reader's scene identically and keeps accessibility working, while
// the existing internal/window code stays in place and remains the default.
//
// # The contract gap it bridges
//
// The two windowing seams disagree on what a window drives:
//
//   - internal/window drives a FRAMEBUFFER: window.Run(cfg, Handler) where the
//     Handler exposes Frame() ([]byte,w,h,changed) and receives native input.
//   - go-widgets/window drives a WIDGET TREE: Backend.Run(root toolkit.Widget),
//     painting root.Draw and dispatching toolkit.Events to root.OnEvent.
//
// [surface] is the adapter: a toolkit.Widget that renders the reader's own
// framebuffer (through the painter's image primitive), routes toolkit.Events
// back into the reader's existing windowapp.Handler, and republishes the
// reader's accessibility tree as accessibility-walk children so screen readers
// still find a real tree behind the single rectangle of application pixels. It
// is the same idea as the upstream toolkit.Surface widget (added to the toolkit
// after the reader's pinned v0.146.0), reimplemented locally here so this
// control needs no toolkit bump — it depends only on API present at v0.146.0.
package windowgw

import (
	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
	gwwindow "github.com/go-widgets/window"

	rwindow "github.com/go-news-reader/reader/internal/window"
	"github.com/go-news-reader/reader/internal/windowapp"
)

// scrollPixelsPerRow converts a go-widgets EventScroll's row delta (±1 per
// notch) into the device-pixel scroll amount the reader's scene expects (its own
// Cocoa backend feeds scene.Scroll a pixel delta). The exact value only sets the
// wheel speed; it is not part of the render/accessibility equivalence this
// control proves.
const scrollPixelsPerRow = 48

// Reader is the slice of the reader's windowapp.Handler this adapter drives:
// the framebuffer/input contract plus the two optional capabilities it forwards.
// windowapp.Handler satisfies it structurally, and depending on the interface
// (not the concrete type) lets the adapter be proven with a recording fake — no
// app, no scene render, no window.
type Reader interface {
	Frame() (buf []byte, w, h int, changed bool)
	Resize(w, h int, scale float64)
	MouseDown(x, y int)
	MouseMove(x, y int)
	MouseUp(x, y int)
	Scroll(dy int)
	Key(name string, r rune)
	Shortcut(r rune, ctrl, meta bool)
	A11yElements() []rwindow.A11yElement
	SystemAppearance(rwindow.SystemAppearance)
	SetSystemClipboard(rwindow.SystemClipboard)
}

// surface is a toolkit widget that shows the reader's self-rendered framebuffer.
//
// It fills the window at the origin, so buffer coordinates and window
// coordinates coincide and no offset arithmetic is needed; the offset is still
// subtracted in OnEvent and added in Children so the adapter stays correct if it
// is ever placed somewhere other than (0,0).
type surface struct {
	toolkit.Base

	h       Reader
	backing float64 // framebuffer device pixels per logical point (Retina = 2)

	// curW/curH track the last size the reader's scene was told, so a per-frame
	// SetBounds only re-scales + resizes the scene when the size actually
	// changed (SetBounds is called before every paint).
	curW, curH int
}

// SetBounds records the new bounds and, when the size changed, tells the
// reader's scene its new device-pixel size and backing scale — the same call
// internal/window's Cocoa backend makes from -windowDidResize:.
func (s *surface) SetBounds(r toolkit.Rect) {
	s.Base.SetBounds(r)
	if r.W == s.curW && r.H == s.curH {
		return
	}
	s.curW, s.curH = r.W, r.H
	s.h.Resize(r.W, r.H, s.backing)
}

// Draw blits the reader's current framebuffer 1:1 at the widget's bounds through
// the painter's image primitive. A backend whose painter cannot draw an image
// (no such backend is used here) simply shows nothing, rather than reaching past
// the painter for the raw buffer.
func (s *surface) Draw(p painter.Painter, _ *toolkit.Theme) {
	ip, ok := p.(painter.ImagePainter)
	if !ok {
		return
	}
	buf, w, h, _ := s.h.Frame()
	if w <= 0 || h <= 0 || len(buf) < w*h*4 {
		return
	}
	r := s.Bounds()
	ip.DrawImage(painter.Rect{X: r.X, Y: r.Y, W: w, H: h}, buf, w, h)
}

// OnEvent translates a go-widgets toolkit.Event into the reader's native-input
// vocabulary and feeds it to the existing windowapp.Handler, moving coordinates
// into the framebuffer's space first.
func (s *surface) OnEvent(ev toolkit.Event) {
	r := s.Bounds()
	x, y := ev.X-r.X, ev.Y-r.Y
	switch ev.Kind {
	case toolkit.EventClick:
		s.h.MouseDown(x, y)
	case toolkit.EventMouseUp:
		s.h.MouseUp(x, y)
	case toolkit.EventMouseMove, toolkit.EventMouseDrag:
		s.h.MouseMove(x, y)
	case toolkit.EventScroll:
		// Route the wheel to the panel under the pointer first (the reader's own
		// backend does the same), then scroll by a device-pixel delta.
		s.h.MouseMove(x, y)
		s.h.Scroll(ev.Delta * scrollPixelsPerRow)
	case toolkit.EventKeyDown:
		if name, ok := namedKey(ev.Code); ok {
			s.h.Key(name, 0)
		}
	case toolkit.EventChar:
		// A committed rune. A command-style chord (⌘/Ctrl) is a shortcut, not
		// text — route it exactly where the reader's own backend routes it, and
		// do NOT type the bare rune into the focused field.
		r := firstRune(ev.Code)
		if r == 0 {
			return
		}
		if ev.Ctrl || ev.Meta {
			s.h.Shortcut(r, ev.Ctrl, ev.Meta)
			return
		}
		s.h.Key("", r)
	}
}

// A11y reports the surface itself as presentation: it is a container for what
// the reader describes, not a control in its own right, so the accessibility
// walk looks through it to the children.
func (s *surface) A11y() toolkit.A11yInfo { return toolkit.A11yInfo{Role: toolkit.RolePresentation} }

// Children turns the reader's accessibility tree into one proxy widget per
// element, so toolkit.WalkA11y — and thus every go-widgets/window platform
// bridge — reads a real tree where there is only a rectangle of pixels. The
// proxies are rebuilt on every call because what the reader shows changes as it
// renders; the walk is not a per-frame path, so a handful of small structs costs
// nothing worth caching a stale answer for.
func (s *surface) Children() []toolkit.Widget {
	els := s.h.A11yElements()
	if len(els) == 0 {
		return nil
	}
	r := s.Bounds()
	out := make([]toolkit.Widget, 0, len(els))
	for _, e := range els {
		p := &a11yProxy{info: toolkit.A11yInfo{Role: toolkit.Role(e.Role), Name: e.Name, Value: e.Value}}
		p.SetBounds(toolkit.Rect{X: r.X + e.X, Y: r.Y + e.Y, W: e.W, H: e.H})
		out = append(out, p)
	}
	return out
}

// a11yProxy stands for one thing the reader drew so the accessibility walk finds
// a described element with a rectangle where the framebuffer shows only pixels.
// It deliberately does not override Draw: Base's no-op is exactly right — the
// reader painted this element long before anyone asked what it was.
type a11yProxy struct {
	toolkit.Base
	info toolkit.A11yInfo
}

func (p *a11yProxy) A11y() toolkit.A11yInfo { return p.info }

// The reader's windowapp.Handler is the production Reader. Asserting it here
// keeps the interface in step with the concrete type it abstracts.
var _ Reader = (*windowapp.Handler)(nil)

// Surface builds the toolkit widget that presents the reader r's framebuffer,
// routes input back into it, and republishes its accessibility tree. backing is
// the framebuffer's device-pixels-per-point (see [BackingScale]). It is what a
// caller hands to gwwindow.Backend.Run.
func Surface(r Reader, backing float64) toolkit.Widget {
	return &surface{h: r, backing: backing}
}

// namedKey maps a go-widgets DOM-style key name to the reader's editing-key
// vocabulary (its Handler.Key switch). ok is false for names the reader's Key
// switch ignores, so an unmapped named key is dropped rather than passed as a
// no-op — matching the reader's own behaviour.
func namedKey(code string) (string, bool) {
	switch code {
	case "Backspace", "Escape", "Enter":
		return code, true
	case "ArrowUp":
		return "Up", true
	case "ArrowDown":
		return "Down", true
	case "ArrowLeft":
		return "Left", true
	case "ArrowRight":
		return "Right", true
	}
	return "", false
}

// firstRune returns the first rune of a one-character key Code, or 0 when the
// Code is empty (a defensive guard; go-widgets emits a single committed rune per
// EventChar).
func firstRune(code string) rune {
	for _, r := range code {
		return r
	}
	return 0
}

// BackingScale derives the framebuffer's device-pixels-per-point from the
// backend's device size and the logical width the caller asked for. It falls
// back to 1 when either is unavailable, so a non-Retina or headless backend maps
// one framebuffer pixel to one point.
//
// It is the piece a caller needs between gwwindow.Open and [Surface]: the reader
// asks for a NativeScale framebuffer (crisp on Retina), so the surface must tell
// the scene that scale for its own hit-testing and font metrics to line up.
func BackingScale(be gwwindow.Backend, logicalW int) float64 {
	dw, _ := be.Size()
	if logicalW > 0 && dw > 0 {
		return float64(dw) / float64(logicalW)
	}
	return 1
}

// WireCapabilities installs the optional host capabilities the reader wants and
// the backend offers: the live system look (dark/light, accent, system font),
// harvested once before the first frame the way internal/window does, and the OS
// text clipboard, so copy/paste crosses application boundaries. A backend
// lacking either capability simply leaves the reader on its defaults.
func WireCapabilities(be gwwindow.Backend, h Reader) {
	if ar, ok := be.(gwwindow.AppearanceReader); ok {
		ap := ar.Appearance()
		sa := rwindow.SystemAppearance{Dark: ap.Dark, Accent: ap.Accent, HasAccent: ap.HasAccent}
		if ttf, err := ar.SystemFontTTF(); err == nil {
			sa.FontTTF = ttf
		}
		h.SystemAppearance(sa)
	}
	if c, ok := be.(gwwindow.Clipboard); ok {
		h.SetSystemClipboard(c)
	}
}
