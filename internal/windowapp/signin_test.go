package windowapp

import (
	"reflect"
	"runtime"
	"testing"

	"github.com/go-news-reader/reader/internal/settings"
	"github.com/go-news-reader/reader/ui"
)

// TestOpenURLInBrowser table-drives the per-OS / per-browser opener command
// mapping, including the fallbacks to the default opener (a "default" choice, an
// OS with no command for the named browser like Safari off macOS, and an
// unrecognised value).
func TestOpenURLInBrowser(t *testing.T) {
	const url = "https://x"
	cases := []struct {
		goos, browser string
		wantName      string
		wantArgs      []string
	}{
		// darwin: `open -a <AppName>`, default → plain `open`.
		{"darwin", settings.SignInBrowserDefault, "open", []string{url}},
		{"darwin", settings.SignInBrowserFirefox, "open", []string{"-a", "Firefox", url}},
		{"darwin", settings.SignInBrowserChrome, "open", []string{"-a", "Google Chrome", url}},
		{"darwin", settings.SignInBrowserSafari, "open", []string{"-a", "Safari", url}},
		{"darwin", settings.SignInBrowserEdge, "open", []string{"-a", "Microsoft Edge", url}},
		// windows: `cmd /c start "" <exe>`, default → rundll32; Safari has no exe → default.
		{"windows", settings.SignInBrowserDefault, "rundll32", []string{"url.dll,FileProtocolHandler", url}},
		{"windows", settings.SignInBrowserFirefox, "cmd", []string{"/c", "start", "", "firefox", url}},
		{"windows", settings.SignInBrowserChrome, "cmd", []string{"/c", "start", "", "chrome", url}},
		{"windows", settings.SignInBrowserEdge, "cmd", []string{"/c", "start", "", "msedge", url}},
		{"windows", settings.SignInBrowserSafari, "rundll32", []string{"url.dll,FileProtocolHandler", url}},
		// linux/other: run the browser binary, default → xdg-open; Safari has no
		// binary → default.
		{"linux", settings.SignInBrowserDefault, "xdg-open", []string{url}},
		{"linux", settings.SignInBrowserFirefox, "firefox", []string{url}},
		{"linux", settings.SignInBrowserChrome, "google-chrome", []string{url}},
		{"linux", settings.SignInBrowserEdge, "microsoft-edge", []string{url}},
		{"linux", settings.SignInBrowserSafari, "xdg-open", []string{url}},
		// An empty / unrecognised browser value also falls back to the default opener.
		{"linux", "", "xdg-open", []string{url}},
		{"linux", "opera", "xdg-open", []string{url}},
	}
	for _, c := range cases {
		name, args := openURLInBrowser(c.goos, c.browser, url)
		if name != c.wantName || !reflect.DeepEqual(args, c.wantArgs) {
			t.Fatalf("openURLInBrowser(%q,%q) = %q %v, want %q %v",
				c.goos, c.browser, name, args, c.wantName, c.wantArgs)
		}
	}
}

// TestDefaultOpenURLIn exercises the real defaultOpenURLIn seam wiring through
// execStart.
func TestDefaultOpenURLIn(t *testing.T) {
	var gotName string
	var gotArgs []string
	orig := execStart
	execStart = func(name string, args ...string) error { gotName, gotArgs = name, args; return nil }
	t.Cleanup(func() { execStart = orig })
	if err := defaultOpenURLIn(settings.SignInBrowserFirefox, "https://ex"); err != nil {
		t.Fatal(err)
	}
	wantName, wantArgs := openURLInBrowser(runtime.GOOS, settings.SignInBrowserFirefox, "https://ex")
	if gotName != wantName || !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("defaultOpenURLIn launched %q %v, want %q %v", gotName, gotArgs, wantName, wantArgs)
	}
}

// TestRouteRedditSignIn covers the HitRedditSignIn route end to end: clicking the
// Reddit editor's sign-in button drives app.LaunchRedditSignIn, which (through the
// New-installed opener seam) launches Reddit's login page in the configured
// browser. It asserts the exact command + URL via the execStart seam so no
// process is spawned.
func TestRouteRedditSignIn(t *testing.T) {
	a := profApp(t)
	var gotName string
	var gotArgs []string
	orig := execStart
	execStart = func(name string, args ...string) error { gotName, gotArgs = name, args; return nil }
	t.Cleanup(func() { execStart = orig })

	h := New(a)
	s := a.Scene()
	s.SetSignInBrowser(settings.SignInBrowserFirefox)
	click(t, h, ui.HitAccounts) // open the accounts editor (Reddit by default)
	click(t, h, ui.HitRedditSignIn)

	wantName, wantArgs := openURLInBrowser(runtime.GOOS, settings.SignInBrowserFirefox, "https://www.reddit.com/login/")
	if gotName != wantName || !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("sign-in launched %q %v, want %q %v (browser=firefox)", gotName, gotArgs, wantName, wantArgs)
	}
	if last := gotArgs[len(gotArgs)-1]; last != "https://www.reddit.com/login/" {
		t.Fatalf("sign-in URL = %q, want Reddit's login page", last)
	}
}

// TestRouteSignInBrowserSetting covers the HitSignInBrowser settings route: the
// picker records the choice on the scene (persisted via ApplySceneSettings). The
// pill sits far down the scrollable settings body, so the route is driven
// directly rather than via a synthesised coordinate.
func TestRouteSignInBrowserSetting(t *testing.T) {
	a := profApp(t)
	h := New(a)
	s := a.Scene()
	a.VM().OpenSettings.Execute()
	h.runHit(ui.Hit{Kind: ui.HitSignInBrowser, Value: settings.SignInBrowserEdge})
	if got := s.SignInBrowser(); got != settings.SignInBrowserEdge {
		t.Fatalf("sign-in browser after route = %q, want edge", got)
	}
}
