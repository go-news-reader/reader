package ui

import (
	"image"
	"strings"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"

	"github.com/go-news-reader/reader/internal/settings"
	"github.com/go-news-reader/reader/source"
)

// The in-canvas preferences editor (ModeSettings). It is drawn with the same
// go-widgets painter + anti-aliased text as the rest of the app (no separate
// HTML/native page): a profile switcher with rename/new/delete, the selected
// profile's source subscriptions (add via a source palette + channel field,
// remove per chip), a theme picker, and the media-cache path. Every clickable
// region is an sButton/sChip so hit-testing and drawing stay in lock-step.

// Focus names which settings text field currently has keyboard focus.
type Focus int

// Settings-view focus states.
const (
	FocusNone Focus = iota
	FocusChannel
	FocusRename
	FocusCache
)

// sButton is one clickable pill in the settings view.
type sButton struct {
	rect      toolkit.Rect
	label     string
	kind      HitKind
	value     string
	prof, sub int
	active    bool
	danger    bool
}

// sLabel is a section caption in the settings view.
type sLabel struct {
	x, y int
	text string
}

// sChip is one removable subscription in the edited profile.
type sChip struct {
	rect      toolkit.Rect
	label     string
	source    source.Kind
	prof, sub int
}

// settingsKinds is the source palette offered when adding a subscription.
var settingsKinds = []source.Kind{
	source.Reddit, source.HackerNews, source.Syndication,
	source.Bluesky, source.Lemmy, source.Mastodon, source.Usenet,
}

// OpenSettings enters the preferences view, editing the active profile.
func (s *Scene) OpenSettings() {
	s.mode = ModeSettings
	s.selEdit = s.activeProf
	s.sf = FocusNone
	s.channelInput = ""
	s.renameInput = s.selEditName()
	s.cacheInput = s.cachePath
	s.touch()
}

// CloseSettings returns to the feed view.
func (s *Scene) CloseSettings() { s.mode = ModeFeed; s.touch() }

// Focus reports which settings text field currently has keyboard focus.
func (s *Scene) Focus() Focus { return s.sf }

// selEditName returns the edited profile's name (or "" when out of range).
func (s *Scene) selEditName() string {
	if s.selEdit >= 0 && s.selEdit < len(s.Profiles) {
		return s.Profiles[s.selEdit].Name
	}
	return ""
}

// SelectEditProfile picks which profile the editor operates on (clamped) and
// seeds the rename buffer with its name.
func (s *Scene) SelectEditProfile(i int) {
	if i >= 0 && i < len(s.Profiles) {
		s.selEdit = i
		s.renameInput = s.Profiles[i].Name
		s.sf = FocusNone
		s.touch()
	}
}

// NewProfile appends a fresh profile and selects it for editing.
func (s *Scene) NewProfile() {
	s.Profiles = append(s.Profiles, settings.Profile{Name: "Profile " + itoa(len(s.Profiles)+1)})
	s.selEdit = len(s.Profiles) - 1
	s.renameInput = s.Profiles[s.selEdit].Name
	s.touchProfiles()
}

// DeleteProfile removes profile i (never below one profile), re-clamps the
// active + edited indices, and rebuilds the sidebar.
func (s *Scene) DeleteProfile(i int) {
	if i < 0 || i >= len(s.Profiles) || len(s.Profiles) <= 1 {
		return
	}
	s.Profiles = append(s.Profiles[:i], s.Profiles[i+1:]...)
	if i < s.activeProf {
		s.activeProf--
	}
	if s.activeProf >= len(s.Profiles) {
		s.activeProf = len(s.Profiles) - 1
	}
	if i < s.selEdit {
		s.selEdit--
	}
	if s.selEdit >= len(s.Profiles) {
		s.selEdit = len(s.Profiles) - 1
	}
	s.renameInput = s.selEditName()
	s.rebuildSubs()
	s.touchProfiles()
}

// FocusRename / FocusChannel / FocusCache give keyboard focus to a text field.
func (s *Scene) FocusRename()  { s.sf = FocusRename; s.touch() }
func (s *Scene) FocusChannel() { s.sf = FocusChannel; s.touch() }
func (s *Scene) FocusCache()   { s.sf = FocusCache; s.touch() }

// CommitRename applies the rename buffer to the edited profile (blank ignored).
func (s *Scene) CommitRename() {
	name := strings.TrimSpace(s.renameInput)
	if name != "" && s.selEdit >= 0 && s.selEdit < len(s.Profiles) {
		s.Profiles[s.selEdit].Name = name
		s.touchProfiles()
	}
}

// SelectKind records which source the add-subscription palette will use.
func (s *Scene) SelectKind(k source.Kind) { s.newKind = k; s.touch() }

// AddInputSub appends the typed channel (under the selected source) to the
// edited profile and clears the field. Duplicates are ignored.
func (s *Scene) AddInputSub() {
	ch := strings.TrimSpace(s.channelInput)
	s.channelInput = ""
	if s.selEdit < 0 || s.selEdit >= len(s.Profiles) {
		return
	}
	k := s.newKind
	if k == "" {
		k = source.Reddit
	}
	p := &s.Profiles[s.selEdit]
	for _, su := range p.Subs {
		if su.Source == k && strings.EqualFold(su.Channel, ch) {
			return
		}
	}
	p.Subs = append(p.Subs, source.Subscription{Source: k, Channel: ch, Limit: 25})
	if s.selEdit == s.activeProf {
		s.rebuildSubs()
	}
	s.touchProfiles()
}

// RemoveSub drops subscription sub from profile prof.
func (s *Scene) RemoveSub(prof, sub int) {
	if prof < 0 || prof >= len(s.Profiles) {
		return
	}
	subs := s.Profiles[prof].Subs
	if sub < 0 || sub >= len(subs) {
		return
	}
	s.Profiles[prof].Subs = append(subs[:sub], subs[sub+1:]...)
	if prof == s.activeProf {
		s.rebuildSubs()
	}
	s.touchProfiles()
}

// CommitCache applies the cache-path buffer.
func (s *Scene) CommitCache() { s.cachePath = strings.TrimSpace(s.cacheInput); s.touch() }

// layoutSettings computes every button, chip, label and input rectangle.
func (s *Scene) layoutSettings() {
	s.m = s.computeMetrics()
	m := s.m
	s.sButtons = s.sButtons[:0]
	s.sLabels = s.sLabels[:0]
	s.sChips = s.sChips[:0]
	pad := m.pad
	gap := rpxOf(s, 6)
	btnH := m.btnH

	addBtn := func(x, y int, label string, kind HitKind, value string, prof, sub int, active, danger bool) int {
		w := m.tab.width(label) + rpxOf(s, 20)
		s.sButtons = append(s.sButtons, sButton{
			rect: toolkit.Rect{X: x, Y: y, W: w, H: btnH}, label: label,
			kind: kind, value: value, prof: prof, sub: sub, active: active, danger: danger,
		})
		return x + w + gap
	}
	label := func(x, y int, text string) { s.sLabels = append(s.sLabels, sLabel{x: x, y: y, text: text}) }

	y := m.topbarH + pad

	// Done button (top-right, in the topbar band).
	dw := m.tab.width("Done") + rpxOf(s, 24)
	s.sDoneR = toolkit.Rect{X: s.W - pad - dw, Y: (m.topbarH - btnH) / 2, W: dw, H: btnH}

	// PROFILES switcher.
	label(pad, y, "PROFILES")
	y += m.side.height + gap
	x := pad
	for i, p := range s.Profiles {
		x = addBtn(x, y, p.Name, HitSelectProfile, "", i, 0, i == s.selEdit, false)
	}
	addBtn(x, y, "+ New", HitNewProfile, "", 0, 0, false, false)
	y += btnH + pad

	// Rename field + Delete-profile.
	s.sRenameR = toolkit.Rect{X: pad, Y: y, W: rpxOf(s, 200), H: btnH}
	bx := pad + s.sRenameR.W + gap
	bx = addBtn(bx, y, "Rename", HitRenameProfile, "", s.selEdit, 0, s.sf == FocusRename, false)
	addBtn(bx, y, "Delete profile", HitDeleteProfile, "", s.selEdit, 0, false, true)
	y += btnH + pad

	// SUBSCRIPTIONS of the edited profile (removable chips).
	label(pad, y, "SUBSCRIPTIONS")
	y += m.side.height + gap
	x = pad
	if s.selEdit >= 0 && s.selEdit < len(s.Profiles) {
		for j, su := range s.Profiles[s.selEdit].Subs {
			text := sourceLabel(su.Source)
			if su.Channel != "" {
				text += " · " + su.Channel
			}
			chipW := m.tab.width(text+"  ×") + rpxOf(s, 24)
			if x+chipW > s.W-pad {
				x = pad
				y += btnH + gap
			}
			s.sChips = append(s.sChips, sChip{
				rect: toolkit.Rect{X: x, Y: y, W: chipW, H: btnH}, label: text + "  ×",
				source: su.Source, prof: s.selEdit, sub: j,
			})
			x += chipW + gap
		}
	}
	y += btnH + pad

	// ADD SUBSCRIPTION: source palette + channel field + Add.
	label(pad, y, "ADD SUBSCRIPTION")
	y += m.side.height + gap
	x = pad
	for _, k := range settingsKinds {
		x = addBtn(x, y, sourceLabel(k), HitSelectKind, string(k), 0, 0, s.newKind == k, false)
	}
	y += btnH + gap
	s.sChannelR = toolkit.Rect{X: pad, Y: y, W: rpxOf(s, 220), H: btnH}
	addBtn(pad+s.sChannelR.W+gap, y, "Add", HitAddSub, "", 0, 0, false, false)
	y += btnH + pad

	// APPEARANCE: theme picker.
	label(pad, y, "APPEARANCE")
	y += m.side.height + gap
	x = pad
	for _, tn := range []string{settings.ThemeSystem, settings.ThemeLight, settings.ThemeDark} {
		x = addBtn(x, y, titleCase(tn), HitTheme, tn, 0, 0, s.themeName == tn, false)
	}
	y += btnH + pad

	// MEDIA CACHE: editable path.
	label(pad, y, "MEDIA CACHE")
	y += m.side.height + gap
	s.sCacheR = toolkit.Rect{X: pad, Y: y, W: s.W - 2*pad, H: btnH}
}

// drawSettings paints the preferences editor.
func (s *Scene) drawSettings(buf []byte) {
	s.layoutSettings()
	m := s.m
	p := painter.NewPixelPainter(buf, s.W, s.H)
	img := &image.RGBA{Pix: buf, Stride: s.W * 4, Rect: image.Rect(0, 0, s.W, s.H)}
	th := s.theme
	onAccent := th.Background
	if v, ok := th.Extra["OnAccent"]; ok {
		onAccent = v
	}
	muteS := mute(th.OnSurface, th.Surface)

	p.FillRect(painter.Rect{X: 0, Y: 0, W: s.W, H: s.H}, th.Background)

	// Section labels.
	for _, l := range s.sLabels {
		m.side.draw(img, l.x, l.y, l.text, muteS)
	}

	// Buttons.
	for _, b := range s.sButtons {
		fill, txt := th.Surface, th.OnSurface
		if b.active {
			fill, txt = th.Accent, onAccent
		}
		p.FillRoundRect(painter.Rect(b.rect), rpxOf(s, 6), fill)
		border := th.Border
		if b.danger {
			border, txt = rgb(0xD03030), rgb(0xD03030)
		}
		p.StrokeRoundRect(painter.Rect(b.rect), rpxOf(s, 6), border, 1)
		m.tab.draw(img, b.rect.X+rpxOf(s, 10), b.rect.Y+(b.rect.H-m.tab.height)/2, b.label, txt)
	}

	// Subscription chips.
	for _, c := range s.sChips {
		p.FillRoundRect(painter.Rect(c.rect), rpxOf(s, 6), th.Surface)
		p.StrokeRoundRect(painter.Rect(c.rect), rpxOf(s, 6), th.Border, 1)
		s.drawDot(p, c.rect.X+rpxOf(s, 8), c.rect.Y+c.rect.H/2, sourceColor(c.source))
		m.tab.draw(img, c.rect.X+rpxOf(s, 18), c.rect.Y+(c.rect.H-m.tab.height)/2, c.label, th.OnSurface)
	}

	// Text inputs.
	s.drawInput(p, img, s.sRenameR, s.renameInput, "profile name", s.sf == FocusRename, onAccent, muteS)
	s.drawInput(p, img, s.sChannelR, s.channelInput, "channel…", s.sf == FocusChannel, onAccent, muteS)
	s.drawInput(p, img, s.sCacheR, s.cacheInput, "media cache path", s.sf == FocusCache, onAccent, muteS)

	// Topbar (accent) with title + Done, over any overflow.
	p.FillRect(painter.Rect{X: 0, Y: 0, W: s.W, H: m.topbarH}, th.Accent)
	m.title.draw(img, m.pad, (m.topbarH-m.title.height)/2, "Settings", onAccent)
	p.FillRoundRect(painter.Rect(s.sDoneR), rpxOf(s, 6), onAccent)
	m.tab.draw(img, s.sDoneR.X+rpxOf(s, 12), s.sDoneR.Y+(s.sDoneR.H-m.tab.height)/2, "Done", th.Accent)
}

// drawInput paints one text field with placeholder + caret.
func (s *Scene) drawInput(p *painter.PixelPainter, img *image.RGBA, r toolkit.Rect, text, placeholder string, focused bool, onAccent, muteS toolkit.RGBA) {
	if r.W == 0 {
		return
	}
	m := s.m
	th := s.theme
	p.FillRoundRect(painter.Rect(r), rpxOf(s, 6), th.Surface)
	border := th.Border
	if focused {
		border = th.Accent
	}
	p.StrokeRoundRect(painter.Rect(r), rpxOf(s, 6), border, rpxOf(s, 1))
	txt, col := text, th.OnSurface
	if txt == "" {
		txt, col = placeholder, muteS
	}
	tx := r.X + rpxOf(s, 8)
	ty := r.Y + (r.H-m.tab.height)/2
	m.tab.draw(img, tx, ty, txt, col)
	if focused && text != "" {
		cx := tx + m.tab.width(text) + rpxOf(s, 1)
		p.FillRect(painter.Rect{X: cx, Y: ty, W: rpxOf(s, 2), H: m.tab.height}, th.OnSurface)
	}
	_ = onAccent
}

// hitSettings maps a click in the preferences view to an action.
func (s *Scene) hitSettings(x, y int) Hit {
	s.layoutSettings()
	if inRect(s.sDoneR, x, y) {
		return Hit{Kind: HitCloseSettings}
	}
	if inRect(s.sRenameR, x, y) {
		return Hit{Kind: HitRenameProfile, Profile: s.selEdit}
	}
	if inRect(s.sChannelR, x, y) {
		return Hit{Kind: HitFocusChannel}
	}
	if inRect(s.sCacheR, x, y) {
		return Hit{Kind: HitFocusCache}
	}
	for _, c := range s.sChips {
		if inRect(c.rect, x, y) {
			return Hit{Kind: HitRemoveSub, Profile: c.prof, Sub: c.sub}
		}
	}
	for _, b := range s.sButtons {
		if inRect(b.rect, x, y) {
			return Hit{Kind: b.kind, Value: b.value, Profile: b.prof, Sub: b.sub}
		}
	}
	return Hit{Kind: HitNone}
}

// --- tiny helpers ----------------------------------------------------------

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func titleCase(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	return strings.ToUpper(string(r[0])) + string(r[1:])
}
