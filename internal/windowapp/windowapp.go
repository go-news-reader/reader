// Package windowapp adapts an *app.App to the window.Handler contract: it
// exposes the app's double-buffered framebuffer to a native presenter and
// routes native mouse/scroll/key events into the ui.Scene. It is deliberately
// decoupled from the window package (it satisfies the interface structurally),
// so both build on every platform and the routing logic is unit-testable
// without any Cocoa dependency.
package windowapp

import (
	"os/exec"
	"runtime"

	"github.com/go-news-reader/reader/app"
	"github.com/go-news-reader/reader/source"
	"github.com/go-news-reader/reader/ui"
)

// execStart runs a command detached; a seam so tests avoid spawning processes.
var execStart = func(name string, args ...string) error {
	return exec.Command(name, args...).Start()
}

// openURL opens a URL in the user's default browser. A package var so callers
// (and tests) can substitute it.
var openURL = defaultOpenURL

// browserCommand returns the opener command and arguments for a given GOOS.
func browserCommand(goos, url string) (string, []string) {
	switch goos {
	case "darwin":
		return "open", []string{url}
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", url}
	default:
		return "xdg-open", []string{url}
	}
}

// defaultOpenURL launches the platform browser opener for url.
func defaultOpenURL(url string) error {
	name, args := browserCommand(runtime.GOOS, url)
	return execStart(name, args...)
}

// Handler adapts an app to the presenter's data-source/input-sink interface.
type Handler struct{ a *app.App }

// New wraps a to be driven by a native window.
func New(a *app.App) *Handler { return &Handler{a: a} }

// Frame returns the current framebuffer, its device dimensions, and whether it
// changed since the last call.
func (h *Handler) Frame() ([]byte, int, int, bool) {
	buf, changed := h.a.Frame()
	s := h.a.Scene()
	return buf, s.W, s.H, changed
}

// Resize maps a logical size to device pixels via the backing scale.
func (h *Handler) Resize(w, height int, scale float64) {
	s := h.a.Scene()
	s.SetScale(scale)
	s.Resize(w, height)
}

// MouseDown routes a click across the feed, detail and settings views: open an
// item, follow a link, switch a filter or profile, or drive the settings editor.
func (h *Handler) MouseDown(x, y int) {
	s := h.a.Scene()
	switch hit := s.HitTest(x, y); hit.Kind {
	case ui.HitItem:
		s.OpenDetail(hit.Item) // read it in-app, not in a browser
	case ui.HitBack:
		s.CloseDetail()
	case ui.HitOpenExternal:
		// HitOpenExternal only fires when the item has a URL, so one is present.
		url := hit.Item.Link
		if url == "" {
			url = hit.Item.Permalink
		}
		_ = openURL(url)
	case ui.HitSub:
		s.SetActive(hit.Sub)
		s.FocusSearch(false)
	case ui.HitSearch:
		s.FocusSearch(true)
	case ui.HitProfile:
		s.SetActiveProfile(hit.Profile)
		h.a.ApplySceneSettings() // persist + re-aggregate the new profile
	case ui.HitSettings:
		s.OpenSettings()
	case ui.HitCloseSettings:
		s.CommitRename()
		s.CommitCache()
		s.CloseSettings()
		h.a.ApplySceneSettings()
	case ui.HitSelectProfile:
		s.SelectEditProfile(hit.Profile)
	case ui.HitNewProfile:
		s.NewProfile()
	case ui.HitDeleteProfile:
		s.DeleteProfile(hit.Profile)
		h.a.ApplySceneSettings()
	case ui.HitRenameProfile:
		s.FocusRename()
	case ui.HitSelectKind:
		s.SelectKind(source.Kind(hit.Value))
	case ui.HitAddSub:
		s.AddInputSub()
		h.a.ApplySceneSettings()
	case ui.HitRemoveSub:
		s.RemoveSub(hit.Profile, hit.Sub)
		h.a.ApplySceneSettings()
	case ui.HitFocusChannel:
		s.FocusChannel()
	case ui.HitFocusCache:
		s.FocusCache()
	case ui.HitTheme:
		s.SetThemeName(hit.Value)
		h.a.ApplySceneSettings()
	default:
		s.FocusSearch(false)
	}
}

// Scroll scrolls the feed by a device-pixel wheel delta.
func (h *Handler) Scroll(dy int) { h.a.Scene().Scroll(dy) }

// Key handles editing keys and printable runes for whichever view/field is
// focused (topbar search in the feed, or the settings text fields).
func (h *Handler) Key(name string, r rune) {
	s := h.a.Scene()
	switch name {
	case "Backspace":
		s.Backspace()
	case "Escape":
		switch s.Mode() {
		case ui.ModeDetail:
			s.CloseDetail() // Esc returns from the reading view to the feed
		case ui.ModeSettings:
			s.CommitRename()
			s.CommitCache()
			s.CloseSettings()
			h.a.ApplySceneSettings()
		default:
			s.FocusSearch(false)
		}
	case "Enter":
		if s.Mode() == ui.ModeSettings {
			h.commitSettingsField()
			return
		}
		s.FocusSearch(false)
	default:
		if r != 0 {
			s.TypeRune(r)
		}
	}
}

// commitSettingsField applies the settings text field that currently has focus
// (Enter), persisting the change and re-aggregating when it affects the feed.
func (h *Handler) commitSettingsField() {
	s := h.a.Scene()
	switch s.Focus() {
	case ui.FocusChannel:
		s.AddInputSub()
		h.a.ApplySceneSettings()
	case ui.FocusRename:
		s.CommitRename()
		h.a.ApplySceneSettings()
	case ui.FocusCache:
		s.CommitCache()
		h.a.ApplySceneSettings()
	}
}
