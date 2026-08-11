// Package settings is the news reader's persisted preferences: named profiles
// (tabs) that each group a subset of source subscriptions — so "Home", "Tech"
// and "News" feeds stay separate — plus the chosen theme and the media-cache
// location. The model is plain data (a subscription is a source.Subscription:
// source + channel + sort + limit), and file persistence is a thin JSON layer.
package settings

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/go-news-reader/reader/source"
)

// appDir is the per-user config/cache subdirectory name.
const appDir = "go-news-reader"

// Theme options. "system" follows the OS/browser (and the per-OS native look).
const (
	ThemeSystem = "system"
	ThemeLight  = "light"
	ThemeDark   = "dark"
)

// Sign-in browser options. This is the browser the reader launches for a
// provider's browser sign-in flow (today: Reddit). "default" opens the system
// default browser; the named values target a specific installed browser.
//
// Cookie import (see [Settings] Reddit account handling) can only read Firefox's
// plaintext cookies.sqlite, so [DefaultSignInBrowser] is Firefox — signing in
// there is the one flow whose session the reader can subsequently import.
const (
	SignInBrowserDefault = "default"
	SignInBrowserFirefox = "firefox"
	SignInBrowserChrome  = "chrome"
	SignInBrowserSafari  = "safari"
	SignInBrowserEdge    = "edge"
)

// DefaultSignInBrowser is the browser a fresh install launches for sign-in:
// Firefox, the only browser whose session cookie the reader can import.
const DefaultSignInBrowser = SignInBrowserFirefox

// Profile is a named tab grouping a subset of source subscriptions.
type Profile struct {
	Name string                `json:"name"`
	Subs []source.Subscription `json:"subs"`
}

// Settings is the whole persisted preference set.
type Settings struct {
	Profiles  []Profile `json:"profiles"`
	Active    int       `json:"active"`              // index into Profiles
	Theme     string    `json:"theme"`               // system|light|dark
	CachePath string    `json:"cachePath,omitempty"` // media cache dir (repositionable)
	// MediaCacheMB caps the on-disk media (thumbnail) cache, in mebibytes. 0 (a
	// settings file predating the field) means the default; Normalize clamps a set
	// value to [MinMediaCacheMB, MaxMediaCacheMB]. Read the effective byte budget
	// through [Settings.MediaCacheBytes].
	MediaCacheMB int       `json:"mediaCacheMB,omitempty"`
	Accounts     []Account `json:"accounts,omitempty"` // per-provider credentials
	// BrowserSingleTab makes the web-preview browser reuse a single tab instead of
	// keeping a switchable multi-tab strip. It is a tri-state pointer so an unset
	// preference (a fresh install, or a settings file predating this field) can be
	// told apart from an explicit false: nil defaults to single-tab
	// (DefaultBrowserSingleTab) so no tab strip shows out of the box, while a user
	// who opts into multiple tabs persists an explicit false that survives reload.
	// Read it through [Settings.SingleTab], which applies that default.
	BrowserSingleTab *bool `json:"browserSingleTab,omitempty"`
	// ZoomInKey / ZoomOutKey are the base keys (each a single printable rune,
	// stored as a 1-rune string) that, held with a command-style modifier
	// (Ctrl/Cmd), zoom the web preview in / out. Default "=" / "-".
	ZoomInKey  string `json:"zoomInKey,omitempty"`
	ZoomOutKey string `json:"zoomOutKey,omitempty"`

	// Bookmarks are the saved web-preview page URLs (the address bar's star adds
	// / removes the current page). Order is not significant.
	Bookmarks []string `json:"bookmarks,omitempty"`

	// HideBrowserChrome hides the web-preview browser's toolbar + address bar
	// (nav / zoom / URL), rendering the page chrome-free. Default false (shown).
	HideBrowserChrome bool `json:"hideBrowserChrome,omitempty"`

	// SignInBrowser names the browser the reader launches for a provider's
	// browser sign-in flow (today: Reddit) — one of default|firefox|chrome|
	// safari|edge. Blank means the default (Firefox); Normalize backfills it.
	SignInBrowser string `json:"signInBrowser,omitempty"`

	// InfiniteScroll enables fetching and appending the next page of posts when
	// the feed list is scrolled to the bottom. Like BrowserSingleTab it is a
	// tri-state pointer so an unset preference (a fresh install, or a settings file
	// predating this field) is told apart from an explicit false: nil defaults to
	// enabled (DefaultInfiniteScroll) so infinite scroll is on out of the box, while
	// a user who turns it off persists an explicit false that survives reload. Read
	// it through [Settings.InfiniteScrollEnabled], which applies that default.
	InfiniteScroll *bool `json:"infiniteScroll,omitempty"`

	// PluginsDir is where the reader looks for external feed-source plugin
	// executables (loaded over gRPC via hashicorp/go-plugin). Empty — the default
	// for a fresh install or a settings file predating the field — means the
	// standard per-user location; read the effective directory through
	// [Settings.PluginsDirOrDefault]. Set it to point the reader at a custom
	// directory of plugin binaries.
	PluginsDir string `json:"pluginsDir,omitempty"`

	// PreviewTextScale multiplies the reader/preview text size (a text/self
	// post's title, body and meta, and the header of any previewed post). The
	// A−/A+ controls in the preview toolbar adjust it. DefaultPreviewTextScale
	// (1.25) is the fresh default because the base size reads too small.
	// Normalize clamps it to [MinPreviewTextScale, MaxPreviewTextScale], treating
	// 0 (a settings file predating the field) as unset and backfilling the
	// default.
	PreviewTextScale float64 `json:"previewTextScale,omitempty"`
}

// Preview-text scale defaults and bounds. The base preview font read too small,
// so a fresh install starts at DefaultPreviewTextScale (25% larger); the A−/A+
// controls move it within [MinPreviewTextScale, MaxPreviewTextScale].
const (
	DefaultPreviewTextScale = 1.25
	MinPreviewTextScale     = 0.8
	MaxPreviewTextScale     = 2.5
)

// ClampPreviewTextScale confines f to the supported preview-text scale range,
// backfilling a non-positive value (0 from a settings file predating the field)
// to DefaultPreviewTextScale so callers always get a usable multiplier.
func ClampPreviewTextScale(f float64) float64 {
	switch {
	case f <= 0:
		return DefaultPreviewTextScale
	case f < MinPreviewTextScale:
		return MinPreviewTextScale
	case f > MaxPreviewTextScale:
		return MaxPreviewTextScale
	default:
		return f
	}
}

// Media-cache size defaults and bounds. The on-disk thumbnail cache is pruned
// back under this budget; a fresh install uses DefaultMediaCacheMB, and a user
// edit is clamped to [MinMediaCacheMB, MaxMediaCacheMB].
const (
	DefaultMediaCacheMB = 256
	MinMediaCacheMB     = 16
	MaxMediaCacheMB     = 8192
)

// ClampMediaCacheMB confines mb to the supported media-cache range, backfilling
// a non-positive value (0 from a settings file predating the field) to
// DefaultMediaCacheMB so callers always get a usable budget.
func ClampMediaCacheMB(mb int) int {
	switch {
	case mb <= 0:
		return DefaultMediaCacheMB
	case mb < MinMediaCacheMB:
		return MinMediaCacheMB
	case mb > MaxMediaCacheMB:
		return MaxMediaCacheMB
	default:
		return mb
	}
}

// MediaCacheBytes reports the effective on-disk media-cache budget in bytes,
// applying the default and clamping (see ClampMediaCacheMB) before converting
// mebibytes to bytes.
func (s Settings) MediaCacheBytes() int64 {
	return int64(ClampMediaCacheMB(s.MediaCacheMB)) << 20
}

// Default zoom-shortcut base keys (a real-browser feel: Ctrl/Cmd + "="/"-").
const (
	DefaultZoomInKey  = "="
	DefaultZoomOutKey = "-"
)

// DefaultBrowserSingleTab is the web-preview tab mode a fresh install uses:
// single-tab, so no tab strip shows in the preview until the user opts into
// multiple tabs.
const DefaultBrowserSingleTab = true

// DefaultInfiniteScroll is whether a fresh install fetches the next page of
// posts on scrolling to the bottom of the feed: enabled.
const DefaultInfiniteScroll = true

// boolPtr returns a pointer to b, for the tri-state BrowserSingleTab field.
func boolPtr(b bool) *bool { return &b }

// SingleTab reports the effective web-preview tab mode, applying the
// single-tab default when the preference is unset (nil).
func (s *Settings) SingleTab() bool {
	if s.BrowserSingleTab == nil {
		return DefaultBrowserSingleTab
	}
	return *s.BrowserSingleTab
}

// InfiniteScrollEnabled reports the effective infinite-scroll setting, applying
// the enabled default when the preference is unset (nil).
func (s *Settings) InfiniteScrollEnabled() bool {
	if s.InfiniteScroll == nil {
		return DefaultInfiniteScroll
	}
	return *s.InfiniteScroll
}

// Account holds a user's credentials for one provider. Fields are keyed by the
// well-known names from [CredentialSchema] (e.g. "client_id", "instance",
// "token", "addr", "tls"). At most one Account exists per Kind.
//
// Secrets (client secrets, tokens, passwords, session cookies) are persisted in
// the settings.json file, which is written with mode 0600. A future improvement
// is a platform Keychain / secret-store backend; today the values live on disk.
type Account struct {
	Kind   source.Kind       `json:"kind"`
	Fields map[string]string `json:"fields,omitempty"`
}

// Account returns the stored account for kind, if any.
func (s *Settings) Account(kind source.Kind) (Account, bool) {
	for _, a := range s.Accounts {
		if a.Kind == kind {
			return a, true
		}
	}
	return Account{}, false
}

// SetAccount upserts a by its Kind (replacing any existing account for that
// provider, else appending).
func (s *Settings) SetAccount(a Account) {
	for i := range s.Accounts {
		if s.Accounts[i].Kind == a.Kind {
			s.Accounts[i] = a
			return
		}
	}
	s.Accounts = append(s.Accounts, a)
}

// CredField is one credential input in the accounts editor. Secret fields are
// masked when drawn; Bool fields render as a true/false toggle.
type CredField struct {
	Key    string
	Label  string
	Secret bool
	Bool   bool
}

// ProviderCreds is the credential schema for one provider: the fields the
// accounts editor renders and the app maps onto provider construction.
type ProviderCreds struct {
	Kind   source.Kind
	Label  string
	Fields []CredField
}

// CredentialSchema returns the per-provider credential fields the accounts
// editor renders. Reddit is first because signing in is its primary purpose:
// pasting the logged-in browser's reddit_session cookie authenticates reads
// (Reddit's self-serve OAuth app registration is effectively closed to new
// personal projects, so the cookie is the practical path for individual
// read-only use — and it can be imported straight from Firefox).
func CredentialSchema() []ProviderCreds {
	return []ProviderCreds{
		{Kind: source.Reddit, Label: "Reddit", Fields: []CredField{
			{Key: "session_cookie", Label: "Session cookie (reddit_session)", Secret: true},
		}},
		{Kind: source.Mastodon, Label: "Mastodon", Fields: []CredField{
			{Key: "instance", Label: "Instance URL"},
			{Key: "token", Label: "Access token", Secret: true},
		}},
		{Kind: source.Lemmy, Label: "Lemmy", Fields: []CredField{
			{Key: "instance", Label: "Instance URL"},
		}},
		{Kind: source.Usenet, Label: "Usenet", Fields: []CredField{
			{Key: "addr", Label: "Server host:port"},
			{Key: "tls", Label: "Implicit TLS", Bool: true},
			{Key: "username", Label: "Username"},
			{Key: "password", Label: "Password", Secret: true},
			{Key: "indexer_url", Label: "Newznab indexer URL"},
			{Key: "indexer_key", Label: "Newznab API key", Secret: true},
		}},
		{Kind: source.Instagram, Label: "Instagram", Fields: []CredField{
			{Key: "session", Label: "Session cookie", Secret: true},
		}},
		{Kind: source.TikTok, Label: "TikTok", Fields: []CredField{
			{Key: "ms_token", Label: "ms_token", Secret: true},
			{Key: "session", Label: "Session cookie", Secret: true},
		}},
		// X / Twitter: public timelines need no auth, but the authenticated home
		// ("home"/"following") timeline needs the logged-in browser's session
		// cookie ("auth_token=…; ct0=…") — the same import-from-Firefox path as
		// Reddit. The official paywalled bearer API is still unused, so there is no
		// bearer-token field.
		{Kind: source.Twitter, Label: "X (Twitter)", Fields: []CredField{
			{Key: "session", Label: "Session cookie (auth_token; ct0)", Secret: true},
		}},
	}
}

// defaultSubs is the seed subscription set — Reddit is back by default,
// alongside Hacker News, so a fresh install shows a live multi-source feed.
func defaultSubs() []source.Subscription {
	return []source.Subscription{
		{Source: source.Reddit, Channel: "r/golang", Limit: 25},
		{Source: source.Reddit, Channel: "r/programming", Limit: 25},
		{Source: source.Reddit, Channel: "r/worldnews", Limit: 25},
		{Source: source.HackerNews, Channel: "", Limit: 25},
	}
}

// defaultCachePath is the per-user media-cache directory, or "" when the OS
// cache dir cannot be resolved (a headless environment with no HOME).
func defaultCachePath() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, appDir)
}

// pluginsSubdir is the per-user plugins directory name under appDir.
const pluginsSubdir = "plugins"

// defaultPluginsDir is the per-user feed-source plugins directory, or "" when the
// OS config dir cannot be resolved (a headless environment with no HOME).
func defaultPluginsDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, appDir, pluginsSubdir)
}

// PluginsDirOrDefault reports the effective feed-source plugins directory: the
// configured [Settings.PluginsDir] when set, otherwise the standard per-user
// location (which may be "" on a headless host with no config dir).
func (s *Settings) PluginsDirOrDefault() string {
	if s.PluginsDir != "" {
		return s.PluginsDir
	}
	return defaultPluginsDir()
}

// Default returns the seed settings: a single "Home" profile that already
// includes Reddit and Hacker News, the system theme, and the OS media cache.
func Default() *Settings {
	return &Settings{
		Profiles:         []Profile{{Name: "Home", Subs: defaultSubs()}},
		Active:           0,
		Theme:            ThemeSystem,
		CachePath:        defaultCachePath(),
		BrowserSingleTab: boolPtr(DefaultBrowserSingleTab),
		InfiniteScroll:   boolPtr(DefaultInfiniteScroll),
		ZoomInKey:        DefaultZoomInKey,
		ZoomOutKey:       DefaultZoomOutKey,
		SignInBrowser:    DefaultSignInBrowser,
		PreviewTextScale: DefaultPreviewTextScale,
		MediaCacheMB:     DefaultMediaCacheMB,
	}
}

// ValidSignInBrowser reports whether name is a recognised sign-in browser.
func ValidSignInBrowser(name string) bool {
	switch name {
	case SignInBrowserDefault, SignInBrowserFirefox, SignInBrowserChrome,
		SignInBrowserSafari, SignInBrowserEdge:
		return true
	default:
		return false
	}
}

// ActiveProfile returns the currently selected profile, or a safe fallback when
// the index or list is out of range.
func (s *Settings) ActiveProfile() Profile {
	if s.Active >= 0 && s.Active < len(s.Profiles) {
		return s.Profiles[s.Active]
	}
	if len(s.Profiles) > 0 {
		return s.Profiles[0]
	}
	return Profile{Name: "All"}
}

// Normalize repairs an out-of-range active index, a blank theme, an empty
// profile list and a missing cache path, so callers always get a usable value.
func (s *Settings) Normalize() {
	if len(s.Profiles) == 0 {
		s.Profiles = Default().Profiles
	}
	if s.Active < 0 || s.Active >= len(s.Profiles) {
		s.Active = 0
	}
	switch s.Theme {
	case ThemeSystem, ThemeLight, ThemeDark:
	default:
		s.Theme = ThemeSystem
	}
	if s.CachePath == "" {
		s.CachePath = defaultCachePath()
	}
	if s.ZoomInKey == "" {
		s.ZoomInKey = DefaultZoomInKey
	}
	if s.ZoomOutKey == "" {
		s.ZoomOutKey = DefaultZoomOutKey
	}
	s.PreviewTextScale = ClampPreviewTextScale(s.PreviewTextScale)
	s.MediaCacheMB = ClampMediaCacheMB(s.MediaCacheMB)
	if !ValidSignInBrowser(s.SignInBrowser) {
		// A blank field (a settings file predating it) or an unrecognised value
		// falls back to the default sign-in browser (Firefox).
		s.SignInBrowser = DefaultSignInBrowser
	}
	if s.BrowserSingleTab == nil {
		// A settings file predating this field (or a fresh one) defaults to
		// single-tab so the preview shows no tab strip.
		s.BrowserSingleTab = boolPtr(DefaultBrowserSingleTab)
	}
	if s.InfiniteScroll == nil {
		// A settings file predating this field (or a fresh one) defaults to
		// infinite scroll enabled.
		s.InfiniteScroll = boolPtr(DefaultInfiniteScroll)
	}
	s.dedupAccounts()
}

// dedupAccounts keeps at most one account per Kind (first wins) and drops
// entries with a blank Kind, tolerating a hand-edited or corrupt file.
func (s *Settings) dedupAccounts() {
	if len(s.Accounts) == 0 {
		return
	}
	seen := map[source.Kind]bool{}
	out := s.Accounts[:0]
	for _, a := range s.Accounts {
		if a.Kind == "" || seen[a.Kind] {
			continue
		}
		seen[a.Kind] = true
		out = append(out, a)
	}
	s.Accounts = out
}

// DefaultPath is the per-user settings file location
// (~/Library/Application Support/go-news-reader/settings.json on macOS,
// $XDG_CONFIG_HOME/go-news-reader/... on Linux, %AppData%\... on Windows).
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, appDir, "settings.json"), nil
}

// Store is a file-backed settings persister.
type Store struct{ Path string }

// NewStore returns a Store rooted at path.
func NewStore(path string) *Store { return &Store{Path: path} }

// Load reads the settings from the store's path. A missing file yields
// [Default] (not an error); a present-but-corrupt file is a hard error so the
// user notices rather than silently losing their profiles.
func (s *Store) Load() (*Settings, error) {
	data, err := os.ReadFile(s.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return Default(), nil
		}
		return nil, err
	}
	var out Settings
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	out.Normalize()
	return &out, nil
}

// Save writes v to the store's path, creating the parent directory as needed.
func (s *Store) Save(v *Settings) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(s.Path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
