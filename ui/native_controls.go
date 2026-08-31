package ui

import (
	"fmt"

	"github.com/go-news-reader/reader/source"
	"github.com/go-widgets/toolkit"
)

// nativeAccKey is the stable identity of a credential field's native control,
// across frames and providers: the host keeps one live control per key, so a
// provider's password field and another's must not collide.
func nativeAccKey(k source.Kind, key string) string {
	return fmt.Sprintf("acc:%v:%s", k, key)
}

// boolField renders a boolean the way the credential store keeps it.
func boolField(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// This file is the reader's producer for the native-control seam. The reader is
// a self-rendering toolkit.Surface with no walkable widget tree, so it publishes
// the controls it wants backed by real OS widgets the same way it publishes its
// accessibility tree: as a flat list a host reads each frame. See
// go-widgets/toolkit.NativeControl and the application Surface's Controls field.
//
// A draw path emits one descriptor per interactive control it lays out — the
// drawn widget stays as the fallback on a platform whose host cannot embed
// controls — with callbacks routed to the same Scene setters a click or keypress
// would reach. The host holds the real control per Key across frames and pushes a
// value only when it changed on the app's side, so the person's own edit is never
// disturbed.

// resetNativeControls clears the per-frame accumulator. Draw calls it before it
// dispatches to a mode, so each frame's list is built fresh.
func (s *Scene) resetNativeControls() { s.nativeControls = s.nativeControls[:0] }

// addNativeControl records one control the current frame wants natively backed.
func (s *Scene) addNativeControl(c toolkit.NativeControl) {
	s.nativeControls = append(s.nativeControls, c)
}

// NativeControls returns the native controls accumulated over the last Draw, in
// visual order. It is what the windowapp Handler hands the platform through the
// application Surface's Controls field.
func (s *Scene) NativeControls() []toolkit.NativeControl { return s.nativeControls }
