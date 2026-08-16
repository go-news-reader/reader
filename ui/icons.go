package ui

// Nav/chrome icons rendered from the real Iconoir set (github.com/go-iconoir/iconoir)
// through its painter adapter, instead of hand-drawn painter primitives.
//
// The embedded Go fonts (goregular/gobold) carry none of the UI symbol glyphs
// (☰ ⚙ 👤 📡 🔒 …), so drawing them as text produced empty "tofu" boxes. Rather
// than approximate the Iconoir aesthetic with bars and rounded rects, we now
// blit the actual Iconoir 24-grid outline masks: iconoir.DrawIcon rasterizes the
// mask at the target rect's size and composites the anti-aliased coverage via the
// painter, so the icons are crisp and scale with the scene.
//
// Each helper fills the supplied box r; the caller centres r where the glyph
// should sit. The stroke-width argument (lineW) is retained so the drawNavRow /
// Banner.Icon callback signatures stay unchanged — the Iconoir masks carry their
// own outline weight, so it is not otherwise needed.

import (
	"github.com/go-iconoir/iconoir"
	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
)

// iconStroke returns the Iconoir ~1.5px outline stroke width for the current
// scene scale (never below one device pixel). Kept for callers that thread a
// stroke width through the icon-callback signature.
func (s *Scene) iconStroke() int { return max(1, int(1.5*s.Scale+0.5)) }

// iconInset centres a square glyph box of side frac/100 of min(r.W, r.H) inside
// r, so every icon shares one visual weight and alignment within its cell.
func iconInset(r toolkit.Rect, frac int) toolkit.Rect {
	d := min(r.W, r.H) * frac / 100
	return toolkit.Rect{X: r.X + (r.W-d)/2, Y: r.Y + (r.H-d)/2, W: d, H: d}
}

// Cached Iconoir icon lookups. Get is cheap, but caching avoids a registry
// lookup per frame. Names verified present in iconoir v0.1.0.
var (
	iconMenu       = iconoir.MustGet("menu")            // burger / open-sidebar
	iconLock       = iconoir.MustGet("lock")            // auth-banner padlock
	iconUser       = iconoir.MustGet("user")            // sidebar Accounts
	iconList       = iconoir.MustGet("list")            // sidebar Network log
	iconSettings   = iconoir.MustGet("settings")        // sidebar Settings (gear)
	iconSearch     = iconoir.MustGet("search")          // topbar SearchEntry magnifier
	iconPlus       = iconoir.MustGet("plus")            // sidebar "Browse newsgroups" + subscribe
	iconRefresh    = iconoir.MustGet("refresh-double")  // browse view Refresh control + web preview Reload
	iconCheck      = iconoir.MustGet("check")           // subscribed / complete marker
	iconNavLeft    = iconoir.MustGet("nav-arrow-left")  // web preview Back
	iconNavRight   = iconoir.MustGet("nav-arrow-right") // web preview Forward
	iconZoomIn     = iconoir.MustGet("zoom-in")         // web preview zoom in
	iconZoomOut    = iconoir.MustGet("zoom-out")        // web preview zoom out
	iconFit        = iconoir.MustGet("expand")          // web preview best-fit zoom
	iconLockSlash  = iconoir.MustGet("lock-slash")      // address bar: insecure (non-https)
	iconStar       = iconoir.MustGet("star")            // address bar: bookmark toggle (star)
	iconBin        = iconoir.MustGet("bin")             // sidebar context menu: unsubscribe (delete)
	iconFolder     = iconoir.MustGet("folder")          // sidebar virtual folder
	iconFolderPlus = iconoir.MustGet("folder-plus")     // sidebar context menu: new folder
)

// MenuIconNewFolder and MenuIconFolder paint the sidebar folder context-menu
// glyphs into the cell the toolkit Menu hands them, in the row's own ink (so
// they invert on hover). They match toolkit.MenuItem.Icon so the Handler can
// hand them straight to the menu without this package's private icon handles
// leaking out.
func MenuIconNewFolder(p painter.Painter, cell toolkit.Rect, ink toolkit.RGBA) {
	drawIcon(p, cell, iconFolderPlus, ink)
}

func MenuIconFolder(p painter.Painter, cell toolkit.Rect, ink toolkit.RGBA) {
	drawIcon(p, cell, iconFolder, ink)
}

// drawFolderIcon paints the Iconoir "folder" glyph (a sidebar virtual folder).
func drawFolderIcon(p painter.Painter, r toolkit.Rect, col toolkit.RGBA) {
	drawIcon(p, r, iconFolder, col)
}

// bookmarkGold is the fill for a saved (bookmarked) star — a warm gold that
// stays clearly visible on the light address field, unlike the washed-out
// accent that made the toggled state hard to see.
var bookmarkGold = toolkit.RGBA{R: 0xF2, G: 0xA6, B: 0x1C, A: 0xFF}

// drawIcon blits a cached Iconoir mask into r, inset slightly so the glyph keeps
// a little breathing room within its cell (matching the previous ~80% weight).
func drawIcon(p painter.Painter, r toolkit.Rect, ic *iconoir.Icon, col toolkit.RGBA) {
	iconoir.DrawIcon(p, iconInset(r, 88), ic, col)
}

// MenuIconRefresh, MenuIconMarkRead and MenuIconUnsubscribe paint the sidebar
// context menu's row glyphs into the cell the toolkit Menu hands them, in the
// row's own ink (so they invert on hover). They match toolkit.MenuItem.Icon, so
// the Handler can hand them straight to the menu without this package's private
// icon handles leaking out.
func MenuIconRefresh(p painter.Painter, cell toolkit.Rect, ink toolkit.RGBA) {
	drawIcon(p, cell, iconRefresh, ink)
}

func MenuIconMarkRead(p painter.Painter, cell toolkit.Rect, ink toolkit.RGBA) {
	drawIcon(p, cell, iconCheck, ink)
}

func MenuIconUnsubscribe(p painter.Painter, cell toolkit.Rect, ink toolkit.RGBA) {
	drawIcon(p, cell, iconBin, ink)
}

// drawMenuIcon paints the Iconoir "menu" (burger) glyph: three horizontal bars.
func drawMenuIcon(p painter.Painter, r toolkit.Rect, col toolkit.RGBA, lineW int) {
	drawIcon(p, r, iconMenu, col)
}

// drawLockIcon paints the Iconoir "lock" (padlock) glyph.
func drawLockIcon(p painter.Painter, r toolkit.Rect, col toolkit.RGBA, lineW int) {
	drawIcon(p, r, iconLock, col)
}

// drawUserIcon paints the Iconoir "user" (account/person) glyph.
func drawUserIcon(p painter.Painter, r toolkit.Rect, col toolkit.RGBA, lineW int) {
	drawIcon(p, r, iconUser, col)
}

// drawSlidersIcon paints the Iconoir "settings" (gear) glyph.
func drawSlidersIcon(p painter.Painter, r toolkit.Rect, col toolkit.RGBA, lineW int) {
	drawIcon(p, r, iconSettings, col)
}

// drawListIcon paints the Iconoir "list" (network-log) glyph.
func drawListIcon(p painter.Painter, r toolkit.Rect, col toolkit.RGBA, lineW int) {
	drawIcon(p, r, iconList, col)
}

// drawSearchIcon paints the Iconoir "search" (magnifier) glyph. Wired into the
// topbar toolkit.SearchEntry's Icon slot so its left prefix is a real magnifier
// rather than the "?" bitmap-font stand-in.
func drawSearchIcon(p painter.Painter, r toolkit.Rect, ink toolkit.RGBA) {
	drawIcon(p, r, iconSearch, ink)
}

// chromeGlyphBox centres a burger-sized (m.navIcon) square inside r so the
// web-preview browser's toolbar / address icons render at the SAME visual size
// as the topbar burger, instead of filling the (larger) toolbar-button square.
// The size is clamped to r so a small address-bar slot never overflows.
func (s *Scene) chromeGlyphBox(r toolkit.Rect) toolkit.Rect {
	d := s.m.navIcon
	if fit := min(r.W, r.H); d > fit {
		d = fit
	}
	return toolkit.Rect{X: r.X + (r.W-d)/2, Y: r.Y + (r.H-d)/2, W: d, H: d}
}

// chromeIcon returns a toolkit.Browser icon hook that blits ic at burger size
// (chromeGlyphBox) within the button rect. Wiring the toolbar buttons through it
// keeps every preview-chrome glyph aligned to the topbar burger.
func (s *Scene) chromeIcon(ic *iconoir.Icon) func(p painter.Painter, r toolkit.Rect, ink toolkit.RGBA) {
	return func(p painter.Painter, r toolkit.Rect, ink toolkit.RGBA) {
		drawIcon(p, s.chromeGlyphBox(r), ic, ink)
	}
}

// drawPlusIcon paints the Iconoir "plus" glyph (sidebar Browse entry, subscribe).
func drawPlusIcon(p painter.Painter, r toolkit.Rect, col toolkit.RGBA, lineW int) {
	drawIcon(p, r, iconPlus, col)
}

// drawRefreshIcon paints the Iconoir "refresh-double" glyph (browse Refresh).
func drawRefreshIcon(p painter.Painter, r toolkit.Rect, col toolkit.RGBA, lineW int) {
	drawIcon(p, r, iconRefresh, col)
}

// drawCheckIcon paints the Iconoir "check" glyph (a subscribed group marker).
func drawCheckIcon(p painter.Painter, r toolkit.Rect, col toolkit.RGBA, lineW int) {
	drawIcon(p, r, iconCheck, col)
}
