package ui

// The sidebar has one unified width model covering three states: the default
// width, collapsed (burger → width 0, so the feed uses the full surface), and a
// user-dragged width (the divider at the sidebar's right edge). Collapsing wins
// over a dragged width. All widths are logical px scaled through rpxOf; the
// user-dragged value is stored in device px (it comes straight from a pointer
// coordinate) and clamped at read time.

// Sidebar width bounds, in logical (unscaled) pixels.
const (
	defaultSidebarW = 200
	minSidebarW     = 140
	maxSidebarW     = 480
)

// SidebarCollapsed reports whether the sidebar is currently collapsed.
func (s *Scene) SidebarCollapsed() bool { return s.sidebarCollapsed }

// ToggleSidebar collapses or expands the sidebar (transient UI chrome, not
// persisted).
func (s *Scene) ToggleSidebar() {
	s.sidebarCollapsed = !s.sidebarCollapsed
	s.invalidateCards()
	s.touch()
}

// BeginSidebarResize starts a divider drag.
func (s *Scene) BeginSidebarResize() { s.draggingSidebar = true }

// EndSidebarResize ends a divider drag.
func (s *Scene) EndSidebarResize() { s.draggingSidebar = false }

// DraggingSidebar reports whether the divider is being dragged.
func (s *Scene) DraggingSidebar() bool { return s.draggingSidebar }

// MouseMove applies a divider drag: while dragging, the pointer's x becomes the
// new sidebar width (clamped when read). It is a no-op otherwise, so front-ends
// can forward every pointer move unconditionally.
func (s *Scene) MouseMove(x, y int) {
	if !s.draggingSidebar {
		return
	}
	s.sidebarUserW = x
	s.invalidateCards()
	s.touch()
}

// sidebarWidthPx returns the effective sidebar width in device pixels for the
// current state: 0 when collapsed, else the clamped user width or the default.
func (s *Scene) sidebarWidthPx() int {
	if s.sidebarCollapsed {
		return 0
	}
	def := rpxOf(s, defaultSidebarW)
	w := def
	if s.sidebarUserW > 0 {
		w = s.sidebarUserW
	}
	lo := rpxOf(s, minSidebarW)
	hi := rpxOf(s, maxSidebarW)
	if half := s.W / 2; half < hi {
		hi = half
	}
	if hi < lo {
		hi = lo // very narrow window: keep a sensible minimum
	}
	if w < lo {
		w = lo
	}
	if w > hi {
		w = hi
	}
	return w
}
