//go:build darwin

package window

import "testing"

// These tests run on a real macOS device. They exercise the migrated
// Objective-C paths (now backed by github.com/go-macos/objc) end to end: the
// one-time framework load, the runtime class registration, the NSString bridge,
// and the live appearance harvest (effectiveAppearance dark/light +
// controlAccentColor). They are the on-device regression guard for the objc
// migration — the rest of the window package's tests are platform-independent
// seam helpers and never compile this file.

func TestHarvest_LoadAndRegisterClasses(t *testing.T) {
	if err := loadFrameworks(); err != nil {
		t.Fatalf("loadFrameworks: %v", err)
	}
	vc, ac, err := registerClasses()
	if err != nil {
		t.Fatalf("registerClasses: %v", err)
	}
	if vc == 0 || ac == 0 {
		t.Fatalf("registerClasses returned zero classes: view=%v agent=%v", vc, ac)
	}
	// registerClasses is guarded by a sync.Once; a second call must be a stable
	// no-op returning the same classes.
	vc2, ac2, err := registerClasses()
	if err != nil || vc2 != vc || ac2 != ac {
		t.Fatalf("registerClasses not idempotent: %v %v %v", vc2, ac2, err)
	}
}

func TestHarvest_NSStringBridge(t *testing.T) {
	if err := loadFrameworks(); err != nil {
		t.Fatalf("loadFrameworks: %v", err)
	}
	for _, s := range []string{"", "Feeds", "café — 日本語"} {
		if got := goString(nsString(s)); got != s {
			t.Fatalf("NSString round-trip: got %q want %q", got, s)
		}
	}
	if goString(0) != "" {
		t.Fatal("goString(nil) should be empty")
	}
}

func TestHarvest_ReadAppearance(t *testing.T) {
	if err := loadFrameworks(); err != nil {
		t.Fatalf("loadFrameworks: %v", err)
	}
	// The whole point of the harvest: read the live system look without crashing.
	dark, r, g, b, hasAccent := readAppearance()
	t.Logf("PROOF appearance harvest: dark=%v hasAccent=%v accent=#%02X%02X%02X", dark, hasAccent, r, g, b)
	// controlAccentColor exists on every supported macOS (10.14+), so on a real
	// device the accent must be readable and its components in range.
	if !hasAccent {
		t.Fatal("readAppearance reported no accent color on a real macOS device")
	}
	// r/g/b are uint8 so they are inherently in [0,255]; assert the accent is not
	// the all-zero sentinel that the no-accent path returns.
	if r == 0 && g == 0 && b == 0 {
		t.Fatal("readAppearance returned a black accent, expected a real system accent")
	}
	// unitToByte clamping is deterministic; a second read must agree.
	dark2, r2, g2, b2, has2 := readAppearance()
	if dark2 != dark || has2 != hasAccent || r2 != r || g2 != g || b2 != b {
		t.Fatalf("readAppearance not stable: (%v %v %d %d %d) vs (%v %v %d %d %d)",
			dark, hasAccent, r, g, b, dark2, has2, r2, g2, b2)
	}
}
