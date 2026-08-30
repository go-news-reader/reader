package settings

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-keyring/keyring"

	"github.com/go-news-reader/reader/source"
)

// memSecrets is an in-memory [SecretStore] double. avail toggles
// Available(); setErr/getErr/delErr inject failures on the matching op for a
// specific ref (empty ref matches any).
type memSecrets struct {
	m       map[string][]byte
	avail   bool
	setErr  error
	getErr  error
	delErr  error
	failRef string // ref the injected error applies to ("" = any)
	sets    int
	deletes int
	gets    int
}

func newMemSecrets() *memSecrets { return &memSecrets{m: map[string][]byte{}, avail: true} }

func (s *memSecrets) hit(ref string) bool { return s.failRef == "" || s.failRef == ref }

func (s *memSecrets) Set(ref string, secret []byte) error {
	if s.setErr != nil && s.hit(ref) {
		return s.setErr
	}
	s.sets++
	cp := make([]byte, len(secret))
	copy(cp, secret)
	s.m[ref] = cp
	return nil
}

func (s *memSecrets) Get(ref string) ([]byte, error) {
	s.gets++
	if s.getErr != nil && s.hit(ref) {
		return nil, s.getErr
	}
	b, ok := s.m[ref]
	if !ok {
		return nil, ErrSecretNotFound
	}
	return b, nil
}

// vaultBlob unmarshals the single-item vault blob (the JSON object stored under
// vaultAccount) into a ref->value map. Absent blob returns an empty map. The
// consolidation means EVERY secret lives here, not under its own secretRef item.
func (s *memSecrets) vaultBlob(t *testing.T) map[string]string {
	t.Helper()
	b, ok := s.m[vaultAccount]
	if !ok {
		return map[string]string{}
	}
	var blob map[string]string
	if err := json.Unmarshal(b, &blob); err != nil {
		t.Fatalf("vault blob is not valid JSON: %v (%q)", err, b)
	}
	return blob
}

func (s *memSecrets) Delete(ref string) error {
	if s.delErr != nil && s.hit(ref) {
		return s.delErr
	}
	s.deletes++
	delete(s.m, ref)
	return nil
}

func (s *memSecrets) Available() bool { return s.avail }

// mustBlob marshals a ref->value map to the JSON bytes stored under
// vaultAccount, for seeding a steady-state (blob-present) vault in tests.
func mustBlob(t *testing.T, blob map[string]string) []byte {
	t.Helper()
	b, err := json.Marshal(blob)
	if err != nil {
		t.Fatalf("marshal blob: %v", err)
	}
	return b
}

// redditAccount is a helper account carrying a secret (session_cookie) and no
// other fields.
func redditAccount(cookie string) Account {
	return Account{Kind: source.Reddit, Fields: map[string]string{"session_cookie": cookie}}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

// TestSaveVaultOmitsSecretFromDisk proves that with a reachable vault the secret
// is stored in the vault and never written to settings.json, while non-secret
// fields remain on disk.
func TestSaveVaultOmitsSecretFromDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	sec := newMemSecrets()
	st := &Store{Path: path, Secrets: sec}

	s := Default()
	s.SetAccount(Account{Kind: source.Usenet, Fields: map[string]string{
		"addr":     "news.example.com:563", // non-secret
		"password": "hunter2",              // secret
	}})
	if err := st.Save(s); err != nil {
		t.Fatalf("save: %v", err)
	}

	disk := readFile(t, path)
	if strings.Contains(disk, "hunter2") {
		t.Errorf("secret leaked to disk:\n%s", disk)
	}
	if !strings.Contains(disk, "news.example.com:563") {
		t.Errorf("non-secret field missing from disk:\n%s", disk)
	}
	if got := sec.vaultBlob(t)[secretRef(source.Usenet, "password")]; got != "hunter2" {
		t.Errorf("vault password = %q, want hunter2", got)
	}
	// The consolidated model keeps exactly one vault item: the blob, never a
	// per-secret item under the ref itself.
	if len(sec.m) != 1 {
		t.Errorf("vault holds %d items, want 1 (the blob); m=%v", len(sec.m), sec.m)
	}
	if _, ok := sec.m[secretRef(source.Usenet, "password")]; ok {
		t.Errorf("secret stored under its own item, not folded into the one blob")
	}
	// The caller's in-memory Settings must be untouched (app still needs it).
	acc, _ := s.Account(source.Usenet)
	if acc.Fields["password"] != "hunter2" {
		t.Errorf("Save mutated caller's Settings: %v", acc.Fields)
	}
}

// TestRoundTripThroughVault saves with a vault, then loads and confirms the
// secret is rehydrated from the vault into the returned Settings.
func TestRoundTripThroughVault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	sec := newMemSecrets()
	st := &Store{Path: path, Secrets: sec}

	s := Default()
	s.SetAccount(redditAccount("cookie-abc"))
	if err := st.Save(s); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := st.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	acc, ok := got.Account(source.Reddit)
	if !ok || acc.Fields["session_cookie"] != "cookie-abc" {
		t.Fatalf("rehydrated account = %+v (ok=%v)", acc, ok)
	}
}

// TestLoadWithoutSecretsSkipsVaultThenHydrateFills proves the split that lets a
// windowed caller show its Dock/menu-bar/tray before any keychain prompt:
// LoadWithoutSecrets returns account metadata with NO vault-held secret, and a
// later HydrateSecrets fills it from the vault.
func TestLoadWithoutSecretsSkipsVaultThenHydrateFills(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	sec := newMemSecrets()
	st := &Store{Path: path, Secrets: sec}

	s := Default()
	s.SetAccount(redditAccount("cookie-abc"))
	if err := st.Save(s); err != nil {
		t.Fatalf("save: %v", err)
	}

	// The secret lives only in the vault (purged from disk), so LoadWithoutSecrets
	// — which must not touch the vault — comes back without it.
	bare, err := st.LoadWithoutSecrets()
	if err != nil {
		t.Fatalf("load without secrets: %v", err)
	}
	acc, ok := bare.Account(source.Reddit)
	if !ok {
		t.Fatal("account metadata should load without the vault")
	}
	if acc.Fields["session_cookie"] != "" {
		t.Errorf("LoadWithoutSecrets leaked a vault secret: %q", acc.Fields["session_cookie"])
	}

	// HydrateSecrets then fills it from the vault.
	if err := st.HydrateSecrets(bare); err != nil {
		t.Fatalf("hydrate: %v", err)
	}
	if acc, _ = bare.Account(source.Reddit); acc.Fields["session_cookie"] != "cookie-abc" {
		t.Errorf("HydrateSecrets did not fill from the vault: %+v", acc.Fields)
	}
}

// TestFallbackKeepsSecretOnDisk proves the documented headless degradation: when
// the vault is unavailable the secret is written to settings.json and read back
// from it, with no loss of functionality.
func TestFallbackKeepsSecretOnDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	sec := newMemSecrets()
	sec.avail = false
	st := &Store{Path: path, Secrets: sec}

	s := Default()
	s.SetAccount(redditAccount("cookie-fallback"))
	if err := st.Save(s); err != nil {
		t.Fatalf("save: %v", err)
	}
	if !strings.Contains(readFile(t, path), "cookie-fallback") {
		t.Errorf("fallback should keep secret on disk")
	}
	if sec.sets != 0 {
		t.Errorf("vault written while unavailable: sets=%d", sec.sets)
	}
	got, err := st.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	acc, _ := got.Account(source.Reddit)
	if acc.Fields["session_cookie"] != "cookie-fallback" {
		t.Errorf("fallback reload = %+v", acc)
	}
}

// TestMigrationFromPlaintext writes a legacy settings.json (secret in plaintext)
// then loads it with a reachable vault: the secret must move to the vault, be
// purged from disk, and the returned Settings must still carry it. A second load
// must be a no-op (idempotent).
func TestMigrationFromPlaintext(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	// Write a legacy file with the secret in plaintext (no vault).
	legacy := &Store{Path: path, Secrets: &memSecrets{m: map[string][]byte{}, avail: false}}
	s := Default()
	s.SetAccount(redditAccount("legacy-cookie"))
	if err := legacy.Save(s); err != nil {
		t.Fatalf("legacy save: %v", err)
	}
	if !strings.Contains(readFile(t, path), "legacy-cookie") {
		t.Fatalf("precondition: legacy file should hold plaintext")
	}

	// Now load with a reachable vault → migrate + purge.
	sec := newMemSecrets()
	st := &Store{Path: path, Secrets: sec}
	got, err := st.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	acc, _ := got.Account(source.Reddit)
	if acc.Fields["session_cookie"] != "legacy-cookie" {
		t.Errorf("migrated Settings lost secret: %+v", acc)
	}
	if sec.vaultBlob(t)[secretRef(source.Reddit, "session_cookie")] != "legacy-cookie" {
		t.Errorf("secret not moved to vault blob: %v", sec.m)
	}
	if strings.Contains(readFile(t, path), "legacy-cookie") {
		t.Errorf("plaintext not purged from disk after migration")
	}

	// Idempotency: a second load moves nothing (sets unchanged from migration).
	setsAfterMigration := sec.sets
	if _, err := st.Load(); err != nil {
		t.Fatalf("second load: %v", err)
	}
	if sec.sets != setsAfterMigration {
		t.Errorf("second load re-migrated: sets %d -> %d", setsAfterMigration, sec.sets)
	}
}

// TestClearedSecretDeletedFromVault proves a secret cleared by the user is
// removed from the vault on the next Save, leaving no stale value.
func TestClearedSecretDeletedFromVault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	sec := newMemSecrets()
	st := &Store{Path: path, Secrets: sec}

	s := Default()
	s.SetAccount(redditAccount("to-be-cleared"))
	if err := st.Save(s); err != nil {
		t.Fatalf("save 1: %v", err)
	}
	ref := secretRef(source.Reddit, "session_cookie")
	if _, ok := sec.vaultBlob(t)[ref]; !ok {
		t.Fatalf("precondition: vault blob should hold the secret")
	}
	// Clear the cookie and save again.
	s.SetAccount(redditAccount(""))
	if err := st.Save(s); err != nil {
		t.Fatalf("save 2: %v", err)
	}
	if _, ok := sec.vaultBlob(t)[ref]; ok {
		t.Errorf("cleared secret still in vault blob")
	}
	// The last secret going empty removes the whole blob item, leaving nothing.
	if _, ok := sec.m[vaultAccount]; ok {
		t.Errorf("emptied vault should delete the blob item, got %v", sec.m)
	}
}

// TestSaveVaultSetError surfaces a vault write failure from Save.
func TestSaveVaultSetError(t *testing.T) {
	sentinel := errors.New("vault down")
	sec := newMemSecrets()
	sec.setErr = sentinel
	st := &Store{Path: filepath.Join(t.TempDir(), "settings.json"), Secrets: sec}
	s := Default()
	s.SetAccount(redditAccount("x"))
	if err := st.Save(s); !errors.Is(err, sentinel) {
		t.Fatalf("Save err = %v, want %v", err, sentinel)
	}
}

// TestSaveVaultDeleteError surfaces a vault delete failure (empty-secret path)
// from Save.
func TestSaveVaultDeleteError(t *testing.T) {
	sentinel := errors.New("delete failed")
	sec := newMemSecrets()
	sec.delErr = sentinel
	st := &Store{Path: filepath.Join(t.TempDir(), "settings.json"), Secrets: sec}
	s := Default()
	s.SetAccount(redditAccount("")) // empty -> Delete path
	if err := st.Save(s); !errors.Is(err, sentinel) {
		t.Fatalf("Save err = %v, want %v", err, sentinel)
	}
}

// TestLoadHydrateGetError: a vault READ failure (e.g. the user declines the
// keychain prompt → userCanceled) must NOT fail Load — the reader still starts,
// with that account simply left unhydrated (unauthenticated).
func TestLoadHydrateGetError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	// Seed a purged file (secret absent on disk) so Load takes the Get path.
	sec := newMemSecrets()
	st := &Store{Path: path, Secrets: sec}
	s := Default()
	s.SetAccount(redditAccount("seed"))
	if err := st.Save(s); err != nil {
		t.Fatalf("seed save: %v", err)
	}
	sec.getErr = errors.New("read failed")
	loaded, err := st.Load()
	if err != nil {
		t.Fatalf("Load err = %v, want nil (a vault read failure must not stop startup)", err)
	}
	if len(loaded.Accounts) != 1 {
		t.Fatalf("accounts = %d, want 1", len(loaded.Accounts))
	}
	if v := loaded.Accounts[0].Fields["session_cookie"]; v != "" {
		t.Fatalf("session_cookie hydrated despite read failure: %q", v)
	}
}

// TestLoadMigrateSetError: a vault WRITE failure while migrating a legacy
// plaintext secret must NOT fail Load — the plaintext is left on disk as the
// fallback and the reader still starts.
func TestLoadMigrateSetError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	legacy := &Store{Path: path, Secrets: &memSecrets{m: map[string][]byte{}, avail: false}}
	s := Default()
	s.SetAccount(redditAccount("legacy"))
	if err := legacy.Save(s); err != nil {
		t.Fatalf("legacy save: %v", err)
	}
	sec := newMemSecrets()
	sec.setErr = errors.New("migrate set failed")
	st := &Store{Path: path, Secrets: sec}
	loaded, err := st.Load()
	if err != nil {
		t.Fatalf("Load err = %v, want nil (a vault write failure must not stop startup)", err)
	}
	if v := loaded.Accounts[0].Fields["session_cookie"]; v != "legacy" {
		t.Fatalf("plaintext fallback lost: session_cookie = %q, want \"legacy\"", v)
	}
}

// TestSecretKeysForUnknownKind covers the nil-return branch for a kind with no
// secret fields / unknown kind.
func TestSecretKeysForUnknownKind(t *testing.T) {
	if m := secretKeysFor(source.HackerNews); m != nil { // HackerNews has no schema entry
		t.Errorf("secretKeysFor(HackerNews) = %v, want nil", m)
	}
	if m := secretKeysFor(source.Lemmy); m != nil { // Lemmy schema has only a non-secret field
		t.Errorf("secretKeysFor(Lemmy) = %v, want nil", m)
	}
}

// TestCloneAccountsNil covers the nil-slice branch of cloneAccounts.
func TestCloneAccountsNil(t *testing.T) {
	if got := cloneAccounts(nil); got != nil {
		t.Errorf("cloneAccounts(nil) = %v, want nil", got)
	}
}

// TestHydrateNilFieldsMap covers hydration into an account whose Fields map is
// nil (allocated on demand).
func TestHydrateNilFieldsMap(t *testing.T) {
	sec := newMemSecrets()
	sec.m[vaultAccount] = mustBlob(t, map[string]string{
		secretRef(source.Reddit, "session_cookie"): "from-vault",
	})
	st := &Store{Secrets: sec}
	out := &Settings{Accounts: []Account{{Kind: source.Reddit}}} // nil Fields
	if migrated := st.hydrateSecrets(out); migrated {
		t.Fatalf("hydrate migrated = %v, want false", migrated)
	}
	if out.Accounts[0].Fields["session_cookie"] != "from-vault" {
		t.Errorf("nil Fields not hydrated: %+v", out.Accounts[0])
	}
}

// TestDefaultSecretStoreSelected checks the nil-seam default returns the
// production keyring backend.
func TestDefaultSecretStoreSelected(t *testing.T) {
	st := &Store{}
	if _, ok := st.secrets().(keyringSecrets); !ok {
		t.Errorf("default secrets() = %T, want keyringSecrets", st.secrets())
	}
}

// TestSetDefaultSecretStore checks the process-wide default seam: a Store with no
// explicit Secrets uses the installed default, an explicit per-Store seam still
// wins over it, and the returned restore reverts to the production keyring backend.
func TestSetDefaultSecretStore(t *testing.T) {
	sec := newMemSecrets()
	restore := SetDefaultSecretStore(sec)
	if got := (&Store{}).secrets(); got != sec {
		t.Fatalf("seam-less secrets() = %T, want the installed default", got)
	}
	own := newMemSecrets()
	if got := (&Store{Secrets: own}).secrets(); got != own {
		t.Fatalf("explicit Secrets should win over the default, got %T", got)
	}
	restore()
	if _, ok := (&Store{}).secrets().(keyringSecrets); !ok {
		t.Fatalf("restore should revert to the keyring backend, got %T", (&Store{}).secrets())
	}
}

// TestHydrateSecretAbsentEverywhere covers the not-found continue branch: an
// account whose secret key is on neither disk nor vault leaves the field unset.
func TestHydrateSecretAbsentEverywhere(t *testing.T) {
	sec := newMemSecrets() // empty vault
	st := &Store{Secrets: sec}
	out := &Settings{Accounts: []Account{{Kind: source.Reddit, Fields: map[string]string{}}}}
	if migrated := st.hydrateSecrets(out); migrated {
		t.Fatalf("hydrate migrated = %v, want false", migrated)
	}
	if _, ok := out.Accounts[0].Fields["session_cookie"]; ok {
		t.Errorf("absent secret should stay unset, got %+v", out.Accounts[0].Fields)
	}
}

// TestLoadMigratePurgeSaveError covers the branch where the post-migration purge
// Save fails: migration succeeds into the vault, but the file cannot be
// rewritten (its directory is read-only), so Load surfaces the write error.
func TestLoadMigratePurgeSaveError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("run as root ignores directory write bits") // outer gate: privilege
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	legacy := &Store{Path: path, Secrets: &memSecrets{m: map[string][]byte{}, avail: false}}
	s := Default()
	s.SetAccount(redditAccount("legacy"))
	if err := legacy.Save(s); err != nil {
		t.Fatalf("legacy save: %v", err)
	}
	if err := os.Chmod(path, 0o400); err != nil { // read-only file: O_WRONLY reopen fails
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

	sec := newMemSecrets()
	st := &Store{Path: path, Secrets: sec}
	if _, err := st.Load(); err == nil {
		t.Fatalf("Load should fail when purge-save cannot write")
	}
	// The secret still reached the vault blob before the failed purge.
	if sec.vaultBlob(t)[secretRef(source.Reddit, "session_cookie")] != "legacy" {
		t.Errorf("migration should have stored to vault blob")
	}
}

// TestHydrateReadsVaultOnce is the point of the whole consolidation: hydrating a
// settings carrying SEVERAL secrets across several accounts reads the vault
// exactly ONCE (one keychain / Touch ID prompt), because every secret lives in
// the single blob item — not once per secret.
func TestHydrateReadsVaultOnce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	sec := newMemSecrets()
	st := &Store{Path: path, Secrets: sec}

	s := Default()
	s.SetAccount(redditAccount("reddit-cookie"))
	s.SetAccount(Account{Kind: source.Mastodon, Fields: map[string]string{
		"instance": "https://m.example", "token": "mast-token",
	}})
	s.SetAccount(Account{Kind: source.Usenet, Fields: map[string]string{
		"addr": "news.example:563", "password": "pw", "indexer_key": "ik",
	}})
	if err := st.Save(s); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Steady state: the blob is present. LoadWithoutSecrets never touches the
	// vault, so the count below is exactly HydrateSecrets' doing.
	bare, err := st.LoadWithoutSecrets()
	if err != nil {
		t.Fatalf("load bare: %v", err)
	}
	sec.gets = 0
	if err := st.HydrateSecrets(bare); err != nil {
		t.Fatalf("hydrate: %v", err)
	}
	if sec.gets != 1 {
		t.Fatalf("hydrate performed %d vault reads for 4 secrets across 3 accounts, want exactly 1 (one prompt)", sec.gets)
	}
	// And that one read actually filled every secret.
	if r, _ := bare.Account(source.Reddit); r.Fields["session_cookie"] != "reddit-cookie" {
		t.Errorf("reddit secret not filled: %+v", r.Fields)
	}
	if m, _ := bare.Account(source.Mastodon); m.Fields["token"] != "mast-token" {
		t.Errorf("mastodon secret not filled: %+v", m.Fields)
	}
	if u, _ := bare.Account(source.Usenet); u.Fields["password"] != "pw" || u.Fields["indexer_key"] != "ik" {
		t.Errorf("usenet secrets not filled from the single read: %+v", u.Fields)
	}
}

// TestMigrateFromPerRefItems covers the one-time upgrade of a pre-blob install:
// OLD per-item secrets (one keychain item per secret, no blob) are recovered
// into the accounts, folded into the single blob, and the old items deleted;
// and a second hydrate then reads only the blob (one prompt).
func TestMigrateFromPerRefItems(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	sec := newMemSecrets()
	// OLD format: a keychain item per secret, keyed by secretRef, and no blob.
	redRef := secretRef(source.Reddit, "session_cookie")
	usrRef := secretRef(source.Usenet, "password")
	sec.m[redRef] = []byte("old-reddit")
	sec.m[usrRef] = []byte("old-pw")

	// A settings.json holding the account metadata (non-secret fields), written
	// with an unavailable vault so nothing is stored under the blob yet.
	meta := Default()
	meta.SetAccount(Account{Kind: source.Reddit})
	meta.SetAccount(Account{Kind: source.Usenet, Fields: map[string]string{"addr": "news.example:563"}})
	seed := &Store{Path: path, Secrets: &memSecrets{m: map[string][]byte{}, avail: false}}
	if err := seed.Save(meta); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	st := &Store{Path: path, Secrets: sec}
	got, err := st.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	// (a) secrets recovered into the accounts.
	if r, _ := got.Account(source.Reddit); r.Fields["session_cookie"] != "old-reddit" {
		t.Errorf("reddit secret not recovered: %+v", r.Fields)
	}
	if u, _ := got.Account(source.Usenet); u.Fields["password"] != "old-pw" {
		t.Errorf("usenet secret not recovered: %+v", u.Fields)
	}
	// (b) the blob now exists and holds both.
	blob := sec.vaultBlob(t)
	if blob[redRef] != "old-reddit" || blob[usrRef] != "old-pw" {
		t.Errorf("blob missing migrated secrets: %v", blob)
	}
	// (c) the old per-item entries are deleted; only the blob remains.
	if _, ok := sec.m[redRef]; ok {
		t.Errorf("old reddit item not deleted after migration")
	}
	if _, ok := sec.m[usrRef]; ok {
		t.Errorf("old usenet item not deleted after migration")
	}
	if len(sec.m) != 1 {
		t.Errorf("vault should hold only the blob after migration, has %d items: %v", len(sec.m), sec.m)
	}
	// (d) a second hydrate touches ONLY the blob (one read, no per-item reads).
	bare, err := st.LoadWithoutSecrets()
	if err != nil {
		t.Fatalf("second load bare: %v", err)
	}
	sec.gets = 0
	if err := st.HydrateSecrets(bare); err != nil {
		t.Fatalf("second hydrate: %v", err)
	}
	if sec.gets != 1 {
		t.Fatalf("second hydrate read the vault %d times, want 1 (blob only)", sec.gets)
	}
}

// TestHydrateCorruptBlob covers the unparseable-blob branch: a blob that is not
// valid JSON leaves the accounts unauthenticated (rather than failing the load)
// and must NOT fall back to the retired per-item path — the blob's presence
// means migration already happened.
func TestHydrateCorruptBlob(t *testing.T) {
	sec := newMemSecrets()
	sec.m[vaultAccount] = []byte("}{ not json")
	// A retired per-item value the corrupt-blob path must NOT read or delete.
	sec.m[secretRef(source.Reddit, "session_cookie")] = []byte("should-not-be-read")

	st := &Store{Secrets: sec}
	out := &Settings{Accounts: []Account{{Kind: source.Reddit, Fields: map[string]string{}}}}
	if migrated := st.hydrateSecrets(out); migrated {
		t.Fatalf("corrupt blob should not report a migration")
	}
	if v, ok := out.Accounts[0].Fields["session_cookie"]; ok {
		t.Errorf("corrupt blob should leave the account unauthenticated, got %q", v)
	}
	if _, ok := sec.m[secretRef(source.Reddit, "session_cookie")]; !ok {
		t.Errorf("corrupt-blob path touched an old per-item entry it should have ignored")
	}
}

// TestMigratePerRefSetError covers the migration path where the old items are
// readable but writing the consolidated blob fails: the secret is used in memory
// this run (the app still authenticates), the blob is not created, and the old
// item is kept for a later retry.
func TestMigratePerRefSetError(t *testing.T) {
	sec := newMemSecrets()
	ref := secretRef(source.Reddit, "session_cookie")
	sec.m[ref] = []byte("old-reddit")
	sec.setErr = errors.New("blob write failed")
	sec.failRef = vaultAccount // only the blob write fails; per-item reads succeed

	st := &Store{Secrets: sec}
	out := &Settings{Accounts: []Account{{Kind: source.Reddit, Fields: map[string]string{}}}}
	if migrated := st.hydrateSecrets(out); migrated {
		t.Fatalf("a failed blob write is not a plaintext migration")
	}
	if out.Accounts[0].Fields["session_cookie"] != "old-reddit" {
		t.Errorf("recovered secret not used in memory: %+v", out.Accounts[0].Fields)
	}
	if _, ok := sec.m[vaultAccount]; ok {
		t.Errorf("blob should not exist after a failed migration write")
	}
	if _, ok := sec.m[ref]; !ok {
		t.Errorf("old per-item entry should be kept when the blob write fails")
	}
}

// TestKeyringSecretsBackend covers the production keyringSecrets backend
// hermetically by swapping the keyring package-function seams, so every branch
// (round-trip, ErrNotFound mapping, error passthrough, availability) runs on any
// host — including the headless CI lane with no live vault.
func TestKeyringSecretsBackend(t *testing.T) {
	store := map[string][]byte{}
	origSet, origGet, origDel, origAvail := keyringSet, keyringGet, keyringDelete, keyringAvailable
	t.Cleanup(func() {
		keyringSet, keyringGet, keyringDelete, keyringAvailable = origSet, origGet, origDel, origAvail
	})
	keyringSet = func(service, account string, secret []byte, _ ...keyring.Option) error {
		if service != secretService {
			t.Errorf("Set service = %q, want %q", service, secretService)
		}
		store[account] = append([]byte(nil), secret...)
		return nil
	}
	keyringGet = func(service, account string, _ ...keyring.Option) ([]byte, error) {
		if b, ok := store[account]; ok {
			return b, nil
		}
		return nil, keyring.ErrNotFound
	}
	keyringDelete = func(service, account string) error { delete(store, account); return nil }
	keyringAvailable = func() bool { return true }

	var ks keyringSecrets
	if !ks.Available() {
		t.Fatal("Available() = false")
	}
	// Not found maps to ErrSecretNotFound.
	if _, err := ks.Get("missing"); !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("Get(missing) = %v, want ErrSecretNotFound", err)
	}
	// Round-trip.
	if err := ks.Set("acct", []byte("value")); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if b, err := ks.Get("acct"); err != nil || string(b) != "value" {
		t.Fatalf("Get = (%q, %v)", b, err)
	}
	// Error passthrough on a non-NotFound failure.
	boom := errors.New("keyring boom")
	keyringGet = func(string, string, ...keyring.Option) ([]byte, error) { return nil, boom }
	if _, err := ks.Get("acct"); !errors.Is(err, boom) {
		t.Fatalf("Get passthrough = %v, want %v", err, boom)
	}
	// Delete.
	if err := ks.Delete("acct"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := store["acct"]; ok {
		t.Errorf("Delete left entry behind")
	}
}

// TestKeyringSecretsOnDevice exercises the production keyringSecrets backend
// against the host's REAL credential vault. It is gated by
// READER_KEYRING_ONDEVICE=1 (the outer gate). When that gate is set the vault
// MUST be reachable — an unreachable vault under the gate is a FAILURE, not a
// skip — so a green run is proof the round-trip actually ran on this device.
func TestKeyringSecretsOnDevice(t *testing.T) {
	if os.Getenv("READER_KEYRING_ONDEVICE") != "1" {
		t.Skip("set READER_KEYRING_ONDEVICE=1 to run the on-device vault round-trip")
	}
	var ks keyringSecrets
	if !ks.Available() {
		t.Fatal("READER_KEYRING_ONDEVICE=1 but no credential vault is reachable")
	}
	ref := secretRef(source.Reddit, "session_cookie") + ".ondevice-test"
	t.Cleanup(func() { _ = ks.Delete(ref) })

	// Not-found maps to ErrSecretNotFound.
	_ = ks.Delete(ref)
	if _, err := ks.Get(ref); !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("Get(absent) = %v, want ErrSecretNotFound", err)
	}
	// Set then Get round-trips the exact bytes.
	want := []byte("on-device-secret-\x00\xffvalue")
	if err := ks.Set(ref, want); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := ks.Get(ref)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("round-trip = %q, want %q", got, want)
	}
	// Delete removes it; a second Delete is not an error.
	if err := ks.Delete(ref); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := ks.Delete(ref); err != nil {
		t.Fatalf("Delete(absent): %v", err)
	}
	if _, err := ks.Get(ref); !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("Get(after delete) = %v, want ErrSecretNotFound", err)
	}
}

// TestKeyringSecretsUserPresence proves the biometric setting threads
// keyring.WithUserPresence into the vault write: an option is passed when it is
// on, none when it is off, and the toggle restores cleanly.
func TestKeyringSecretsUserPresence(t *testing.T) {
	orig := keyringSet
	defer func() { keyringSet = orig }()
	gotOpts := -1
	keyringSet = func(_, _ string, _ []byte, opts ...keyring.Option) error {
		gotOpts = len(opts)
		return nil
	}

	restore := SetSecretUserPresence(true)
	if err := (keyringSecrets{}).Set("acct", []byte("s")); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if gotOpts != 1 {
		t.Fatalf("biometric on: opts = %d, want 1 (WithUserPresence)", gotOpts)
	}
	restore() // back to the previous (off) value
	gotOpts = -1
	if err := (keyringSecrets{}).Set("acct", []byte("s")); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if gotOpts != 0 {
		t.Fatalf("biometric off: opts = %d, want 0", gotOpts)
	}
}

// TestKeyringSecretsGetUserPresence proves the read side threads
// keyring.WithUserPresence when biometric unlock is on, and none when off — the
// read half of the gate.
func TestKeyringSecretsGetUserPresence(t *testing.T) {
	origGet := keyringGet
	defer func() { keyringGet = origGet }()
	gotOpts := -1
	keyringGet = func(_, _ string, opts ...keyring.Option) ([]byte, error) {
		gotOpts = len(opts)
		return []byte("s"), nil
	}

	restore := SetSecretUserPresence(true)
	if _, err := (keyringSecrets{}).Get("acct"); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if gotOpts != 1 {
		t.Fatalf("biometric on: Get opts = %d, want 1", gotOpts)
	}
	restore()
	gotOpts = -1
	if _, err := (keyringSecrets{}).Get("acct"); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if gotOpts != 0 {
		t.Fatalf("biometric off: Get opts = %d, want 0", gotOpts)
	}
}

// TestKeyringSecretsPresenceFallback proves that when the platform refuses the
// user-presence gate (e.g. an unsigned macOS build → -34018), Set does NOT fail
// but falls back to storing the secret ungated, so nothing is lost.
func TestKeyringSecretsPresenceFallback(t *testing.T) {
	origSet := keyringSet
	defer func() { keyringSet = origSet }()
	var gatedTries, ungatedTries int
	keyringSet = func(_, _ string, _ []byte, opts ...keyring.Option) error {
		if len(opts) > 0 {
			gatedTries++
			return errors.New("keychain: set: OSStatus -34018") // platform refuses the gate
		}
		ungatedTries++
		return nil
	}

	restore := SetSecretUserPresence(true)
	defer restore()
	if err := (keyringSecrets{}).Set("acct", []byte("s")); err != nil {
		t.Fatalf("Set should fall back, not fail: %v", err)
	}
	if gatedTries != 1 {
		t.Fatalf("gated attempts = %d, want 1", gatedTries)
	}
	if ungatedTries != 1 {
		t.Fatalf("ungated fallback = %d, want 1 (the secret must still be stored)", ungatedTries)
	}
}
