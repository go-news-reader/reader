// Copyright (c) the go-news-reader authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package settings

import "sync"

// MemorySecrets is an in-memory [SecretStore]: the vault a TEST should use.
//
// Nothing automated may reach the host's real vault. On macOS the Keychain
// grants an item to the binary that created it and asks a human about anybody
// else, so a `go test` binary — a different binary on every build — raises an
// authorisation dialog on the first read. Waiting for it, the whole package
// times out after ten minutes; dismissing it, every read fails with OSStatus
// -128 (errUserCanceled). Neither is a fact about the code under test. See #174.
//
// One vault per TEST, not per [Store]: the vault is a property of the host, so a
// test that gave each store its own would "lose" a secret the moment it reopened
// the settings file, and would quietly stop covering the off-disk path it exists
// to cover.
//
//	v := settings.NewMemorySecrets()
//	st := &settings.Store{Path: path, Secrets: v}
//
// It is safe for concurrent use, because a Store is.
type MemorySecrets struct {
	mu sync.Mutex
	m  map[string][]byte
}

// NewMemorySecrets returns an empty, reachable in-memory vault.
func NewMemorySecrets() *MemorySecrets {
	return &MemorySecrets{m: map[string][]byte{}}
}

// Set stores secret under account, replacing any existing value.
func (s *MemorySecrets) Set(account string, secret []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Copied, not aliased: the caller's slice is the live settings value and a
	// vault that handed back a view of it would hide a lost write.
	cp := make([]byte, len(secret))
	copy(cp, secret)
	s.m[account] = cp
	return nil
}

// Get returns the secret stored under account, or [ErrSecretNotFound].
func (s *MemorySecrets) Get(account string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[account]
	if !ok {
		return nil, ErrSecretNotFound
	}
	cp := make([]byte, len(v))
	copy(cp, v)
	return cp, nil
}

// Delete removes the secret stored under account. Deleting an absent secret is
// not an error, as it is not for the real vault.
func (s *MemorySecrets) Delete(account string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, account)
	return nil
}

// Available reports true: an in-memory vault is always reachable, which is what
// makes it exercise the off-disk path rather than the plaintext fallback.
func (s *MemorySecrets) Available() bool { return true }
