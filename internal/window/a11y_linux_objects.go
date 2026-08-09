// The exported AT-SPI objects: an application root, and one object per element.
//
// Properties are served by hand rather than through godbus's prop helper. A
// client reads Name, ChildCount and Parent constantly, and the tree is rebuilt
// on every damaged frame; a property map would have to be rewritten just as
// often, where reading straight from the snapshot costs nothing.
//
//go:build linux

package window

import (
	"github.com/godbus/dbus/v5"
)

// ---- application root -------------------------------------------------

// accRoot is the application object every AT-SPI client starts from.
type accRoot struct{ b *bridge }

func (r *accRoot) GetRole() (uint32, *dbus.Error)     { return roleApplication, nil }
func (r *accRoot) GetRoleName() (string, *dbus.Error) { return "application", nil }
func (r *accRoot) GetLocalizedRoleName() (string, *dbus.Error) {
	return "application", nil
}
func (r *accRoot) GetIndexInParent() (int32, *dbus.Error) { return 0, nil }
func (r *accRoot) GetChildAtIndex(i int32) (accRef, *dbus.Error) {
	if _, ok := r.b.at(int(i)); !ok {
		return nullRef, nil
	}
	return r.b.ref(childPath(int(i))), nil
}
func (r *accRoot) GetChildren() ([]accRef, *dbus.Error) {
	elems := r.b.snapshot()
	out := make([]accRef, 0, len(elems))
	for i := range elems {
		out = append(out, r.b.ref(childPath(i)))
	}
	return out, nil
}
func (r *accRoot) GetApplication() (accRef, *dbus.Error) {
	return r.b.ref(accRootPath), nil
}
func (r *accRoot) GetState() ([]uint32, *dbus.Error)               { return stateBits(), nil }
func (r *accRoot) GetAttributes() (map[string]string, *dbus.Error) { return map[string]string{}, nil }
func (r *accRoot) GetInterfaces() ([]string, *dbus.Error) {
	return []string{ifaceAccessible, ifaceApplication}, nil
}
func (r *accRoot) GetRelationSet() ([]struct {
	V0 uint32
	V1 []accRef
}, *dbus.Error) {
	return nil, nil
}

// Application interface. RegisterEventListener is called by clients that want
// change notifications; accepting and ignoring it is honest — the tree is always
// read fresh, so there is nothing stale to notify about — and refusing it makes
// some clients give up on the application entirely.
func (r *accRoot) RegisterEventListener(string) *dbus.Error   { return nil }
func (r *accRoot) DeregisterEventListener(string) *dbus.Error { return nil }
func (r *accRoot) GetLocale(uint32) (string, *dbus.Error)     { return "C", nil }

// Get serves org.freedesktop.DBus.Properties for the root.
func (r *accRoot) Get(iface, prop string) (dbus.Variant, *dbus.Error) {
	switch prop {
	case "Name":
		return dbus.MakeVariant("News Reader"), nil
	case "Description":
		return dbus.MakeVariant("news reader"), nil
	case "AccessibleId":
		return dbus.MakeVariant("go-news-reader"), nil
	case "Locale":
		return dbus.MakeVariant("C"), nil
	case "HelpText":
		return dbus.MakeVariant(""), nil
	case "ChildCount":
		return dbus.MakeVariant(int32(len(r.b.snapshot()))), nil
	case "Parent":
		r.b.mu.Lock()
		p := r.b.parent
		r.b.mu.Unlock()
		return dbus.MakeVariant(p), nil
	case "ToolkitName":
		return dbus.MakeVariant("go-widgets"), nil
	case "Version":
		return dbus.MakeVariant("1"), nil
	case "AtspiVersion":
		return dbus.MakeVariant("2.1"), nil
	case "Id":
		return dbus.MakeVariant(int32(0)), nil
	}
	return dbus.Variant{}, dbus.NewError("org.freedesktop.DBus.Error.UnknownProperty", []any{prop})
}

func (r *accRoot) GetAll(iface string) (map[string]dbus.Variant, *dbus.Error) {
	out := map[string]dbus.Variant{}
	names := []string{"Name", "Description", "AccessibleId", "Locale", "HelpText", "ChildCount", "Parent"}
	if iface == ifaceApplication {
		names = []string{"ToolkitName", "Version", "AtspiVersion", "Id"}
	}
	for _, n := range names {
		if v, err := r.Get(iface, n); err == nil {
			out[n] = v
		}
	}
	return out, nil
}

func (r *accRoot) Set(string, string, dbus.Variant) *dbus.Error { return nil }

// ---- one element ------------------------------------------------------

// accChild describes a single element. It holds its index rather than a copy of
// the element, so it always reads the current snapshot: the tree is rebuilt on
// every damaged frame and a cached copy would describe a screen that is gone.
type accChild struct {
	b   *bridge
	idx int
}

func (c *accChild) elem() A11yElement {
	e, _ := c.b.at(c.idx)
	return e
}

func (c *accChild) GetRole() (uint32, *dbus.Error) {
	e, ok := c.b.at(c.idx)
	if !ok {
		return roleInvalid, nil
	}
	return atspiRole(e.Role), nil
}

func (c *accChild) GetRoleName() (string, *dbus.Error) {
	r, _ := c.GetRole()
	return atspiRoleName(r), nil
}
func (c *accChild) GetLocalizedRoleName() (string, *dbus.Error) { return c.GetRoleName() }

func (c *accChild) GetIndexInParent() (int32, *dbus.Error) { return int32(c.idx), nil }

// Elements are a flat list: the scene already publishes reading order, and
// inventing a hierarchy here would be a second, competing description.
func (c *accChild) GetChildAtIndex(int32) (accRef, *dbus.Error) { return nullRef, nil }
func (c *accChild) GetChildren() ([]accRef, *dbus.Error)        { return nil, nil }

func (c *accChild) GetApplication() (accRef, *dbus.Error) { return c.b.ref(accRootPath), nil }
func (c *accChild) GetState() ([]uint32, *dbus.Error)     { return stateBits(), nil }
func (c *accChild) GetAttributes() (map[string]string, *dbus.Error) {
	return map[string]string{}, nil
}
func (c *accChild) GetInterfaces() ([]string, *dbus.Error) {
	return []string{ifaceAccessible, ifaceComponent, ifaceAction}, nil
}
func (c *accChild) GetRelationSet() ([]struct {
	V0 uint32
	V1 []accRef
}, *dbus.Error) {
	return nil, nil
}

func (c *accChild) Get(iface, prop string) (dbus.Variant, *dbus.Error) {
	e := c.elem()
	switch prop {
	case "Name":
		return dbus.MakeVariant(e.Name), nil
	case "Description":
		return dbus.MakeVariant(e.Value), nil
	case "AccessibleId", "HelpText":
		return dbus.MakeVariant(""), nil
	case "Locale":
		return dbus.MakeVariant("C"), nil
	case "ChildCount":
		return dbus.MakeVariant(int32(0)), nil
	case "Parent":
		return dbus.MakeVariant(c.b.ref(accRootPath)), nil
	case "NActions":
		return dbus.MakeVariant(int32(1)), nil
	}
	return dbus.Variant{}, dbus.NewError("org.freedesktop.DBus.Error.UnknownProperty", []any{prop})
}

func (c *accChild) GetAll(iface string) (map[string]dbus.Variant, *dbus.Error) {
	out := map[string]dbus.Variant{}
	names := []string{"Name", "Description", "AccessibleId", "Locale", "HelpText", "ChildCount", "Parent"}
	if iface == ifaceAction {
		names = []string{"NActions"}
	}
	for _, n := range names {
		if v, err := c.Get(iface, n); err == nil {
			out[n] = v
		}
	}
	return out, nil
}

func (c *accChild) Set(string, string, dbus.Variant) *dbus.Error { return nil }

// ---- Component --------------------------------------------------------

// GetExtents reports where the element is. AT-SPI asks in a coordinate type:
// 0 is screen, 1 is window. The element rects are in the window's own device
// pixels, so the window answer is exact and the screen answer needs the window's
// origin, which the X11 back-end knows.
func (c *accChild) GetExtents(coordType uint32) (struct {
	X, Y, W, H int32
}, *dbus.Error) {
	e := c.elem()
	ox, oy := 0, 0
	if coordType == 0 {
		ox, oy = windowOrigin()
	}
	return struct{ X, Y, W, H int32 }{
		int32(e.X + ox), int32(e.Y + oy), int32(e.W), int32(e.H),
	}, nil
}

func (c *accChild) GetPosition(coordType uint32) (int32, int32, *dbus.Error) {
	ext, _ := c.GetExtents(coordType)
	return ext.X, ext.Y, nil
}

func (c *accChild) GetSize() (int32, int32, *dbus.Error) {
	e := c.elem()
	return int32(e.W), int32(e.H), nil
}

func (c *accChild) Contains(x, y int32, coordType uint32) (bool, *dbus.Error) {
	ext, _ := c.GetExtents(coordType)
	return x >= ext.X && x < ext.X+ext.W && y >= ext.Y && y < ext.Y+ext.H, nil
}

func (c *accChild) GetAccessibleAtPoint(x, y int32, coordType uint32) (accRef, *dbus.Error) {
	if ok, _ := c.Contains(x, y, coordType); ok {
		return c.b.ref(childPath(c.idx)), nil
	}
	return nullRef, nil
}

func (c *accChild) GetLayer() (uint32, *dbus.Error)    { return 3, nil } // widget layer
func (c *accChild) GetMDIZOrder() (int16, *dbus.Error) { return 0, nil }
func (c *accChild) GetAlpha() (float64, *dbus.Error)   { return 1.0, nil }
func (c *accChild) GrabFocus() (bool, *dbus.Error)     { return false, nil }

// ---- Action -----------------------------------------------------------

// Every element offers exactly one action, "click", which replays a real click
// at its centre through the SAME input path a mouse takes — the same choice the
// macOS bridge makes, for the same reason: every behaviour a click has is had by
// the action, with no second implementation to drift.
func (c *accChild) GetName(int32) (string, *dbus.Error)          { return "click", nil }
func (c *accChild) GetLocalizedName(int32) (string, *dbus.Error) { return "click", nil }
func (c *accChild) GetDescription(int32) (string, *dbus.Error)   { return "activate this element", nil }
func (c *accChild) GetKeyBinding(int32) (string, *dbus.Error)    { return "", nil }

func (c *accChild) GetActions() ([]struct{ V0, V1, V2 string }, *dbus.Error) {
	return []struct{ V0, V1, V2 string }{{"click", "activate this element", ""}}, nil
}

func (c *accChild) DoAction(int32) (bool, *dbus.Error) {
	e, ok := c.b.at(c.idx)
	if !ok || axHandler == nil {
		return false, nil
	}
	x, y := e.X+e.W/2, e.Y+e.H/2
	axHandler.MouseDown(x, y)
	axHandler.MouseUp(x, y)
	return true, nil
}

// stateBits is the AT-SPI state bitfield: two 32-bit words, with a state's
// number as its bit index. Everything published is on screen and usable, so the
// same set applies to all of it.
func stateBits() []uint32 {
	var lo, hi uint32
	for _, s := range []uint32{stateEnabled, stateSensitive, stateShowing, stateVisible, stateFocusable} {
		if s < 32 {
			lo |= 1 << s
		} else {
			hi |= 1 << (s - 32)
		}
	}
	return []uint32{lo, hi}
}

// ---- Cache ------------------------------------------------------------

// cacheItem is one entry of org.a11y.atspi.Cache.GetItems, whose signature is
// a((so)(so)(so)iiassusau): the object, its application, its parent, its index
// and child count, the interfaces it implements, its name, role, description
// and state.
//
// The field order and types are not negotiable — they are positional on the
// wire — and were read off a live GTK application's cache rather than from
// documentation.
type cacheItem struct {
	Ref         accRef
	App         accRef
	Parent      accRef
	Index       int32
	ChildCount  int32
	Interfaces  []string
	Name        string
	Role        uint32
	Description string
	State       []uint32
}

// accCache serves the whole tree in one call.
//
// This interface is not an optimisation, it is how a client READS an
// application: at-spi2's own client library calls GetItems and walks the result,
// and an application that does not implement it appears on the bus, answers
// every Accessible method correctly, and still shows up empty — the exact
// symptom measured here before this existed ("Error in GetItems … does not
// implement the interface 'org.a11y.atspi.Cache'").
type accCache struct{ b *bridge }

// item builds the cache entry for the nth element.
func (c *accCache) item(i int) (cacheItem, bool) {
	e, ok := c.b.at(i)
	if !ok {
		return cacheItem{}, false
	}
	root := c.b.ref(accRootPath)
	return cacheItem{
		Ref: c.b.ref(childPath(i)), App: root, Parent: root,
		Index: int32(i), ChildCount: 0,
		Interfaces:  []string{ifaceAccessible, ifaceComponent, ifaceAction},
		Name:        e.Name,
		Role:        atspiRole(e.Role),
		Description: e.Value,
		State:       stateBits(),
	}, true
}

func (c *accCache) GetItems() ([]cacheItem, *dbus.Error) {
	elems := c.b.snapshot()
	root := c.b.ref(accRootPath)

	// The root's parent in the CACHE is the null reference, not what Embed
	// returned. A GTK application reports it that way and a client builds its
	// tree from these links, so naming the registry here puts the application
	// somewhere the client will not look for it.
	items := make([]cacheItem, 0, len(elems)+1)
	items = append(items, cacheItem{
		Ref: root, App: root, Parent: accRef{Name: "", Path: "/org/a11y/atspi/null"},
		Index: -1, ChildCount: int32(len(elems)),
		Interfaces:  []string{ifaceAccessible, ifaceApplication},
		Name:        "News Reader",
		Role:        roleApplication,
		Description: "news reader",
		State:       stateBits(),
	})
	for i := range elems {
		if it, ok := c.item(i); ok {
			items = append(items, it)
		}
	}
	return items, nil
}
