// Native Linux (X11) back-end: it opens a window over the X protocol and
// presents the caller's RGBA framebuffer with PutImage, feeding native
// mouse/wheel/key input back to the [Handler]. Like the macOS and win32
// back-ends it is a pure presenter and never imports the widget layer.
//
// The X connection is spoken with github.com/jezek/xgb (pure Go, no cgo), so
// the app builds and links with CGO_ENABLED=0. A ~16 ms ticker damage-gates
// repaints; each repaint converts the buffer RGBA->BGRA (little-endian ZPixmap
// order) and PutImages it, chunked by scanline rows so no single request
// exceeds the server's maximum request length. All X requests are serialised
// through one mutex because a *xgb.Conn is written from both the ticker
// goroutine and the event loop.
//
//go:build linux

package window

import (
	"sync"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

// x11 holds the live connection state for the single process window.
type x11 struct {
	conn    *xgb.Conn
	win     xproto.Window
	gc      xproto.Gcontext
	depth   byte
	maxReq  uint32 // MaximumRequestLength, in 4-byte units
	handler Handler

	mu      sync.Mutex // serialises X requests + guards the buffers below
	buf     []byte     // latest framebuffer (RGBA)
	scratch []byte     // RGBA->BGRA conversion target
	w, h    int
	keysyms *keymap
}

// present pulls the latest frame and repaints when it changed.
func (x *x11) present() {
	if x.handler == nil {
		return
	}
	buf, w, h, changed := x.handler.Frame()
	if !changed {
		return
	}
	x.mu.Lock()
	x.buf, x.w, x.h = buf, w, h
	x.repaintLocked()
	x.mu.Unlock()
}

// repaintLocked converts the current buffer to BGRA and PutImages it in
// row-chunks. The caller holds x.mu.
func (x *x11) repaintLocked() {
	buf, w, h := x.buf, x.w, x.h
	if len(buf) == 0 || w == 0 || h == 0 {
		return
	}
	if cap(x.scratch) < len(buf) {
		x.scratch = make([]byte, len(buf))
	}
	scratch := x.scratch[:len(buf)]
	rgbaToBGRA(scratch, buf)

	stride := w * 4
	rows := putImageRows(x.maxReq, stride)
	for y0 := 0; y0 < h; y0 += rows {
		n := rows
		if y0+n > h {
			n = h - y0
		}
		data := scratch[y0*stride : (y0+n)*stride]
		xproto.PutImage(x.conn, xproto.ImageFormatZPixmap, xproto.Drawable(x.win),
			x.gc, uint16(w), uint16(n), 0, int16(y0), 0, x.depth, data)
	}
}

// Run opens the X11 window and pumps its event loop until the connection ends.
func Run(cfg Config, h Handler) error {
	if cfg.Width == 0 {
		cfg.Width = 1000
	}
	if cfg.Height == 0 {
		cfg.Height = 700
	}
	conn, err := xgb.NewConn()
	if err != nil {
		return err
	}
	defer conn.Close()

	setup := xproto.Setup(conn)
	screen := setup.DefaultScreen(conn)

	wid, err := xproto.NewWindowId(conn)
	if err != nil {
		return err
	}
	x := &x11{
		conn:    conn,
		win:     wid,
		depth:   screen.RootDepth,
		maxReq:  uint32(setup.MaximumRequestLength),
		handler: h,
	}

	const eventMask = xproto.EventMaskExposure |
		xproto.EventMaskButtonPress |
		xproto.EventMaskKeyPress |
		xproto.EventMaskStructureNotify
	xproto.CreateWindow(conn, screen.RootDepth, wid, screen.Root,
		0, 0, uint16(cfg.Width), uint16(cfg.Height), 0,
		xproto.WindowClassInputOutput, screen.RootVisual,
		xproto.CwBackPixel|xproto.CwEventMask,
		[]uint32{screen.BlackPixel, eventMask})

	// Window title (WM_NAME).
	xproto.ChangeProperty(conn, xproto.PropModeReplace, wid,
		xproto.AtomWmName, xproto.AtomString, 8,
		uint32(len(cfg.Title)), []byte(cfg.Title))

	gid, err := xproto.NewGcontextId(conn)
	if err != nil {
		return err
	}
	xproto.CreateGC(conn, gid, xproto.Drawable(wid),
		xproto.GcForeground, []uint32{screen.WhitePixel})
	x.gc = gid

	x.keysyms = loadKeymap(conn, setup)

	xproto.MapWindow(conn, wid)

	// Seed the scene at the requested size (scale 1.0; X has no per-window
	// backing scale without reading Xft.dpi, and 1.0 is a correct default).
	h.Resize(int(cfg.Width), int(cfg.Height), 1.0)
	x.present()

	// ~60 Hz damage-gated repaint on its own goroutine.
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		t := time.NewTicker(16 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				x.present()
			}
		}
	}()

	for {
		ev, err := conn.WaitForEvent()
		if ev == nil && err == nil {
			return nil // connection closed
		}
		if err != nil {
			continue
		}
		x.handleEvent(ev)
	}
}

// handleEvent maps one X event to Handler calls.
func (x *x11) handleEvent(ev xgb.Event) {
	switch e := ev.(type) {
	case xproto.ExposeEvent:
		x.mu.Lock()
		x.repaintLocked()
		x.mu.Unlock()
	case xproto.ConfigureNotifyEvent:
		if x.handler != nil {
			x.handler.Resize(int(e.Width), int(e.Height), 1.0)
			x.present()
		}
	case xproto.ButtonPressEvent:
		if x.handler == nil {
			return
		}
		if dy, ok := x11ButtonScroll(byte(e.Detail)); ok {
			x.handler.Scroll(dy)
			x.present()
			return
		}
		if e.Detail == 1 {
			x.handler.MouseDown(int(e.EventX), int(e.EventY))
			x.present()
		}
	case xproto.KeyPressEvent:
		if x.handler == nil {
			return
		}
		name, r := x11KeyDecode(x.keysyms.lookup(xproto.Keycode(e.Detail)))
		if name != "" || r != 0 {
			x.handler.Key(name, r)
			x.present()
		}
	}
}

// keymap resolves keycodes to keysyms from the server's keyboard mapping.
type keymap struct {
	first   xproto.Keycode
	perCode int
	syms    []xproto.Keysym
}

// loadKeymap fetches the keyboard mapping; on any failure it returns an empty
// map whose lookup yields the no-op keysym 0 (decoded as "no key").
func loadKeymap(conn *xgb.Conn, setup *xproto.SetupInfo) *keymap {
	first := setup.MinKeycode
	count := int(setup.MaxKeycode) - int(setup.MinKeycode) + 1
	km := &keymap{first: first}
	if count <= 0 {
		return km
	}
	reply, err := xproto.GetKeyboardMapping(conn, first, byte(count)).Reply()
	if err != nil || reply == nil {
		return km
	}
	km.perCode = int(reply.KeysymsPerKeycode)
	km.syms = reply.Keysyms
	return km
}

// lookup returns the base (unshifted) keysym for a keycode, or 0 if unmapped.
func (k *keymap) lookup(code xproto.Keycode) uint32 {
	if k.perCode == 0 {
		return 0
	}
	i := (int(code) - int(k.first)) * k.perCode
	if i < 0 || i >= len(k.syms) {
		return 0
	}
	return uint32(k.syms[i])
}
