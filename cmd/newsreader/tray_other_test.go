//go:build !darwin

package main

import "testing"

// TestMakeStatusItemNoOp: off macOS there is no menu bar, so the real
// makeStatusItem yields an empty, no-op status item (its Close does nothing).
// This exercises the non-darwin tray stub on the coverage runner (ubuntu).
func TestMakeStatusItemNoOp(t *testing.T) {
	s := makeStatusItem(nil)
	if s == nil {
		t.Fatal("makeStatusItem must return a usable handle even off macOS")
	}
	s.Close() // no-op; must not panic
}
