package ui

import (
	"strconv"
	"testing"
)

func TestDownloadStateAndQueued(t *testing.T) {
	s := New(900, 500, ThemeFor(OSMac, false))
	if len(s.Downloads()) != 0 {
		t.Fatal("no downloads initially")
	}
	s.SetDownloads([]DownloadItem{
		{ID: "a", Status: DLQueued}, {ID: "b", Status: DLActive}, {ID: "c", Status: DLDone},
	})
	if !s.IsDownloadQueued("a") || !s.IsDownloadQueued("b") {
		t.Fatal("queued + active count as queued")
	}
	if s.IsDownloadQueued("c") {
		t.Fatal("a finished download is not queued")
	}
	if s.IsDownloadQueued("x") {
		t.Fatal("unknown id is not queued")
	}
}

func TestDownloadPanelHeightAndRect(t *testing.T) {
	s := New(900, 500, ThemeFor(OSMac, false))
	s.SetSubs(nil)
	s.layout()
	if s.downloadPanelH() != 0 {
		t.Fatal("empty download list → no panel")
	}
	s.SetDownloads([]DownloadItem{{ID: "a"}})
	s.layout()
	if s.downloadPanelH() <= 0 || s.feedBottom() >= s.H {
		t.Fatal("one download → a panel that shrinks the feed")
	}
	// Not the feed view → hidden.
	s.OpenSettings()
	if s.downloadPanelH() != 0 || s.downloadPanelRect().W != 0 {
		t.Fatal("panel hides outside the feed view")
	}
	s.CloseSettings()
	// Capped at dlMaxRows.
	many := make([]DownloadItem, 12)
	for i := range many {
		many[i] = DownloadItem{ID: strconv.Itoa(i)}
	}
	s.SetDownloads(many)
	s.layout()
	capped := rpxOf(s, dlHeaderH) + dlMaxRows*rpxOf(s, dlRowH)
	if s.downloadPanelH() != capped {
		t.Fatalf("panel height = %d, want capped %d", s.downloadPanelH(), capped)
	}
	if r := s.downloadPanelRect(); r.H != capped || r.X != s.m.sidebarW {
		t.Fatalf("panel rect = %+v", r)
	}
	s.layoutDownloadPanel()
	if s.dlClearR.W == 0 {
		t.Fatal("Clear button should be laid out")
	}
	// Hidden panel: layout clears the Clear rect.
	s.SetDownloads(nil)
	s.layout()
	s.layoutDownloadPanel()
	if s.dlClearR.W != 0 {
		t.Fatal("hidden panel should have no Clear button")
	}
}

func TestDownloadPanelDrawAllStatuses(t *testing.T) {
	for _, dark := range []bool{false, true} {
		s := New(900, 500, ThemeFor(OSMac, dark))
		s.SetSubs(nil)
		s.SetDownloads([]DownloadItem{
			{ID: "a", Name: "a.jpg", Status: DLQueued, Total: 5},
			{ID: "b", Name: "b.mkv", Status: DLActive, Done: 2, Total: 5},
			{ID: "c", Name: "c.zip", Status: DLDone, Done: 5, Total: 5},
			{ID: "d", Name: "d.iso", Status: DLFailed},
			{ID: "e", Name: "e.rar", Status: DLQueued}, // > dlMaxRows → row cap exercised
			{ID: "f", Name: "f.7z", Status: DLQueued},
		})
		buf := make([]byte, s.W*s.H*4)
		s.Draw(buf) // exercises drawDownloadPanel, downloadRow, downloadStatusText (all states)
	}
	// downloadStatusText's default arm (invalid status) is defensive.
	if downloadStatusText(DownloadItem{Status: DownloadStatus(99)}) != "" {
		t.Fatal("unknown status → empty label")
	}
}

func TestDownloadPanelHitTest(t *testing.T) {
	s := New(900, 500, ThemeFor(OSMac, false))
	s.SetSubs(nil)
	s.SetDownloads([]DownloadItem{{ID: "a", Name: "a"}})
	s.layout()
	s.layoutDownloadPanel()
	c := s.dlClearR
	if h := s.HitTest(c.X+c.W/2, c.Y+c.H/2); h.Kind != HitClearDownloads {
		t.Fatalf("Clear hit = %v, want HitClearDownloads", h.Kind)
	}
	// Elsewhere in the panel is inert (mid-panel, clear of the sidebar/preview grips).
	r := s.downloadPanelRect()
	if h := s.HitTest(r.X+r.W/2, r.Y+r.H-2); h.Kind != HitNone {
		t.Fatalf("panel body hit = %v, want HitNone", h.Kind)
	}
}
