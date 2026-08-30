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
	// Biometric unlock defaults ON; stub the gate so this hydration test does not
	// reach the real LocalAuthentication prompt (see TestActivateAfterVaultBiometric*
	// for the gate's own allow/deny/off coverage).
	old := biometricAuth
	biometricAuth = func(string) error { return nil }
	defer func() { biometricAuth = old }()

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

// activateVaultApp builds an app around a vault that holds a purged Reddit
// secret (as a real prior run left it), with biometric unlock set to enabled,
// ready to exercise the ActivateAfterVault gate. It returns the app; the caller
// substitutes biometricAuth before driving.
func activateVaultApp(t *testing.T, biometricEnabled, primed bool) *App {
	t.Helper()
	path := filepath.Join(t.TempDir(), "s.json")
	st := testStore(t, path)
	enabled := biometricEnabled
	seed := &settings.Settings{
		Profiles: subProfile(source.Subscription{Source: source.Reddit, Channel: "golang"}),
		Active:   0, Theme: settings.ThemeSystem,
		BiometricUnlock: &enabled,
		BiometricPrimed: primed,
		Accounts:        []settings.Account{{Kind: source.Reddit, Fields: map[string]string{"session_cookie": "abc"}}},
	}
	if err := st.Save(seed); err != nil {
		t.Fatal(err)
	}
	set, err := st.LoadWithoutSecrets()
	if err != nil {
		t.Fatal(err)
	}
	a := New(Config{Registry: newReg(), Settings: set, Store: st, OS: ui.OSMac})
	a.SetRegistryBuilder(func(o feeds.Options) *source.Registry { return newReg() })
	a.SetRefreshHook(func() {})
	a.SetCookieFinder(fakeCookieFinder{})
	a.DeferSceneWrites()
	return a
}

// driveActivate runs ActivateAfterVault and drains the posted work on this
// goroutine (standing in for the render thread) until onLoaded fires.
func driveActivate(t *testing.T, a *App) {
	t.Helper()
	loaded := make(chan struct{})
	a.ActivateAfterVault(func() { close(loaded) })
	waitFor(t, func() bool {
		a.Frame()
		select {
		case <-loaded:
			return true
		default:
			return false
		}
	})
}

// TestActivateAfterVaultBiometricAllows: with biometric unlock ENABLED and the
// gate returning nil (authenticated), ActivateAfterVault hydrates the vault
// exactly as when the gate is off.
func TestActivateAfterVaultBiometricAllows(t *testing.T) {
	a := activateVaultApp(t, true, true)
	var called bool
	old := biometricAuth
	biometricAuth = func(string) error { called = true; return nil }
	defer func() { biometricAuth = old }()

	driveActivate(t, a)

	if !called {
		t.Fatal("biometric gate was not called with biometric unlock enabled")
	}
	if acct, ok := a.set.Account(source.Reddit); !ok || acct.Fields["session_cookie"] != "abc" {
		t.Fatalf("secret not hydrated after an allowed gate: %+v ok=%v", acct, ok)
	}
}

// TestActivateAfterVaultBiometricDenies: with biometric unlock ENABLED and the
// gate returning an error (cancelled/failed), ActivateAfterVault SKIPS the vault
// read — the secret stays empty — but still runs onLoaded so the reader opens.
func TestActivateAfterVaultBiometricDenies(t *testing.T) {
	a := activateVaultApp(t, true, true)
	old := biometricAuth
	biometricAuth = func(string) error { return errors.New("cancelled") }
	defer func() { biometricAuth = old }()

	driveActivate(t, a)

	if acct, _ := a.set.Account(source.Reddit); acct.Fields["session_cookie"] != "" {
		t.Fatalf("secret must NOT hydrate after a denied gate: %+v", acct.Fields)
	}
}

// TestActivateAfterVaultBiometricDisabled: with biometric unlock DISABLED, the
// gate is never consulted and the vault hydrates straight away.
func TestActivateAfterVaultBiometricDisabled(t *testing.T) {
	a := activateVaultApp(t, false, false)
	old := biometricAuth
	biometricAuth = func(string) error { t.Fatal("gate must not be called when biometric unlock is disabled"); return nil }
	defer func() { biometricAuth = old }()

	driveActivate(t, a)

	if acct, ok := a.set.Account(source.Reddit); !ok || acct.Fields["session_cookie"] != "abc" {
		t.Fatalf("secret not hydrated with biometric disabled: %+v ok=%v", acct, ok)
	}
}

// TestActivateAfterVaultPrimesOnFirstUnlock: with biometric ENABLED but NOT yet
// primed, the FIRST unlock does NOT prompt Touch ID (that would stack on the
// keychain's own first-access grant); it hydrates, then records BiometricPrimed
// so the NEXT launch gates with Touch ID. The flag is persisted.
func TestActivateAfterVaultPrimesOnFirstUnlock(t *testing.T) {
	a := activateVaultApp(t, true, false) // enabled, not primed
	old := biometricAuth
	biometricAuth = func(string) error { t.Fatal("gate must not fire before the vault is primed"); return nil }
	defer func() { biometricAuth = old }()

	driveActivate(t, a)

	if acct, ok := a.set.Account(source.Reddit); !ok || acct.Fields["session_cookie"] != "abc" {
		t.Fatalf("secret not hydrated on the priming run: %+v ok=%v", acct, ok)
	}
	if !a.set.BiometricPrimed {
		t.Fatal("first successful unlock should set BiometricPrimed")
	}
	// It must be persisted, so the next launch gates with Touch ID.
	reloaded, err := a.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.BiometricPrimed {
		t.Fatal("BiometricPrimed was not persisted")
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
