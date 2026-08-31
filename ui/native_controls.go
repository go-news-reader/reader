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

// resetNativeControls clears the per-frame accumulators. Draw calls it before it
// dispatches to a mode, so each frame's list is built fresh.
func (s *Scene) resetNativeControls() {
	s.nativeControls = s.nativeControls[:0]
	for k := range s.nativeHits {
		delete(s.nativeHits, k)
	}
	for k := range s.nativeSetFocus {
		delete(s.nativeSetFocus, k)
	}
}

// addNativeControl records one control the current frame wants natively backed.
func (s *Scene) addNativeControl(c toolkit.NativeControl) {
	s.nativeControls = append(s.nativeControls, c)
}

// addNativeButton records a drawn button's native counterpart: a push-button
// descriptor plus the Hit its activation should run. The reader dispatches clicks
// by geometry, so a native button has no action of its own here; the windowapp
// Handler wires the descriptor's OnActivate to run this Hit (see NativeHit).
func (s *Scene) addNativeButton(key, label string, rect toolkit.Rect, hit Hit) {
	if s.nativeHits == nil {
		s.nativeHits = map[string]Hit{}
	}
	s.nativeHits[key] = hit
	s.nativeControls = append(s.nativeControls, toolkit.NativeControl{
		Kind: toolkit.NativeButton, Key: key,
		Rect: rect, Visible: true, Text: label,
	})
}

// NativeHit returns the Hit a native button's activation should run, if the key
// names one. The windowapp Handler uses it to give each button descriptor an
// OnActivate that dispatches through the same path a click would.
func (s *Scene) NativeHit(key string) (Hit, bool) {
	h, ok := s.nativeHits[key]
	return h, ok
}

// addNativeSettingsField records a settings text field's native counterpart: a
// text-entry descriptor whose keystrokes flow into the field's buffer (OnText,
// keeping Scene focus in step), plus the record the windowapp Handler needs to
// wire Enter — the descriptor's OnActivate — to commitSettingsField for this
// field. It is the settings-view analogue of addNativeButton: the reader edits
// and commits by geometry/keyboard, so the native control routes back through the
// same Scene setter and the same commit a keypress would reach.
func (s *Scene) addNativeSettingsField(key string, focus Focus, rect toolkit.Rect, text string) {
	if s.nativeSetFocus == nil {
		s.nativeSetFocus = map[string]Focus{}
	}
	s.nativeSetFocus[key] = focus
	s.nativeControls = append(s.nativeControls, toolkit.NativeControl{
		Kind: toolkit.NativeEntry, Key: key,
		Rect: rect, Visible: true, Text: text,
		OnText: func(t string) { s.setSettingsField(focus, t) },
	})
}

// NativeSettingsCommit reports the settings field a native entry's Enter should
// commit, if the key names one. The windowapp Handler uses it to give the
// descriptor an OnActivate that focuses that field and runs commitSettingsField —
// the same path a keyboard Enter takes when the drawn field has focus.
func (s *Scene) NativeSettingsCommit(key string) (Focus, bool) {
	f, ok := s.nativeSetFocus[key]
	return f, ok
}

// setSettingsField focuses a settings text field and replaces its buffer with
// text — the native-entry counterpart of typing into it. The zoom-key fields hold
// a single printable rune, so only the last rune of text is kept, matching
// TypeRune's rule.
func (s *Scene) setSettingsField(f Focus, text string) {
	s.sf = f
	switch f {
	case FocusZoomIn:
		s.zoomInInput = lastRune(text)
	case FocusZoomOut:
		s.zoomOutInput = lastRune(text)
	default:
		if p := s.focusedField(); p != nil {
			*p = text
		}
	}
	s.touch()
}

// FocusSettingsField gives keyboard focus to a settings field by name. The
// windowapp Handler calls it before commitSettingsField so a native entry's Enter
// commits the field the person was editing even if no keystroke set focus first.
func (s *Scene) FocusSettingsField(f Focus) { s.sf = f; s.touch() }

// lastRune returns the last rune of s as a string, or "" if s is empty — the one
// printable rune a zoom-key field keeps.
func lastRune(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return ""
	}
	return string(r[len(r)-1])
}

// NativeControls returns the native controls accumulated over the last Draw, in
// visual order. It is what the windowapp Handler hands the platform through the
// application Surface's Controls field.
func (s *Scene) NativeControls() []toolkit.NativeControl { return s.nativeControls }
