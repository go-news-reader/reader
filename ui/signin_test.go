package ui

import (
	"testing"

	"github.com/go-news-reader/reader/internal/settings"
	"github.com/go-news-reader/reader/source"
)

// TestSignInBrowserAccessor covers the scene's sign-in-browser getter/setter,
// its invalid-value fallback, and that Settings() snapshots the value.
func TestSignInBrowserAccessor(t *testing.T) {
	s := New(900, 700, ThemeFor(OSLinux, false))
	// Fresh scenes default to Firefox (the only importable browser).
	if got := s.SignInBrowser(); got != settings.DefaultSignInBrowser {
		t.Fatalf("default sign-in browser = %q, want %q", got, settings.DefaultSignInBrowser)
	}
	// A valid value is recorded and snapshotted.
	s.SetSignInBrowser(settings.SignInBrowserChrome)
	if got := s.SignInBrowser(); got != settings.SignInBrowserChrome {
		t.Fatalf("sign-in browser = %q, want chrome", got)
	}
	if got := s.Settings().SignInBrowser; got != settings.SignInBrowserChrome {
		t.Fatalf("Settings().SignInBrowser = %q, want chrome", got)
	}
	// An unrecognised value falls back to the default rather than persisting junk.
	s.SetSignInBrowser("mosaic")
	if got := s.SignInBrowser(); got != settings.DefaultSignInBrowser {
		t.Fatalf("invalid sign-in browser => %q, want fallback to default", got)
	}
}

// TestSignInBrowserSettingControl covers the settings-view picker: each pill
// hit-tests to HitSignInBrowser carrying its value, and the active pill tracks
// the current choice.
func TestSignInBrowserSettingControl(t *testing.T) {
	s := New(1000, 900, ThemeFor(OSLinux, false))
	s.SetSignInBrowser(settings.SignInBrowserEdge)
	s.OpenSettings()
	s.layoutSettings()

	seen := map[string]bool{}
	var activeVal string
	for _, b := range s.sButtons {
		if b.kind != HitSignInBrowser {
			continue
		}
		seen[b.value] = true
		x, y := center(b.rect.X, b.rect.Y, b.rect.W, b.rect.H)
		if h := s.hitSettings(x, y); h.Kind != HitSignInBrowser || h.Value != b.value {
			t.Fatalf("sign-in pill %q hit = %+v", b.value, h)
		}
		if b.active {
			activeVal = b.value
		}
	}
	for _, b := range signInBrowsers {
		if !seen[b.value] {
			t.Fatalf("settings view missing sign-in-browser pill %q", b.value)
		}
	}
	if activeVal != settings.SignInBrowserEdge {
		t.Fatalf("active sign-in pill = %q, want edge", activeVal)
	}
}

// TestAccountsSignInButton covers the Reddit editor's "Sign in to Reddit in
// browser" button: it is laid out for Reddit, hit-tests to HitRedditSignIn, sits
// distinct from the import button, and is absent for other providers.
func TestAccountsSignInButton(t *testing.T) {
	s := New(1000, 700, ThemeFor(OSLinux, false))
	s.OpenAccounts() // Reddit by default
	s.layoutAccounts()
	if s.accSignInR.W == 0 {
		t.Fatal("Reddit editor should show the sign-in button")
	}
	sx, sy := center(s.accSignInR.X, s.accSignInR.Y, s.accSignInR.W, s.accSignInR.H)
	if h := s.accountsHitTest(sx, sy); h.Kind != HitRedditSignIn {
		t.Fatalf("sign-in button hit = %+v, want HitRedditSignIn", h)
	}
	// The import button is still present and distinct.
	if s.accImportR.W == 0 || s.accImportR.X == s.accSignInR.X {
		t.Fatalf("import button should sit beside sign-in: sign=%+v import=%+v", s.accSignInR, s.accImportR)
	}
	ix, iy := center(s.accImportR.X, s.accImportR.Y, s.accImportR.W, s.accImportR.H)
	if h := s.accountsHitTest(ix, iy); h.Kind != HitImportRedditFirefox {
		t.Fatalf("import button hit = %+v, want HitImportRedditFirefox", h)
	}

	// A non-Reddit provider shows neither button.
	s.SelectAccount(source.Mastodon)
	s.layoutAccounts()
	if s.accSignInR.W != 0 {
		t.Fatal("non-Reddit provider must not show the sign-in button")
	}
}

// TestAccountsSignInButtonScrolls exercises the scroll-shift of the sign-in
// button's rect when the Reddit form overflows a short window.
func TestAccountsSignInButtonScrolls(t *testing.T) {
	s := New(360, 220, ThemeFor(OSLinux, false))
	s.OpenAccounts()
	s.Scroll(1 << 20)
	s.layoutAccounts()
	if s.accScroll.offset <= 0 {
		t.Fatalf("form did not scroll: %d", s.accScroll.offset)
	}
	if s.accSignInR.W == 0 {
		t.Fatal("sign-in button should still be laid out while scrolled")
	}
}
