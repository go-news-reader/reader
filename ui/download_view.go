package ui

// The download-manager panel: a docked zone across the bottom of the feed +
// preview area listing the binary posts being downloaded (a post is queued by
// ticking its checkbox on a complete group card). It is composed with the
// toolkit's box layout (a VBox of per-download HBox rows) rather than
// hand-computed rects — each row is name | progress bar | status.

import (
	"fmt"
	"image"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
)

// DownloadStatus is the lifecycle state of a queued download.
type DownloadStatus int

const (
	DLQueued DownloadStatus = iota // waiting for a worker
	DLActive                       // downloading (Done/Total parts)
	DLDone                         // reconstructed + saved
	DLFailed                       // errored
)

// DownloadItem is one row of the download panel, keyed by the post's release
// base (the same id the group card's checkbox toggles).
type DownloadItem struct {
	ID     string
	Name   string
	Status DownloadStatus
	Done   int
	Total  int
}

// Download-panel sizing, in logical (unscaled) pixels.
const (
	dlHeaderH = 26 // header row (title + Clear)
	dlRowH    = 26 // one download row
	dlMaxRows = 4  // rows shown before the list stops growing the panel
)

// SetDownloads replaces the download list (the app pushes the full snapshot).
func (s *Scene) SetDownloads(items []DownloadItem) { s.downloads = items; s.touch() }

// Downloads returns the current download list.
func (s *Scene) Downloads() []DownloadItem { return s.downloads }

// IsDownloadQueued reports whether id has an in-flight (queued or active)
// download — the checked state of its group card checkbox.
func (s *Scene) IsDownloadQueued(id string) bool {
	for _, d := range s.downloads {
		if d.ID == id && (d.Status == DLQueued || d.Status == DLActive) {
			return true
		}
	}
	return false
}

// downloadPanelH is the panel's device-pixel height, or 0 when hidden (not the
// feed view, or nothing to show).
func (s *Scene) downloadPanelH() int {
	if s.mode != ModeFeed || len(s.downloads) == 0 {
		return 0
	}
	rows := len(s.downloads)
	if rows > dlMaxRows {
		rows = dlMaxRows
	}
	return rpxOf(s, dlHeaderH) + rows*rpxOf(s, dlRowH)
}

// feedBottom is the bottom edge of the feed/preview content: the window height
// minus the download panel and the status bar (both docked below the feed).
// Every feed/preview layout, draw and scroll path uses it so they leave room.
func (s *Scene) feedBottom() int { return s.H - s.statusBarH() - s.downloadPanelH() }

// downloadPanelRect is the panel's rect (feed+preview width, docked bottom), or
// empty when hidden. It spans from the sidebar's right edge so the sidebar keeps
// its full height.
func (s *Scene) downloadPanelRect() toolkit.Rect {
	h := s.downloadPanelH()
	if h == 0 {
		return toolkit.Rect{}
	}
	return toolkit.Rect{X: s.m.sidebarW, Y: s.feedBottom(), W: s.W - s.m.sidebarW, H: h}
}

// layoutDownloadPanel records the "Clear" button rect for hit-testing.
func (s *Scene) layoutDownloadPanel() {
	r := s.downloadPanelRect()
	if r.W == 0 {
		s.dlClearR = toolkit.Rect{}
		return
	}
	m := s.m
	cw := m.side.width("Clear") + 2*m.pad
	s.dlClearR = toolkit.Rect{X: r.X + r.W - m.pad - cw, Y: r.Y + (rpxOf(s, dlHeaderH)-m.badgeH)/2, W: cw, H: m.badgeH}
}

// drawDownloadPanel paints the panel entirely from composed go-widgets — nothing
// is hand-drawn. The panel surface + its 1px top divider are toolkit.Backdrop
// fills; the header title is a toolkit.Label in the reader's TrueType face; the
// "Clear" affordance is a stock toolkit.Button pill (its centred label lands at
// the same x the old +pad inset produced, since dlClearR is the text width plus
// 2·pad); and the download rows are a toolkit VBox of HBox rows. The img param is
// kept for signature parity with the other docked-draw methods but is unused now
// that no glyph is blitted directly.
func (s *Scene) drawDownloadPanel(p *painter.PixelPainter, img *image.RGBA) {
	_ = img
	r := s.downloadPanelRect()
	if r.W == 0 {
		return
	}
	m := s.m
	th := s.theme
	s.layoutDownloadPanel()

	// Panel surface + top divider: solid toolkit.Backdrop fills, not bare painter
	// rects, so the panel ground is widget-composed.
	surf := &toolkit.Backdrop{Fill: th.SurfaceAlt}
	surf.SetBounds(r)
	surf.Draw(p, th)
	div := &toolkit.Backdrop{Fill: th.Border}
	div.SetBounds(toolkit.Rect{X: r.X, Y: r.Y, W: r.W, H: 1})
	div.Draw(p, th)

	headerH := rpxOf(s, dlHeaderH)
	// Header title: a top-anchored toolkit.Label (same origin/ink/face as the old
	// glyph-blit) instead of a hand-drawn text run.
	title := toolkit.NewLabel(fmt.Sprintf("Downloads (%d)", len(s.downloads)))
	title.Font, title.Ink, title.VAlign = m.side.font, th.OnSurface, toolkit.VTop
	title.SetBounds(toolkit.Rect{X: r.X + m.pad, Y: r.Y + (headerH-m.side.height)/2, W: s.dlClearR.X - (r.X + m.pad), H: m.side.height})
	title.Draw(p, th)

	// Clear: a stock toolkit.Button at dlClearR (hit-tested unchanged via
	// HitClearDownloads), replacing the FillRoundRect + StrokeRoundRect + glyph-blit.
	clear := toolkit.NewButton("Clear", nil)
	clear.Font = m.side.font
	clear.SetBounds(s.dlClearR)
	clear.Draw(p, th)

	// Rows: one HBox per download inside a VBox, laid out by the toolkit.
	rowsN := len(s.downloads)
	if rowsN > dlMaxRows {
		rowsN = dlMaxRows
	}
	box := toolkit.NewVBox()
	box.Spacing = -1 // no inter-row gap
	for i := 0; i < rowsN; i++ {
		box.AddFixed(s.downloadRow(s.downloads[i]), rpxOf(s, dlRowH))
	}
	box.SetBounds(toolkit.Rect{X: r.X + m.pad, Y: r.Y + headerH, W: r.W - 2*m.pad, H: rowsN * rpxOf(s, dlRowH)})
	box.Draw(p, th)
}

// downloadRow builds one download row as an HBox: name (flex) | progress bar
// (flex) | status (fixed), all in the reader's TrueType face.
func (s *Scene) downloadRow(d DownloadItem) toolkit.Widget {
	row := toolkit.NewHBox()
	row.Spacing = rpxOf(s, 8)

	name := toolkit.NewLabel(d.Name)
	name.Font = ttFont(false, rpxOf(s, 12))

	pb := toolkit.NewProgressBar()
	pb.Font = ttFont(false, rpxOf(s, 10))
	if d.Total > 0 {
		pb.SetFraction(float64(d.Done) / float64(d.Total))
		pb.Label = fmt.Sprintf("%d/%d", d.Done, d.Total)
	}

	status := toolkit.NewLabel(downloadStatusText(d))
	status.Align = toolkit.AlignRight
	status.Font = ttFont(false, rpxOf(s, 11))

	row.AddFlex(name, 2)
	row.AddFlex(pb, 3)
	row.AddFixed(status, rpxOf(s, 72))
	return row
}

// downloadStatusText labels a download's state.
func downloadStatusText(d DownloadItem) string {
	switch d.Status {
	case DLQueued:
		return "queued"
	case DLActive:
		return "downloading"
	case DLDone:
		return "saved"
	case DLFailed:
		return "failed"
	}
	return ""
}
