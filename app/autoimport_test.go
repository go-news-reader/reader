package app

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-news-reader/reader/feeds"
	"github.com/go-news-reader/reader/internal/settings"
	"github.com/go-news-reader/reader/source"
	"github.com/go-news-reader/reader/ui"
)

// errImportProbe is a sentinel browser-import failure for the error path.
var errImportProbe = errors.New("probe: no browser session")

// autoImportApp builds an app around set with a captured registry rebuild and the
// given browser session, ready to exercise AutoImportSessions.
func autoImportApp(t *testing.T, set *settings.Settings, session string, gotOpts *feeds.Options) *App {
	t.Helper()
	path := filepath.Join(t.TempDir(), "s.json")
	a := New(Config{
		Registry: newReg(fakeProv{kind: source.Twitter}), Settings: set,
		Store: testStore(t, path), OS: ui.OSMac,
	})
	a.SetRegistryBuilder(func(o feeds.Options) *source.Registry { *gotOpts = o; return newReg() })
	a.SetRefreshHook(func() {})
	a.SetCookieFinder(fakeCookieFinder{session: session})
	return a
}

func subProfile(subs ...source.Subscription) []settings.Profile {
	return []settings.Profile{{Name: "Home", Subs: subs}}
}

// TestActivateAfterVault covers the deferred startup the window fires once it is
// on screen: the blocking vault read runs on a goroutine and the state it enables
// is applied on the render thread via post, so the secret (read only now, not at
// load) hydrates into the accounts, rebuildRegistry hands it to the authenticated
// provider, and onLoaded runs — all off the render thread's blocking path.
func TestActivateAfterVault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	st := testStore(t, path)
	// Persist a Reddit account whose secret goes into the vault and is purged from
	// disk, exactly as a real prior run left it.
	seed := &settings.Settings{
		Profiles: subProfile(source.Subscription{Source: source.Reddit, Channel: "golang"}),
		Active:   0, Theme: settings.ThemeSystem,
		Accounts: []settings.Account{{Kind: source.Reddit, Fields: map[string]string{"session_cookie": "abc"}}},
	}
	if err := st.Save(seed); err != nil {
		t.Fatal(err)
	}
	// The window path loads WITHOUT secrets: the account is present but its secret
	// field is empty until ActivateAfterVault reads the vault.
	set, err := st.LoadWithoutSecrets()
	if err != nil {
		t.Fatal(err)
	}
	if acct, _ := set.Account(source.Reddit); acct.Fields["session_cookie"] != "" {
		t.Fatalf("secret should not be loaded yet: %+v", acct.Fields)
	}

	var gotOpts feeds.Options
	a := New(Config{Registry: newReg(), Settings: set, Store: st, OS: ui.OSMac})
	a.SetRegistryBuilder(func(o feeds.Options) *source.Registry { gotOpts = o; return newReg() })
	a.SetRefreshHook(func() {})
	a.SetCookieFinder(fakeCookieFinder{}) // no browser session: AutoImportSessions imports nothing
	// ActivateAfterVault applies its state on the render thread via post, exactly
	// as the window front-end drives it; defer scene writes so post enqueues and
	// Frame (below) drains it on this goroutine standing in for the render thread.
	a.DeferSceneWrites()

	loaded := make(chan struct{})
	a.ActivateAfterVault(func() { close(loaded) })

	// The keychain read runs on a goroutine, then the activation (and onLoaded) is
	// posted; drive Frame on the render thread until that posted work has run.
	waitFor(t, func() bool {
		a.Frame()
		select {
		case <-loaded:
			return true
		default:
			return false
		}
	})

	if acct, ok := a.set.Account(source.Reddit); !ok || acct.Fields["session_cookie"] != "abc" {
		t.Fatalf("secret not hydrated from the vault: %+v ok=%v", acct, ok)
	}
	if gotOpts.RedditSessionCookie != "abc" {
		t.Fatalf("rebuilt options RedditSessionCookie = %q, want abc (registry not reactivated)", gotOpts.RedditSessionCookie)
	}
}

func TestAutoImportSessionsFillsMissingAccount(t *testing.T) {
	set := &settings.Settings{
		Profiles: subProfile(source.Subscription{Source: source.Twitter, Channel: "nasa"}),
		Active:   0, Theme: settings.ThemeSystem,
	}
	var gotOpts feeds.Options
	a := autoImportApp(t, set, "SESSION-STR", &gotOpts)

	a.AutoImportSessions()

	if gotOpts.TwitterSession != "SESSION-STR" {
		t.Fatalf("rebuilt options TwitterSession = %q, want SESSION-STR", gotOpts.TwitterSession)
	}
	loaded := a.set
	acct, ok := loaded.Account(source.Twitter)
	if !ok || acct.Fields["session"] != "SESSION-STR" {
		t.Fatalf("session not stored: %+v ok=%v", acct, ok)
	}
	if got := a.VM().Status.Get(); got == "" || !strings.Contains(got, "Imported") {
		t.Fatalf("status = %q", got)
	}
}

func TestAutoImportSessionsTikTokSplitsFields(t *testing.T) {
	set := &settings.Settings{
		Profiles: subProfile(source.Subscription{Source: source.TikTok, Channel: "someone"}),
		Active:   0, Theme: settings.ThemeSystem,
	}
	var gotOpts feeds.Options
	a := autoImportApp(t, set, "sessionid=SID; msToken=MTOK", &gotOpts)

	a.AutoImportSessions()

	if gotOpts.TikTokSession != "SID" || gotOpts.TikTokMSToken != "MTOK" {
		t.Fatalf("tiktok fields = %q / %q, want SID / MTOK", gotOpts.TikTokSession, gotOpts.TikTokMSToken)
	}
}

func TestAutoImportSessionsSkips(t *testing.T) {
	twSub := source.Subscription{Source: source.Twitter, Channel: "nasa"}
	off := false
	cases := []struct {
		name    string
		set     *settings.Settings
		session string
	}{
		{
			name: "disabled by setting",
			set: &settings.Settings{
				Profiles: subProfile(twSub), Active: 0, Theme: settings.ThemeSystem,
				AutoImportSessions: &off,
			},
			session: "SESSION-STR",
		},
		{
			name:    "no subscription of that kind",
			set:     &settings.Settings{Profiles: subProfile(), Active: 0, Theme: settings.ThemeSystem},
			session: "SESSION-STR",
		},
		{
			name: "account already configured",
			set: &settings.Settings{
				Profiles: subProfile(twSub), Active: 0, Theme: settings.ThemeSystem,
				Accounts: []settings.Account{{Kind: source.Twitter, Fields: map[string]string{"session": "OLD"}}},
			},
			session: "SESSION-STR",
		},
		{
			name:    "browser holds no session",
			set:     &settings.Settings{Profiles: subProfile(twSub), Active: 0, Theme: settings.ThemeSystem},
			session: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotOpts feeds.Options
			a := autoImportApp(t, tc.set, tc.session, &gotOpts)
			a.AutoImportSessions()
			if gotOpts.TwitterSession != "" {
				t.Fatalf("expected no import, but rebuilt with TwitterSession=%q", gotOpts.TwitterSession)
			}
			if got := a.VM().Status.Get(); strings.Contains(got, "Imported") {
				t.Fatalf("expected no import status, got %q", got)
			}
		})
	}
}

func TestAutoImportSessionsImportError(t *testing.T) {
	set := &settings.Settings{
		Profiles: subProfile(source.Subscription{Source: source.Twitter, Channel: "nasa"}),
		Active:   0, Theme: settings.ThemeSystem,
	}
	path := filepath.Join(t.TempDir(), "s.json")
	var gotOpts feeds.Options
	a := New(Config{Registry: newReg(fakeProv{kind: source.Twitter}), Settings: set, Store: testStore(t, path), OS: ui.OSMac})
	a.SetRegistryBuilder(func(o feeds.Options) *source.Registry { gotOpts = o; return newReg() })
	a.SetRefreshHook(func() {})
	a.SetCookieFinder(fakeCookieFinder{err: errImportProbe})

	a.AutoImportSessions()

	if gotOpts.TwitterSession != "" {
		t.Fatalf("an import error must not apply a session, got %q", gotOpts.TwitterSession)
	}
}
