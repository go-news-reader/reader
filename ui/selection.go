package ui

import (
	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
)

// This file drives the toolkit's cross-widget text selection (toolkit.
// TextSelection) from the reader's pointer input, so the user can drag-select
// the text in a reading surface and copy it (Cmd/Ctrl+C — see Copy). The runs
// the selection spans are (re)collected each frame by the surface's draw code
// (setSelectableRuns), which keeps them in lock-step with scrolling / resizing.

// setSelectableRuns replaces the selectable runs with those a reading surface
// just laid out (its VBox of Labels, via toolkit.CollectRuns). The draw code
// calls it before painting the highlight + text, so the selection always
// reflects what is currently on screen.
func (s *Scene) setSelectableRuns(runs []toolkit.TextRun) { s.textSel.SetRuns(runs) }

// beginSelectableFrame resets the per-frame selectable-run accumulator. The feed
// draw builds its selection from several surfaces (the sidebar labels, every
// visible card, the preview text); each appends its on-screen runs via
// addSelectableRuns, and commitSelectableRuns hands the flat set to the
// selection. Because the runs are all in screen space, the selection then spans
// the surfaces in document order (top→bottom, left→right).
func (s *Scene) beginSelectableFrame() { s.selAccum = s.selAccum[:0] }

// addSelectableRuns appends runs to the accumulator, translated by (dx, dy) into
// screen space. Callers pass surface-local runs (a card's local sprite runs, the
// sidebar's sprite-local runs) with the surface's on-screen offset; already-
// screen-space runs (the preview text) pass dx=dy=0.
func (s *Scene) addSelectableRuns(runs []toolkit.TextRun, dx, dy int) {
	for _, r := range runs {
		r.Bounds.X += dx
		r.Bounds.Y += dy
		s.selAccum = append(s.selAccum, r)
	}
}

// commitSelectableRuns replaces the selection's run set with everything the frame
// accumulated (the toolkit sorts + de-dupes empties), keeping an active drag's
// endpoints clamped to the new runs.
func (s *Scene) commitSelectableRuns() { s.textSel.SetRuns(s.selAccum) }

// drawSelectionHighlight paints the current selection's highlight into p. It is
// drawn BEHIND the text (the surface calls it just before drawing its Labels),
// so an opaque tint reads as a clean highlight without hiding the glyphs. The
// detail view uses this; the feed uses drawSelectionOverHighlight instead.
func (s *Scene) drawSelectionHighlight(p painter.Painter) {
	s.textSel.Draw(p, s.selectionFill())
}

// drawSelectionOverHighlight paints the selection OVER already-painted content
// (the feed's cached card/sidebar sprites and the preview text). Since the
// glyphs are already on screen, the fill is a translucent accent (A≈0x55) that
// the painter blends src-over, so the highlighted text stays readable — the same
// trick the search field's copy feedback uses. It reads clearly in both light
// and dark themes because the accent is a saturated colour laid at low alpha.
func (s *Scene) drawSelectionOverHighlight(p painter.Painter) {
	s.textSel.Draw(p, s.selectionOverFill())
}

// selectionOverFill is the translucent highlight for the over-paint path: the
// theme accent at a low alpha so the painter blends it over the glyphs.
func (s *Scene) selectionOverFill() toolkit.RGBA {
	c := s.theme.Accent
	c.A = 0x55
	return c
}

// selectionFill is the highlight colour: the theme accent softened toward the
// background so dark text stays readable on top of it, in both light and dark
// themes.
func (s *Scene) selectionFill() toolkit.RGBA {
	return mixRGBA(s.theme.Accent, s.theme.Background, 0.62)
}

// mixRGBA blends a toward b by t in [0,1] (t=0 → a, t=1 → b), opaque.
func mixRGBA(a, b toolkit.RGBA, t float64) toolkit.RGBA {
	lerp := func(x, y uint8) uint8 { return uint8(float64(x) + (float64(y)-float64(x))*t + 0.5) }
	return toolkit.RGBA{R: lerp(a.R, b.R), G: lerp(a.G, b.G), B: lerp(a.B, b.B), A: 0xFF}
}

// SelectionBegin starts a text selection at screen point (x, y) over the runs of
// the reading surface drawn last frame. A press with no drag leaves the
// selection empty (so a plain click clears any prior selection).
func (s *Scene) SelectionBegin(x, y int) {
	s.textSel.Begin(x, y)
	s.selecting = true
	s.touch()
}

// SelectionEnd finishes a drag; the selection stays in place for a copy.
func (s *Scene) SelectionEnd() {
	if !s.selecting {
		return
	}
	s.selecting = false
	s.textSel.End()
	s.touch()
}

// ClearSelection discards the current selection.
func (s *Scene) ClearSelection() {
	s.textSel.Clear()
	s.selecting = false
	s.touch()
}

// HasSelection reports whether a non-empty text selection is active.
func (s *Scene) HasSelection() bool { return !s.textSel.IsEmpty() }

// SelectableAt reports whether a press at screen point (x, y) should begin a
// text selection: the reading surfaces whose runs are collected each frame —
// the full-screen detail view, or the preview pane's text summary (not the
// embedded web browser, which handles its own selection). A front-end consults
// it before starting a drag-selection.
func (s *Scene) SelectableAt(x, y int) bool {
	switch {
	case s.mode == ModeDetail:
		return true
	case s.mode == ModeFeed:
		// The preview pane's text summary (never the embedded web browser).
		if s.previewHas && !s.webPreviewItem() && inRect(s.previewR, x, y) {
			return true
		}
		// The feed list of cards: a press here that hit-tested to no interactive
		// target still begins a selection, so a drag over a card title selects it.
		// (The sidebar's middle list is a TreeView whose rows resolve to actions,
		// not selectable text, so it no longer participates in text selection.)
		return inRect(s.feedListRegion(), x, y)
	default:
		return false
	}
}

// feedListRegion is the feed's card viewport in screen coords: right of the
// sidebar, below the topbar, left of the preview pane (when open), above the
// download panel / status bar. A press inside it may begin a card selection.
func (s *Scene) feedListRegion() toolkit.Rect {
	right := s.W
	if s.previewR.W > 0 {
		right = s.previewR.X
	}
	// A width ≤ 0 (a pathologically narrow window where the sidebar meets the
	// pane) yields a rect inRect treats as empty, so no clamp is needed.
	return toolkit.Rect{X: s.m.sidebarW, Y: s.m.topbarH, W: right - s.m.sidebarW, H: s.feedBottom() - s.m.topbarH}
}
