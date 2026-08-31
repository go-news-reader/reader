package ui

import (
	"testing"

	"github.com/go-news-reader/reader/source"
	"github.com/go-widgets/toolkit"
)

// TestAccountsEmitNativeControls proves the reader publishes the accounts
// editor's fields as native-control descriptors: a secret field becomes a secure
// entry, the boolean field a checkbox, and each descriptor's callback writes back
// to the same credential store a keypress or click would.
func TestAccountsEmitNativeControls(t *testing.T) {
	s := New(900, 600, ThemeFor(OSLinux, false))
	s.OpenAccounts()
	s.SelectAccount(source.Usenet) // has addr, tls(bool), username, password(secret), indexer_url, indexer_key(secret)
	s.Draw(make([]byte, s.W*s.H*4))

	byKey := map[string]toolkit.NativeControl{}
	for _, c := range s.NativeControls() {
		byKey[c.Key] = c
	}
	if len(byKey) == 0 {
		t.Fatal("accounts screen emitted no native controls")
	}

	pw, ok := byKey[nativeAccKey(source.Usenet, "password")]
	if !ok {
		t.Fatal("no native control for the usenet password field")
	}
	if pw.Kind != toolkit.NativeSecureEntry {
		t.Errorf("password kind = %v, want NativeSecureEntry", pw.Kind)
	}
	if (pw.Rect == toolkit.Rect{}) || !pw.Visible {
		t.Errorf("password control geometry = %+v visible=%v", pw.Rect, pw.Visible)
	}
	if pw.OnText == nil {
		t.Fatal("password control has no OnText callback")
	}
	pw.OnText("s3cr3t")
	if got := s.accFieldValue(source.Usenet, "password"); got != "s3cr3t" {
		t.Errorf("after OnText, stored password = %q, want s3cr3t", got)
	}

	// A non-secret field is a plain entry.
	if u := byKey[nativeAccKey(source.Usenet, "username")]; u.Kind != toolkit.NativeEntry {
		t.Errorf("username kind = %v, want NativeEntry", u.Kind)
	}

	// The boolean field is a checkbox whose toggle sets the store.
	tls, ok := byKey[nativeAccKey(source.Usenet, "tls")]
	if !ok || tls.Kind != toolkit.NativeCheckbox {
		t.Fatalf("tls control = %+v, want a NativeCheckbox", tls)
	}
	tls.OnBool(true)
	if got := s.accFieldValue(source.Usenet, "tls"); got != "true" {
		t.Errorf("after OnBool(true), stored tls = %q, want true", got)
	}
	tls.OnBool(false)
	if got := s.accFieldValue(source.Usenet, "tls"); got != "false" {
		t.Errorf("after OnBool(false), stored tls = %q, want false", got)
	}
}

// TestSettingsFieldsEmitNativeControls proves the preferences view publishes each
// of its text fields as a native entry whose keystrokes flow to the same buffer,
// and that a zoom-key field keeps only one rune.
func TestSettingsFieldsEmitNativeControls(t *testing.T) {
	s := New(900, 600, ThemeFor(OSLinux, false))
	s.OpenSettings()
	s.Draw(make([]byte, s.W*s.H*4))

	byKey := map[string]toolkit.NativeControl{}
	for _, c := range s.NativeControls() {
		byKey[c.Key] = c
	}
	for _, key := range []string{
		"set:rename", "set:channel", "set:cache", "set:cachesize",
		"set:cachebackend", "set:zoomin", "set:zoomout",
	} {
		c, ok := byKey[key]
		if !ok {
			t.Fatalf("settings field %q emitted no native control", key)
		}
		if c.Kind != toolkit.NativeEntry {
			t.Errorf("%s kind = %v, want NativeEntry", key, c.Kind)
		}
		if c.OnText == nil {
			t.Errorf("%s has no OnText callback", key)
		}
		if (c.Rect == toolkit.Rect{}) || !c.Visible {
			t.Errorf("%s geometry = %+v visible=%v", key, c.Rect, c.Visible)
		}
		if f, ok := s.NativeSettingsCommit(key); !ok {
			t.Errorf("%s has no commit-focus record", key)
			_ = f
		}
	}

	// OnText writes the field's buffer (and focuses it).
	byKey["set:rename"].OnText("Renamed")
	if s.renameInput != "Renamed" || s.Focus() != FocusRename {
		t.Errorf("rename OnText: buffer=%q focus=%v, want \"Renamed\"/FocusRename", s.renameInput, s.Focus())
	}
	// A zoom-key field holds a single printable rune, so only the last survives.
	byKey["set:zoomin"].OnText("abc")
	if s.zoomInInput != "c" {
		t.Errorf("zoom-in field kept %q, want last rune \"c\"", s.zoomInInput)
	}
}

// TestNativeControlsResetEachFrame proves the accumulator is cleared per Draw, so
// controls from one frame do not linger into the next.
func TestNativeControlsResetEachFrame(t *testing.T) {
	s := New(900, 600, ThemeFor(OSLinux, false))
	s.OpenAccounts()
	s.SelectAccount(source.Reddit)
	s.Draw(make([]byte, s.W*s.H*4))
	n := len(s.NativeControls())
	if n == 0 {
		t.Fatal("expected some controls on the accounts screen")
	}
	// Leaving the accounts screen: the next frame emits none.
	s.CloseAccounts()
	s.Draw(make([]byte, s.W*s.H*4))
	for _, c := range s.NativeControls() {
		if c.Key == nativeAccKey(source.Reddit, "session_cookie") {
			t.Fatal("accounts control lingered after leaving the screen")
		}
	}
}

// TestTopbarSearchEmitsNativeControl proves the feed's search box publishes a
// native text field bound to the same SearchEntry observable the filter reads.
func TestTopbarSearchEmitsNativeControl(t *testing.T) {
	s := New(900, 600, ThemeFor(OSLinux, false))
	s.Draw(make([]byte, s.W*s.H*4)) // feed mode: draws the topbar
	var search *toolkit.NativeControl
	for i, c := range s.NativeControls() {
		if c.Key == "topbar:search" {
			search = &s.NativeControls()[i]
			break
		}
	}
	if search == nil {
		t.Fatal("the topbar search box emitted no native control")
	}
	if search.Kind != toolkit.NativeEntry || search.OnText == nil {
		t.Fatalf("topbar search descriptor = %+v, want an entry with a callback", search)
	}
	search.OnText("golang")
	if got := s.Search(); got != "golang" {
		t.Errorf("after OnText, topbar search = %q, want golang", got)
	}
}

// TestAccountsActionButtonsEmitNativeButtons proves action buttons become native
// button descriptors carrying the Hit their click runs.
func TestAccountsActionButtonsEmitNativeButtons(t *testing.T) {
	s := New(900, 600, ThemeFor(OSLinux, false))
	s.OpenAccounts()
	s.SelectAccount(source.Reddit)
	s.Draw(make([]byte, s.W*s.H*4))

	var done *toolkit.NativeControl
	for i, c := range s.NativeControls() {
		if c.Key == "acc:done" {
			done = &s.NativeControls()[i]
		}
	}
	if done == nil || done.Kind != toolkit.NativeButton || done.Text != "Done" {
		t.Fatalf("Done native button = %+v", done)
	}
	hit, ok := s.NativeHit("acc:done")
	if !ok || hit.Kind != HitCloseAccounts {
		t.Fatalf("NativeHit(acc:done) = %+v ok=%v, want HitCloseAccounts", hit, ok)
	}
	// An unknown key has no hit.
	if _, ok := s.NativeHit("nope"); ok {
		t.Error("NativeHit for an unknown key should be false")
	}
}
