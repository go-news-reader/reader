package settings

import (
	"os"
	"testing"

	"github.com/go-keyring/keyring"
)

// TestMain puts an in-memory vault behind the keyring façade for every test in
// this package, before any of them runs.
//
// It is a guard rather than a convenience. A test that constructs the
// production [Store] — `NewStore(p)`, which most of them do, because most of
// them are not about secrets — reaches the HOST's credential vault the moment
// its settings happen to carry an account with a secret field. On macOS the
// Keychain grants an item to the binary that wrote it and asks a human about
// anybody else, and a `go test` binary is a different binary on every build: the
// read raises an authorisation dialog, and the package either waits ten minutes
// for it or fails with `keychain: get: OSStatus -128`. TestAccountsRoundTrip did
// exactly that, intermittently, depending on what the machine's window server
// felt like doing (#174).
//
// Swapping the four package seams here means no test in this package can reach
// the host vault whatever it constructs, which is the only version of this rule
// that cannot be forgotten by the next test. Tests that need the façade to fail
// still override these vars themselves and restore them.
func TestMain(m *testing.M) {
	vault := NewMemorySecrets()
	keyringSet = func(_, account string, secret []byte, _ ...keyring.Option) error { return vault.Set(account, secret) }
	keyringGet = func(_, account string, _ ...keyring.Option) ([]byte, error) {
		b, err := vault.Get(account)
		if err != nil {
			// The façade's own sentinel, so keyringSecrets.Get maps it the way it
			// maps the real one -- a fake that answered differently would leave the
			// mapping untested.
			return nil, keyring.ErrNotFound
		}
		return b, nil
	}
	keyringDelete = func(_, account string) error { return vault.Delete(account) }
	keyringAvailable = vault.Available
	os.Exit(m.Run())
}
