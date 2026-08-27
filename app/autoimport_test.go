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
