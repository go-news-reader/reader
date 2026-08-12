package ui

import (
	"testing"

	"github.com/go-widgets/toolkit"
)

// The Adwaita/Fluent OS palettes used to live here as local copies. They were
// promoted VERBATIM into go-widgets/toolkit (AdwaitaLight/Dark, FluentLight/Dark)
// as of v0.153.0, and ThemeFor now returns the built-ins directly.
//
// These tests reproduce the REPLACED local palettes verbatim (the exact literals
// the deleted adwaitaLight/adwaitaDark/fluentLight/fluentDark carried, tagged via
// the same withOnAccent convention) and assert the toolkit built-ins are
// field-for-field identical — the proof the swap is behaviour-preserving, per the
// prove-against-replaced-code rule. If a future toolkit release drifts a channel,
// one of these fails loudly instead of silently changing the reader's look.

// oldAdwaitaLight is the deleted adwaitaLight(), reproduced verbatim.
func oldAdwaitaLight() *toolkit.Theme {
	return withOnAccent(&toolkit.Theme{
		Background:   rgb(0xFAFAFA),
		Surface:      rgb(0xFFFFFF),
		SurfaceAlt:   rgb(0xF0F0F0),
		OnBackground: rgb(0x2E3436),
		OnSurface:    rgb(0x2E3436),
		Accent:       rgb(0x3584E4),
		Border:       rgb(0xD4D4D4),
	}, rgb(0xFFFFFF))
}

// oldAdwaitaDark is the deleted adwaitaDark(), reproduced verbatim.
func oldAdwaitaDark() *toolkit.Theme {
	return withOnAccent(&toolkit.Theme{
		Background:   rgb(0x242424),
		Surface:      rgb(0x303030),
		SurfaceAlt:   rgb(0x1E1E1E),
		OnBackground: rgb(0xFFFFFF),
		OnSurface:    rgb(0xEEEEEE),
		Accent:       rgb(0x3584E4),
		Border:       rgb(0x1B1B1B),
	}, rgb(0xFFFFFF))
}

// oldFluentLight is the deleted fluentLight(), reproduced verbatim.
func oldFluentLight() *toolkit.Theme {
	return withOnAccent(&toolkit.Theme{
		Background:   rgb(0xF3F3F3),
		Surface:      rgb(0xFFFFFF),
		SurfaceAlt:   rgb(0xEBEBEB),
		OnBackground: rgb(0x202020),
		OnSurface:    rgb(0x202020),
		Accent:       rgb(0x0067C0),
		Border:       rgb(0xDFDFDF),
	}, rgb(0xFFFFFF))
}

// oldFluentDark is the deleted fluentDark(), reproduced verbatim.
func oldFluentDark() *toolkit.Theme {
	return withOnAccent(&toolkit.Theme{
		Background:   rgb(0x202020),
		Surface:      rgb(0x2B2B2B),
		SurfaceAlt:   rgb(0x272727),
		OnBackground: rgb(0xFFFFFF),
		OnSurface:    rgb(0xF0F0F0),
		Accent:       rgb(0x4CC2FF),
		Border:       rgb(0x1D1D1D),
	}, rgb(0x000000))
}

// assertSamePalette compares every canonical field plus Extra["OnAccent"].
func assertSamePalette(t *testing.T, name string, got, want *toolkit.Theme) {
	t.Helper()
	fields := []struct {
		field string
		g, w  toolkit.RGBA
	}{
		{"Background", got.Background, want.Background},
		{"Surface", got.Surface, want.Surface},
		{"SurfaceAlt", got.SurfaceAlt, want.SurfaceAlt},
		{"OnBackground", got.OnBackground, want.OnBackground},
		{"OnSurface", got.OnSurface, want.OnSurface},
		{"Accent", got.Accent, want.Accent},
		{"Border", got.Border, want.Border},
		{"Extra[OnAccent]", got.Extra["OnAccent"], want.Extra["OnAccent"]},
	}
	for _, f := range fields {
		if f.g != f.w {
			t.Errorf("%s.%s = %+v, want %+v", name, f.field, f.g, f.w)
		}
	}
}

func TestThemeBuiltinsMatchReplacedPalettes(t *testing.T) {
	assertSamePalette(t, "AdwaitaLight", toolkit.AdwaitaLight(), oldAdwaitaLight())
	assertSamePalette(t, "AdwaitaDark", toolkit.AdwaitaDark(), oldAdwaitaDark())
	assertSamePalette(t, "FluentLight", toolkit.FluentLight(), oldFluentLight())
	assertSamePalette(t, "FluentDark", toolkit.FluentDark(), oldFluentDark())
}

// TestThemeForReturnsBuiltins guards that the OS mapping wires the built-ins in
// (Linux→Adwaita, Windows→Fluent) rather than any stale copy.
func TestThemeForReturnsBuiltins(t *testing.T) {
	cases := []struct {
		os   string
		dark bool
		want *toolkit.Theme
	}{
		{OSLinux, false, toolkit.AdwaitaLight()},
		{OSLinux, true, toolkit.AdwaitaDark()},
		{OSWindows, false, toolkit.FluentLight()},
		{OSWindows, true, toolkit.FluentDark()},
	}
	for _, c := range cases {
		assertSamePalette(t, "ThemeFor("+c.os+")", ThemeFor(c.os, c.dark), c.want)
	}
}
