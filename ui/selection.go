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

// drawSelectionHighlight paints the current selection's highlight into p. It is
// drawn BEHIND the text (the surface calls it just before drawing its Labels),
// so an opaque tint reads as a clean highlight without hiding the glyphs.
func (s *Scene) drawSelectionHighlight(p painter.Painter) {
	s.textSel.Draw(p, s.selectionFill())
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
	case s.mode == ModeFeed && s.previewHas && !s.webPreviewItem():
		return inRect(s.previewR, x, y)
	default:
		return false
	}
}
