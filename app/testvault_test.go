package app

import (
	"sync"
	"testing"

	"github.com/go-news-reader/reader/internal/settings"
)

// testStore is a settings store whose account secrets live in an in-memory
// vault instead of the host's real one.
//
// No test may reach the real vault. On macOS the Keychain grants an item to the
// binary that created it and asks a HUMAN about anybody else, so a `go test`
// binary — a different binary on every build — raises an authorisation dialog on
// the first read of a secret it did not write. Left up, the package times out
// after ten minutes; dismissed, every read fails with `keychain: get: OSStatus
// -128`. Four tests here did both, depending on what the machine's window server
// felt like doing, which is not a property of the code they were testing (#174).
//
// The vault is keyed by the settings PATH, so every store a test opens over the
// same file shares one — which is what the host is. Sharing is not tidiness: a
// per-store vault would "lose" each secret the moment the test reopened the file
// to verify it, and the verification would silently stop covering the off-disk
// path it exists to cover. Since each test builds its path under its own
// t.TempDir, one path means one test.
func testStore(t *testing.T, path string) *settings.Store {
	t.Helper()
	return &settings.Store{Path: path, Secrets: testVault(t, path)}
}

var (
	testVaultMu sync.Mutex
	testVaults  = map[string]*settings.MemorySecrets{}
)

// testVault returns the one in-memory vault for path, creating it on first ask
// and dropping it when the test that first asked is done.
func testVault(t *testing.T, path string) *settings.MemorySecrets {
	t.Helper()
	testVaultMu.Lock()
	defer testVaultMu.Unlock()
	if v, ok := testVaults[path]; ok {
		return v
	}
	v := settings.NewMemorySecrets()
	testVaults[path] = v
	t.Cleanup(func() {
		testVaultMu.Lock()
		defer testVaultMu.Unlock()
		delete(testVaults, path)
	})
	return v
}
