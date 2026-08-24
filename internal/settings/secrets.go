// secrets.go moves the reader's account secrets (session cookies, access
// tokens, passwords, API keys) off the settings.json file and into the host's
// native credential vault — macOS Keychain, Windows Credential Manager or the
// Linux Secret Service — through the pure-Go, CGO-free cross-platform façade
// github.com/go-keyring/keyring.
//
// Which fields are secret is defined by [CredentialSchema]: a [CredField] with
// Secret == true. When a usable vault is reachable ([SecretStore.Available]),
// [Store.Save] writes those field values to the vault and omits them from the
// on-disk JSON, and [Store.Load] reads them back in so the running app is
// unaffected. When no vault is reachable — a headless Linux box with no Secret
// Service daemon is the canonical case — the reader degrades cleanly: the
// secrets stay in settings.json (mode 0600), exactly as they did before this
// feature, with no loss of functionality. The façade never falls back to a
// silent plaintext write of its own; that on-disk fallback is this package's
// explicit, documented choice, made only when [SecretStore.Available] is false.
package settings

import (
	"errors"

	"github.com/go-keyring/keyring"

	"github.com/go-news-reader/reader/source"
)

// secretService is the vault "service" every reader secret is filed under; the
// per-secret "account" is [secretRef]. It matches the on-disk config subdir
// name so a human inspecting the Keychain/Credential Manager sees a recognisable
// owner.
const secretService = appDir

// ErrSecretNotFound is returned by [SecretStore.Get] when no secret is stored
// for the given reference. It is the package-local mapping of
// keyring.ErrNotFound so callers of this package need not import the façade.
var ErrSecretNotFound = errors.New("settings: secret not found")

// SecretStore is the seam to the off-disk credential vault. The production
// implementation ([keyringSecrets]) wraps github.com/go-keyring/keyring; tests
// substitute an in-memory double. account is a [secretRef]; secret is the raw
// value bytes.
type SecretStore interface {
	// Set stores secret under account, replacing any existing value.
	Set(account string, secret []byte) error
	// Get returns the secret stored under account, or [ErrSecretNotFound] when
	// none exists.
	Get(account string) ([]byte, error)
	// Delete removes the secret stored under account. Deleting an absent secret
	// is not an error.
	Delete(account string) error
	// Available reports whether a usable vault is reachable. When it returns
	// false the caller keeps secrets in settings.json (the documented fallback).
	Available() bool
}

// keyring package-function seams. Keeping the façade calls behind package vars
// lets the production keyringSecrets backend be unit-tested across every branch
// (the ErrNotFound mapping, the error passthrough) without a live vault — tests
// swap these for fakes.
var (
	keyringSet       = keyring.Set
	keyringGet       = keyring.Get
	keyringDelete    = keyring.Delete
	keyringAvailable = keyring.Available
)

// defaultSecretStore, when non-nil, is the SecretStore a [Store] with no explicit
// Secrets seam uses in place of the production keyring backend. It is a
// process-wide test seam: a downstream package (app, windowapp) installs one
// in-memory vault through [SetDefaultSecretStore] in its TestMain so that every
// Store it builds — including the ones it constructs internally and cannot reach
// to set Secrets on — routes secret I/O through that vault rather than the host's
// real credential store. A fresh test binary must never touch the real vault (on
// macOS it would make Keychain prompt a human). nil selects the production backend.
var defaultSecretStore SecretStore

// SetDefaultSecretStore installs store as the process-wide default [SecretStore]
// for Stores built without an explicit Secrets seam, and returns a function that
// restores the previous default. Passing nil restores the production keyring
// backend. It exists for test isolation (see defaultSecretStore) and is not for
// production code paths.
func SetDefaultSecretStore(store SecretStore) (restore func()) {
	prev := defaultSecretStore
	defaultSecretStore = store
	return func() { defaultSecretStore = prev }
}

// keyringSecrets is the production [SecretStore]: the go-keyring/keyring façade
// keyed by (secretService, account). keyring.ErrNotFound is remapped to the
// package-local [ErrSecretNotFound].
type keyringSecrets struct{}

func (keyringSecrets) Set(account string, secret []byte) error {
	if secretUserPresence {
		// Store the secret behind an interactive user-presence gate, so reading it
		// back (at startup) prompts the platform biometric/consent check — Touch ID
		// on macOS, Windows Hello / the desktop's authentication agent elsewhere —
		// instead of nothing (or the login-password prompt).
		return keyringSet(secretService, account, secret, keyring.WithUserPresence())
	}
	return keyringSet(secretService, account, secret)
}

// secretUserPresence gates whether new secrets are stored behind an interactive
// user-presence check (biometric unlock). Off by default; SetSecretUserPresence
// flips it from the persisted setting before the vault is written.
var secretUserPresence bool

// SetSecretUserPresence turns biometric-gated secret storage on or off for
// subsequent writes and returns a function restoring the previous value. The app
// calls it from the persisted "biometric unlock" preference before pushing
// secrets; existing secrets pick up (or drop) the gate the next time they are
// re-written (a settings save re-pushes them).
func SetSecretUserPresence(v bool) (restore func()) {
	prev := secretUserPresence
	secretUserPresence = v
	return func() { secretUserPresence = prev }
}

func (keyringSecrets) Get(account string) ([]byte, error) {
	b, err := keyringGet(secretService, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil, ErrSecretNotFound
	}
	if err != nil {
		return nil, err
	}
	return b, nil
}

func (keyringSecrets) Delete(account string) error {
	return keyringDelete(secretService, account)
}

func (keyringSecrets) Available() bool { return keyringAvailable() }

// secretRef is the vault "account" a provider's secret field is filed under:
// "<kind>:<fieldKey>" (e.g. "reddit:session_cookie"). Stable and unique per
// (provider, field) so multiple providers' secrets coexist.
func secretRef(kind source.Kind, key string) string {
	return string(kind) + ":" + key
}

// secretKeysFor returns the set of field keys that [CredentialSchema] marks
// Secret for kind (nil when kind is unknown or has no secret fields).
func secretKeysFor(kind source.Kind) map[string]bool {
	for _, pc := range CredentialSchema() {
		if pc.Kind != kind {
			continue
		}
		var m map[string]bool
		for _, f := range pc.Fields {
			if f.Secret {
				if m == nil {
					m = map[string]bool{}
				}
				m[f.Key] = true
			}
		}
		return m
	}
	return nil
}

// cloneAccounts deep-copies accts (the slice and each Fields map) so a
// disk-bound copy can have its secret fields removed without disturbing the
// caller's in-memory Settings, which must keep the real values for the app.
func cloneAccounts(accts []Account) []Account {
	if accts == nil {
		return nil
	}
	out := make([]Account, len(accts))
	for i, a := range accts {
		out[i] = Account{Kind: a.Kind}
		if a.Fields != nil {
			f := make(map[string]string, len(a.Fields))
			for k, v := range a.Fields {
				f[k] = v
			}
			out[i].Fields = f
		}
	}
	return out
}

// pushSecrets stores every populated secret field of v's accounts into the
// vault and returns a disk-bound copy of v with those fields removed, so the
// settings.json written by [Store.Save] carries no secret material. A secret
// field that is empty (a user who cleared a cookie) is deleted from the vault so
// no stale value lingers. Non-secret fields and all other settings are carried
// through untouched. v itself is not modified.
func (s *Store) pushSecrets(v *Settings) (*Settings, error) {
	diskAccts := cloneAccounts(v.Accounts)
	store := s.secrets()
	for i := range diskAccts {
		for key := range secretKeysFor(diskAccts[i].Kind) {
			ref := secretRef(diskAccts[i].Kind, key)
			val, ok := diskAccts[i].Fields[key]
			if ok && val != "" {
				if err := store.Set(ref, []byte(val)); err != nil {
					return nil, err
				}
				delete(diskAccts[i].Fields, key)
			} else {
				// Empty or absent: drop any previously stored value so a cleared
				// secret does not survive in the vault.
				if err := store.Delete(ref); err != nil {
					return nil, err
				}
				delete(diskAccts[i].Fields, key)
			}
		}
	}
	disk := *v
	disk.Accounts = diskAccts
	return &disk, nil
}

// hydrateSecrets fills out's secret fields from the vault and migrates any
// plaintext secret still present in a freshly loaded settings.json (a file
// written before this feature, or by the on-disk fallback while the vault was
// unavailable) into the vault. It reports whether such a migration happened, so
// [Store.Load] can purge the plaintext from disk. When no vault is reachable it
// is a no-op (false, nil): the plaintext already in out is the fallback and is
// left in place. out is modified in place.
func (s *Store) hydrateSecrets(out *Settings) (migrated bool) {
	store := s.secrets()
	if !store.Available() {
		return false
	}
	for i := range out.Accounts {
		for key := range secretKeysFor(out.Accounts[i].Kind) {
			ref := secretRef(out.Accounts[i].Kind, key)
			val, ok := out.Accounts[i].Fields[key]
			if ok && val != "" {
				// Plaintext on disk: move it into the vault, keeping the value in
				// memory so the running app is unaffected. The purge of the disk
				// copy happens in Load via a follow-up Save. If the vault write fails
				// (e.g. the user declined the keychain prompt), leave the plaintext on
				// disk as the fallback rather than failing the whole load — the reader
				// must still start.
				if err := store.Set(ref, []byte(val)); err != nil {
					continue
				}
				migrated = true
				continue
			}
			// Absent from disk: read it back from the vault. A missing secret, or an
			// unreadable vault (the user declined the prompt → userCanceled, or a
			// transient vault error), leaves this account unauthenticated rather than
			// failing the load: a keychain hiccup must never stop the reader starting.
			b, gErr := store.Get(ref)
			if gErr != nil {
				continue
			}
			if out.Accounts[i].Fields == nil {
				out.Accounts[i].Fields = map[string]string{}
			}
			out.Accounts[i].Fields[key] = string(b)
		}
	}
	return migrated
}
