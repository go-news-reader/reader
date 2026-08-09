// Connecting to the accessibility bus, and keeping the published tree current.
//
//go:build linux

package window

import (
	"os"
	"sync"

	"github.com/godbus/dbus/v5"
)

// The window's on-screen origin in device pixels, recorded by the X11 back-end
// when the window is configured. AT-SPI asks for extents in screen coordinates
// as well as window ones, and only the back-end knows where the window is.
var (
	winOriginMu sync.Mutex
	winOriginX  int
	winOriginY  int
)

// setWindowOrigin records the window position for screen-coordinate extents.
func setWindowOrigin(x, y int) {
	winOriginMu.Lock()
	winOriginX, winOriginY = x, y
	winOriginMu.Unlock()
}

// axHandler is the Handler the X11 back-end is driving. The bridge is reached
// from D-Bus method calls on their own goroutines, which have no path back to
// the x11 value, so Run publishes it here once.
var axHandler Handler

// setA11yHandler is called by the back-end as it starts.
func setA11yHandler(h Handler) { axHandler = h }

// startA11y brings the bridge up: find the accessibility bus, export the
// application root, and register with the registry.
//
// Every step is allowed to fail quietly. A machine with no accessibility stack
// running — which is most of them — has no org.a11y.Bus to answer, and a news
// reader must not refuse to start over that. The window simply presents pixels,
// exactly as it did before this file existed.
func startA11y() {
	if _, ok := axHandler.(Accessible); !ok {
		return
	}
	addr, err := a11yBusAddress()
	if err != nil || addr == "" {
		return
	}
	conn, err := dbus.Dial(addr)
	if err != nil {
		return
	}
	if err := conn.Auth(nil); err != nil {
		conn.Close()
		return
	}
	if err := conn.Hello(); err != nil {
		conn.Close()
		return
	}

	b := &bridge{conn: conn, name: conn.Names()[0], parent: nullRef, elems: pendingElems}
	root := &accRoot{b: b}
	for _, iface := range []string{ifaceAccessible, ifaceApplication} {
		if err := conn.Export(root, accRootPath, iface); err != nil {
			conn.Close()
			return
		}
	}
	if err := conn.Export(root, accRootPath, "org.freedesktop.DBus.Properties"); err != nil {
		conn.Close()
		return
	}

	// The cache is how a client actually reads the tree; without it the
	// application is discovered and then appears empty.
	if err := conn.Export(&accCache{b: b}, accCachePath, ifaceCache); err != nil {
		conn.Close()
		return
	}

	// Export what is already known so the application is complete the first time
	// anything looks at it.
	for i := range b.elems {
		child := &accChild{b: b, idx: i}
		p := childPath(i)
		for _, iface := range []string{ifaceAccessible, ifaceComponent, ifaceAction, "org.freedesktop.DBus.Properties"} {
			if err := conn.Export(child, p, iface); err != nil {
				conn.Close()
				return
			}
		}
	}
	b.exported = len(b.elems)

	// Embed hands the registry our root and returns the parent to report. An
	// application that exports its objects but skips this is reachable on the bus
	// and never discovered by anything.
	var parent accRef
	call := conn.Object("org.a11y.atspi.Registry", accRootPath).Call(
		"org.a11y.atspi.Socket.Embed", 0, b.ref(accRootPath))
	if call.Err == nil {
		_ = call.Store(&parent)
		b.mu.Lock()
		b.parent = parent
		b.mu.Unlock()
	}

	axBridge = b
	axStarted = true
}

// a11yBusAddress asks the SESSION bus where the accessibility bus is.
//
// AT-SPI runs on its own bus; org.a11y.Bus on the session bus is only the
// pointer to it. AT_SPI_BUS_ADDRESS overrides the lookup, which is how a test
// harness points an application at a bus it controls.
func a11yBusAddress() (string, error) {
	if a := os.Getenv("AT_SPI_BUS_ADDRESS"); a != "" {
		return a, nil
	}
	sess, err := dbus.SessionBus()
	if err != nil {
		return "", err
	}
	var addr string
	err = sess.Object("org.a11y.Bus", "/org/a11y/bus").
		Call("org.a11y.Bus.GetAddress", 0).Store(&addr)
	return addr, err
}

// refreshA11yLinux republishes the tree for the frame just drawn.
//
// It is called from the same damage signal that triggers a repaint, so the
// description never lags the pixels. New object paths are exported as the tree
// grows and then kept: a client may hold a reference across frames, and
// unexporting underneath it would turn a live reference into an error. Paths
// beyond the current tree simply report an invalid role and are not listed as
// children, so they are unreachable rather than wrong.
func refreshA11yLinux() {
	acc, ok := axHandler.(Accessible)
	if !ok {
		return
	}

	// Drop what cannot be announced before publishing: an unnamed or zero-area
	// element is a stop a screen-reader user has to skip past for nothing.
	var keep []A11yElement
	for _, e := range acc.A11yElements() {
		if axSkip(e) || e.Role == "document" {
			continue
		}
		keep = append(keep, e)
	}
	pendingElems = keep

	// Register only once the tree is populated. A client reads the whole cache
	// the moment an application appears and does not read it again unaided, so
	// registering first and filling in afterwards hands it a permanent snapshot
	// of an empty application — which is exactly what it showed.
	axOnce.Do(startA11y)
	if !axStarted || axBridge == nil {
		return
	}

	b := axBridge
	b.mu.Lock()
	b.elems = keep
	need := len(keep)
	have := b.exported
	b.mu.Unlock()

	for i := have; i < need; i++ {
		child := &accChild{b: b, idx: i}
		p := childPath(i)
		for _, iface := range []string{ifaceAccessible, ifaceComponent, ifaceAction, "org.freedesktop.DBus.Properties"} {
			if err := b.conn.Export(child, p, iface); err != nil {
				return
			}
		}
	}
	if need > have {
		b.mu.Lock()
		b.exported = need
		b.mu.Unlock()
		// Tell any client that already snapshotted the cache about the new
		// objects. Without this a tree that grows after registration is invisible
		// to everything that read it once.
		cache := &accCache{b: b}
		root := b.ref(accRootPath)
		for i := have; i < need; i++ {
			item, ok := cache.item(i)
			if !ok {
				continue
			}
			_ = b.conn.Emit(accCachePath, ifaceCache+".AddAccessible", item)
			// An AT-SPI client builds its model from the event stream as much as
			// from the cache, so a tree that only appears in the cache has nothing
			// to attach it to. The signature is "siiv(so)": what changed, two
			// indices, the payload, and the source's application.
			_ = b.conn.Emit(accRootPath, ifaceEventObject+".ChildrenChanged",
				"add", int32(i), int32(0),
				dbus.MakeVariant(b.ref(childPath(i))), root)
		}
	}
}

// pendingElems is the tree built before the bridge has registered, so startA11y
// can publish a populated application rather than an empty one.
var pendingElems []A11yElement

// windowOrigin is where the window sits on screen, in device pixels, so screen
// coordinates can be derived from the window-relative rects the handler
// publishes. The X11 back-end records it when the window is configured.
func windowOrigin() (int, int) {
	winOriginMu.Lock()
	defer winOriginMu.Unlock()
	return winOriginX, winOriginY
}
