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
	s.SetAccount(Account{Kind: source.Reddit, Fields: map[string]string{"client_id": "a"}})
	if len(s.Accounts) != 1 {
		t.Fatalf("append expected, got %d", len(s.Accounts))
	}
	// Upsert replaces in place rather than appending a duplicate.
	s.SetAccount(Account{Kind: source.Reddit, Fields: map[string]string{"client_id": "b"}})
	if len(s.Accounts) != 1 {
		t.Fatalf("upsert should not append: %d", len(s.Accounts))
	}
	got, ok := s.Account(source.Reddit)
	if !ok || got.Fields["client_id"] != "b" {
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
			{Kind: source.Reddit, Fields: map[string]string{"client_id": "first"}},
			{Kind: source.Reddit, Fields: map[string]string{"client_id": "dup"}},
			{Kind: "", Fields: map[string]string{"x": "y"}},
			{Kind: source.Mastodon, Fields: map[string]string{"instance": "m"}},
		},
	}
	s.Normalize()
	if len(s.Accounts) != 2 {
		t.Fatalf("dedup => %d accounts: %+v", len(s.Accounts), s.Accounts)
	}
	r, _ := s.Account(source.Reddit)
	if r.Fields["client_id"] != "first" {
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
	// Reddit exposes the four OAuth fields, with the two secrets masked.
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
	for _, k := range []string{"client_id", "client_secret", "username", "password"} {
		if _, ok := keys[k]; !ok {
			t.Fatalf("reddit missing field %q", k)
		}
	}
	if !keys["client_secret"].Secret || !keys["password"].Secret {
		t.Fatal("reddit secrets should be masked")
	}
	if keys["client_id"].Secret {
		t.Fatal("client_id should not be secret")
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
	in.SetAccount(Account{Kind: source.Reddit, Fields: map[string]string{"client_id": "id", "client_secret": "sec"}})
	if err := st.Save(in); err != nil {
		t.Fatal(err)
	}
	out, err := st.Load()
	if err != nil {
		t.Fatal(err)
	}
	got, ok := out.Account(source.Reddit)
	if !ok || got.Fields["client_secret"] != "sec" {
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
