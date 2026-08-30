//go:build darwin

package biometric

import (
	"context"

	la "github.com/go-macos/localauthentication"
)

func authenticate(reason string) error {
	// No owner check available (no Touch ID and no passcode, or unbundled): do
	// NOT gate — locking the vault behind an unusable prompt would strand the
	// user. Only prompt (and only enforce) when a check is actually available.
	if la.Available(la.PolicyOwner) != nil {
		return nil
	}
	return la.Evaluate(context.Background(), la.PolicyOwner, reason)
}
