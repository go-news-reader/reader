package ui

import (
	"fmt"
	"image"
	"strings"
	"time"

	"github.com/go-widgets/mvvm"
	"github.com/go-widgets/mvvm/tkbind"
	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
)

// pixImg recovers the concrete pixel buffer (and an *image.RGBA over it) from the
// painter a widget's Draw is handed. It lets reader widgets be RETAINED — built
// once and redrawn into whatever buffer the current frame supplies — instead of
// captured at construction and rebuilt every frame. ok is false for a non-pixel
// painter, in which case the widget draws nothing. This is the seam the whole
// toolkit-migration relies on (getFace text needs the raw *image.RGBA).
func pixImg(pt painter.Painter) (*painter.PixelPainter, *image.RGBA, bool) {
	p, ok := pt.(*painter.PixelPainter)
	if !ok {
		return nil, nil, false
	}
	img := &image.RGBA{Pix: p.Buf, Stride: p.Width * 4, Rect: image.Rect(0, 0, p.Width, p.Height)}
	return p, img, true
}

// The in-canvas Network-log view (ModeLog). It shows, newest-first, the HTTP
// exchanges every provider made — method, host+path, status (colour-coded) and
// duration — so a user can diagnose failures (e.g. a Reddit 403 from a
// datacenter IP) without leaving the app. It is drawn with the same painter +
// anti-aliased text as the rest of the UI and fed live from an injected source,
// so opening it always reflects the latest traffic.

// LogEntry is one HTTP exchange as the log view needs it. It mirrors
// internal/httplog.Entry so the ui package stays free of that dependency; the
// app converts between the two.
type LogEntry struct {
	When   time.Time
	Method string
	URL    string
	Status int
	Bytes  int64
	Dur    time.Duration
	Err    string
}

// SetLogSource installs the callback the log view queries each frame for the
// current exchanges (newest-first). Nil leaves the view empty.
func (s *Scene) SetLogSource(fn func() []LogEntry) { s.logSource = fn; s.touch() }

// SetRenderCacheStats installs the callback the Settings view queries for the
// web-preview render cache's current size (pages, bytes). Nil shows an empty
// cache in the caption.
func (s *Scene) SetRenderCacheStats(fn func() (pages, bytes int)) { s.renderCacheStats = fn }

// logEntries returns the current log entries (nil when no source is wired).
func (s *Scene) logEntries() []LogEntry {
	if s.logSource == nil {
		return nil
	}
	return s.logSource()
}

// LogEntries exposes the current log entries (for front-ends/tests).
func (s *Scene) LogEntries() []LogEntry { return s.logEntries() }

// OpenLog enters the Network-log view.
func (s *Scene) OpenLog() {
	s.mode = ModeLog
	s.logScroll.offset = 0
	s.touch()
}

// CloseLog returns from the Network-log view to the feed.
func (s *Scene) CloseLog() {
	s.mode = ModeFeed
	s.touch()
}

// layoutLog computes the back-button rect, the per-row height and the total
// content height (for scroll clamping).
func (s *Scene) layoutLog() {
	s.clampSize()
	s.m = s.computeMetrics()
	m := s.m
	y := (m.topbarH - m.searchH) / 2
	s.logBackR = toolkit.Rect{X: m.pad, Y: y, W: m.pad*2 + m.side.width("< Back"), H: m.searchH}
	s.logRowH = rpxOf(s, 44)
	s.syncLogList()
	s.logScroll.refresh(len(s.logEntries())*s.logRowH, s.H-m.topbarH)
}

// ensureLogBinding lazily wires the ObservableList → Container binding the first
// time the log view lays out: BindContainer maps each LogEntry to a retained
// logRow item in a vertical box, rebuilding only when the list actually changes.
func (s *Scene) ensureLogBinding() {
	if s.logContainer != nil {
		return
	}
	s.logList = mvvm.NewObservableList[LogEntry]()
	s.logContainer = toolkit.NewContainer(&toolkit.BoxLayout{Vertical: true, Spacing: -1})
	tkbind.BindContainer(s.logList, s.logContainer, func(e LogEntry) toolkit.Item {
		return toolkit.Item{Widget: &logRow{s: s, e: e}, Size: s.logRowH}
	}, s.touch)
}

// syncLogList refreshes the ObservableList from the pull source when the entries
// (or the row height, on zoom) change — driving BindContainer to rebuild the
// retained rows, instead of rebuilding a VBox every frame.
func (s *Scene) syncLogList() {
	s.ensureLogBinding()
	entries := s.logEntries()
	var top time.Time
	if len(entries) > 0 {
		top = entries[0].When
	}
	if len(entries) == s.logSyncN && top == s.logSyncTop && s.logRowH == s.logSyncRowH {
		return
	}
	s.logList.Clear()
	s.logList.Append(entries...)
	s.logSyncN, s.logSyncTop, s.logSyncRowH = len(entries), top, s.logRowH
}

// drawLog paints the Network-log view.
// logRow is one network-log entry as a toolkit widget so the log view lays rows
// out with a VBox: it composes two box lines (method | URL, then status |
// right-aligned duration) plus a bottom divider, all via getFace textLines.
type logRow struct {
	toolkit.Base
	s *Scene
	e LogEntry
}

func (w *logRow) Draw(pt painter.Painter, th *toolkit.Theme) {
	p, _, ok := pixImg(pt) // Labels draw through the painter; no *image.RGBA needed
	if !ok {
		return
	}
	s, b := w.s, w.Bounds()
	m := s.m
	muteS := mute(th.OnSurface, th.Surface)
	methodFace := getFace(rpxOf(s, 13), true)
	urlFace := getFace(rpxOf(s, 13), false)
	durW := rpxOf(s, 72)
	mw := methodFace.width(w.e.Method) + m.pad

	// Text is stock toolkit.Label with the reader's fallback fonts; the URL and
	// status lines ellipsise to their flex width instead of pre-truncating.
	methodFont, urlFont, metaFont := ttFont(true, rpxOf(s, 13)), ttFont(false, rpxOf(s, 13)), ttFont(false, rpxOf(s, 12))
	mkLabel := func(text string, font toolkit.Font, ink toolkit.RGBA) *toolkit.Label {
		l := toolkit.NewLabel(text)
		l.Font, l.Ink, l.Ellipsis = font, ink, true
		return l
	}

	// Line 1: method (fixed) | elided URL (flex).
	l1 := toolkit.NewHBox()
	l1.Spacing = -1
	l1.AddFixed(mkLabel(w.e.Method, methodFont, th.OnSurface), mw)
	l1.AddFlex(mkLabel(shortURL(w.e.URL), urlFont, muteS), 1)
	l1.SetBounds(toolkit.Rect{X: b.X, Y: b.Y, W: b.W, H: urlFace.height})
	l1.Draw(p, th)

	// Line 2: status/error colour-coded (flex) | duration right-aligned (fixed).
	text, col := fmt.Sprintf("%d", w.e.Status), statusColor(w.e.Status)
	if w.e.Err != "" {
		text, col = w.e.Err, rgb(0xD03030)
	}
	l2 := toolkit.NewHBox()
	l2.Spacing = -1
	l2.AddFlex(mkLabel(text, metaFont, col), 1)
	dur := mkLabel(formatDur(w.e.Dur), metaFont, muteS)
	dur.Align = toolkit.AlignRight
	l2.AddFixed(dur, durW)
	l2.SetBounds(toolkit.Rect{X: b.X, Y: b.Y + urlFace.height + rpxOf(s, 4), W: b.W, H: m.meta.height})
	l2.Draw(p, th)

	// Bottom divider: a 1px toolkit.Backdrop panel, not a bare painter fill, so
	// the whole row is composed of widgets.
	div := &toolkit.Backdrop{Fill: th.Border}
	div.SetBounds(toolkit.Rect{X: b.X, Y: b.Y + b.H - 1, W: b.W, H: 1})
	div.Draw(p, th)
}

// logBackButton is the "< Back" affordance as a self-contained widget: a rounded
// Surface pill (there is no stock toolkit widget for a borderless, left-aligned
// rounded button — Button centres its label and strokes a border, Chip is a
// square SurfaceAlt) composing a toolkit.Label for the accent caption. Clicks are
// still routed by the scene's hit-test (logHitTest → HitCloseLog), so the close
// flow in internal/windowapp is unchanged; this widget only paints the visual.
type logBackButton struct {
	toolkit.Base
	s *Scene
}

func (w *logBackButton) Draw(pt painter.Painter, th *toolkit.Theme) {
	s, b := w.s, w.Bounds()
	m := s.m
	ground := &toolkit.Backdrop{Fill: th.Surface, Radius: rpxOf(s, 6)}
	ground.SetBounds(b)
	ground.Draw(pt, th)
	lbl := toolkit.NewLabel("< Back")
	lbl.Font, lbl.Ink, lbl.VAlign = ttFont(false, rpxOf(s, 13)), th.Accent, toolkit.VTop
	lbl.SetBounds(toolkit.Rect{X: b.X + m.pad, Y: b.Y + (b.H-m.side.height)/2, W: b.W - m.pad, H: m.side.height})
	lbl.Draw(pt, th)
}

func (s *Scene) drawLog(buf []byte) {
	s.layoutLog()
	m := s.m
	th := s.theme
	onAccent := th.Background
	if v, ok := th.Extra["OnAccent"]; ok {
		onAccent = v
	}
	muteS := mute(th.OnSurface, th.Surface)
	p := painter.NewPixelPainter(buf, s.W, s.H)

	// Window background: a solid toolkit.Backdrop panel, not a bare painter fill.
	bg := &toolkit.Backdrop{Fill: th.Background}
	bg.SetBounds(toolkit.Rect{X: 0, Y: 0, W: s.W, H: s.H})
	bg.Draw(p, th)

	entries := s.logEntries()
	x := m.pad * 2
	// Rows stop at the shared gutter when the scrollbar is shown (same rule as the
	// feed/browse), so nothing paints under the bar.
	w := s.scrollClampRight(s.W-m.pad, s.W, 0, s.scrollbarNeeded(s.logScroll.contentH, s.H-m.topbarH)) - x

	if len(entries) == 0 {
		// Empty-state caption as a toolkit.Label (top-anchored to match the row
		// origin), not a hand-drawn text run.
		empty := toolkit.NewLabel("No requests yet")
		empty.Font, empty.Ink, empty.VAlign = ttFont(false, rpxOf(s, 12)), muteS, toolkit.VTop
		empty.SetBounds(toolkit.Rect{X: x, Y: m.topbarH + m.pad, W: w, H: m.meta.height})
		empty.Draw(p, th)
	}

	// The rows are a data-bound toolkit.Container (see ensureLogBinding): an
	// ObservableList[LogEntry] → Container of retained logRow items via
	// tkbind.BindContainer. Position it in the scrolled content area and draw it;
	// each logRow recovers this frame's buffer via pixImg.
	s.logContainer.SetBounds(toolkit.Rect{X: x, Y: m.topbarH + m.pad - s.logScroll.offset, W: w, H: len(entries) * s.logRowH})
	s.logContainer.Draw(p, th)

	// Scrollbar down the right edge when the log overflows the viewport.
	s.drawVScrollbar(p, toolkit.Rect{X: 0, Y: m.topbarH, W: s.W, H: s.H - m.topbarH}, 0, s.logScroll.contentH, s.logScroll.offset)

	// Topbar (accent) over any overflow: an accent Backdrop panel carrying the
	// "< Back" pill widget and a title Label — all composed, nothing hand-drawn.
	topbar := &toolkit.Backdrop{Fill: th.Accent}
	topbar.SetBounds(toolkit.Rect{X: 0, Y: 0, W: s.W, H: m.topbarH})
	topbar.Draw(p, th)

	back := &logBackButton{s: s}
	back.SetBounds(toolkit.Rect(s.logBackR))
	back.Draw(p, th)

	tx := s.logBackR.X + s.logBackR.W + m.pad
	title := toolkit.NewLabel("Network log")
	title.Font, title.Ink, title.VAlign = ttFont(true, rpxOf(s, 15)), onAccent, toolkit.VTop
	title.SetBounds(toolkit.Rect{X: tx, Y: (m.topbarH - m.title.height) / 2, W: s.W - tx, H: m.title.height})
	title.Draw(p, th)
}

// logHitTest maps a click in the log view to Back / None.
func (s *Scene) logHitTest(x, y int) Hit {
	s.layoutLog()
	if inRect(s.logBackR, x, y) {
		return Hit{Kind: HitCloseLog}
	}
	return Hit{Kind: HitNone}
}

// statusColor colour-codes an HTTP status: 2xx green, 3xx neutral, 4xx/5xx red.
func statusColor(code int) toolkit.RGBA {
	switch {
	case code >= 200 && code < 300:
		return rgb(0x1E9E52)
	case code >= 300 && code < 400:
		return rgb(0x9AA0A6)
	case code >= 400:
		return rgb(0xD03030)
	default:
		return rgb(0x9AA0A6)
	}
}

// shortURL drops the scheme so the host+path (the part that matters) fits in the
// row; long values are elided by the caller via truncate.
func shortURL(raw string) string {
	if i := strings.Index(raw, "://"); i >= 0 {
		return raw[i+3:]
	}
	return raw
}

// formatDur renders a duration in seconds with two decimals, e.g. "0.42s".
func formatDur(d time.Duration) string { return fmt.Sprintf("%.2fs", d.Seconds()) }
