package ui

import (
	"image"
	"strings"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"

	"github.com/go-news-reader/reader/internal/settings"
	"github.com/go-news-reader/reader/source"
)

// The in-canvas per-provider credentials editor (ModeAccounts). Its primary
// purpose is to authenticate Reddit: pasting a client id + secret switches the
// Reddit provider from the anonymous, intermittently-403ing ".json" endpoints
// to OAuth against oauth.reddit.com. The view is a provider selector plus the
// selected provider's credential fields (from settings.CredentialSchema),
// rendered with the same painter + anti-aliased text as the rest of the app.
// Secret fields are masked; the Usenet TLS field renders as a toggle.

// accProvBtn is one provider pill in the selector.
type accProvBtn struct {
	rect   toolkit.Rect
	kind   source.Kind
	label  string
	active bool
}

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
	s.accScrollY = 0 // a fresh open starts at the top, like the other views
	s.touch()
}

// CloseAccounts returns to the feed view.
func (s *Scene) CloseAccounts() { s.mode = ModeFeed; s.touch() }

// SelectAccount picks which provider the editor operates on.
func (s *Scene) SelectAccount(k source.Kind) {
	s.accSel = k
	s.accFocus = ""
	s.accScrollY = 0 // the new provider has a different field set; start at the top
	s.touch()
}

// SelectedAccount reports which provider the accounts editor is operating on.
func (s *Scene) SelectedAccount() source.Kind { return s.accSel }

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

// mask returns a bullet string the width (in runes) of secret, for display.
func mask(secret string) string { return strings.Repeat("•", len([]rune(secret))) }

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
	y := top - s.accScrollY

	// Provider selector.
	label(pad, y, "PROVIDER")
	y += m.side.height + gap
	x := pad
	for _, pc := range settings.CredentialSchema() {
		lbl := pc.Label
		if s.accConfigured(pc.Kind) {
			lbl = "• " + pc.Label // a leading dot marks a configured provider
		}
		w := m.tab.width(lbl) + rpxOf(s, 20)
		if x+w > s.W-pad {
			x = pad
			y += btnH + gap
		}
		s.accProvBtns = append(s.accProvBtns, accProvBtn{
			rect: toolkit.Rect{X: x, Y: y, W: w, H: btnH}, kind: pc.Kind, label: lbl, active: pc.Kind == s.accSel,
		})
		x += w + gap
	}
	y += btnH + pad

	// Selected provider's credential fields.
	pc := credsFor(s.accSel)
	label(pad, y, strings.ToUpper(pc.Label)+" CREDENTIALS")
	y += m.side.height + gap
	if s.accSel == source.Reddit {
		label(pad, y, "Create a script app at reddit.com/prefs/apps → paste client id + secret")
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
	s.accContentH = (y + s.accScrollY) - m.topbarH
}

// drawAccounts paints the credentials editor.
func (s *Scene) drawAccounts(buf []byte) {
	s.layoutAccounts()
	m := s.m
	p := painter.NewPixelPainter(buf, s.W, s.H)
	img := &image.RGBA{Pix: buf, Stride: s.W * 4, Rect: image.Rect(0, 0, s.W, s.H)}
	th := s.theme
	onAccent := themeOnAccent(th)
	muteS := mute(th.OnSurface, th.Surface)

	p.FillRect(painter.Rect{X: 0, Y: 0, W: s.W, H: s.H}, th.Background)

	// Every element is drawn as a widget positioned at its box-computed rect,
	// reusing the settings editor's pill/field widgets.
	for _, l := range s.accLabels {
		lbl := &textLine{face: m.side, text: l.text, ink: muteS, img: img}
		lbl.SetBounds(toolkit.Rect{X: l.x, Y: l.y, W: s.W, H: m.side.height})
		lbl.Draw(p, th)
	}
	for _, b := range s.accProvBtns {
		w := &settingsButton{s: s, label: b.label, active: b.active, p: p, img: img}
		w.SetBounds(b.rect)
		w.Draw(p, th)
	}
	for _, f := range s.accRows {
		if f.isBool {
			lbl := "Off"
			if s.accFieldValue(s.accSel, f.key) == "true" {
				lbl = "On"
			}
			w := &settingsButton{s: s, label: lbl, active: lbl == "On", p: p, img: img}
			w.SetBounds(f.rect)
			w.Draw(p, th)
			continue
		}
		val := s.accFieldValue(s.accSel, f.key)
		if f.secret {
			val = mask(val)
		}
		w := &settingsField{s: s, text: val, placeholder: "…", focused: f.focused, p: p, img: img}
		w.SetBounds(f.rect)
		w.Draw(p, th)
	}

	// Topbar (accent) with Back, title and Done, over any scroll overflow.
	p.FillRect(painter.Rect{X: 0, Y: 0, W: s.W, H: m.topbarH}, th.Accent)
	p.FillRoundRect(painter.Rect(s.accBackR), rpxOf(s, 6), onAccent)
	m.tab.draw(img, s.accBackR.X+rpxOf(s, 10), s.accBackR.Y+(s.accBackR.H-m.tab.height)/2, "‹ Back", th.Accent)
	m.title.draw(img, s.accBackR.X+s.accBackR.W+m.pad, (m.topbarH-m.title.height)/2, "Accounts", onAccent)
	p.FillRoundRect(painter.Rect(s.accDoneR), rpxOf(s, 6), onAccent)
	m.tab.draw(img, s.accDoneR.X+rpxOf(s, 12), s.accDoneR.Y+(s.accDoneR.H-m.tab.height)/2, "Done", th.Accent)
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
