// Package biometric gates a sensitive action behind the platform's owner check
// — Touch ID, or the device password — so a self-signed build can protect the
// vault read without a keychain entitlement.
//
// It exists because the alternative, storing keychain items behind a
// SecAccessControl user-presence flag, needs the data-protection keychain and a
// keychain-access-groups entitlement that AMFI rejects on a self-signed app. A
// LocalAuthentication LAContext prompt needs no entitlement — only that the
// process runs inside a .app bundle — so the secrets stay stored ungated and the
// gate lives here, in front of the read.
package biometric

// Authenticate is the seam the app calls to gate the vault read behind the
// platform's owner check (Touch ID, or the device password). A package var so
// tests substitute it. Returns nil to ALLOW (authenticated, or no biometric
// available so we must not lock the user out); a non-nil error only when a
// check WAS available and the person failed or cancelled it.
var Authenticate = authenticate
