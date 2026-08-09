package ui

import (
	"testing"

	"github.com/go-news-reader/reader/internal/settings"
	"github.com/go-news-reader/reader/source"
)

// centerHit returns the center of a rect.
func center(x, y, w, h int) (int, int) { return x + w/2, y + h/2 }

func TestAccountsOpenCloseAndSelect(t *testing.T) {
	s := New(900, 600, ThemeFor(OSLinux, false))
	// Fresh scene: accBuf is nil until seeded.
	if got := s.accFieldValue(source.Reddit, "session_cookie"); got != "" {
		t.Fatalf("nil buffer lookup = %q", got)
	}
	s.OpenAccounts() // accSel == "" -> defaults to Reddit
	if s.Mode() != ModeAccounts || s.accSel != source.Reddit {
		t.Fatalf("open => mode=%v sel=%v", s.Mode(), s.accSel)
	}
	s.SelectAccount(source.Usenet)
	if s.accSel != source.Usenet || s.accFocus != "" {
		t.Fatalf("select => sel=%v focus=%q", s.accSel, s.accFocus)
	}
	s.OpenAccounts() // accSel already set -> unchanged branch
	if s.accSel != source.Usenet {
		t.Fatal("re-open should keep the selected provider")
	}
	s.CloseAccounts()
	if s.Mode() != ModeFeed {
		t.Fatal("close should return to the feed")
	}
}

func TestAccountsSeedAndEdit(t *testing.T) {
	s := New(900, 600, ThemeFor(OSLinux, false))
	s.SetAccounts([]settings.Account{
		{Kind: source.Reddit, Fields: map[string]string{"session_cookie": "cook"}},
		{Kind: source.Lemmy, Fields: nil},                                     // empty map -> skipped by EditedAccounts
		{Kind: source.Mastodon, Fields: map[string]string{"instance": "   "}}, // blank -> skipped
	})
	if !s.accConfigured(source.Reddit) {
		t.Fatal("reddit should read as configured")
	}
	if s.accConfigured(source.Twitter) {
		t.Fatal("twitter has no creds, should be unconfigured")
	}
	// EditedAccounts keeps only providers with a non-empty field.
	ed := s.EditedAccounts()
	if len(ed) != 1 || ed[0].Kind != source.Reddit || ed[0].Fields["session_cookie"] != "cook" {
		t.Fatalf("edited accounts = %+v", ed)
	}

	// Settings() carries the edited accounts.
	if _, ok := s.Settings().Account(source.Reddit); !ok {
		t.Fatal("Settings() should include the reddit account")
	}

	// Type into a focused field (ModeAccounts path). Clear the seeded buffer so
	// the typed value stands alone.
	s.SetAccounts(nil)
	s.OpenAccounts()
	s.FocusAccountField("session_cookie")
	s.TypeRune('b')
	s.TypeRune('o')
	if s.accFieldValue(source.Reddit, "session_cookie") != "bo" {
		t.Fatalf("typed value = %q", s.accFieldValue(source.Reddit, "session_cookie"))
	}
	s.Backspace()
	if s.accFieldValue(source.Reddit, "session_cookie") != "b" {
		t.Fatalf("after backspace = %q", s.accFieldValue(source.Reddit, "session_cookie"))
	}
	// Backspace with no focus is a no-op.
	s.FocusAccountField("")
	s.Backspace()
	// TypeRune with no focus is ignored.
	s.TypeRune('x')
	if s.accFieldValue(source.Reddit, "session_cookie") != "b" {
		t.Fatalf("unfocused edits leaked: %q", s.accFieldValue(source.Reddit, "session_cookie"))
	}
}

func TestAccountsToggleBool(t *testing.T) {
	s := New(900, 600, ThemeFor(OSLinux, false))
	s.OpenAccounts()
	s.SelectAccount(source.Usenet)
	s.ToggleAccountBool("tls") // nil buffers -> allocate, set "true"
	if s.accFieldValue(source.Usenet, "tls") != "true" {
		t.Fatalf("toggle on = %q", s.accFieldValue(source.Usenet, "tls"))
	}
	s.ToggleAccountBool("tls") // flip back to "false"
	if s.accFieldValue(source.Usenet, "tls") != "false" {
		t.Fatalf("toggle off = %q", s.accFieldValue(source.Usenet, "tls"))
	}
}

func TestCredsForFallback(t *testing.T) {
	// A kind absent from the schema falls back to the first provider (Reddit).
	if credsFor(source.HackerNews).Kind != source.Reddit {
		t.Fatal("unknown kind should fall back to reddit")
	}
	if credsFor(source.Mastodon).Kind != source.Mastodon {
		t.Fatal("known kind should resolve to itself")
	}
}

func TestAccountsHitTest(t *testing.T) {
	s := New(900, 700, ThemeFor(OSLinux, false))
	s.OpenAccounts() // Reddit selected -> text fields incl. secret
	s.layoutAccounts()

	// Back and Done both commit.
	bx, by := center(s.accBackR.X, s.accBackR.Y, s.accBackR.W, s.accBackR.H)
	if s.accountsHitTest(bx, by).Kind != HitCloseAccounts {
		t.Fatal("back should close")
	}
	dx, dy := center(s.accDoneR.X, s.accDoneR.Y, s.accDoneR.W, s.accDoneR.H)
	if s.accountsHitTest(dx, dy).Kind != HitCloseAccounts {
		t.Fatal("done should close")
	}
	// A provider pill selects.
	pb := s.accProvBtns[1]
	px, py := center(pb.rect.X, pb.rect.Y, pb.rect.W, pb.rect.H)
	if h := s.accountsHitTest(px, py); h.Kind != HitSelectAccount || h.Value != string(pb.kind) {
		t.Fatalf("provider hit = %+v", h)
	}
	// A text credential field focuses it.
	fr := s.accRows[0]
	fx, fy := center(fr.rect.X, fr.rect.Y, fr.rect.W, fr.rect.H)
	if h := s.accountsHitTest(fx, fy); h.Kind != HitFocusAccountField || h.Value != fr.key {
		t.Fatalf("field hit = %+v", h)
	}
	// The Usenet TLS field hit-tests as a bool toggle.
	s.SelectAccount(source.Usenet)
	s.layoutAccounts()
	var boolRow accFieldRow
	for _, r := range s.accRows {
		if r.isBool {
			boolRow = r
		}
	}
	tx, ty := center(boolRow.rect.X, boolRow.rect.Y, boolRow.rect.W, boolRow.rect.H)
	if h := s.accountsHitTest(tx, ty); h.Kind != HitToggleAccountBool || h.Value != "tls" {
		t.Fatalf("bool hit = %+v", h)
	}
	// A miss (bottom-right corner) returns HitNone.
	if s.accountsHitTest(s.W-1, s.H-1).Kind != HitNone {
		t.Fatal("miss should be HitNone")
	}
	// HitTest dispatches to accountsHitTest while in ModeAccounts.
	if s.HitTest(bx, by).Kind != HitCloseAccounts {
		t.Fatal("HitTest should route through accountsHitTest")
	}
}

func TestAccountsSidebarEntry(t *testing.T) {
	s := newScene()
	s.layout()
	// The pinned 👤 Accounts entry sits above the Network log; clicking it opens
	// the accounts editor.
	x, y := center(s.accountsR.X, s.accountsR.Y, s.accountsR.W, s.accountsR.H)
	if s.HitTest(x, y).Kind != HitAccounts {
		t.Fatal("sidebar Accounts entry should report HitAccounts")
	}
}

func TestAccountsScroll(t *testing.T) {
	// A short window forces the credential form to exceed the viewport so the
	// scroll path and its clamp run.
	s := New(360, 240, ThemeFor(OSLinux, false))
	s.OpenAccounts()
	s.SelectAccount(source.Usenet) // Usenet's six-field form comfortably overflows
	s.Scroll(1 << 20)              // to the bottom
	if s.accScroll.offset <= 0 {
		t.Fatalf("form did not scroll: %d", s.accScroll.offset)
	}
	s.Scroll(-(1 << 20)) // clamps back to 0
	if s.accScroll.offset != 0 {
		t.Fatalf("scroll did not clamp to 0: %d", s.accScroll.offset)
	}
}

func TestAccountsSnapshot(t *testing.T) {
	s := New(900, 700, ThemeFor(OSLinux, false)) // adwaita has Extra["OnAccent"]
	s.SetAccounts([]settings.Account{
		{Kind: source.Reddit, Fields: map[string]string{"session_cookie": "supersecret"}},
	})
	s.OpenAccounts()                      // Reddit fields, with a masked secret
	s.FocusAccountField("session_cookie") // exercise the focused-caret branch
	s.TypeRune('u')
	buf := renderPNG(t, s, "accounts")

	// The accent topbar is present: sample a strip clear of the Back/title text.
	acc := s.theme.Accent
	got := px(buf, s.W, s.W/2, 4)
	if got.R != acc.R || got.G != acc.G || got.B != acc.B {
		t.Fatalf("accounts topbar pixel = %v, want accent %v", got, acc)
	}

	// The session_cookie field renders masked: its stored value never appears as
	// raw text, but its bullet mask is drawn. Assert via the layout: the secret
	// row exists and its buffer holds the raw secret (drawn as bullets).
	s.layoutAccounts()
	var sawSecret bool
	for _, r := range s.accRows {
		if r.key == "session_cookie" && r.secret {
			sawSecret = true
			if s.accFieldValue(source.Reddit, "session_cookie") != "supersecretu" {
				t.Fatalf("secret buffer not preserved: %q", s.accFieldValue(source.Reddit, "session_cookie"))
			}
		}
	}
	if !sawSecret {
		t.Fatal("session_cookie should be a masked field")
	}

	// The Usenet provider draws a bool toggle (On/Off) — render that state too.
	s.SelectAccount(source.Usenet)
	s.ToggleAccountBool("tls")
	renderPNG(t, s, "accounts-usenet")
}

func TestSetAccountField(t *testing.T) {
	s := New(900, 600, ThemeFor(OSLinux, false))
	// nil buffer path: allocates the outer + inner maps.
	s.SetAccountField(source.Reddit, "session_cookie", "one")
	if got := s.accFieldValue(source.Reddit, "session_cookie"); got != "one" {
		t.Fatalf("first write = %q", got)
	}
	// existing buffer path: reuses the maps, overwrites.
	s.SetAccountField(source.Reddit, "session_cookie", "two")
	if got := s.accFieldValue(source.Reddit, "session_cookie"); got != "two" {
		t.Fatalf("second write = %q", got)
	}
}

func TestAccountsImportButton(t *testing.T) {
	s := New(900, 700, ThemeFor(OSLinux, false))
	s.OpenAccounts() // Reddit selected by default
	s.layoutAccounts()
	if s.accImportR.W == 0 {
		t.Fatal("Reddit editor should show the Firefox import button")
	}
	ix, iy := center(s.accImportR.X, s.accImportR.Y, s.accImportR.W, s.accImportR.H)
	if h := s.accountsHitTest(ix, iy); h.Kind != HitImportRedditFirefox {
		t.Fatalf("import button hit = %+v, want HitImportRedditFirefox", h)
	}

	// A non-Reddit provider shows no import button and never yields the hit.
	s.SelectAccount(source.Mastodon)
	s.layoutAccounts()
	if s.accImportR.W != 0 {
		t.Fatal("non-Reddit provider must not show the import button")
	}
}

func TestAccountsImportButtonScrolls(t *testing.T) {
	// A short window forces the Reddit form (incl. the import button) to overflow,
	// exercising the scroll-shift of the import-button rect.
	s := New(360, 220, ThemeFor(OSLinux, false))
	s.OpenAccounts() // Reddit
	s.Scroll(1 << 20)
	s.layoutAccounts()
	if s.accScroll.offset <= 0 {
		t.Fatalf("form did not scroll: %d", s.accScroll.offset)
	}
	if s.accImportR.W == 0 {
		t.Fatal("import button should still be laid out while scrolled")
	}
}

func TestAccountsProviderWrap(t *testing.T) {
	// A narrow window forces the provider selector to wrap onto a second row.
	s := New(380, 700, ThemeFor(OSLinux, false))
	s.OpenAccounts()
	s.layoutAccounts()
	rows := map[int]bool{}
	for _, b := range s.accProvBtns {
		rows[b.rect.Y] = true
	}
	if len(rows) < 2 {
		t.Fatalf("provider pills did not wrap: %d row(s)", len(rows))
	}
	renderPNG(t, s, "accounts-wrapped")
}
