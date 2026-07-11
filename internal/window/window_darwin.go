// Package window opens a native macOS window that presents a caller-supplied
// RGBA framebuffer and feeds native mouse/scroll/key events back to it. It is
// a pure presenter: it draws the pixels the [Handler] hands it (the widgets are
// rasterised elsewhere, in the ui/app packages) and never imports them, so it
// stays a generic surface.
//
// Everything is driven through the Objective-C runtime via
// github.com/ebitengine/purego — no cgo — so the whole app builds and links
// with CGO_ENABLED=0 (the fleet-wide requirement). A custom NSView subclass
// blits an NSBitmapImageRep built from the current buffer in -drawRect:; native
// -mouseDown:/-scrollWheel:/-keyDown: are decoded and forwarded to the Handler;
// a repeating NSTimer damage-gates repaints so an async feed load appears and
// nothing repaints when the frame is unchanged.
//
//go:build darwin

package window

import (
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// selectors resolved once at first use.
var (
	selAlloc                     = objc.RegisterName("alloc")
	selInit                      = objc.RegisterName("init")
	selRetain                    = objc.RegisterName("retain")
	selSharedApplication         = objc.RegisterName("sharedApplication")
	selSetActivationPolicy       = objc.RegisterName("setActivationPolicy:")
	selActivateIgnoringOtherApps = objc.RegisterName("activateIgnoringOtherApps:")
	selRun                       = objc.RegisterName("run")
	selStringWithUTF8String      = objc.RegisterName("stringWithUTF8String:")
	selSetTitle                  = objc.RegisterName("setTitle:")
	selSetContentView            = objc.RegisterName("setContentView:")
	selContentView               = objc.RegisterName("contentView")
	selMakeKeyAndOrderFront      = objc.RegisterName("makeKeyAndOrderFront:")
	selMakeFirstResponder        = objc.RegisterName("makeFirstResponder:")
	selCenter                    = objc.RegisterName("center")
	selInitWithContentRect       = objc.RegisterName("initWithContentRect:styleMask:backing:defer:")
	selInitWithFrame             = objc.RegisterName("initWithFrame:")
	selSetDelegate               = objc.RegisterName("setDelegate:")
	selBackingScaleFactor        = objc.RegisterName("backingScaleFactor")
	selBounds                    = objc.RegisterName("bounds")
	selSetNeedsDisplay           = objc.RegisterName("setNeedsDisplay:")
	selWindow                    = objc.RegisterName("window")
	selLocationInWindow          = objc.RegisterName("locationInWindow")
	selScrollingDeltaY           = objc.RegisterName("scrollingDeltaY")
	selKeyCode                   = objc.RegisterName("keyCode")
	selCharsIgnoringMods         = objc.RegisterName("charactersIgnoringModifiers")
	selLengthOfBytes             = objc.RegisterName("lengthOfBytesUsingEncoding:")
	selGetCString                = objc.RegisterName("getCString:maxLength:encoding:")
	selInitBitmapRep             = objc.RegisterName("initWithBitmapDataPlanes:pixelsWide:pixelsHigh:bitsPerSample:samplesPerPixel:hasAlpha:isPlanar:colorSpaceName:bytesPerRow:bitsPerPixel:")
	selDrawInRect                = objc.RegisterName("drawInRect:")
	selScheduledTimer            = objc.RegisterName("scheduledTimerWithTimeInterval:target:selector:userInfo:repeats:")
)

// NSWindowStyleMask bits.
const (
	styleTitled         = 1 << 0
	styleClosable       = 1 << 1
	styleMiniaturizable = 1 << 2
	styleResizable      = 1 << 3
)

const (
	backingStoreBuffered = 2
	activationPolicyReg  = 0 // NSApplicationActivationPolicyRegular
	nsUTF8Encoding       = 4 // NSUTF8StringEncoding
	frameInterval        = 1.0 / 60.0
)

// NSPoint / NSSize / NSRect mirror the CoreGraphics geometry structs. purego
// marshals them by value across the amd64/arm64 calling conventions (four
// float64s for a rect), the same way go-reddit's webview passes CGRect.
type nsPoint struct{ X, Y float64 }
type nsSize struct{ W, H float64 }
type nsRect struct {
	Origin nsPoint
	Size   nsSize
}

// present state, shared between the timer tick and -drawRect:. There is a
// single window per process, so package-level state is sufficient.
var (
	mu       sync.Mutex
	handler  Handler
	curBuf   []byte // latest framebuffer; kept alive so the bitmap rep can read it
	curW     int
	curH     int
	curScale = 1.0
	view     objc.ID
	win      objc.ID
)

// frameworksLoaded guards the one-time dlopen of Foundation/AppKit.
var frameworksLoaded bool

func loadFrameworks() error {
	if frameworksLoaded {
		return nil
	}
	for _, p := range []string{
		"/System/Library/Frameworks/Foundation.framework/Foundation",
		"/System/Library/Frameworks/AppKit.framework/AppKit",
	} {
		if _, err := purego.Dlopen(p, purego.RTLD_GLOBAL|purego.RTLD_NOW); err != nil {
			return fmt.Errorf("window: dlopen %s: %w", p, err)
		}
	}
	frameworksLoaded = true
	return nil
}

// nsString builds an NSString from a Go string.
func nsString(s string) objc.ID {
	return objc.ID(objc.GetClass("NSString")).Send(selStringWithUTF8String, s)
}

// goString copies an NSString's UTF-8 bytes into a Go-owned buffer, avoiding
// any uintptr→Pointer arithmetic on the ObjC-owned bytes.
func goString(nsstr objc.ID) string {
	if nsstr == 0 {
		return ""
	}
	n := int(nsstr.Send(selLengthOfBytes, nsUTF8Encoding))
	if n <= 0 {
		return ""
	}
	buf := make([]byte, n+1)
	if nsstr.Send(selGetCString, unsafe.Pointer(&buf[0]), len(buf), nsUTF8Encoding) == 0 {
		return ""
	}
	return string(buf[:n])
}

// classesOnce registers the view and agent classes exactly once.
var (
	classesOnce sync.Once
	viewClass   objc.Class
	agentClass  objc.Class
	classesErr  error
)

func registerClasses() (objc.Class, objc.Class, error) {
	classesOnce.Do(func() {
		viewClass, classesErr = objc.RegisterClass(
			"GoNewsReaderView", objc.GetClass("NSView"), nil, nil,
			[]objc.MethodDef{
				{Cmd: objc.RegisterName("isFlipped"), Fn: viewIsFlipped},
				{Cmd: objc.RegisterName("acceptsFirstResponder"), Fn: viewAcceptsFirstResponder},
				{Cmd: objc.RegisterName("drawRect:"), Fn: viewDrawRect},
				{Cmd: objc.RegisterName("mouseDown:"), Fn: viewMouseDown},
				{Cmd: objc.RegisterName("scrollWheel:"), Fn: viewScrollWheel},
				{Cmd: objc.RegisterName("keyDown:"), Fn: viewKeyDown},
			})
		if classesErr != nil {
			return
		}
		agentClass, classesErr = objc.RegisterClass(
			"GoNewsReaderAgent", objc.GetClass("NSObject"), nil, nil,
			[]objc.MethodDef{
				{Cmd: objc.RegisterName("tick:"), Fn: agentTick},
				{Cmd: objc.RegisterName("windowDidResize:"), Fn: agentWindowDidResize},
			})
	})
	return viewClass, agentClass, classesErr
}

// viewIsFlipped makes the view use a top-left origin, matching the buffer.
func viewIsFlipped(_ objc.ID, _ objc.SEL) bool { return true }

// viewAcceptsFirstResponder lets the view receive keyDown: events.
func viewAcceptsFirstResponder(_ objc.ID, _ objc.SEL) bool { return true }

// viewDrawRect blits the current framebuffer, scaled to fill the view bounds.
// The (NSRect) dirty-rect argument the runtime passes is intentionally not
// declared: it rides in the float registers and is ignored — we always redraw
// the whole surface from the latest buffer.
func viewDrawRect(self objc.ID, _ objc.SEL) {
	mu.Lock()
	buf, w, h := curBuf, curW, curH
	mu.Unlock()
	if len(buf) == 0 || w == 0 || h == 0 {
		return
	}
	rep := newBitmapRep(buf, w, h)
	if rep == 0 {
		return
	}
	bounds := objc.Send[nsRect](self, selBounds)
	rep.Send(selDrawInRect, bounds)
	// buf must stay alive until drawInRect: has read it.
	runtime.KeepAlive(buf)
}

// newBitmapRep wraps an RGBA buffer in an NSBitmapImageRep that references (does
// not copy) the bytes. The planes array is only read during init; buf must
// outlive the rep's use (the caller keeps it alive).
func newBitmapRep(buf []byte, w, h int) objc.ID {
	planes := [1]unsafe.Pointer{unsafe.Pointer(&buf[0])}
	rep := objc.ID(objc.GetClass("NSBitmapImageRep")).Send(selAlloc).Send(
		selInitBitmapRep,
		unsafe.Pointer(&planes[0]), // unsigned char **planes
		w,                          // pixelsWide
		h,                          // pixelsHigh
		8,                          // bitsPerSample
		4,                          // samplesPerPixel
		true,                       // hasAlpha
		false,                      // isPlanar
		nsString("NSDeviceRGBColorSpace"),
		w*4, // bytesPerRow
		32,  // bitsPerPixel
	)
	return rep
}

// present pulls the latest frame and, if it changed, stores it and marks the
// view for redisplay. Runs on the main thread (timer tick / after an event).
func present() {
	if handler == nil {
		return
	}
	buf, w, h, changed := handler.Frame()
	if !changed {
		return
	}
	mu.Lock()
	curBuf, curW, curH = buf, w, h
	mu.Unlock()
	if view != 0 {
		view.Send(selSetNeedsDisplay, true)
	}
}

// agentTick is the NSTimer callback: damage-gated repaint each frame.
func agentTick(_ objc.ID, _ objc.SEL, _ objc.ID) { present() }

// agentWindowDidResize re-derives the device size from the content view bounds
// and backing scale and forwards it to the Handler, then repaints.
func agentWindowDidResize(_ objc.ID, _ objc.SEL, _ objc.ID) {
	if handler == nil || win == 0 {
		return
	}
	scale := float64(objc.Send[float64](win, selBackingScaleFactor))
	if scale <= 0 {
		scale = 1
	}
	cv := win.Send(selContentView)
	b := objc.Send[nsRect](cv, selBounds)
	mu.Lock()
	curScale = scale
	mu.Unlock()
	handler.Resize(int(b.Size.W*scale), int(b.Size.H*scale), scale)
	present()
}

// viewCoords converts an event's -locationInWindow (window base coords,
// bottom-left origin) to device pixels with a top-left origin.
func viewCoords(self, event objc.ID) (int, int) {
	p := objc.Send[nsPoint](event, selLocationInWindow)
	b := objc.Send[nsRect](self, selBounds)
	mu.Lock()
	scale := curScale
	mu.Unlock()
	x := p.X
	y := b.Size.H - p.Y // flip to top-left origin (points)
	return int(x * scale), int(y * scale)
}

func viewMouseDown(self objc.ID, _ objc.SEL, event objc.ID) {
	if handler == nil {
		return
	}
	x, y := viewCoords(self, event)
	handler.MouseDown(x, y)
	present()
}

func viewScrollWheel(self objc.ID, _ objc.SEL, event objc.ID) {
	if handler == nil {
		return
	}
	dy := float64(objc.Send[float64](event, selScrollingDeltaY))
	mu.Lock()
	scale := curScale
	mu.Unlock()
	// AppKit's scrollingDeltaY is positive when scrolling up; negate so a
	// downward wheel increases ScrollY (browser convention).
	handler.Scroll(int(-dy * scale))
	present()
}

func viewKeyDown(_ objc.ID, _ objc.SEL, event objc.ID) {
	if handler == nil {
		return
	}
	name, r := decodeKey(event)
	handler.Key(name, r)
	present()
}

// decodeKey maps an NSEvent keyDown to a symbolic editing-key name or a
// printable rune. keyCode is checked first so Return/Escape/Delete never leak
// through as control runes.
func decodeKey(event objc.ID) (name string, r rune) {
	switch uint16(event.Send(selKeyCode)) {
	case 51: // Delete (Backspace)
		return "Backspace", 0
	case 53: // Escape
		return "Escape", 0
	case 36, 76: // Return, Keypad Enter
		return "Enter", 0
	}
	s := goString(event.Send(selCharsIgnoringMods))
	rs := []rune(s)
	if len(rs) == 1 && rs[0] >= 0x20 {
		return "", rs[0]
	}
	return "", 0
}

// Run opens the window and enters the Cocoa run loop. It blocks until the app
// quits, and must run on the main OS thread (the caller does
// runtime.LockOSThread).
func Run(cfg Config, h Handler) error {
	if err := loadFrameworks(); err != nil {
		return err
	}
	vc, ac, err := registerClasses()
	if err != nil {
		return err
	}
	if cfg.Width == 0 {
		cfg.Width = 1000
	}
	if cfg.Height == 0 {
		cfg.Height = 700
	}
	handler = h

	app := objc.ID(objc.GetClass("NSApplication")).Send(selSharedApplication)
	app.Send(selSetActivationPolicy, activationPolicyReg)

	rect := nsRect{Size: nsSize{W: cfg.Width, H: cfg.Height}}
	style := uint(styleTitled | styleClosable | styleMiniaturizable | styleResizable)
	win = objc.ID(objc.GetClass("NSWindow")).Send(selAlloc).
		Send(selInitWithContentRect, rect, style, backingStoreBuffered, false)
	win.Send(selRetain)

	view = objc.ID(vc).Send(selAlloc).Send(selInitWithFrame, rect)
	view.Send(selRetain)
	win.Send(selSetContentView, view)

	agent := objc.ID(ac).Send(selAlloc).Send(selInit)
	agent.Send(selRetain)
	win.Send(selSetDelegate, agent)

	// Seed the scene size from the backing scale, then prime the first frame.
	scale := float64(objc.Send[float64](win, selBackingScaleFactor))
	if scale <= 0 {
		scale = 1
	}
	curScale = scale
	h.Resize(int(cfg.Width*scale), int(cfg.Height*scale), scale)
	present()

	// ~60 Hz damage-gated repaint; also lets an async feed load appear.
	objc.ID(objc.GetClass("NSTimer")).Send(selScheduledTimer,
		float64(frameInterval), agent, objc.RegisterName("tick:"), objc.ID(0), true)

	win.Send(selSetTitle, nsString(cfg.Title))
	win.Send(selCenter)
	win.Send(selMakeKeyAndOrderFront, objc.ID(0))
	win.Send(selMakeFirstResponder, view)
	app.Send(selActivateIgnoringOtherApps, true)
	app.Send(selRun) // blocks until the app quits
	return nil
}
