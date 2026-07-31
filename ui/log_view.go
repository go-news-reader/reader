package ui

import (
	"fmt"
	"image"
	"strings"
	"time"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
)

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
	s.logScrollY = 0
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
	s.logContentH = len(s.logEntries()) * s.logRowH
	s.logScrollY = clampPanelScroll(s.logScrollY, s.logContentH, s.H-m.topbarH)
}

// drawLog paints the Network-log view.
// logRow is one network-log entry as a toolkit widget so the log view lays rows
// out with a VBox: it composes two box lines (method | URL, then status |
// right-aligned duration) plus a bottom divider, all via getFace textLines.
type logRow struct {
	toolkit.Base
	s   *Scene
	e   LogEntry
	p   *painter.PixelPainter
	img *image.RGBA
}

func (w *logRow) Draw(_ painter.Painter, th *toolkit.Theme) {
	s, b := w.s, w.Bounds()
	m := s.m
	muteS := mute(th.OnSurface, th.Surface)
	methodFace := getFace(rpxOf(s, 13), true)
	urlFace := getFace(rpxOf(s, 13), false)
	durW := rpxOf(s, 72)
	mw := methodFace.width(w.e.Method) + m.pad

	// Line 1: method (fixed) | elided URL (flex).
	l1 := toolkit.NewHBox()
	l1.Spacing = -1
	l1.AddFixed(&textLine{face: methodFace, text: w.e.Method, ink: th.OnSurface, img: w.img}, mw)
	l1.AddFlex(&textLine{face: urlFace, text: truncate(urlFace, shortURL(w.e.URL), b.W-mw), ink: muteS, img: w.img}, 1)
	l1.SetBounds(toolkit.Rect{X: b.X, Y: b.Y, W: b.W, H: urlFace.height})
	l1.Draw(w.p, th)

	// Line 2: status/error colour-coded (flex) | duration right-aligned (fixed).
	text, col := fmt.Sprintf("%d", w.e.Status), statusColor(w.e.Status)
	if w.e.Err != "" {
		text, col = w.e.Err, rgb(0xD03030)
	}
	l2 := toolkit.NewHBox()
	l2.Spacing = -1
	l2.AddFlex(&textLine{face: m.meta, text: truncate(m.meta, text, b.W-durW), ink: col, img: w.img}, 1)
	l2.AddFixed(&textLine{face: m.meta, text: formatDur(w.e.Dur), ink: muteS, img: w.img, alignRight: true}, durW)
	l2.SetBounds(toolkit.Rect{X: b.X, Y: b.Y + urlFace.height + rpxOf(s, 4), W: b.W, H: m.meta.height})
	l2.Draw(w.p, th)

	w.p.FillRect(painter.Rect{X: b.X, Y: b.Y + b.H - 1, W: b.W, H: 1}, th.Border)
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
	img := &image.RGBA{Pix: buf, Stride: s.W * 4, Rect: image.Rect(0, 0, s.W, s.H)}

	p.FillRect(painter.Rect{X: 0, Y: 0, W: s.W, H: s.H}, th.Background)

	entries := s.logEntries()
	x := m.pad * 2
	// Rows stop at the shared gutter when the scrollbar is shown (same rule as the
	// feed/browse), so nothing paints under the bar.
	w := s.scrollClampRight(s.W-m.pad, s.W, 0, s.scrollbarNeeded(s.logContentH, s.H-m.topbarH)) - x

	if len(entries) == 0 {
		m.meta.draw(img, x, m.topbarH+m.pad, "No requests yet", muteS)
	}

	// Rows are a VBox of logRow widgets: each row is two box-composed lines
	// (method | URL, then status | duration).
	rowH := s.logRowH
	col := toolkit.NewVBox()
	col.Spacing = -1
	for _, e := range entries {
		col.AddFixed(&logRow{s: s, e: e, p: p, img: img}, rowH)
	}
	col.SetBounds(toolkit.Rect{X: x, Y: m.topbarH + m.pad - s.logScrollY, W: w, H: len(entries) * rowH})
	col.Draw(p, th)

	// Scrollbar down the right edge when the log overflows the viewport.
	s.drawVScrollbar(p, toolkit.Rect{X: 0, Y: m.topbarH, W: s.W, H: s.H - m.topbarH}, 0, s.logContentH, s.logScrollY)

	// Topbar (accent) over any overflow: "< Back" + title.
	p.FillRect(painter.Rect{X: 0, Y: 0, W: s.W, H: m.topbarH}, th.Accent)
	p.FillRoundRect(painter.Rect(s.logBackR), rpxOf(s, 6), th.Surface)
	m.side.draw(img, s.logBackR.X+m.pad, s.logBackR.Y+(s.logBackR.H-m.side.height)/2, "< Back", th.Accent)
	tx := s.logBackR.X + s.logBackR.W + m.pad
	m.title.draw(img, tx, (m.topbarH-m.title.height)/2, "Network log", onAccent)
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
