package ui

import (
	"strconv"
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
	FocusCacheSize    // the media-cache size-limit (MB) field
	FocusCacheBackend // the media-cache backend (plugin path) field
	FocusZoomIn       // the "zoom in" browser-shortcut key field
	FocusZoomOut      // the "zoom out" browser-shortcut key field
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
	source.Redgifs,
}

// signInBrowsers is the sign-in-browser picker (label + persisted value), in
// display order. "Default" is the system default browser.
var signInBrowsers = []struct{ label, value string }{
	{"Default", settings.SignInBrowserDefault},
	{"Firefox", settings.SignInBrowserFirefox},
	{"Chrome", settings.SignInBrowserChrome},
	{"Safari", settings.SignInBrowserSafari},
	{"Edge", settings.SignInBrowserEdge},
}

// --- settings widgets (box-composed; the layout below positions
// them with toolkit boxes, and drawSettings renders each as a widget rather than
// hand-drawing pills/labels/fields) ---

// layoutBtnRow lays specs left-to-right in a toolkit HBox at (x, y) and appends
// the resulting sButtons (with their box-computed rects) for drawing + hit-test.
// AddFixed(content width) reproduces the previous manual flow's positions.
func (s *Scene) layoutBtnRow(x, y int, specs []sButton) {
	m := s.m
	row := toolkit.NewHBox()
	row.Spacing = rpxOf(s, 6)
	// Placeholder slots just to read back the box-computed rects; the pills
	// themselves are drawn as generic toolkit.Button in drawSettings.
	ws := make([]*flowSlot, len(specs))
	for i := range specs {
		ws[i] = &flowSlot{}
		row.AddFixed(ws[i], m.tab.width(specs[i].label)+rpxOf(s, 20))
	}
	row.SetBounds(toolkit.Rect{X: x, Y: y, W: s.W, H: m.btnH})
	for i := range specs {
		specs[i].rect = ws[i].Bounds()
		s.sButtons = append(s.sButtons, specs[i])
	}
}

// OpenSettings enters the preferences view, editing the active profile.
func (s *Scene) OpenSettings() {
	s.mode = ModeSettings
	s.selEdit = s.activeProf
	s.sf = FocusNone
	s.channelInput = ""
	s.renameInput = s.selEditName()
	s.cacheInput = s.cachePath
	s.cacheSizeInput = strconv.Itoa(s.cacheSizeMB)
	s.cacheBackendInput = s.cacheBackend
	s.zoomInInput = s.BrowserZoomInKey()
	s.zoomOutInput = s.BrowserZoomOutKey()
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

// FocusCacheSize gives keyboard focus to the media-cache size-limit (MB) field.
func (s *Scene) FocusCacheSize() { s.sf = FocusCacheSize; s.touch() }

// FocusCacheBackend gives keyboard focus to the cache-backend (plugin path) field.
func (s *Scene) FocusCacheBackend() { s.sf = FocusCacheBackend; s.touch() }

// FocusZoomIn / FocusZoomOut give keyboard focus to a zoom-key field.
func (s *Scene) FocusZoomIn()  { s.sf = FocusZoomIn; s.touch() }
func (s *Scene) FocusZoomOut() { s.sf = FocusZoomOut; s.touch() }

// CommitZoomKeys applies the two zoom-key buffers (each a single printable rune)
// to the browser zoom bindings. A blank buffer leaves that binding unchanged.
func (s *Scene) CommitZoomKeys() { s.SetBrowserZoomKeys(s.zoomInInput, s.zoomOutInput) }

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

// normalizeChannel canonicalises a user-typed subscription channel. For Reddit a
// bare name is a subreddit, so it gains an "r/" prefix; an explicit "r/" or "u/"
// (a redditor's feed) is kept verbatim so the sidebar and feed filter both show
// and match the prefixed form. A "search:<query>" channel — a saved keyword
// search that the Reddit provider turns into a live post-search feed — is also
// kept verbatim (it is not a subreddit). Other sources are returned unchanged.
func normalizeChannel(k source.Kind, ch string) string {
	ch = strings.TrimSpace(ch)
	if k != source.Reddit || ch == "" {
		return ch
	}
	low := strings.ToLower(ch)
	if strings.HasPrefix(low, "r/") || strings.HasPrefix(low, "u/") || strings.HasPrefix(low, "search:") {
		return ch
	}
	return "r/" + ch
}

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
	ch = normalizeChannel(k, ch)
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

// CommitCacheBackend applies the cache-backend buffer: blank / "local" keeps the
// built-in disk cache, a filesystem path selects an external cache plugin. The
// change takes effect on the next launch (the backend is chosen at startup), like
// the plugins directory.
func (s *Scene) CommitCacheBackend() {
	s.cacheBackend = strings.TrimSpace(s.cacheBackendInput)
	s.touch()
}

// CommitCacheSize applies the media-cache size-limit (MB) buffer: on a valid
// integer it sets the limit, clamped to [MinMediaCacheMB, MaxMediaCacheMB]
// (0 → DefaultMediaCacheMB); on a parse error the current value is kept. Either
// way the buffer is re-synced to the resulting value so the field shows the
// real, clamped limit rather than the raw typed text.
func (s *Scene) CommitCacheSize() {
	if mb, err := strconv.Atoi(strings.TrimSpace(s.cacheSizeInput)); err == nil {
		s.cacheSizeMB = settings.ClampMediaCacheMB(mb)
	}
	s.cacheSizeInput = strconv.Itoa(s.cacheSizeMB)
	s.touch()
}

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

	label := func(x, y int, text string) { s.sLabels = append(s.sLabels, sLabel{x: x, y: y, text: text}) }

	y := m.topbarH + pad

	// Done button (top-right, in the topbar band).
	dw := m.tab.width("Done") + rpxOf(s, 24)
	s.sDoneR = toolkit.Rect{X: s.W - pad - dw, Y: (m.topbarH - btnH) / 2, W: dw, H: btnH}

	// PROFILES switcher — a button row laid out by an HBox.
	label(pad, y, "PROFILES")
	y += m.side.height + gap
	profSpecs := make([]sButton, 0, len(s.Profiles)+1)
	for i, p := range s.Profiles {
		profSpecs = append(profSpecs, sButton{label: p.Name, kind: HitSelectProfile, prof: i, active: i == s.selEdit})
	}
	profSpecs = append(profSpecs, sButton{label: "+ New", kind: HitNewProfile})
	s.layoutBtnRow(pad, y, profSpecs)
	y += btnH + pad

	// Rename field + Delete-profile (buttons flow after the docked input).
	s.sRenameR = toolkit.Rect{X: pad, Y: y, W: rpxOf(s, 200), H: btnH}
	s.layoutBtnRow(pad+s.sRenameR.W+gap, y, []sButton{
		{label: "Rename", kind: HitRenameProfile, prof: s.selEdit, active: s.sf == FocusRename},
		{label: "Delete profile", kind: HitDeleteProfile, prof: s.selEdit, danger: true},
	})
	y += btnH + pad

	// SUBSCRIPTIONS of the edited profile (removable chips, a wrapping flow).
	label(pad, y, "SUBSCRIPTIONS")
	y += m.side.height + gap
	x := pad
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
	kindSpecs := make([]sButton, 0, len(settingsKinds))
	for _, k := range settingsKinds {
		kindSpecs = append(kindSpecs, sButton{label: sourceLabel(k), kind: HitSelectKind, value: string(k), active: s.newKind == k})
	}
	s.layoutBtnRow(pad, y, kindSpecs)
	y += btnH + gap
	s.sChannelR = toolkit.Rect{X: pad, Y: y, W: rpxOf(s, 220), H: btnH}
	s.layoutBtnRow(pad+s.sChannelR.W+gap, y, []sButton{{label: "Add", kind: HitAddSub}})
	y += btnH + pad

	// APPEARANCE: theme picker.
	label(pad, y, "APPEARANCE")
	y += m.side.height + gap
	themeSpecs := make([]sButton, 0, 3)
	for _, tn := range []string{settings.ThemeSystem, settings.ThemeLight, settings.ThemeDark} {
		themeSpecs = append(themeSpecs, sButton{label: titleCase(tn), kind: HitTheme, value: tn, active: s.themeName == tn})
	}
	s.layoutBtnRow(pad, y, themeSpecs)
	y += btnH + pad

	// SIGN-IN BROWSER: which browser a provider sign-in (Reddit) launches. Cookie
	// import only works from Firefox, so it is both the default and the caption's
	// note.
	label(pad, y, "SIGN-IN BROWSER")
	y += m.side.height + gap
	label(pad, y, "Launches Reddit sign-in; only Firefox's session can be imported")
	y += m.side.height + gap
	signInSpecs := make([]sButton, 0, len(signInBrowsers))
	for _, b := range signInBrowsers {
		signInSpecs = append(signInSpecs, sButton{label: b.label, kind: HitSignInBrowser, value: b.value, active: s.signInBrowser == b.value})
	}
	s.layoutBtnRow(pad, y, signInSpecs)
	y += btnH + pad

	// WEB PREVIEW: browser tab-mode picker (multiple / single).
	label(pad, y, "WEB PREVIEW")
	y += m.side.height + gap
	single := s.BrowserSingleTab()
	s.layoutBtnRow(pad, y, []sButton{
		{label: "Multiple tabs", kind: HitBrowserTabs, value: "multi", active: !single},
		{label: "Single tab", kind: HitBrowserTabs, value: "single", active: single},
	})
	y += btnH + pad

	// Browser toolbar (address bar + nav) visibility.
	hidden := s.BrowserChromeHidden()
	s.layoutBtnRow(pad, y, []sButton{
		{label: "Toolbar shown", kind: HitBrowserChrome, value: "shown", active: !hidden},
		{label: "Toolbar hidden", kind: HitBrowserChrome, value: "hidden", active: hidden},
	})
	y += btnH + pad

	// RENDER CACHE: a "Clear render cache" button plus a caption showing the
	// current size, so a rendered page is not re-rendered when re-selected and the
	// cache can be dropped on demand.
	label(pad, y, "RENDER CACHE")
	y += m.side.height + gap
	pages, cbytes := 0, 0
	if s.renderCacheStats != nil {
		pages, cbytes = s.renderCacheStats()
	}
	label(pad, y, itoa(pages)+" pages cached ("+itoa(cbytes>>20)+" MB)")
	y += m.side.height + gap
	s.layoutBtnRow(pad, y, []sButton{{label: "Clear render cache", kind: HitClearRenderCache}})
	y += btnH + pad

	// ZOOM SHORTCUT KEYS: two single-rune fields for the Ctrl/Cmd + key binding
	// that zooms the web preview in / out.
	label(pad, y, "ZOOM SHORTCUT KEYS (with Ctrl/Cmd)")
	y += m.side.height + gap
	kw := rpxOf(s, 54)
	labelY := y + (btnH-m.side.height)/2
	label(pad, labelY, "Zoom in")
	fx := pad + m.tab.width("Zoom in") + gap
	s.sZoomInR = toolkit.Rect{X: fx, Y: y, W: kw, H: btnH}
	fx += kw + rpxOf(s, 18)
	label(fx, labelY, "Zoom out")
	fx += m.tab.width("Zoom out") + gap
	s.sZoomOutR = toolkit.Rect{X: fx, Y: y, W: kw, H: btnH}
	y += btnH + pad

	// MEDIA CACHE: editable path plus a size limit (MB) that caps the on-disk
	// thumbnail cache; the pruner keeps the directory under this budget.
	label(pad, y, "MEDIA CACHE")
	y += m.side.height + gap
	s.sCacheR = toolkit.Rect{X: pad, Y: y, W: s.W - 2*pad, H: btnH}
	y += btnH + pad
	label(pad, y, "Cache limit (MB)")
	y += m.side.height + gap
	s.sCacheSizeR = toolkit.Rect{X: pad, Y: y, W: s.W - 2*pad, H: btnH}
	y += btnH + pad
	// Backend selector: blank/"local" = built-in disk cache, a path = a cache
	// plugin binary (a shared/network cache) launched at startup.
	label(pad, y, "Cache backend (plugin path)")
	y += m.side.height + gap
	s.sCacheBackendR = toolkit.Rect{X: pad, Y: y, W: s.W - 2*pad, H: btnH}
	y += btnH + pad

	// FEED: infinite scroll on/off — fetch and append the next page of posts when
	// the feed list is scrolled to the bottom. Placed last so it does not shift the
	// sections above. (Pull-to-refresh at the top is always on and needs no toggle.)
	label(pad, y, "FEED")
	y += m.side.height + gap
	label(pad, y, "Fetch the next page when you scroll to the bottom (pull down at the top to refresh)")
	y += m.side.height + gap
	infinite := s.InfiniteScroll()
	s.layoutBtnRow(pad, y, []sButton{
		{label: "Infinite scroll on", kind: HitInfiniteScroll, value: "on", active: infinite},
		{label: "Infinite scroll off", kind: HitInfiniteScroll, value: "off", active: !infinite},
	})
	y += btnH + pad
	label(pad, y, "Close the preview pane when you switch to another subscription or group")
	y += m.side.height + gap
	dismiss := s.DismissPreviewOnSwitch()
	s.layoutBtnRow(pad, y, []sButton{
		{label: "Dismiss preview on switch", kind: HitDismissPreviewOnSwitch, value: "on", active: dismiss},
		{label: "Keep preview open", kind: HitDismissPreviewOnSwitch, value: "off", active: !dismiss},
	})
	y += btnH + pad

	// SECURITY: biometric unlock of the credential vault. Fully enforced on macOS
	// today (Touch ID); on Windows/Linux the write-side gate is stored and the
	// interactive prompt (Windows Hello / a desktop authentication agent) follows
	// once their verifiers land, so the switch is forward-compatible everywhere.
	label(pad, y, "SECURITY")
	y += m.side.height + gap
	label(pad, y, "Unlock the saved sign-ins with your fingerprint or face (Touch ID / Windows Hello) instead of a password — needs a code-signed build")
	y += m.side.height + gap
	bio := s.BiometricUnlock()
	s.layoutBtnRow(pad, y, []sButton{
		{label: "Biometric unlock on", kind: HitBiometricUnlock, value: "on", active: bio},
		{label: "Biometric unlock off", kind: HitBiometricUnlock, value: "off", active: !bio},
	})
	y += btnH + pad

	// The editor can be taller than the surface (many subscriptions, high zoom, a
	// short window); the whole body scrolls, with the topbar + Done painted over
	// the overflow. Everything above was laid out unscrolled; clamp the offset
	// (robust to a taller window) and shift the scrollable elements up by it. The
	// Done button lives in the fixed topbar band, so it is not shifted.
	s.settingsScroll.refresh(y-m.topbarH, s.H-m.topbarH)
	if dy := -s.settingsScroll.offset; dy != 0 {
		for i := range s.sButtons {
			s.sButtons[i].rect.Y += dy
		}
		for i := range s.sChips {
			s.sChips[i].rect.Y += dy
		}
		for i := range s.sLabels {
			s.sLabels[i].y += dy
		}
		s.sRenameR.Y += dy
		s.sChannelR.Y += dy
		s.sCacheR.Y += dy
		s.sCacheSizeR.Y += dy
		s.sCacheBackendR.Y += dy
		s.sZoomInR.Y += dy
		s.sZoomOutR.Y += dy
	}
}

// drawSettings paints the preferences editor.
func (s *Scene) drawSettings(buf []byte) {
	s.layoutSettings()
	m := s.m
	p := painter.NewPixelPainter(buf, s.W, s.H)
	th := s.theme
	onAccent := themeOnAccent(th)
	muteS := mute(th.OnSurface, th.Surface)

	// The full-surface ground is a toolkit.Backdrop (a solid fill == the old
	// Background FillRect), so even the backing is a composed widget rather than
	// a hand-drawn rectangle.
	bg := &toolkit.Backdrop{Fill: th.Background}
	bg.SetBounds(toolkit.Rect{X: 0, Y: 0, W: s.W, H: s.H})
	bg.Draw(p, th)

	// Every element is a stock go-widgets widget with the reader's fallback font.
	// Section captions are toolkit.Label (muted ink).
	labelFont := ttFont(false, rpxOf(s, 13))
	for _, l := range s.sLabels {
		w := toolkit.NewLabel(l.text)
		w.Font, w.Ink = labelFont, muteS
		w.SetBounds(toolkit.Rect{X: l.x, Y: l.y, W: s.W, H: m.side.height})
		w.Draw(p, th)
	}
	// Pills are toolkit.Button (Selected = active choice, ButtonDanger = destructive).
	pillFont := ttFont(true, rpxOf(s, 12))
	for _, b := range s.sButtons {
		w := toolkit.NewButton(b.label, nil)
		w.Selected().Set(b.active)
		w.Font = pillFont
		if b.danger {
			w.Style = toolkit.ButtonDanger
		}
		w.SetBounds(b.rect)
		w.Draw(p, th)
	}
	// Subscription chips are toolkit.Chip with a source-coloured leading dot.
	for _, c := range s.sChips {
		w := toolkit.NewChip(c.label)
		w.Dot, w.Font = sourceColor(c.source), pillFont
		w.SetBounds(c.rect)
		w.Draw(p, th)
	}
	for _, f := range []struct {
		r    toolkit.Rect
		text string
		ph   string
		foc  bool
	}{
		{s.sRenameR, s.renameInput, "profile name", s.sf == FocusRename},
		{s.sChannelR, s.channelInput, "channel…", s.sf == FocusChannel},
		{s.sCacheR, s.cacheInput, "media cache path", s.sf == FocusCache},
		{s.sCacheSizeR, s.cacheSizeInput, "cache limit (MB)", s.sf == FocusCacheSize},
		{s.sCacheBackendR, s.cacheBackendInput, "local (or /path/to/cache-plugin)", s.sf == FocusCacheBackend},
		{s.sZoomInR, s.zoomInInput, "=", s.sf == FocusZoomIn},
		{s.sZoomOutR, s.zoomOutInput, "-", s.sf == FocusZoomOut},
	} {
		// Text fields are generic toolkit.Entry (placeholder from the toolkit's own
		// Entry.Placeholder), with the caret parked at the end since the reader
		// drives the text itself.
		w := toolkit.NewEntry(f.text)
		w.Placeholder = f.ph
		w.SetFocused(f.foc)
		w.Font = ttFont(true, rpxOf(s, 12))
		w.SetBounds(f.r)
		w.Draw(p, th)
	}

	// Topbar over any overflow, composed from widgets: an accent Backdrop band,
	// the title as a toolkit.Label, and a stock toolkit.Button for Done. Its rect
	// is s.sDoneR, so hit-testing in hitSettings is unchanged. (The Done label is
	// centred, which lands at the same x as the old +12px inset because sDoneR is
	// the text width plus 24px.)
	bar := &toolkit.Backdrop{Fill: th.Accent}
	bar.SetBounds(toolkit.Rect{X: 0, Y: 0, W: s.W, H: m.topbarH})
	bar.Draw(p, th)
	title := toolkit.NewLabel("Settings")
	title.Font, title.Ink = m.title.font, onAccent
	title.SetBounds(toolkit.Rect{X: m.pad, Y: (m.topbarH - m.title.height) / 2, W: s.W, H: m.title.height})
	title.Draw(p, th)
	done := toolkit.NewButton("Done", nil)
	done.Font = m.tab.font
	done.SetBounds(s.sDoneR)
	done.Draw(p, th)
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
	if inRect(s.sCacheSizeR, x, y) {
		return Hit{Kind: HitFocusCacheSize}
	}
	if inRect(s.sCacheBackendR, x, y) {
		return Hit{Kind: HitFocusCacheBackend}
	}
	if inRect(s.sZoomInR, x, y) {
		return Hit{Kind: HitFocusZoomIn}
	}
	if inRect(s.sZoomOutR, x, y) {
		return Hit{Kind: HitFocusZoomOut}
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
