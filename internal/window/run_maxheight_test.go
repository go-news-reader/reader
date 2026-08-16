// Copyright (c) 2026 the go-news-reader authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package window

import "testing"

// TestLaunchHeight covers the three launch-height branches by substituting the
// visibleScreenHeight seam: a usable screen height is adopted, an unavailable
// screen falls back to the configured height, and a reported-but-non-positive
// height is treated as unavailable.
func TestLaunchHeight(t *testing.T) {
	orig := visibleScreenHeight
	defer func() { visibleScreenHeight = orig }()

	visibleScreenHeight = func() (int, bool) { return 1200, true }
	if got := launchHeight(700); got != 1200 {
		t.Errorf("screen available: launchHeight(700) = %d, want 1200", got)
	}
	visibleScreenHeight = func() (int, bool) { return 0, false }
	if got := launchHeight(700); got != 700 {
		t.Errorf("no screen: launchHeight(700) = %d, want 700 (fallback)", got)
	}
	visibleScreenHeight = func() (int, bool) { return 0, true }
	if got := launchHeight(700); got != 700 {
		t.Errorf("non-positive screen height: launchHeight(700) = %d, want 700 (fallback)", got)
	}
}
