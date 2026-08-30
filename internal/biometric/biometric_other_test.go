//go:build !darwin

package biometric

import "testing"

// TestAuthenticateNoop covers the off-macOS stub: there is no LocalAuthentication
// on this platform, so the seam must ALLOW (return nil) rather than gate the
// vault behind a prompt that cannot exist.
func TestAuthenticateNoop(t *testing.T) {
	if err := Authenticate("unlock your saved sign-ins"); err != nil {
		t.Fatalf("authenticate off macOS = %v, want nil (must not gate)", err)
	}
}
