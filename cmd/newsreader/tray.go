package main

// statusItem is the reader's menu-bar presence — a "tray icon" — put up as the
// window opens. The AppKit call that creates it lives in tray_darwin.go (a no-op
// stub in tray_other.go elsewhere), so this file, and the reader, stay portable:
// off macOS there is simply no menu bar to fill.
type statusItem struct{ closeFn func() }

// Close removes the item from the menu bar. Safe on a nil receiver and an empty
// item, so emitWindow can defer it before onReady has assigned one.
func (s *statusItem) Close() {
	if s == nil || s.closeFn == nil {
		return
	}
	s.closeFn()
}

// newStatusItem is a seam over the per-platform [makeStatusItem] so tests
// substitute the AppKit call, the way [openWindow] and windowapp's openURLIn are
// seamed at the same native boundary.
var newStatusItem = makeStatusItem
