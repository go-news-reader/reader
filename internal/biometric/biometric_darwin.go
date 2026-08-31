//go:build darwin

package biometric

import (
	"context"

	la "github.com/go-macos/localauthentication"
)

func authenticate(reason string) error {
	// Gate does exactly the convenience contract we want: prompt when the owner
	// check (Touch ID, or the device password) is available, and return nil
	// WITHOUT prompting when it is not — so a person with no biometric or passcode
	// is never locked out of their own vault. It errors only when an available
	// check is failed or cancelled.
	return la.Gate(context.Background(), la.PolicyOwner, reason)
}
