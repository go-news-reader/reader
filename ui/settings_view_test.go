package ui

import (
	"testing"

	"github.com/go-news-reader/reader/internal/settings"
	"github.com/go-news-reader/reader/source"
)

// profScene builds a scene with two profiles (Home active) themed adwaita.
func profScene() *Scene {
	s := New(900, 640, ThemeFor(OSLinux, false))
	s.SetProfiles([]settings.Profile{
		{Name: "Home", Subs: []source.Subscription{
			{Source: source.Reddit, Channel: "golang", Limit: 25},
			{Source: source.HackerNews, Limit: 25},
		}},
		{Name: "Tech", Subs: []source.Subscription{
			{Source: source.Lemmy, Channel: "technology", Limit: 25},
		}},
	}, 0)
	return s
}

func TestProfileSwitchAndAccessors(t *testing.T) {
	s := profScene()
	if s.ActiveProfileIndex() != 0 || s.ActiveProfile().Name != "Home" {
		t.Fatalf("active = %d %q", s.ActiveProfileIndex(), s.ActiveProfile().Name)
	}
	// Home's subs drive the sidebar.
	if len(s.Subs) != 2 || s.Subs[0].Channel != "golang" {
		t.Fatalf("home subs = %+v", s.Subs)
	}
	// Switch to Tech -> its single sub, filter reset to All.
	s.Active = 0
	s.SetActiveProfile(1)
	if s.ActiveProfileIndex() != 1 || len(s.Subs) != 1 || s.Subs[0].Channel != "technology" {
		t.Fatalf("tech subs = %+v", s.Subs)
	}
	if s.Active != AllFilter {
		t.Fatalf("filter not reset: %d", s.Active)
	}
	// Out-of-range clamps to 0.
	s.SetActiveProfile(9)
	if s.ActiveProfileIndex() != 0 {
		t.Fatalf("oob clamp = %d", s.ActiveProfileIndex())
	}
	// Empty profiles -> synthetic fallback.
	e := New(400, 300, nil)
	if e.ActiveProfile().Name != "All" {
		t.Fatalf("empty fallback = %q", e.ActiveProfile().Name)
	}
}

func TestSettingsSnapshotAndScalars(t *testing.T) {
	s := profScene()
	s.SetThemeName(settings.ThemeDark)
	s.SetCachePath("/tmp/media")
	if s.ThemeName() != settings.ThemeDark || s.CachePath() != "/tmp/media" {
		t.Fatalf("scalars = %q %q", s.ThemeName(), s.CachePath())
	}
	s.SetMediaCacheMB(512)
	if s.MediaCacheMB() != 512 {
		t.Fatalf("MediaCacheMB = %d", s.MediaCacheMB())
	}
	set := s.Settings()
	if set.Theme != settings.ThemeDark || set.CachePath != "/tmp/media" || len(set.Profiles) != 2 || set.Active != 0 {
		t.Fatalf("settings snapshot = %+v", set)
	}
	if set.MediaCacheMB != 512 {
		t.Fatalf("settings snapshot MediaCacheMB = %d, want 512", set.MediaCacheMB)
	}
}

func TestCommitCacheSize(t *testing.T) {
	s := profScene()
	s.SetMediaCacheMB(256)
	s.OpenSettings()
	// The buffer is seeded from the current value on open.
	if s.cacheSizeInput != "256" {
		t.Fatalf("seed buffer = %q, want 256", s.cacheSizeInput)
	}
	// A valid in-range value is applied and the buffer re-synced.
	s.FocusCacheSize()
	if s.Focus() != FocusCacheSize {
		t.Fatalf("focus = %d, want FocusCacheSize", s.Focus())
	}
	s.cacheSizeInput = " 1024 "
	s.CommitCacheSize()
	if s.MediaCacheMB() != 1024 || s.cacheSizeInput != "1024" {
		t.Fatalf("in-range commit = %d buf=%q", s.MediaCacheMB(), s.cacheSizeInput)
	}
	// Below the floor clamps up.
	s.cacheSizeInput = "1"
	s.CommitCacheSize()
	if s.MediaCacheMB() != settings.MinMediaCacheMB {
		t.Fatalf("low clamp = %d, want %d", s.MediaCacheMB(), settings.MinMediaCacheMB)
	}
	// Above the ceiling clamps down.
	s.cacheSizeInput = "99999"
	s.CommitCacheSize()
	if s.MediaCacheMB() != settings.MaxMediaCacheMB {
		t.Fatalf("high clamp = %d, want %d", s.MediaCacheMB(), settings.MaxMediaCacheMB)
	}
	// Zero backfills to the default.
	s.cacheSizeInput = "0"
	s.CommitCacheSize()
	if s.MediaCacheMB() != settings.DefaultMediaCacheMB {
		t.Fatalf("zero => %d, want default %d", s.MediaCacheMB(), settings.DefaultMediaCacheMB)
	}
	// A parse error keeps the current value and resets the buffer to show it.
	s.cacheSizeInput = "not-a-number"
	s.CommitCacheSize()
	if s.MediaCacheMB() != settings.DefaultMediaCacheMB || s.cacheSizeInput != itoa(settings.DefaultMediaCacheMB) {
		t.Fatalf("bad parse => %d buf=%q", s.MediaCacheMB(), s.cacheSizeInput)
	}
}

func TestCacheSizeFieldLayoutAndHit(t *testing.T) {
	s := profScene()
	s.OpenSettings()
	s.layoutSettings()
	// The field rect is laid out (non-zero) in settings mode.
	if s.sCacheSizeR.W == 0 || s.sCacheSizeR.H == 0 {
		t.Fatalf("cache-size rect empty: %+v", s.sCacheSizeR)
	}
	// A click inside it focuses the cache-size field.
	if h := s.hitSettings(s.sCacheSizeR.X+2, s.sCacheSizeR.Y+2); h.Kind != HitFocusCacheSize {
		t.Fatalf("cache-size hit = %+v, want HitFocusCacheSize", h)
	}
	// focusedField routes typing into the cache-size buffer.
	s.FocusCacheSize()
	s.cacheSizeInput = ""
	s.TypeRune('9')
	if s.cacheSizeInput != "9" {
		t.Fatalf("cache-size type = %q", s.cacheSizeInput)
	}
}

func TestCacheSizeScrollShift(t *testing.T) {
	s := New(420, 240, ThemeFor(OSMac, false))
	var many []source.Subscription
	for i := 0; i < 8; i++ {
		many = append(many, source.Subscription{Source: source.Reddit, Channel: "chan", Limit: 25})
	}
	s.SetProfiles([]settings.Profile{{Name: "P", Subs: many}}, 0)
	s.OpenSettings()
	s.layoutSettings()
	if s.settingsScroll.contentH <= s.H-s.m.topbarH {
		t.Skip("content fits; window too tall to exercise overflow")
	}
	before := s.sCacheSizeR.Y
	for i := 0; i < 40; i++ {
		s.Scroll(120)
	}
	if s.sCacheSizeR.Y >= before {
		t.Fatalf("cache-size field did not shift up on scroll: before=%d after=%d", before, s.sCacheSizeR.Y)
	}
	if h := s.HitTest(s.sCacheSizeR.X+4, s.sCacheSizeR.Y+s.sCacheSizeR.H/2); h.Kind != HitFocusCacheSize {
		t.Fatalf("click on scrolled cache-size field = %v, want HitFocusCacheSize", h.Kind)
	}
}

func TestOpenCloseSettings(t *testing.T) {
	s := profScene()
	s.SetCachePath("/c")
	s.OpenSettings()
	if s.Mode() != ModeSettings || s.selEdit != 0 || s.Focus() != FocusNone {
		t.Fatalf("open state: mode=%v sel=%d focus=%d", s.Mode(), s.selEdit, s.Focus())
	}
	if s.renameInput != "Home" || s.cacheInput != "/c" {
		t.Fatalf("seed: rename=%q cache=%q", s.renameInput, s.cacheInput)
	}
	s.CloseSettings()
	if s.Mode() != ModeFeed {
		t.Fatal("close should return to feed")
	}
	// selEditName out-of-range branch.
	s.selEdit = 99
	if s.selEditName() != "" {
		t.Fatal("oob selEditName should be empty")
	}
}

func TestEditProfileSelectNewDelete(t *testing.T) {
	s := profScene()
	s.OpenSettings()
	// SelectEditProfile valid seeds rename; invalid is a no-op.
	s.SelectEditProfile(1)
	if s.selEdit != 1 || s.renameInput != "Tech" {
		t.Fatalf("select = %d %q", s.selEdit, s.renameInput)
	}
	s.SelectEditProfile(-1)
	if s.selEdit != 1 {
		t.Fatal("invalid select changed selEdit")
	}
	// NewProfile appends and selects it.
	s.NewProfile()
	if len(s.Profiles) != 3 || s.selEdit != 2 || s.Profiles[2].Name != "Profile 3" {
		t.Fatalf("new = %+v sel=%d", s.Profiles, s.selEdit)
	}
	// Delete the middle profile: indices below the active/edit shift down.
	s.activeProf = 2
	s.selEdit = 2
	s.DeleteProfile(1) // 1 < activeProf and 1 < selEdit -> both decrement
	if len(s.Profiles) != 2 || s.activeProf != 1 || s.selEdit != 1 {
		t.Fatalf("delete-shift: profs=%d active=%d sel=%d", len(s.Profiles), s.activeProf, s.selEdit)
	}
	// Delete the last while active/edit point past the new end -> clamp.
	s.activeProf = 1
	s.selEdit = 1
	s.DeleteProfile(1)
	if len(s.Profiles) != 1 || s.activeProf != 0 || s.selEdit != 0 {
		t.Fatalf("delete-clamp: profs=%d active=%d sel=%d", len(s.Profiles), s.activeProf, s.selEdit)
	}
	// Guards: last profile can't be deleted; out-of-range is a no-op.
	s.DeleteProfile(0)
	s.DeleteProfile(5)
	if len(s.Profiles) != 1 {
		t.Fatalf("guard failed: %d", len(s.Profiles))
	}
}

func TestRenameAndCache(t *testing.T) {
	s := profScene()
	s.OpenSettings()
	s.FocusRename()
	s.renameInput = "Renamed"
	s.CommitRename()
	if s.Profiles[0].Name != "Renamed" {
		t.Fatalf("rename = %q", s.Profiles[0].Name)
	}
	// Blank rename is ignored.
	s.renameInput = "   "
	s.CommitRename()
	if s.Profiles[0].Name != "Renamed" {
		t.Fatal("blank rename should be ignored")
	}
	// Out-of-range selEdit is ignored.
	s.selEdit = 99
	s.renameInput = "X"
	s.CommitRename()
	// Cache commit.
	s.FocusCache()
	s.cacheInput = " /new/cache "
	s.CommitCache()
	if s.CachePath() != "/new/cache" {
		t.Fatalf("cache = %q", s.CachePath())
	}
}

func TestAddRemoveSub(t *testing.T) {
	s := profScene() // active=0, selEdit set to 0 via SetProfiles
	s.OpenSettings()
	s.SelectKind(source.Bluesky)
	if s.newKind != source.Bluesky {
		t.Fatal("select kind")
	}
	s.FocusChannel()
	s.channelInput = "gophers.social"
	s.AddInputSub() // selEdit == activeProf -> sidebar rebuilds
	if n := len(s.Profiles[0].Subs); n != 3 {
		t.Fatalf("add: %d subs", n)
	}
	if len(s.Subs) != 3 {
		t.Fatalf("sidebar not rebuilt: %d", len(s.Subs))
	}
	// Duplicate (same source+channel, case-insensitive) ignored.
	s.channelInput = "Gophers.Social"
	s.AddInputSub()
	if len(s.Profiles[0].Subs) != 3 {
		t.Fatal("duplicate added")
	}
	// Empty kind defaults to Reddit, and a bare Reddit name gains the r/ prefix.
	s.newKind = ""
	s.channelInput = "rust"
	s.AddInputSub()
	last := s.Profiles[0].Subs[len(s.Profiles[0].Subs)-1]
	if last.Source != source.Reddit || last.Channel != "r/rust" {
		t.Fatalf("default kind = %+v, want reddit r/rust", last)
	}
	// A "u/" redditor channel is kept verbatim (routes to their submissions).
	s.newKind = source.Reddit
	s.channelInput = "u/spez"
	s.AddInputSub()
	if last := s.Profiles[0].Subs[len(s.Profiles[0].Subs)-1]; last.Channel != "u/spez" {
		t.Fatalf("user channel = %q, want u/spez", last.Channel)
	}
	// Add into a non-active profile does NOT rebuild the active sidebar.
	s.selEdit = 1
	n := len(s.Subs)
	s.newKind = source.Reddit
	s.channelInput = "linux"
	s.AddInputSub()
	if len(s.Subs) != n {
		t.Fatal("non-active add rebuilt sidebar")
	}
	// Out-of-range selEdit no-op.
	s.selEdit = 99
	s.channelInput = "x"
	s.AddInputSub()

	// RemoveSub from the active profile rebuilds; guards cover bad indices.
	s.selEdit = 0
	before := len(s.Profiles[0].Subs)
	s.RemoveSub(0, 0)
	if len(s.Profiles[0].Subs) != before-1 || len(s.Subs) != before-1 {
		t.Fatalf("remove: %d subs / %d sidebar", len(s.Profiles[0].Subs), len(s.Subs))
	}
	s.RemoveSub(9, 0)  // prof oob
	s.RemoveSub(0, 99) // sub oob
	s.RemoveSub(-1, 0) // prof neg
	if len(s.Profiles[0].Subs) != before-1 {
		t.Fatal("bad-index removes mutated")
	}
	// Remove from non-active profile leaves the sidebar alone.
	sc := len(s.Subs)
	s.RemoveSub(1, 0)
	if len(s.Subs) != sc {
		t.Fatal("non-active remove touched sidebar")
	}
}

func TestSettingsTypeRuneBackspace(t *testing.T) {
	s := profScene()
	s.OpenSettings()
	// No focus -> typing is ignored.
	s.TypeRune('a')
	if s.channelInput != "" || s.renameInput != "Home" {
		t.Fatal("typed with no focus")
	}
	s.FocusChannel()
	s.TypeRune('g')
	s.TypeRune('o')
	if s.channelInput != "go" {
		t.Fatalf("channel = %q", s.channelInput)
	}
	s.Backspace()
	if s.channelInput != "g" {
		t.Fatalf("channel bs = %q", s.channelInput)
	}
	s.FocusRename()
	s.renameInput = ""
	s.TypeRune('N')
	if s.renameInput != "N" {
		t.Fatalf("rename type = %q", s.renameInput)
	}
	s.FocusCache()
	s.cacheInput = ""
	s.TypeRune('/')
	if s.cacheInput != "/" {
		t.Fatalf("cache type = %q", s.cacheInput)
	}
	// Backspace on an empty focused field is a no-op (no panic).
	s.cacheInput = ""
	s.Backspace()
	// No-focus backspace no-op.
	s.sf = FocusNone
	s.Backspace()
	// trimLastRune on empty input.
	if trimLastRune("") != "" {
		t.Fatal("trimLastRune empty")
	}
	// Scroll is inert in the settings view.
	s.feedScroll.offset = 5
	s.Scroll(100)
	if s.feedScroll.offset != 5 {
		t.Fatalf("settings scroll moved: %d", s.feedScroll.offset)
	}
}

func TestSettingsHitAndDraw(t *testing.T) {
	s := profScene()
	s.OpenSettings()
	s.layoutSettings()

	// Done button.
	if s.hitSettings(s.sDoneR.X+2, s.sDoneR.Y+2).Kind != HitCloseSettings {
		t.Fatal("done hit")
	}
	// Rename input field.
	if h := s.hitSettings(s.sRenameR.X+2, s.sRenameR.Y+2); h.Kind != HitRenameProfile {
		t.Fatalf("rename hit = %+v", h)
	}
	// Channel input field.
	if s.hitSettings(s.sChannelR.X+2, s.sChannelR.Y+2).Kind != HitFocusChannel {
		t.Fatal("channel hit")
	}
	// Cache input field.
	if s.hitSettings(s.sCacheR.X+2, s.sCacheR.Y+2).Kind != HitFocusCache {
		t.Fatal("cache hit")
	}
	// A subscription chip -> remove.
	c := s.sChips[0]
	if h := s.hitSettings(c.rect.X+2, c.rect.Y+2); h.Kind != HitRemoveSub || h.Sub != 0 {
		t.Fatalf("chip hit = %+v", h)
	}
	// Each button kind resolves through the generic loop.
	kinds := map[HitKind]bool{}
	for _, b := range s.sButtons {
		h := s.hitSettings(b.rect.X+2, b.rect.Y+2)
		kinds[h.Kind] = true
	}
	for _, want := range []HitKind{HitSelectProfile, HitNewProfile, HitRenameProfile, HitDeleteProfile, HitSelectKind, HitAddSub, HitTheme} {
		if !kinds[want] {
			t.Fatalf("button kind %d never hit", want)
		}
	}
	// A miss returns HitNone.
	if s.hitSettings(s.W-1, s.H-1).Kind != HitNone {
		t.Fatal("settings miss should be none")
	}

	// Draw the settings view (snapshot) in several states to cover input
	// placeholder + focused caret and the active/danger button styles.
	renderPNG(t, s, "settings")
	s.SelectKind(source.Reddit)
	s.FocusChannel()
	s.channelInput = "golang"
	s.FocusRename() // rename focused with text -> caret branch
	s.renameInput = "Home"
	renderPNG(t, s, "settings-typing")
}

func TestSettingsEdgeCases(t *testing.T) {
	// HitTest dispatches to hitSettings while in ModeSettings.
	s := profScene()
	s.OpenSettings()
	s.layoutSettings() // populate sDoneR
	if s.HitTest(s.sDoneR.X+2, s.sDoneR.Y+2).Kind != HitCloseSettings {
		t.Fatal("HitTest should route to settings while in ModeSettings")
	}

	// Chip-wrap branch: many subs in a narrow window force a new chip row.
	narrow := New(400, 640, ThemeFor(OSLinux, false))
	var many []source.Subscription
	for i := 0; i < 8; i++ {
		many = append(many, source.Subscription{Source: source.Reddit, Channel: "somelongchannel", Limit: 25})
	}
	narrow.SetProfiles([]settings.Profile{{Name: "P", Subs: many}}, 0)
	narrow.OpenSettings()
	narrow.layoutSettings()
	rows := map[int]bool{}
	for _, c := range narrow.sChips {
		rows[c.rect.Y] = true
	}
	if len(rows) < 2 {
		t.Fatalf("chips did not wrap: %d rows", len(rows))
	}
	renderPNG(t, narrow, "settings-wrapped")
}

// TestSettingsRenderCacheControl covers the render-cache row: the caption's
// non-nil stats branch and the "Clear render cache" button's hit routing.
func TestSettingsRenderCacheControl(t *testing.T) {
	s := profScene()
	s.SetRenderCacheStats(func() (pages, bytes int) { return 3, 200 << 20 })
	s.OpenSettings()
	s.layoutSettings()

	found := false
	for _, b := range s.sButtons {
		if b.kind != HitClearRenderCache {
			continue
		}
		if s.hitSettings(b.rect.X+2, b.rect.Y+2).Kind == HitClearRenderCache {
			found = true
		}
	}
	if !found {
		t.Fatal("no Clear-render-cache button hit region")
	}
	// Draw with a populated cache caption (the non-nil stats branch).
	renderPNG(t, s, "settings-render-cache")
}

func TestItoaAndTitleCase(t *testing.T) {
	if itoa(0) != "0" || itoa(42) != "42" {
		t.Fatalf("itoa: %q %q", itoa(0), itoa(42))
	}
	if titleCase("") != "" || titleCase("dark") != "Dark" {
		t.Fatalf("titleCase: %q %q", titleCase(""), titleCase("dark"))
	}
}

func TestSidebarProfileTabsAndDraw(t *testing.T) {
	s := profScene()
	s.SetItems(sampleItems())
	m := s.computeMetrics()
	s.HitTest(0, 0) // trigger layout so profTabs is populated
	// A click on the second profile tab switches profiles.
	tab := s.profTabs[1]
	if h := s.HitTest(tab.rect.X+2, tab.rect.Y+2); h.Kind != HitProfile || h.Profile != 1 {
		t.Fatalf("profile tab hit = %+v", h)
	}
	// The subscription rows sit below the tab band.
	firstSubY := m.topbarH + m.profileTabH + m.sideItemH/2
	if h := s.HitTest(10, firstSubY); h.Kind != HitSub || h.Sub != AllFilter {
		t.Fatalf("All row under tabs = %+v", h)
	}
	// Draw with tabs + an active tab highlight.
	renderPNG(t, s, "feed-profiles")
}
