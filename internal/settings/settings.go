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
	Accounts  []Account `json:"accounts,omitempty"`  // per-provider credentials
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
// editor renders. Reddit is first because authenticated OAuth is its primary
// purpose: client id + secret enable app-only OAuth (reads public listings from
// IPs where the anonymous ".json" endpoints 403); adding username + password
// switches to the per-user "script" grant.
func CredentialSchema() []ProviderCreds {
	return []ProviderCreds{
		{Kind: source.Reddit, Label: "Reddit", Fields: []CredField{
			{Key: "client_id", Label: "Client ID"},
			{Key: "client_secret", Label: "Client secret", Secret: true},
			{Key: "username", Label: "Username"},
			{Key: "password", Label: "Password", Secret: true},
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
		{Kind: source.Twitter, Label: "X / Twitter", Fields: []CredField{
			{Key: "token", Label: "Bearer token", Secret: true},
		}},
	}
}

// defaultSubs is the seed subscription set — Reddit is back by default,
// alongside Hacker News, so a fresh install shows a live multi-source feed.
func defaultSubs() []source.Subscription {
	return []source.Subscription{
		{Source: source.Reddit, Channel: "golang", Limit: 25},
		{Source: source.Reddit, Channel: "programming", Limit: 25},
		{Source: source.Reddit, Channel: "worldnews", Limit: 25},
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

// Default returns the seed settings: a single "Home" profile that already
// includes Reddit and Hacker News, the system theme, and the OS media cache.
func Default() *Settings {
	return &Settings{
		Profiles:  []Profile{{Name: "Home", Subs: defaultSubs()}},
		Active:    0,
		Theme:     ThemeSystem,
		CachePath: defaultCachePath(),
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
