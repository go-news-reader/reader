// AT-SPI bridge: it publishes the handler's description of what it is showing
// on the accessibility bus, where Orca and other Linux screen readers read it.
//
// The window presents ONE X11 surface holding a rasterised UI. To AT-SPI that is
// an opaque rectangle with no structure at all, so without this the application
// is unreadable and unnavigable. The same problem the macOS bridge solves, in a
// completely different shape: instead of subclassing a view, an application
// EXPORTS D-Bus objects and registers them with a registry daemon.
//
// # The two buses
//
// AT-SPI does not live on the session bus. The session bus only carries
// org.a11y.Bus, whose GetAddress hands back the address of a SECOND bus that the
// accessibility traffic actually flows over. Every toolkit follows that
// indirection and so does this: connecting to the session bus and exporting
// there would look perfectly correct and be invisible to every screen reader.
//
// # Registration
//
// Objects are exported under /org/a11y/atspi/accessible/, then
// org.a11y.atspi.Socket.Embed hands the registry our (bus name, root path) and
// returns the parent to report — an application that skips Embed is exported,
// reachable, and never discovered.
//
// The interface signatures below were taken from a running GTK application over
// the same bus rather than from documentation, so they match what a client
// actually calls.
//
// # Verified end to end
//
// Measured in a Debian 13 VM running at-spi2-core, with a probe validated
// against a GTK application first. at-spi2's own client library reads:
//
//	application "News Reader"
//	  button "Toggle sidebar"   (0,0 48x48)         ['click']
//	  entry  "Search"           (212,10 776x28)     ['click']
//	  button "All Sources"      (0,74 200x34)       ['click']
//	  panel  "Cool URIs Don't Change (1998)"  (212,809 396x84)  ['click']
//
// and driving it works: invoking the click action on "Toggle sidebar" through
// AT-SPI collapses the sidebar, and the published tree follows — 33 elements
// with the sidebar rows, 28 without.
//
// Four things had to be right, none of them guessable, each found by measuring
// rather than by reading:
//
//   - accRef.Name must be a plain string. In godbus, dbus.Sender is an
//     ANNOTATION type ("inject the caller's name"), not a wire type, so using it
//     silently changed the "(so)" signature every AT-SPI method passes around
//     and the registry never listed the application.
//   - org.a11y.atspi.Cache must exist. A client reads an application through
//     GetItems, not by walking it; without the interface the application was
//     discovered and then appeared empty, with at-spi logging exactly that.
//     The item struct is positional and was read off a live GTK application.
//   - Registration must wait until the tree is POPULATED. A client reads the
//     whole cache the moment an application appears and does not read it again
//     unaided, so registering on the first empty frame handed it a permanent
//     snapshot of an empty application.
//   - The root's parent in the cache is the null reference, not what Embed
//     returned.
//
//go:build linux

package window

import (
	"strconv"
	"sync"

	"github.com/godbus/dbus/v5"
)

// AT-SPI object paths. Children live under accPathPrefix + their index, so a
// path is enough to find the element it describes and no lookup table has to be
// kept in step with the tree.
const (
	accRootPath   = "/org/a11y/atspi/accessible/root"
	accPathPrefix = "/org/a11y/atspi/accessible/"

	ifaceAccessible  = "org.a11y.atspi.Accessible"
	ifaceApplication = "org.a11y.atspi.Application"
	ifaceComponent   = "org.a11y.atspi.Component"
	ifaceAction      = "org.a11y.atspi.Action"
	ifaceCache       = "org.a11y.atspi.Cache"
	ifaceEventObject = "org.a11y.atspi.Event.Object"

	accCachePath = "/org/a11y/atspi/cache"
)

// AT-SPI role numbers, read out of pyatspi on a live system rather than guessed:
// a client announces the role, so a wrong number renames every element.
const (
	roleInvalid     uint32 = 0
	roleAlert       uint32 = 2
	roleFiller      uint32 = 20
	roleImage       uint32 = 27
	roleLabel       uint32 = 29
	roleList        uint32 = 31
	rolePanel       uint32 = 39
	rolePushButton  uint32 = 43
	roleStatusBar   uint32 = 54
	roleText        uint32 = 61
	roleToolBar     uint32 = 63
	roleWindow      uint32 = 69
	roleApplication uint32 = 75
	roleEntry       uint32 = 79
	roleDocFrame    uint32 = 82
)

// AT-SPI state numbers (same provenance as the roles).
const (
	stateEnabled   uint32 = 8
	stateFocusable uint32 = 11
	stateSensitive uint32 = 24
	stateShowing   uint32 = 25
	stateVisible   uint32 = 30
)

// atspiRole maps a neutral role name (see A11yElement) to its AT-SPI number.
// Anything unrecognised becomes a panel — AT-SPI's "a thing containing things",
// the honest answer for a role this table does not know.
func atspiRole(neutral string) uint32 {
	switch neutral {
	case "button":
		return rolePushButton
	case "text", "status":
		return roleLabel
	case "textbox", "searchbox":
		return roleEntry
	case "img":
		return roleImage
	case "list", "listbox":
		return roleList
	case "toolbar":
		return roleToolBar
	case "alert", "dialog":
		return roleAlert
	case "document":
		return roleDocFrame
	case "group", "navigation", "banner", "presentation":
		return rolePanel
	default:
		return rolePanel
	}
}

// atspiRoleName is the human-readable role a client may read instead of the
// number. The strings are AT-SPI's own spelling, not ours.
func atspiRoleName(r uint32) string {
	switch r {
	case rolePushButton:
		return "push button"
	case roleLabel:
		return "label"
	case roleEntry:
		return "entry"
	case roleImage:
		return "image"
	case roleList:
		return "list"
	case roleToolBar:
		return "tool bar"
	case roleAlert:
		return "alert"
	case roleDocFrame:
		return "document frame"
	case roleApplication:
		return "application"
	case roleWindow:
		return "window"
	case rolePanel:
		return "panel"
	case roleFiller:
		return "filler"
	case roleText:
		return "text"
	case roleStatusBar:
		return "status bar"
	case roleList + 1: // list item
		return "list item"
	default:
		return "unknown"
	}
}

// accRef is AT-SPI's object reference: the bus name that owns it and its path,
// the "(so)" every AT-SPI method passes around.
//
// Name is a plain string, NOT dbus.Sender. In godbus, dbus.Sender is an
// ANNOTATION type — a parameter of that type means "inject the caller's name"
// rather than "a string on the wire" — so using it here silently changes the
// struct's signature and every method carrying a reference stops matching what
// AT-SPI expects.
type accRef struct {
	Name string
	Path dbus.ObjectPath
}

// nullRef is the reference AT-SPI uses for "nothing".
var nullRef = accRef{Name: "org.a11y.atspi.Registry", Path: "/org/a11y/atspi/null"}

// bridge holds the live connection and the snapshot being described.
type bridge struct {
	conn *dbus.Conn
	name string // our unique bus name on the accessibility bus

	mu       sync.Mutex
	elems    []A11yElement
	exported int // how many child paths have been exported so far
	parent   accRef
}

var (
	axBridge  *bridge
	axOnce    sync.Once
	axStarted bool
)

// childPath is the object path for the nth element.
func childPath(n int) dbus.ObjectPath {
	return dbus.ObjectPath(accPathPrefix + strconv.Itoa(n))
}

// snapshot returns the elements currently being described.
func (b *bridge) snapshot() []A11yElement {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.elems
}

// at returns the nth element of the snapshot.
func (b *bridge) at(n int) (A11yElement, bool) {
	e := b.snapshot()
	if n < 0 || n >= len(e) {
		return A11yElement{}, false
	}
	return e[n], true
}

// ref builds a reference to one of our own objects.
func (b *bridge) ref(p dbus.ObjectPath) accRef {
	return accRef{Name: b.name, Path: p}
}
