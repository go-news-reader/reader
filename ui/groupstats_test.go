package ui

import (
	"testing"

	"github.com/go-news-reader/reader/source"
)

func TestGroupStatsLabel(t *testing.T) {
	if got := groupStatsLabel(source.GroupStats{}); got != "" {
		t.Errorf("unsampled label = %q, want empty", got)
	}
	got := groupStatsLabel(source.GroupStats{Sampled: 200, Binaries: 144, Images: 104})
	if got != "~72% bin · ~52% img" {
		t.Errorf("label = %q, want ~72%% bin · ~52%% img", got)
	}
}

func TestSceneGroupStatsCache(t *testing.T) {
	s := New(760, 460, ThemeFor(OSMac, false))
	if s.HasGroupStats("alt.bin.x") {
		t.Fatal("fresh scene should have no cached stats")
	}
	if _, ok := s.groupStats("alt.bin.x"); ok {
		t.Fatal("groupStats should miss before a scan")
	}
	s.SetGroupStats("alt.bin.x", source.GroupStats{Sampled: 100, Binaries: 80, Images: 60})
	if !s.HasGroupStats("alt.bin.x") {
		t.Fatal("stats should be cached after SetGroupStats")
	}
	st, ok := s.groupStats("alt.bin.x")
	if !ok || st.Binaries != 80 || st.Images != 60 {
		t.Fatalf("groupStats = %+v,%v", st, ok)
	}
}

// browseScene builds a browser showing a couple of groups, laid out so the tree
// rows (and the keyboard selection) exist.
func groupBrowseScene() *Scene {
	s := New(820, 520, ThemeFor(OSMac, false))
	s.SetUsenetServer("news.example.org:119")
	s.SetBrowseGroups([]source.GroupInfo{
		{Name: "altbinpics", Count: 12340},
		{Name: "altbinmovies", Count: 9100},
	})
	s.OpenBrowse()
	s.layoutBrowse()
	return s
}

func TestSelectedBrowseGroup(t *testing.T) {
	s := groupBrowseScene()
	name, ok := s.SelectedBrowseGroup()
	if !ok || name == "" {
		t.Fatalf("selected group = %q,%v, want a group", name, ok)
	}
	// Out-of-range selection → not ok.
	s.browseSel = -1
	if _, ok := s.SelectedBrowseGroup(); ok {
		t.Error("negative browseSel should yield no selection")
	}
	s.browseSel = 1 << 20
	if _, ok := s.SelectedBrowseGroup(); ok {
		t.Error("overflow browseSel should yield no selection")
	}

	// A hierarchy node (not a group) under the selection → not ok. Dotted names
	// build parent nodes ("alt", "alt.bin") above the leaf groups.
	h := New(820, 520, ThemeFor(OSMac, false))
	h.SetUsenetServer("news.example.org:119")
	h.SetBrowseGroups([]source.GroupInfo{
		{Name: "alt.bin.pics", Count: 5},
		{Name: "alt.bin.movies", Count: 7},
	})
	h.OpenBrowse()
	h.layoutBrowse()
	h.browseSel = 0 // the top "alt" parent node, which is not a group
	if _, ok := h.SelectedBrowseGroup(); ok {
		t.Error("a hierarchy (non-group) node should yield no selection")
	}
}

func TestBrowseRowShowsStats(t *testing.T) {
	s := groupBrowseScene()
	name, _ := s.SelectedBrowseGroup()
	s.SetGroupStats(name, source.GroupStats{Sampled: 100, Binaries: 72, Images: 52})
	buf := make([]byte, s.W*s.H*4)
	s.Draw(buf) // drawBrowseRow appends the "~72% bin · ~52% img" suffix for that row
}
