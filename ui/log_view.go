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
}

// drawLog paints the Network-log view.
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
	methodFace := getFace(rpxOf(s, 13), true)
	urlFace := getFace(rpxOf(s, 13), false)
	metaFace := m.meta
	x := m.pad * 2
	w := s.W - x - m.pad
	durW := rpxOf(s, 72) // reserved right column for the duration

	if len(entries) == 0 {
		metaFace.draw(img, x, m.topbarH+m.pad, "No requests yet", muteS)
	}

	rowH := s.logRowH
	top := m.topbarH + m.pad - s.logScrollY
	for i, e := range entries {
		ry := top + i*rowH
		if ry+rowH < m.topbarH || ry >= s.H {
			continue // fully off-screen; skip
		}
		// Line 1: method + elided host/path of the URL.
		methodFace.draw(img, x, ry, e.Method, th.OnSurface)
		mw := methodFace.width(e.Method) + m.pad
		url := truncate(urlFace, shortURL(e.URL), w-mw)
		urlFace.draw(img, x+mw, ry, url, muteS)

		// Line 2: status (or error) colour-coded, duration right-aligned.
		sy := ry + urlFace.height + rpxOf(s, 4)
		status, col := e.Status, toolkit.RGBA{}
		var text string
		if e.Err != "" {
			text, col = e.Err, rgb(0xD03030)
		} else {
			text, col = fmt.Sprintf("%d", status), statusColor(status)
		}
		metaFace.draw(img, x, sy, truncate(metaFace, text, w-durW), col)
		metaFace.drawRight(img, x+w, sy, formatDur(e.Dur), muteS)

		p.FillRect(painter.Rect{X: x, Y: ry + rowH - 1, W: w, H: 1}, th.Border)
	}

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
