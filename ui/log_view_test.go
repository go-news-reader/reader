package ui

import (
	"testing"
	"time"
)

// sampleLog returns a fixed set of exchanges: a 200, a 403, a redirect, and a
// transport error — exercising every status colour branch and the error path.
func sampleLog() []LogEntry {
	return []LogEntry{
		{Method: "GET", URL: "https://www.reddit.com/r/golang/hot.json", Status: 200, Bytes: 1234, Dur: 420 * time.Millisecond},
		{Method: "GET", URL: "https://www.reddit.com/r/pics/hot.json", Status: 403, Bytes: 87, Dur: 310 * time.Millisecond},
		{Method: "GET", URL: "https://hn.algolia.com/api/v1/x", Status: 301, Bytes: 0, Dur: 55 * time.Millisecond},
		{Method: "POST", URL: "https://blocked.example/api", Err: "dial tcp: connection refused", Dur: 12 * time.Millisecond},
	}
}

func TestOpenCloseLog(t *testing.T) {
	s := newScene()
	s.OpenLog()
	if s.Mode() != ModeLog || s.logScrollY != 0 {
		t.Fatalf("OpenLog state: mode=%v scroll=%d", s.Mode(), s.logScrollY)
	}
	s.CloseLog()
	if s.Mode() != ModeFeed {
		t.Fatal("CloseLog should return to feed")
	}
}

func TestLogSourceAndEntries(t *testing.T) {
	s := newScene()
	if s.LogEntries() != nil {
		t.Fatal("no source -> nil entries")
	}
	s.SetLogSource(sampleLog)
	if got := s.LogEntries(); len(got) != 4 {
		t.Fatalf("entries = %d, want 4", len(got))
	}
}

func TestDrawLog(t *testing.T) {
	s := newScene()
	s.SetLogSource(sampleLog)
	s.OpenLog()
	buf := renderPNG(t, s, "log")
	// The log topbar is an accent fill; sample a strip clear of the buttons.
	acc := s.theme.Accent
	if got := px(buf, s.W, s.W/2, 4); got.R != acc.R || got.G != acc.G || got.B != acc.B {
		t.Fatalf("log topbar pixel = %v, want accent %v", got, acc)
	}
	// The "< Back" button hits.
	m := s.computeMetrics()
	if s.HitTest(m.pad+5, m.topbarH/2).Kind != HitCloseLog {
		t.Fatal("back button should close the log")
	}
	// A click in the list area is none.
	if s.HitTest(s.W/2, s.H-10).Kind != HitNone {
		t.Fatal("list click should be none")
	}
}

func TestDrawLogEmptyAndNoAccent(t *testing.T) {
	// No source and a theme without Extra["OnAccent"] (WhiteSur) -> empty-state
	// line + the onAccent-absent branch.
	s := New(700, 500, ThemeFor(OSMac, false))
	s.OpenLog()
	if buf := renderPNG(t, s, "log-empty"); len(buf) == 0 {
		t.Fatal("no buffer")
	}
}

func TestLogScroll(t *testing.T) {
	s := New(500, 300, nil)
	// Many entries so the list overflows and the offscreen-skip branch runs.
	many := make([]LogEntry, 60)
	for i := range many {
		many[i] = LogEntry{Method: "GET", URL: "https://x/y", Status: 200, Dur: time.Second}
	}
	s.SetLogSource(func() []LogEntry { return many })
	s.OpenLog()
	buf := make([]byte, s.W*s.H*4)
	s.Draw(buf)
	s.Scroll(1 << 20) // to max -> early rows scroll above the viewport
	if s.logScrollY <= 0 {
		t.Fatalf("log scroll did not advance: %d", s.logScrollY)
	}
	s.Draw(buf) // redraw scrolled (exercises the offscreen-continue branch)
	s.Scroll(-1 << 20)
	if s.logScrollY != 0 {
		t.Fatalf("neg clamp = %d", s.logScrollY)
	}
}

func TestStatusColor(t *testing.T) {
	cases := []struct {
		code    int
		r, g, b uint8
	}{
		{200, 0x1E, 0x9E, 0x52}, // 2xx green
		{204, 0x1E, 0x9E, 0x52},
		{301, 0x9A, 0xA0, 0xA6}, // 3xx neutral
		{404, 0xD0, 0x30, 0x30}, // 4xx red
		{500, 0xD0, 0x30, 0x30}, // 5xx red
		{100, 0x9A, 0xA0, 0xA6}, // <200 -> default neutral
	}
	for _, c := range cases {
		got := statusColor(c.code)
		if got.R != c.r || got.G != c.g || got.B != c.b {
			t.Fatalf("statusColor(%d) = %v", c.code, got)
		}
	}
}

func TestShortURLAndFormatDur(t *testing.T) {
	if got := shortURL("https://host/path?q=1"); got != "host/path?q=1" {
		t.Fatalf("shortURL scheme strip = %q", got)
	}
	if got := shortURL("host/only"); got != "host/only" {
		t.Fatalf("shortURL no-scheme = %q", got)
	}
	if got := formatDur(420 * time.Millisecond); got != "0.42s" {
		t.Fatalf("formatDur = %q", got)
	}
}
