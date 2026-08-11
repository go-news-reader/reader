// Copyright (c) 2026 the go-news-reader authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package window

import (
	"time"

	"github.com/go-widgets/toolkit"
	gw "github.com/go-widgets/window"
)

// wheelPixelsPerNotch is how far one wheel notch scrolls, in device pixels. It
// is the value this package's own back-ends used before they were replaced, kept
// so the scroll feel does not change under the user.
const wheelPixelsPerNotch = 40

// Run opens a native window through go-widgets/window and shows the handler's
// framebuffer in it.
//
// This package used to carry its own Cocoa, X11 and win32 back-ends — about
// 3,800 lines that go-widgets/window already had, including the three
// accessibility bridges, which were upstreamed there. What is left here is the
// contract this application speaks (Handler, Config, A11yElement) and the
// translation between it and the toolkit's, because the two genuinely differ:
// see the named helpers below, each of which exists to fix a mismatch that would
// otherwise fail silently rather than loudly.
//
// The handler renders its own pixels, so the window is asked for a framebuffer
// at the panel's real resolution (gw.NativeScale) rather than one pixel per
// logical point. Without that the reader would go visibly soft on a Retina
// display, which is what it renders device-sized to avoid.
func Run(cfg Config, h Handler) error {
	surf := toolkit.NewSurface(nil)

	win, err := gw.Open(gw.Config{
		Title:       cfg.Title,
		Width:       int(cfg.Width),
		Height:      int(cfg.Height),
		RenderScale: gw.NativeScale,
	})
	if err != nil {
		return err
	}

	// The handler is told the framebuffer size in RENDER pixels plus the scale,
	// which is what its own back-ends always passed it (points x backing). It
	// lays out in device pixels and uses the scale for type sizes; handing it
	// logical points instead makes it render a quarter-size buffer into a
	// full-size window, which is exactly what the first on-device run did.
	scale := 1.0
	if s, ok := win.(gw.Scaler); ok && s.RenderScale() > 0 {
		scale = s.RenderScale()
	}

	// A back-end that reaches the OS pasteboard becomes the toolkit-wide
	// clipboard AND the app's, so copy/paste crosses application boundaries.
	if c, ok := win.(gw.Clipboard); ok {
		toolkit.SetClipboard(c)
		if cc, ok := h.(ClipboardController); ok {
			cc.SetSystemClipboard(c)
		}
	}

	ap := newAppearancePump(win, h)

	var lastW, lastH int
	surf.Frame = func() ([]byte, int, int) {
		r := surf.Bounds()
		if r.W != lastW || r.H != lastH {
			lastW, lastH = r.W, r.H
			h.Resize(r.W, r.H, scale)
		}
		ap.poll()
		buf, w, hh, _ := h.Frame()
		return buf, w, hh
	}
	surf.OnInput = func(ev toolkit.Event) { route(h, ev) }
	if a, ok := h.(Accessible); ok {
		surf.Elements = func() []toolkit.SurfaceElement { return elements(a) }
	}

	// The handler's Frame is also this application's CLOCK, not just its
	// painter: background goroutines enqueue scene mutations that are only ever
	// applied there, animated previews advance there, and a debounced page
	// render fires there. Its own back-end ran an NSTimer for exactly this.
	//
	// So the tick is not a repaint policy. Without it a fetch that completed
	// would sit in its queue for as long as the window stayed idle, and the
	// reader would show its first frame forever.
	//
	// The cost against the old design is one blit per tick even when nothing
	// changed: the old back-end asked the handler first and presented only on a
	// change, which cannot be done from here without touching the scene off the
	// render thread — the one thing this application's design forbids.
	if r, ok := win.(gw.Repainter); ok {
		stop := make(chan struct{})
		defer close(stop)
		go func() {
			t := time.NewTicker(time.Second / 60)
			defer t.Stop()
			for {
				select {
				case <-t.C:
					r.Repaint()
				case <-stop:
					return
				}
			}
		}()
	}

	ap.start()
	return win.Run(surf)
}

// route turns a toolkit event into the handler's vocabulary.
//
// The pointer kinds map one for one, but two things do not. A wheel Delta is in
// ROWS here and DEVICE PIXELS there. And a chord with a command modifier goes to
// Shortcut rather than Key: Key takes a name and a rune with no room to say
// which modifiers were held, which is why this application grew a separate sink
// for it in the first place.
func route(h Handler, ev toolkit.Event) {
	switch ev.Kind {
	case toolkit.EventClick:
		h.MouseDown(ev.X, ev.Y)
	case toolkit.EventMouseDrag, toolkit.EventMouseMove:
		h.MouseMove(ev.X, ev.Y)
	case toolkit.EventMouseUp:
		h.MouseUp(ev.X, ev.Y)
	case toolkit.EventScroll:
		h.Scroll(ev.Delta * wheelPixelsPerNotch)
	case toolkit.EventKeyDown:
		if ev.Ctrl || ev.Meta {
			if s, ok := h.(ShortcutSink); ok {
				if r := shortcutRune(ev.Code); r != 0 {
					s.Shortcut(r, ev.Ctrl, ev.Meta)
					return
				}
			}
		}
		if name := keyName(ev.Code); name != "" {
			h.Key(name, 0)
		}
	case toolkit.EventChar:
		for _, r := range ev.Code {
			h.Key("", r)
			return
		}
	}
}

// keyName translates the toolkit's DOM-style key names into the ones this
// application already answers to, and drops the rest.
//
// The arrows are the whole reason this exists: the toolkit says "ArrowUp" and
// this application has always said "Up". Nothing errors when they disagree —
// the key simply stops doing anything, which is the kind of silence that gets
// noticed a week later by a user and not by a test.
func keyName(code string) string {
	switch code {
	case "ArrowUp":
		return "Up"
	case "ArrowDown":
		return "Down"
	case "ArrowLeft":
		return "Left"
	case "ArrowRight":
		return "Right"
	case "Backspace", "Escape", "Enter":
		return code
	}
	return ""
}

// shortcutRune is the base character of a modified chord, or 0 when the chord
// is not on a character key. A one-rune Code is that character; anything longer
// is a named key, which is not what Shortcut is for.
func shortcutRune(code string) rune {
	n := 0
	var first rune
	for _, r := range code {
		if n == 0 {
			first = r
		}
		n++
	}
	if n != 1 {
		return 0
	}
	return first
}

// elements maps what the application says it is showing into the toolkit's
// shape, so the accessibility bridges in go-widgets/window read a real tree
// instead of one opaque rectangle.
func elements(a Accessible) []toolkit.SurfaceElement {
	src := a.A11yElements()
	if len(src) == 0 {
		return nil
	}
	out := make([]toolkit.SurfaceElement, 0, len(src))
	for _, e := range src {
		out = append(out, toolkit.SurfaceElement{
			Role:  toolkit.Role(e.Role),
			Name:  e.Name,
			Value: e.Value,
			X:     e.X, Y: e.Y, W: e.W, H: e.H,
		})
	}
	return out
}

// appearancePump turns the window's PULLED appearance into the PUSHES this
// application's handler expects.
//
// The back-end answers a question; the handler wants to be told when something
// changed. Polling and forwarding only on a difference is what the per-OS
// back-end here used to do, so the handler sees exactly what it saw before. The
// system font is fetched once at start: it is around 8 MB, and asking for it on
// every frame would be the most expensive thing the reader does.
type appearancePump struct {
	src  gw.AppearanceReader
	sink AppearanceSink
	last SystemAppearance
	set  bool
}

func newAppearancePump(win gw.Backend, h Handler) *appearancePump {
	p := &appearancePump{}
	src, ok := win.(gw.AppearanceReader)
	if !ok {
		return p
	}
	sink, ok := h.(AppearanceSink)
	if !ok {
		return p
	}
	p.src, p.sink = src, sink
	return p
}

// start pushes the first reading, carrying the system font with it.
func (p *appearancePump) start() {
	if p.src == nil {
		return
	}
	ap := p.read()
	if ttf, err := p.src.SystemFontTTF(); err == nil {
		ap.FontTTF = ttf
	}
	p.last, p.set = ap, true
	p.sink.SystemAppearance(ap)
}

// poll pushes only when the look changed, and never re-sends the font: the
// handler keeps the one it was given at start.
func (p *appearancePump) poll() {
	if p.src == nil || !p.set {
		return
	}
	ap := p.read()
	// Compared field by field, not with ==: SystemAppearance carries the font
	// bytes and a slice is not comparable. Only the look is compared anyway --
	// the font never changes mid-session, and re-sending 8 MB because a struct
	// compare said "different" would be the worst kind of correct.
	if ap.Dark == p.last.Dark && ap.HasAccent == p.last.HasAccent && ap.Accent == p.last.Accent {
		return
	}
	p.last.Dark, p.last.Accent, p.last.HasAccent = ap.Dark, ap.Accent, ap.HasAccent
	p.sink.SystemAppearance(ap)
}

func (p *appearancePump) read() SystemAppearance {
	a := p.src.Appearance()
	return SystemAppearance{Dark: a.Dark, Accent: a.Accent, HasAccent: a.HasAccent}
}
