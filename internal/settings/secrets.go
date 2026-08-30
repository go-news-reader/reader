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
	"encoding/json"
	"errors"

	"github.com/go-keyring/keyring"

	"github.com/go-news-reader/reader/source"
)

// secretService is the vault "service" every reader secret is filed under; the
// "account" is [vaultAccount]. It matches the on-disk config subdir name so a
// human inspecting the Keychain/Credential Manager sees a recognisable owner.
const secretService = appDir

// vaultAccount is the single vault "account" the whole credential blob is filed
// under: ALL of the reader's secrets live in ONE keychain item — a JSON object
// mapping each [secretRef] to its value — rather than one item per secret. That
// is the whole point of this file: macOS Keychain prompts once per ITEM, so one
// item means unlocking the vault at startup (and, with biometric unlock on, the
// Touch ID gate) prompts ONCE regardless of how many accounts are configured.
const vaultAccount = "vault"

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
		if err := keyringSet(secretService, account, secret, keyring.WithUserPresence()); err == nil {
			return nil
		}
		// The platform refused the gate. The common case is an UNSIGNED macOS build:
		// a user-presence Keychain item needs the data-protection keychain, which
		// requires the app to be code-signed with the keychain entitlement, so an
		// unsigned build fails with errSecMissingEntitlement (-34018). Rather than
		// fail the save — and risk losing the secret — fall back to storing it
		// ungated. Biometric unlock is simply not in effect (the reader still works,
		// with the standard credential prompt); it takes effect in a signed build.
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
	var opts []keyring.Option
	if secretUserPresence {
		// Read the biometric-gated half: on macOS the item's own SecAccessControl
		// already raises Touch ID during the read, so this is redundant-but-harmless
		// there; on Windows/Linux it is what triggers the façade's presence check
		// (Windows Hello / the desktop authentication agent) before the secret is
		// returned.
		opts = append(opts, keyring.WithUserPresence())
	}
	b, err := keyringGet(secretService, account, opts...)
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

// secretRef is the KEY a provider's secret field is filed under inside the
// vault blob: "<kind>:<fieldKey>" (e.g. "reddit:session_cookie"). Stable and
// unique per (provider, field) so every provider's secrets coexist in the one
// JSON object. (Before the blob consolidation it was also the per-item vault
// "account"; it is now purely a map key — and, during the one-time migration of
// a pre-blob install, the old per-item account name we read the legacy items
// back from. See [Store.hydrateSecrets].)
func secretRef(kind source.Kind, key string) string {
	return string(kind) + ":" + key
}

// HasStoredSecret reports whether any configured account currently carries a
// non-empty secret field (in memory, after hydration) — i.e. whether a biometric
// unlock gate would have anything to protect.
func (s *Settings) HasStoredSecret() bool {
	for _, a := range s.Accounts {
		for key := range secretKeysFor(a.Kind) {
			if a.Fields[key] != "" {
				return true
			}
		}
	}
	return false
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

// pushSecrets collects every populated secret field of v's accounts into ONE
// JSON blob keyed by [secretRef], writes it under the single [vaultAccount] item
// with one Set, and returns a disk-bound copy of v with those secret fields
// removed so the settings.json written by [Store.Save] carries no secret
// material. When no secret is populated at all — a fresh install, or a vault the
// user has fully cleared — the blob item is Deleted instead, so nothing stale
// lingers. Non-secret fields and all other settings are carried through
// untouched. v itself is not modified.
func (s *Store) pushSecrets(v *Settings) (*Settings, error) {
	diskAccts := cloneAccounts(v.Accounts)
	blob := map[string]string{}
	for i := range diskAccts {
		for key := range secretKeysFor(diskAccts[i].Kind) {
			if val, ok := diskAccts[i].Fields[key]; ok && val != "" {
				blob[secretRef(diskAccts[i].Kind, key)] = val
			}
			// Strip the secret field from the disk copy whether or not it had a
			// value: the vault, never settings.json, is its home.
			delete(diskAccts[i].Fields, key)
		}
	}

	store := s.secrets()
	if len(blob) == 0 {
		// No secrets: remove any prior blob so a fully-cleared vault leaves nothing
		// behind (one item to delete, not one per former secret).
		if err := store.Delete(vaultAccount); err != nil {
			return nil, err
		}
	} else {
		// json.Marshal of a map[string]string cannot fail, so the error is elided:
		// a returned err here would be a permanently-dead branch.
		data, _ := json.Marshal(blob)
		if err := store.Set(vaultAccount, data); err != nil {
			return nil, err
		}
	}

	disk := *v
	disk.Accounts = diskAccts
	return &disk, nil
}

// hydrateSecrets fills out's secret fields from the vault and migrates any
// plaintext secret still present in a freshly loaded settings.json (a file
// written before this feature, or by the on-disk fallback while the vault was
// unavailable) into the vault. It reports whether such a plaintext migration
// happened, so [Store.Load] can purge the plaintext from disk. When no vault is
// reachable it is a no-op (false): the plaintext already in out is the fallback
// and is left in place. out is modified in place.
//
// The steady-state cost is a SINGLE read of the one [vaultAccount] blob item —
// one keychain prompt — however many accounts are configured. The exceptions
// both run at most once: the first launch after upgrading a pre-blob install
// reads (and retires) the old per-item secrets, and a settings.json still
// carrying plaintext is migrated into the blob.
func (s *Store) hydrateSecrets(out *Settings) (migrated bool) {
	store := s.secrets()
	if !store.Available() {
		return false
	}

	// Read the ONE blob item a single time.
	blob := map[string]string{}
	b, err := store.Get(vaultAccount)
	switch {
	case err == nil:
		if json.Unmarshal(b, &blob) != nil {
			// Corrupt/unparseable blob: treat it as an empty (but PRESENT) vault, so
			// affected accounts are left unauthenticated rather than failing the load
			// — and, crucially, do NOT fall back to the retired per-item path (the
			// blob exists, so migration already happened; re-reading old items would
			// re-prompt for nothing).
			blob = map[string]string{}
		}
	case errors.Is(err, ErrSecretNotFound):
		// No blob: either a fresh/empty vault or a pre-blob install still holding
		// per-item secrets. Recover the latter into a blob once (this is the only
		// launch that reads the old items, so the only one that prompts per item).
		blob = s.migratePerRefItems(store, out)
	default:
		// The vault is present but unreadable (the user declined the prompt →
		// userCanceled, or a transient error). Leave every account unauthenticated
		// rather than failing the load — a keychain hiccup must never stop the
		// reader — and keep any plaintext on disk as the fallback.
		return false
	}

	// Fill each account's secret fields from the blob, folding any plaintext still
	// on disk into it as we go.
	plaintext := false
	for i := range out.Accounts {
		for key := range secretKeysFor(out.Accounts[i].Kind) {
			ref := secretRef(out.Accounts[i].Kind, key)
			if val, ok := out.Accounts[i].Fields[key]; ok && val != "" {
				// Plaintext on disk wins and is folded into the blob; the value stays
				// in memory so the running app is unaffected, and Load purges the disk
				// copy once the blob write below confirms the vault took it.
				blob[ref] = val
				plaintext = true
				continue
			}
			// Absent on disk: fill from the blob when present. A key in neither place
			// leaves the account unauthenticated.
			v, ok := blob[ref]
			if !ok {
				continue
			}
			if out.Accounts[i].Fields == nil {
				out.Accounts[i].Fields = map[string]string{}
			}
			out.Accounts[i].Fields[key] = v
		}
	}

	if plaintext {
		// Persist the folded-in plaintext to the one blob item. On success report
		// the migration so Load purges the plaintext from disk; on failure (the
		// user declined, or a transient vault error) leave the plaintext on disk as
		// the fallback and do NOT fail the load.
		data, _ := json.Marshal(blob)
		if store.Set(vaultAccount, data) == nil {
			migrated = true
		}
	}
	return migrated
}

// migratePerRefItems recovers the secrets of a pre-blob install — one keychain
// item per secret, keyed by [secretRef] — into the single-blob format, and
// returns them as the blob map. It reads each account's secret field from its
// old per-item account, and, if any are found, writes them all into the one
// [vaultAccount] blob with a single Set and then deletes the old items, so every
// subsequent launch reads only the blob (one prompt). It is idempotent: once the
// blob exists hydrateSecrets never calls this again, and a run whose blob write
// fails simply keeps the old items for the next attempt. A fresh/empty vault
// yields an empty map with nothing written.
func (s *Store) migratePerRefItems(store SecretStore, out *Settings) map[string]string {
	recovered := map[string]string{}
	for i := range out.Accounts {
		for key := range secretKeysFor(out.Accounts[i].Kind) {
			ref := secretRef(out.Accounts[i].Kind, key)
			b, err := store.Get(ref)
			if err != nil {
				// Absent (a never-configured field) or unreadable: nothing to recover.
				continue
			}
			recovered[ref] = string(b)
		}
	}
	if len(recovered) == 0 {
		return recovered // fresh/empty vault: no legacy items to migrate.
	}
	data, _ := json.Marshal(recovered)
	if store.Set(vaultAccount, data) != nil {
		// Could not write the blob: leave the old items in place and use the
		// recovered values in memory this run; a later launch retries.
		return recovered
	}
	for ref := range recovered {
		// Retire the old per-item entries; a delete hiccup is harmless (the blob now
		// wins, so hydrateSecrets never reads the stragglers again).
		_ = store.Delete(ref)
	}
	return recovered
}
