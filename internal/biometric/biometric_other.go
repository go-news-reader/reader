//go:build !darwin

package biometric

// authenticate is a no-op off macOS: there is no LocalAuthentication, so the
// biometric-unlock preference simply does not gate the vault. (Windows Hello /
// a Linux agent can be wired later.)
func authenticate(string) error { return nil }
