package settings

import (
	"errors"
	"testing"
)

// The in-memory vault is production code in this package — the tests of four
// others depend on it behaving like the real one — so it is tested like
// production code rather than trusted because it looks obvious.
func TestMemorySecrets(t *testing.T) {
	v := NewMemorySecrets()

	if !v.Available() {
		t.Error("an in-memory vault reported itself unreachable, which would send every caller down the plaintext fallback")
	}
	if _, err := v.Get("reddit:session_cookie"); !errors.Is(err, ErrSecretNotFound) {
		t.Errorf("Get on an empty vault = %v, want ErrSecretNotFound", err)
	}
	// Deleting what was never there is not an error, as it is not for the real
	// vault -- pushSecrets deletes unconditionally to clear a secret the user
	// emptied, and an error there would fail a Save that did nothing wrong.
	if err := v.Delete("reddit:session_cookie"); err != nil {
		t.Errorf("Delete of an absent secret = %v, want nil", err)
	}

	secret := []byte("reddit_session=xyz")
	if err := v.Set("reddit:session_cookie", secret); err != nil {
		t.Fatalf("Set: %v", err)
	}
	// The caller's slice is the live settings value: a vault that aliased it
	// would let a later mutation rewrite history, and one that handed the same
	// backing array back would let a caller corrupt the store.
	secret[0] = 'X'
	got, err := v.Get("reddit:session_cookie")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != "reddit_session=xyz" {
		t.Errorf("stored secret = %q, want the value as it was at Set", got)
	}
	got[0] = 'Y'
	if again, _ := v.Get("reddit:session_cookie"); string(again) != "reddit_session=xyz" {
		t.Errorf("after mutating what Get returned, the vault holds %q", again)
	}

	if err := v.Set("reddit:session_cookie", []byte("second")); err != nil {
		t.Fatalf("Set (replace): %v", err)
	}
	if again, _ := v.Get("reddit:session_cookie"); string(again) != "second" {
		t.Errorf("after replacing, the vault holds %q, want %q", again, "second")
	}

	if err := v.Delete("reddit:session_cookie"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := v.Get("reddit:session_cookie"); !errors.Is(err, ErrSecretNotFound) {
		t.Errorf("Get after Delete = %v, want ErrSecretNotFound", err)
	}
}
