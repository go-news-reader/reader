package ui

import (
	"sort"
	"strings"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"

	"github.com/go-news-reader/reader/internal/settings"
	"github.com/go-news-reader/reader/source"
)

// The in-canvas per-provider credentials editor (ModeAccounts). Its primary
// purpose is to authenticate Reddit: pasting the logged-in browser's
// reddit_session cookie switches the Reddit provider off the anonymous,
// intermittently-403ing ".json" endpoints. The cookie can also be imported
// straight from Firefox with one click. The view is a provider selector plus the
// selected provider's credential fields (from settings.CredentialSchema),
// rendered with the same painter + anti-aliased text as the rest of the app.
// Secret fields are masked; the Usenet TLS field renders as a toggle.

// Reddit editor button captions. Sign-in launches the configured browser at
// Reddit's login page; import lifts the resulting cookie (Firefox only).
const (
	redditSignInLabel     = "Sign in to Reddit in browser"
	redditImportLabel     = "Import session from Firefox"
	redditImportSubsLabel = "Import subscriptions"
)

// importSessionLabel captions the one-click Firefox import shown for the social
// providers that authenticate with a session cookie (Instagram, TikTok, X).
const importSessionLabel = "Import session from Firefox"

// hasSessionField reports whether provider k authenticates with a plain "session"
// cookie field (Instagram, TikTok, X) — the providers that get the generic
// import-from-Firefox affordance. Reddit is excluded: it uses its own
// "session_cookie" field and its own richer button row.
func hasSessionField(k source.Kind) bool {
	if k == source.Reddit {
		return false
	}
	for _, f := range credsFor(k).Fields {
		if f.Key == "session" {
			return true
		}
	}
	return false
}

// accProvBtn is one provider pill in the selector.
type accProvBtn struct {
	rect   toolkit.Rect
	kind   source.Kind
	label  string
	active bool
}

// flowSlot is a do-nothing placeholder widget used only to read back the
// positions a toolkit.FlowLayout computes for the provider pills.
type flowSlot struct{ toolkit.Base }

// accFieldRow is one credential input for the selected provider.
type accFieldRow struct {
	rect    toolkit.Rect
	key     string
	secret  bool
	isBool  bool
	focused bool
}

// SetAccounts seeds the editable credential buffers from the persisted accounts.
func (s *Scene) SetAccounts(accts []settings.Account) {
	s.accBuf = map[source.Kind]map[string]string{}
	for _, a := range accts {
		m := map[string]string{}
		for k, v := range a.Fields {
			m[k] = v
		}
		s.accBuf[a.Kind] = m
	}
	s.touch()
}

// EditedAccounts projects the editable buffers back into persisted accounts, in
// the schema's stable order, dropping providers (and individual fields) whose
// values are blank so an untouched provider leaves no empty account behind.
func (s *Scene) EditedAccounts() []settings.Account {
	var out []settings.Account
	for _, pc := range settings.CredentialSchema() {
		m := s.accBuf[pc.Kind]
		if len(m) == 0 {
			continue
		}
		fields := map[string]string{}
		for _, f := range pc.Fields {
			if strings.TrimSpace(m[f.Key]) != "" {
				fields[f.Key] = m[f.Key]
			}
		}
		if len(fields) > 0 {
			out = append(out, settings.Account{Kind: pc.Kind, Fields: fields})
		}
	}
	return out
}

// OpenAccounts enters the credentials editor (defaulting to Reddit, the reason
// the feature exists).
func (s *Scene) OpenAccounts() {
	s.mode = ModeAccounts
	if s.accSel == "" {
		s.accSel = source.Reddit
	}
	s.accFocus = ""
	s.accScroll.offset = 0 // a fresh open starts at the top, like the other views
	s.touch()
}

// CloseAccounts returns to the feed view.
func (s *Scene) CloseAccounts() { s.mode = ModeFeed; s.touch() }

// SelectAccount picks which provider the editor operates on.
func (s *Scene) SelectAccount(k source.Kind) {
	s.accSel = k
	s.accFocus = ""
	s.accScroll.offset = 0 // the new provider has a different field set; start at the top
	s.touch()
}

// SelectedAccount reports which provider the accounts editor is operating on.
func (s *Scene) SelectedAccount() source.Kind { return s.accSel }

// SetFollowImportKinds records which source kinds can import the connected
// account's follows (their provider implements source.FollowImporter), so the
// accounts editor shows the "Import subscriptions" affordance for exactly those.
// The app calls it whenever it (re)builds the provider registry.
func (s *Scene) SetFollowImportKinds(kinds []source.Kind) {
	m := make(map[source.Kind]bool, len(kinds))
	for _, k := range kinds {
		m[k] = true
	}
	s.followImportKinds = m
	s.touch()
}

// canImportFollows reports whether the selected provider offers the "Import
// subscriptions" affordance (its registered provider implements
// source.FollowImporter).
func (s *Scene) canImportFollows(k source.Kind) bool { return s.followImportKinds[k] }

// FollowImportKinds returns the source kinds currently marked follow-capable, in
// lexical order — the set [Scene.SetFollowImportKinds] last recorded. It lets a
// caller (and tests) read back which providers offer the "Import subscriptions"
// action without going through the accounts-editor hit test.
func (s *Scene) FollowImportKinds() []source.Kind {
	ks := make([]source.Kind, 0, len(s.followImportKinds))
	for k := range s.followImportKinds {
		ks = append(ks, k)
	}
	sort.Slice(ks, func(i, j int) bool { return ks[i] < ks[j] })
	return ks
}

// FocusAccountField gives keyboard focus to a credential field.
func (s *Scene) FocusAccountField(key string) { s.accFocus = key; s.touch() }

// ToggleAccountBool flips a boolean credential field (e.g. Usenet TLS).
func (s *Scene) ToggleAccountBool(key string) {
	if s.accFieldValue(s.accSel, key) == "true" {
		s.accSetField(key, "false")
	} else {
		s.accSetField(key, "true")
	}
	s.touch()
}

// SetAccountField writes value into provider k's credential buffer for key,
// lazily allocating. It lets the app inject a credential obtained outside the
// editor — e.g. a reddit_session cookie imported from Firefox — so the editor
// reflects it and a commit persists it.
func (s *Scene) SetAccountField(k source.Kind, key, value string) {
	if s.accBuf == nil {
		s.accBuf = map[source.Kind]map[string]string{}
	}
	m := s.accBuf[k]
	if m == nil {
		m = map[string]string{}
		s.accBuf[k] = m
	}
	m[key] = value
	s.touch()
}

// accFieldValue reads the current buffer value for (kind, key).
func (s *Scene) accFieldValue(k source.Kind, key string) string {
	if m := s.accBuf[k]; m != nil {
		return m[key]
	}
	return ""
}

// accSetField writes val into the selected provider's buffer, lazily allocating.
func (s *Scene) accSetField(key, val string) {
	if s.accBuf == nil {
		s.accBuf = map[source.Kind]map[string]string{}
	}
	m := s.accBuf[s.accSel]
	if m == nil {
		m = map[string]string{}
		s.accBuf[s.accSel] = m
	}
	m[key] = val
}

// accConfigured reports whether provider k has any non-empty credential.
func (s *Scene) accConfigured(k source.Kind) bool {
	for _, v := range s.accBuf[k] {
		if strings.TrimSpace(v) != "" {
			return true
		}
	}
	return false
}

// credsFor returns the credential schema for k (Reddit as a safe fallback).
func credsFor(k source.Kind) settings.ProviderCreds {
	sc := settings.CredentialSchema()
	for _, pc := range sc {
		if pc.Kind == k {
			return pc
		}
	}
	return sc[0]
}

// layoutAccounts computes the provider selector, credential rows and the
// topbar's Back/Done buttons, applying the vertical scroll offset.
func (s *Scene) layoutAccounts() {
	s.m = s.computeMetrics()
	m := s.m
	s.accProvBtns = s.accProvBtns[:0]
	s.accRows = s.accRows[:0]
	s.accLabels = s.accLabels[:0]
	pad := m.pad
	gap := rpxOf(s, 6)
	btnH := m.btnH

	label := func(x, y int, text string) { s.accLabels = append(s.accLabels, sLabel{x: x, y: y, text: text}) }

	// Topbar band: "‹ Back" (left, after the title) and "Done" (right).
	bw := m.tab.width("‹ Back") + rpxOf(s, 20)
	s.accBackR = toolkit.Rect{X: pad, Y: (m.topbarH - btnH) / 2, W: bw, H: btnH}
	dw := m.tab.width("Done") + rpxOf(s, 24)
	s.accDoneR = toolkit.Rect{X: s.W - pad - dw, Y: (m.topbarH - btnH) / 2, W: dw, H: btnH}

	top := m.topbarH + pad
	y := top // laid out unscrolled; the scroll shift is applied after clamping

	// Provider selector — a wrapping row of pills, positioned by a toolkit
	// FlowLayout (the app dogfooding the toolkit's own layout instead of a
	// hand-rolled wrap loop). Each slot's box-computed bounds become a provider
	// button's rect for drawing + hit-testing.
	label(pad, y, "PROVIDER")
	y += m.side.height + gap
	schema := settings.CredentialSchema()
	labels := make([]string, len(schema))
	flow := toolkit.NewContainer(&toolkit.FlowLayout{RowHeight: btnH, HGap: gap, VGap: gap})
	for i, pc := range schema {
		lbl := pc.Label
		if s.accConfigured(pc.Kind) {
			lbl = "• " + pc.Label // a leading dot marks a configured provider
		}
		labels[i] = lbl
		flow.Add(toolkit.Item{Widget: &flowSlot{}, Size: m.tab.width(lbl) + rpxOf(s, 20)})
	}
	flow.SetBounds(toolkit.Rect{X: pad, Y: y, W: s.W - 2*pad, H: btnH})
	bottom := y
	for i, pc := range schema {
		r := flow.Items()[i].Widget.Bounds()
		s.accProvBtns = append(s.accProvBtns, accProvBtn{
			rect: r, kind: pc.Kind, label: labels[i], active: pc.Kind == s.accSel,
		})
		if r.Y+r.H > bottom {
			bottom = r.Y + r.H
		}
	}
	y = bottom + pad

	// Selected provider's credential fields.
	pc := credsFor(s.accSel)
	label(pad, y, strings.ToUpper(pc.Label)+" CREDENTIALS")
	y += m.side.height + gap
	if s.accSel == source.Reddit {
		label(pad, y, "Log into Reddit in Firefox, then import the reddit_session cookie below")
		y += m.side.height + gap
	}

	labelW := rpxOf(s, 150)
	for _, f := range pc.Fields {
		fieldW := s.W - 2*pad - labelW
		if f.Bool {
			fieldW = rpxOf(s, 90)
		}
		// Each field row is an HBox: caption column (fixed) | input/toggle.
		row := toolkit.NewHBox()
		row.Spacing = -1
		capSlot, fldSlot := toolkit.NewLabel(""), toolkit.NewLabel("")
		row.AddFixed(capSlot, labelW)
		row.AddFixed(fldSlot, fieldW)
		row.SetBounds(toolkit.Rect{X: pad, Y: y, W: s.W - 2*pad, H: btnH})
		label(capSlot.Bounds().X, y+(btnH-m.side.height)/2, f.Label)
		s.accRows = append(s.accRows, accFieldRow{
			rect: fldSlot.Bounds(), key: f.Key, secret: f.Secret, isBool: f.Bool, focused: s.accFocus == f.Key,
		})
		y += btnH + gap
	}

	// Reddit gets a one-click "import from Firefox" affordance: it lifts the
	// logged-in reddit_session cookie out of the user's Firefox profile so signing
	// in is a single button rather than a manual copy-paste. Other providers show
	// no button (its rect stays zero, so it never hit-tests).
	s.accImportR = toolkit.Rect{}
	s.accImportSubsR = toolkit.Rect{}
	s.accSignInR = toolkit.Rect{}
	isw := m.tab.width(redditImportSubsLabel) + rpxOf(s, 24)
	if s.accSel == source.Reddit {
		// "Sign in to Reddit in browser" launches the configured browser at Reddit's
		// login page; "Import session from Firefox" lifts the resulting cookie;
		// "Import subscriptions" pulls the connected account's follows. They sit on
		// one row (sign-in, import-session, import-subs); the sign-in/import buttons
		// are Reddit-specific and the import-subs button appears when Reddit can
		// import follows.
		sw := m.tab.width(redditSignInLabel) + rpxOf(s, 24)
		s.accSignInR = toolkit.Rect{X: pad, Y: y, W: sw, H: btnH}
		iw := m.tab.width(redditImportLabel) + rpxOf(s, 24)
		s.accImportR = toolkit.Rect{X: pad + sw + gap, Y: y, W: iw, H: btnH}
		if s.canImportFollows(s.accSel) {
			s.accImportSubsR = toolkit.Rect{X: s.accImportR.X + iw + gap, Y: y, W: isw, H: btnH}
		}
		y += btnH + gap
	} else if s.canImportFollows(s.accSel) {
		// Any other follow-capable provider (e.g. Mastodon) gets a standalone
		// "Import subscriptions" button on its own row.
		s.accImportSubsR = toolkit.Rect{X: pad, Y: y, W: isw, H: btnH}
		y += btnH + gap
	}

	// Instagram / TikTok / X get a single one-click "Import session from Firefox"
	// button — the same affordance as Reddit's, lifting the logged-in session
	// cookie out of the user's Firefox profile so the home/following timeline can
	// authenticate. Other providers show none (the rect stays zero).
	s.accImportSessionR = toolkit.Rect{}
	if hasSessionField(s.accSel) {
		lw := m.tab.width(importSessionLabel) + rpxOf(s, 24)
		s.accImportSessionR = toolkit.Rect{X: pad, Y: y, W: lw, H: btnH}
		y += btnH + gap
	}

	// Everything above was laid out unscrolled; clamp the offset (the refresh-time
	// clamp, robust to a resize) and shift the scrollable body up by it. The topbar
	// Back/Done live in the fixed band and are not shifted — mirrors layoutSettings.
	s.accScroll.refresh(y-m.topbarH, s.H-m.topbarH)
	if dy := -s.accScroll.offset; dy != 0 {
		for i := range s.accProvBtns {
			s.accProvBtns[i].rect.Y += dy
		}
		for i := range s.accRows {
			s.accRows[i].rect.Y += dy
		}
		for i := range s.accLabels {
			s.accLabels[i].y += dy
		}
		if s.accImportR.W > 0 {
			s.accImportR.Y += dy
		}
		if s.accImportSubsR.W > 0 {
			s.accImportSubsR.Y += dy
		}
		if s.accSignInR.W > 0 {
			s.accSignInR.Y += dy
		}
		if s.accImportSessionR.W > 0 {
			s.accImportSessionR.Y += dy
		}
	}
}

// invertedTopbarTheme returns a copy of th in which a ButtonProminent renders as
// the reader's inverted topbar pill — an OnAccent-filled rounded rect with
// accent-coloured text and no visible border (Border == the fill). The copy owns
// a fresh Extra map so tagging accent_fg_color never mutates the shared theme.
func invertedTopbarTheme(th *toolkit.Theme, onAccent toolkit.RGBA) *toolkit.Theme {
	dt := *th
	dt.Accent = onAccent // ButtonProminent fills with Accent…
	dt.Border = onAccent // …and its border blends into the fill.
	dt.Extra = make(map[string]toolkit.RGBA, len(th.Extra)+1)
	for k, v := range th.Extra {
		dt.Extra[k] = v
	}
	dt.Extra["accent_fg_color"] = th.Accent // ButtonProminent's text ink
	return &dt
}

// drawAccounts paints the credentials editor. Every element is a composed
// go-widgets widget: the flat grounds are toolkit.Backdrop, the section captions
// toolkit.Label, the provider pills / credential fields / action buttons
// toolkit.Button + toolkit.Entry, and the accent topbar's Back/Done are inverted
// toolkit.Button pills over a toolkit.Backdrop band. Nothing is hand-drawn.
func (s *Scene) drawAccounts(buf []byte) {
	s.layoutAccounts()
	m := s.m
	p := painter.NewPixelPainter(buf, s.W, s.H)
	th := s.theme
	onAccent := themeOnAccent(th)
	muteS := mute(th.OnSurface, th.Surface)

	// Full-surface ground — a solid-fill Backdrop, not a hand-drawn FillRect.
	bg := &toolkit.Backdrop{Fill: th.Background}
	bg.SetBounds(toolkit.Rect{X: 0, Y: 0, W: s.W, H: s.H})
	bg.Draw(p, th)

	// Section captions are stock toolkit.Labels carrying the reader's fallback font
	// (matching m.side's size/weight) and the muted caption ink.
	labelFont := ttFont(false, rpxOf(s, 13))
	for _, l := range s.accLabels {
		lbl := toolkit.NewLabel(l.text)
		lbl.Font, lbl.Ink = labelFont, muteS
		lbl.SetBounds(toolkit.Rect{X: l.x, Y: l.y, W: s.W, H: m.side.height})
		lbl.Draw(p, th)
	}
	// Provider pills + credential fields are generic go-widgets widgets with the
	// reader's fallback font: Button (Selected = the active provider / an On bool),
	// Entry (with the toolkit's own secret Mask for the masked credentials).
	pillFont := ttFont(true, rpxOf(s, 12))
	for _, b := range s.accProvBtns {
		w := toolkit.NewButton(b.label, nil)
		w.Selected().Set(b.active)
		w.Font = pillFont
		w.SetBounds(b.rect)
		w.Draw(p, th)
	}
	for _, f := range s.accRows {
		key, sel := f.key, s.accSel
		if f.isBool {
			on := s.accFieldValue(sel, key) == "true"
			lbl := "Off"
			if on {
				lbl = "On"
			}
			w := toolkit.NewButton(lbl, nil)
			w.Selected().Set(on)
			w.Font = pillFont
			w.SetBounds(f.rect)
			w.Draw(p, th)
			// Native counterpart: a real checkbox over the drawn pill. Its state
			// flows back to the same field the pill's click toggles.
			s.addNativeControl(toolkit.NativeControl{
				Kind: toolkit.NativeCheckbox, Key: nativeAccKey(sel, key),
				Rect: f.rect, Visible: true, On: on,
				OnBool: func(b bool) { s.SetAccountField(sel, key, boolField(b)) },
			})
			continue
		}
		w := toolkit.NewEntry(s.accFieldValue(sel, key))
		w.Placeholder = "…"
		w.SetFocused(f.focused)
		w.Font = pillFont
		if f.secret {
			w.Mask = '•' // the toolkit masks the display; Text keeps the real secret
		}
		w.SetBounds(f.rect)
		w.Draw(p, th)
		// Native counterpart: a real text field — a secure one for a secret, where
		// a drawn mask cannot hide the keystrokes from the process. Edits flow back
		// to the same field a keypress would.
		kind := toolkit.NativeEntry
		if f.secret {
			kind = toolkit.NativeSecureEntry
		}
		s.addNativeControl(toolkit.NativeControl{
			Kind: kind, Key: nativeAccKey(sel, key),
			Rect: f.rect, Visible: true, Text: s.accFieldValue(sel, key),
			OnText: func(t string) { s.SetAccountField(sel, key, t) },
		})
	}

	// Reddit's "Sign in to Reddit in browser" + "Import session from Firefox" buttons.
	if s.accSignInR.W > 0 {
		w := toolkit.NewButton(redditSignInLabel, nil)
		w.Font = pillFont
		w.SetBounds(s.accSignInR)
		w.Draw(p, th)
	}
	if s.accImportR.W > 0 {
		w := toolkit.NewButton(redditImportLabel, nil)
		w.Font = pillFont
		w.SetBounds(s.accImportR)
		w.Draw(p, th)
	}
	if s.accImportSubsR.W > 0 {
		w := toolkit.NewButton(redditImportSubsLabel, nil)
		w.Font = pillFont
		w.SetBounds(s.accImportSubsR)
		w.Draw(p, th)
	}
	if s.accImportSessionR.W > 0 {
		w := toolkit.NewButton(importSessionLabel, nil)
		w.Font = pillFont
		w.SetBounds(s.accImportSessionR)
		w.Draw(p, th)
	}

	// Topbar (accent) with Back, title and Done, over any scroll overflow. The
	// accent band is a Backdrop; Back/Done are inverted toolkit.Button pills (an
	// OnAccent fill with accent text, via a derived theme) and the title a Label —
	// no hand-drawn round-rects or glyph blits.
	band := &toolkit.Backdrop{Fill: th.Accent}
	band.SetBounds(toolkit.Rect{X: 0, Y: 0, W: s.W, H: m.topbarH})
	band.Draw(p, th)

	invTheme := invertedTopbarTheme(th, onAccent)
	back := toolkit.NewButton("‹ Back", nil)
	back.Style = toolkit.ButtonProminent
	back.Font = m.tab.font
	back.SetBounds(s.accBackR)
	back.Draw(p, invTheme)

	title := toolkit.NewLabel("Accounts")
	title.Font, title.Ink = m.title.font, onAccent
	title.SetBounds(toolkit.Rect{
		X: s.accBackR.X + s.accBackR.W + m.pad, Y: (m.topbarH - m.title.height) / 2,
		W: s.W, H: m.title.height,
	})
	title.Draw(p, th)

	done := toolkit.NewButton("Done", nil)
	done.Style = toolkit.ButtonProminent
	done.Font = m.tab.font
	done.SetBounds(s.accDoneR)
	done.Draw(p, invTheme)
}

// accountsHitTest maps a click in the credentials editor to an action.
func (s *Scene) accountsHitTest(x, y int) Hit {
	s.layoutAccounts()
	if inRect(s.accBackR, x, y) || inRect(s.accDoneR, x, y) {
		return Hit{Kind: HitCloseAccounts}
	}
	// Rows/pills scroll under the topbar, which is painted over them; a click in
	// that band must not select a provider or field through the chrome.
	if y < s.m.topbarH {
		return Hit{Kind: HitNone}
	}
	if s.accSignInR.W > 0 && inRect(s.accSignInR, x, y) {
		return Hit{Kind: HitRedditSignIn}
	}
	if s.accImportR.W > 0 && inRect(s.accImportR, x, y) {
		return Hit{Kind: HitImportRedditFirefox}
	}
	if s.accImportSubsR.W > 0 && inRect(s.accImportSubsR, x, y) {
		return Hit{Kind: HitImportFollows, Value: string(s.accSel)}
	}
	if s.accImportSessionR.W > 0 && inRect(s.accImportSessionR, x, y) {
		return Hit{Kind: HitImportSession}
	}
	for _, b := range s.accProvBtns {
		if inRect(b.rect, x, y) {
			return Hit{Kind: HitSelectAccount, Value: string(b.kind)}
		}
	}
	for _, f := range s.accRows {
		if inRect(f.rect, x, y) {
			if f.isBool {
				return Hit{Kind: HitToggleAccountBool, Value: f.key}
			}
			return Hit{Kind: HitFocusAccountField, Value: f.key}
		}
	}
	return Hit{Kind: HitNone}
}
