package settings

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/go-news-reader/reader/source"
)

func TestDefault(t *testing.T) {
	d := Default()
	if len(d.Profiles) != 1 || d.Profiles[0].Name != "Home" {
		t.Fatalf("default profiles = %+v", d.Profiles)
	}
	if d.Active != 0 || d.Theme != ThemeSystem {
		t.Errorf("default scalars = %+v", d)
	}
	// Reddit is back by default, alongside Hacker News.
	var hasReddit, hasHN bool
	for _, s := range d.Profiles[0].Subs {
		switch s.Source {
		case source.Reddit:
			hasReddit = true
		case source.HackerNews:
			hasHN = true
		}
	}
	if !hasReddit || !hasHN {
		t.Fatalf("default subs missing reddit/hn: %+v", d.Profiles[0].Subs)
	}
}

func TestZoomKeyDefaultsAndRoundTrip(t *testing.T) {
	// Default seeds the real-browser zoom keys.
	if d := Default(); d.ZoomInKey != DefaultZoomInKey || d.ZoomOutKey != DefaultZoomOutKey {
		t.Fatalf("default zoom keys = %q / %q", d.ZoomInKey, d.ZoomOutKey)
	}
	// Normalize backfills blanks (e.g. a settings file predating these fields).
	s := &Settings{Active: 0}
	s.Normalize()
	if s.ZoomInKey != DefaultZoomInKey || s.ZoomOutKey != DefaultZoomOutKey {
		t.Fatalf("normalized zoom keys = %q / %q", s.ZoomInKey, s.ZoomOutKey)
	}
	// A non-default value survives Normalize.
	s2 := &Settings{Profiles: []Profile{{Name: "x"}}, Theme: ThemeDark, CachePath: "/c", ZoomInKey: "+", ZoomOutKey: "_"}
	s2.Normalize()
	if s2.ZoomInKey != "+" || s2.ZoomOutKey != "_" {
		t.Fatalf("normalize clobbered custom zoom keys: %q / %q", s2.ZoomInKey, s2.ZoomOutKey)
	}
	// Save/Load round-trips the two keys.
	p := filepath.Join(t.TempDir(), "s.json")
	st := NewStore(p)
	if err := st.Save(&Settings{Profiles: []Profile{{Name: "P"}}, Theme: ThemeSystem, CachePath: "/c", ZoomInKey: "+", ZoomOutKey: "_"}); err != nil {
		t.Fatal(err)
	}
	out, err := st.Load()
	if err != nil {
		t.Fatal(err)
	}
	if out.ZoomInKey != "+" || out.ZoomOutKey != "_" {
		t.Fatalf("round-trip zoom keys = %q / %q", out.ZoomInKey, out.ZoomOutKey)
	}
}

func TestBrowserSingleTabDefaultAndRoundTrip(t *testing.T) {
	// Default seeds single-tab, so a fresh install shows no preview tab strip.
	d := Default()
	if d.BrowserSingleTab == nil || !*d.BrowserSingleTab || !d.SingleTab() {
		t.Fatalf("default tab mode = %v, want single-tab", d.BrowserSingleTab)
	}
	// Normalize backfills an unset field (a settings file predating it) to
	// single-tab.
	s := &Settings{}
	if s.SingleTab() != DefaultBrowserSingleTab {
		t.Fatal("SingleTab() on an unset field should apply the default")
	}
	s.Normalize()
	if s.BrowserSingleTab == nil || !*s.BrowserSingleTab {
		t.Fatal("Normalize should backfill an unset tab mode to single-tab")
	}
	// An explicit opt-out to multiple tabs survives Normalize and Save/Load,
	// rather than being re-defaulted to single-tab.
	multi := false
	s2 := &Settings{Profiles: []Profile{{Name: "x"}}, Theme: ThemeDark, CachePath: "/c", BrowserSingleTab: &multi}
	s2.Normalize()
	if s2.BrowserSingleTab == nil || *s2.BrowserSingleTab || s2.SingleTab() {
		t.Fatal("Normalize clobbered an explicit multi-tab opt-out")
	}
	p := filepath.Join(t.TempDir(), "s.json")
	st := NewStore(p)
	if err := st.Save(s2); err != nil {
		t.Fatal(err)
	}
	out, err := st.Load()
	if err != nil {
		t.Fatal(err)
	}
	if out.BrowserSingleTab == nil || *out.BrowserSingleTab {
		t.Fatalf("round-trip tab mode = %v, want persisted multi-tab", out.BrowserSingleTab)
	}
}

func TestInfiniteScrollDefaultAndRoundTrip(t *testing.T) {
	// Default seeds infinite scroll enabled.
	d := Default()
	if d.InfiniteScroll == nil || !*d.InfiniteScroll || !d.InfiniteScrollEnabled() {
		t.Fatalf("default infinite scroll = %v, want enabled", d.InfiniteScroll)
	}
	// InfiniteScrollEnabled() on an unset field applies the enabled default.
	s := &Settings{}
	if s.InfiniteScrollEnabled() != DefaultInfiniteScroll {
		t.Fatal("InfiniteScrollEnabled() on an unset field should apply the default")
	}
	// Normalize backfills an unset field (a settings file predating it) to enabled.
	s.Normalize()
	if s.InfiniteScroll == nil || !*s.InfiniteScroll {
		t.Fatal("Normalize should backfill an unset infinite-scroll flag to enabled")
	}
	// An explicit opt-out to off survives Normalize and Save/Load.
	off := false
	s2 := &Settings{Profiles: []Profile{{Name: "x"}}, Theme: ThemeDark, CachePath: "/c", InfiniteScroll: &off}
	s2.Normalize()
	if s2.InfiniteScroll == nil || *s2.InfiniteScroll || s2.InfiniteScrollEnabled() {
		t.Fatal("Normalize clobbered an explicit infinite-scroll opt-out")
	}
	p := filepath.Join(t.TempDir(), "s.json")
	st := NewStore(p)
	if err := st.Save(s2); err != nil {
		t.Fatal(err)
	}
	out, err := st.Load()
	if err != nil {
		t.Fatal(err)
	}
	if out.InfiniteScroll == nil || *out.InfiniteScroll {
		t.Fatalf("round-trip infinite scroll = %v, want persisted off", out.InfiniteScroll)
	}
}

func TestSignInBrowserDefaultAndRoundTrip(t *testing.T) {
	// A fresh install defaults to Firefox — the only browser whose session cookie
	// the reader can subsequently import.
	if d := Default(); d.SignInBrowser != SignInBrowserFirefox {
		t.Fatalf("default sign-in browser = %q, want firefox", d.SignInBrowser)
	}
	// The exported default constant is Firefox.
	if DefaultSignInBrowser != SignInBrowserFirefox {
		t.Fatalf("DefaultSignInBrowser = %q, want firefox", DefaultSignInBrowser)
	}
	// Normalize backfills a blank field (a settings file predating it) and repairs
	// an unrecognised value to the default.
	for _, in := range []string{"", "mosaic"} {
		s := &Settings{SignInBrowser: in}
		s.Normalize()
		if s.SignInBrowser != DefaultSignInBrowser {
			t.Fatalf("Normalize(%q) => %q, want firefox", in, s.SignInBrowser)
		}
	}
	// Every recognised value is preserved by Normalize and survives Save/Load.
	for _, v := range []string{
		SignInBrowserDefault, SignInBrowserFirefox, SignInBrowserChrome,
		SignInBrowserSafari, SignInBrowserEdge,
	} {
		if !ValidSignInBrowser(v) {
			t.Fatalf("ValidSignInBrowser(%q) = false", v)
		}
		s := &Settings{Profiles: []Profile{{Name: "x"}}, Theme: ThemeDark, CachePath: "/c", SignInBrowser: v}
		s.Normalize()
		if s.SignInBrowser != v {
			t.Fatalf("Normalize clobbered sign-in browser %q => %q", v, s.SignInBrowser)
		}
		p := filepath.Join(t.TempDir(), "s.json")
		st := NewStore(p)
		if err := st.Save(s); err != nil {
			t.Fatal(err)
		}
		out, err := st.Load()
		if err != nil {
			t.Fatal(err)
		}
		if out.SignInBrowser != v {
			t.Fatalf("round-trip sign-in browser = %q, want %q", out.SignInBrowser, v)
		}
	}
}

func TestActiveProfile(t *testing.T) {
	s := &Settings{Profiles: []Profile{{Name: "A"}, {Name: "B"}}}
	if s.ActiveProfile().Name != "A" {
		t.Error("active 0 should be A")
	}
	s.Active = 1
	if s.ActiveProfile().Name != "B" {
		t.Error("active 1 should be B")
	}
	s.Active = 99 // out of range -> first
	if s.ActiveProfile().Name != "A" {
		t.Error("oob active should fall back to first")
	}
	empty := &Settings{} // empty list -> synthetic
	if empty.ActiveProfile().Name != "All" {
		t.Errorf("empty => %+v", empty.ActiveProfile())
	}
}

func TestNormalize(t *testing.T) {
	s := &Settings{Active: -5}
	s.Normalize()
	if len(s.Profiles) == 0 || s.Active != 0 || s.Theme != ThemeSystem || s.CachePath == "" {
		t.Errorf("normalize => %+v", s)
	}
	// Valid values are preserved.
	s2 := &Settings{Profiles: []Profile{{Name: "x"}}, Theme: ThemeDark, Active: 0, CachePath: "/keep"}
	s2.Normalize()
	if s2.Theme != ThemeDark || s2.CachePath != "/keep" {
		t.Errorf("valid values changed: %+v", s2)
	}
	// Out-of-range active with a non-empty list clamps to 0.
	s3 := &Settings{Profiles: []Profile{{Name: "a"}, {Name: "b"}}, Active: 7, Theme: ThemeLight}
	s3.Normalize()
	if s3.Active != 0 || s3.Theme != ThemeLight {
		t.Errorf("clamp => %+v", s3)
	}
}

func TestMediaCacheBytes(t *testing.T) {
	cases := []struct {
		name string
		mb   int
		want int64
	}{
		{"default (zero)", 0, int64(DefaultMediaCacheMB) << 20},
		{"clamp low", MinMediaCacheMB - 5, int64(MinMediaCacheMB) << 20},
		{"clamp high", MaxMediaCacheMB + 100, int64(MaxMediaCacheMB) << 20},
		{"in range", 512, 512 << 20},
	}
	for _, c := range cases {
		s := Settings{MediaCacheMB: c.mb}
		if got := s.MediaCacheBytes(); got != c.want {
			t.Errorf("%s: MediaCacheBytes(%d) = %d, want %d", c.name, c.mb, got, c.want)
		}
	}
}

func TestIsCachePlugin(t *testing.T) {
	cases := []struct {
		backend string
		want    bool
	}{
		{"", false},          // unset: built-in disk cache
		{"local", false},     // explicit built-in
		{"  local  ", false}, // trimmed to "local"
		{"   ", false},       // whitespace-only trims to empty
		{"/opt/cache", true}, // a filesystem path: a plugin
		{"./cache-plugin", true},
		{"cache-plugin", true},
	}
	for _, c := range cases {
		if got := IsCachePlugin(c.backend); got != c.want {
			t.Errorf("IsCachePlugin(%q) = %v, want %v", c.backend, got, c.want)
		}
	}
}

func TestNormalizeMediaCacheMB(t *testing.T) {
	// 0 (a settings file predating the field) backfills to the default.
	s := &Settings{}
	s.Normalize()
	if s.MediaCacheMB != DefaultMediaCacheMB {
		t.Errorf("zero => %d, want default %d", s.MediaCacheMB, DefaultMediaCacheMB)
	}
	// Below the floor clamps up; above the ceiling clamps down; an in-range value
	// is preserved.
	for _, c := range []struct{ in, want int }{
		{MinMediaCacheMB - 1, MinMediaCacheMB},
		{MaxMediaCacheMB + 1, MaxMediaCacheMB},
		{1024, 1024},
	} {
		s2 := &Settings{MediaCacheMB: c.in}
		s2.Normalize()
		if s2.MediaCacheMB != c.want {
			t.Errorf("Normalize MediaCacheMB %d => %d, want %d", c.in, s2.MediaCacheMB, c.want)
		}
	}
}

func TestDefaultCachePathError(t *testing.T) {
	// Clear every var os.UserCacheDir consults so it fails on this platform.
	for _, k := range []string{"HOME", "XDG_CACHE_HOME", "AppData"} {
		t.Setenv(k, "")
	}
	if defaultCachePath() != "" {
		t.Skip("UserCacheDir still resolved on this platform")
	}
}

func TestStoreLoadMissingReturnsDefault(t *testing.T) {
	st := NewStore(filepath.Join(t.TempDir(), "nope.json"))
	s, err := st.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Profiles) != 1 {
		t.Errorf("missing file should give defaults, got %+v", s)
	}
}

func TestStoreLoadReadError(t *testing.T) {
	// A directory path exists (not IsNotExist) but is not a readable file.
	if _, err := NewStore(t.TempDir()).Load(); err == nil {
		t.Error("reading a directory should error")
	}
}

func TestStoreLoadCorrupt(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.json")
	os.WriteFile(p, []byte("{not json"), 0o600)
	if _, err := NewStore(p).Load(); err == nil {
		t.Error("corrupt file should error")
	}
}

func TestStoreSaveLoadRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "settings.json")
	st := NewStore(p)
	in := &Settings{
		Profiles: []Profile{{Name: "Work", Subs: []source.Subscription{
			{Source: source.Reddit, Channel: "golang", Sort: "top", Limit: 10},
		}}},
		Active: 0, Theme: ThemeDark, CachePath: "/tmp/cache",
	}
	if err := st.Save(in); err != nil {
		t.Fatal(err)
	}
	out, err := st.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in.Profiles, out.Profiles) || out.Theme != ThemeDark || out.CachePath != "/tmp/cache" {
		t.Errorf("round trip: in=%+v out=%+v", in, out)
	}
}

func TestStoreSaveMkdirError(t *testing.T) {
	base := t.TempDir()
	blocker := filepath.Join(base, "blocker")
	os.WriteFile(blocker, []byte("x"), 0o600)
	if err := NewStore(filepath.Join(blocker, "settings.json")).Save(&Settings{}); err == nil {
		t.Error("mkdir over a file should error")
	}
}

func TestStoreSaveOpenError(t *testing.T) {
	// An existing directory can't be opened for writing.
	if err := NewStore(t.TempDir()).Save(&Settings{}); err == nil {
		t.Error("saving onto a directory should error")
	}
}

func TestStoreSaveNoDirPart(t *testing.T) {
	d := t.TempDir()
	cwd, _ := os.Getwd()
	os.Chdir(d)
	defer os.Chdir(cwd)
	if err := NewStore("bare.json").Save(&Settings{Theme: ThemeSystem}); err != nil {
		t.Fatalf("save bare filename: %v", err)
	}
	if _, err := os.Stat(filepath.Join(d, "bare.json")); err != nil {
		t.Error("bare file not written")
	}
}

func TestDefaultPath(t *testing.T) {
	p, err := DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(p) != "settings.json" || !filepath.IsAbs(p) {
		t.Errorf("DefaultPath = %q", p)
	}
}

func TestAccountLookupAndUpsert(t *testing.T) {
	s := &Settings{}
	if _, ok := s.Account(source.Reddit); ok {
		t.Fatal("empty settings should have no reddit account")
	}
	s.SetAccount(Account{Kind: source.Reddit, Fields: map[string]string{"session_cookie": "a"}})
	if len(s.Accounts) != 1 {
		t.Fatalf("append expected, got %d", len(s.Accounts))
	}
	// Upsert replaces in place rather than appending a duplicate.
	s.SetAccount(Account{Kind: source.Reddit, Fields: map[string]string{"session_cookie": "b"}})
	if len(s.Accounts) != 1 {
		t.Fatalf("upsert should not append: %d", len(s.Accounts))
	}
	got, ok := s.Account(source.Reddit)
	if !ok || got.Fields["session_cookie"] != "b" {
		t.Fatalf("account = %+v ok=%v", got, ok)
	}
	// A second provider appends.
	s.SetAccount(Account{Kind: source.Mastodon, Fields: map[string]string{"instance": "https://m"}})
	if len(s.Accounts) != 2 {
		t.Fatalf("second provider should append: %d", len(s.Accounts))
	}
	if _, ok := s.Account(source.Lemmy); ok {
		t.Fatal("lemmy lookup should miss")
	}
}

func TestNormalizeDedupAccounts(t *testing.T) {
	// Empty account list is left untouched (early return).
	s0 := &Settings{Profiles: []Profile{{Name: "x"}}, Theme: ThemeDark, CachePath: "/c"}
	s0.Normalize()
	if len(s0.Accounts) != 0 {
		t.Fatalf("empty accounts should stay empty: %+v", s0.Accounts)
	}
	// Duplicate kinds collapse to the first; blank kinds are dropped.
	s := &Settings{
		Profiles:  []Profile{{Name: "x"}},
		Theme:     ThemeDark,
		CachePath: "/c",
		Accounts: []Account{
			{Kind: source.Reddit, Fields: map[string]string{"session_cookie": "first"}},
			{Kind: source.Reddit, Fields: map[string]string{"session_cookie": "dup"}},
			{Kind: "", Fields: map[string]string{"x": "y"}},
			{Kind: source.Mastodon, Fields: map[string]string{"instance": "m"}},
		},
	}
	s.Normalize()
	if len(s.Accounts) != 2 {
		t.Fatalf("dedup => %d accounts: %+v", len(s.Accounts), s.Accounts)
	}
	r, _ := s.Account(source.Reddit)
	if r.Fields["session_cookie"] != "first" {
		t.Fatalf("first duplicate should win: %+v", r)
	}
	if _, ok := s.Account(source.Mastodon); !ok {
		t.Fatal("mastodon account dropped")
	}
}

func TestCredentialSchema(t *testing.T) {
	sc := CredentialSchema()
	if len(sc) == 0 || sc[0].Kind != source.Reddit {
		t.Fatalf("schema should start with reddit: %+v", sc)
	}
	// Reddit exposes only the session-cookie field (OAuth was removed), masked.
	var reddit ProviderCreds
	for _, pc := range sc {
		if pc.Kind == source.Reddit {
			reddit = pc
		}
	}
	keys := map[string]CredField{}
	for _, f := range reddit.Fields {
		keys[f.Key] = f
	}
	if len(reddit.Fields) != 1 {
		t.Fatalf("reddit should expose exactly one field, got %+v", reddit.Fields)
	}
	if _, ok := keys["session_cookie"]; !ok {
		t.Fatal("reddit missing session_cookie field")
	}
	if !keys["session_cookie"].Secret {
		t.Fatal("reddit session_cookie should be masked")
	}
	// The removed OAuth fields must not resurface.
	for _, k := range []string{"client_id", "client_secret", "username", "password"} {
		if _, ok := keys[k]; ok {
			t.Fatalf("reddit should no longer expose OAuth field %q", k)
		}
	}
	// Usenet exposes a bool TLS toggle.
	var tlsBool bool
	for _, pc := range sc {
		if pc.Kind == source.Usenet {
			for _, f := range pc.Fields {
				if f.Key == "tls" && f.Bool {
					tlsBool = true
				}
			}
		}
	}
	if !tlsBool {
		t.Fatal("usenet tls should be a bool field")
	}
}

func TestAccountsRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.json")
	st := NewStore(p)
	in := Default()
	in.SetAccount(Account{Kind: source.Reddit, Fields: map[string]string{"session_cookie": "reddit_session=sec"}})
	if err := st.Save(in); err != nil {
		t.Fatal(err)
	}
	out, err := st.Load()
	if err != nil {
		t.Fatal(err)
	}
	got, ok := out.Account(source.Reddit)
	if !ok || got.Fields["session_cookie"] != "reddit_session=sec" {
		t.Fatalf("account not persisted: %+v ok=%v", got, ok)
	}
}

func TestDefaultPathError(t *testing.T) {
	for _, k := range []string{"HOME", "XDG_CONFIG_HOME", "AppData"} {
		t.Setenv(k, "")
	}
	if _, err := DefaultPath(); err == nil {
		t.Skip("UserConfigDir still resolved on this platform")
	}
}

func TestPreviewTextScaleClampAndDefault(t *testing.T) {
	// Default seeds the larger-than-base reader text scale.
	if d := Default(); d.PreviewTextScale != DefaultPreviewTextScale {
		t.Fatalf("default preview text scale = %v, want %v", d.PreviewTextScale, DefaultPreviewTextScale)
	}
	cases := []struct{ in, want float64 }{
		{0, DefaultPreviewTextScale},  // unset (old file) → default
		{-3, DefaultPreviewTextScale}, // negative → default
		{0.5, MinPreviewTextScale},    // below floor → clamp up
		{MinPreviewTextScale, MinPreviewTextScale},
		{1.3, 1.3}, // in range → kept
		{MaxPreviewTextScale, MaxPreviewTextScale},
		{9, MaxPreviewTextScale}, // above ceiling → clamp down
	}
	for _, c := range cases {
		if got := ClampPreviewTextScale(c.in); got != c.want {
			t.Errorf("ClampPreviewTextScale(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestPreviewTextScaleNormalizeAndRoundTrip(t *testing.T) {
	// Normalize clamps an out-of-range value from a hand-edited file.
	s := Default()
	s.PreviewTextScale = 99
	s.Normalize()
	if s.PreviewTextScale != MaxPreviewTextScale {
		t.Fatalf("Normalize did not clamp: %v", s.PreviewTextScale)
	}
	// A settings file predating the field (0) backfills the default on load.
	s.PreviewTextScale = 0
	s.Normalize()
	if s.PreviewTextScale != DefaultPreviewTextScale {
		t.Fatalf("Normalize did not backfill default: %v", s.PreviewTextScale)
	}

	// Full JSON round-trip through the store preserves an in-range value.
	dir := t.TempDir()
	st := NewStore(filepath.Join(dir, "settings.json"))
	want := Default()
	want.PreviewTextScale = 1.75
	if err := st.Save(want); err != nil {
		t.Fatal(err)
	}
	out, err := st.Load()
	if err != nil {
		t.Fatal(err)
	}
	if out.PreviewTextScale != 1.75 {
		t.Fatalf("round-trip preview text scale = %v, want 1.75", out.PreviewTextScale)
	}
}

func TestSourceFetchConcurrency(t *testing.T) {
	cases := []struct {
		name string
		n    int
		want int
	}{
		{"default (zero)", 0, DefaultSourceConcurrency},
		{"floor (one kept)", 1, 1},
		{"clamp high", MaxSourceConcurrency + 50, MaxSourceConcurrency},
		{"in range", 6, 6},
	}
	for _, c := range cases {
		s := Settings{SourceConcurrency: c.n}
		if got := s.SourceFetchConcurrency(); got != c.want {
			t.Errorf("%s: SourceFetchConcurrency(%d) = %d, want %d", c.name, c.n, got, c.want)
		}
	}
}
