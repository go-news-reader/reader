package ui

import "github.com/go-widgets/toolkit"

// OS-adaptive look & feel. The reader picks a palette that matches the host
// platform so the wasm UI feels native: macOS uses the toolkit's WhiteSur
// (Big Sur) theme, GNOME/Linux an Adwaita palette, Windows a Fluent palette.
// The front-end detects the OS from the browser and passes one of the tokens
// below; light/dark follows the system (or the user's explicit choice).

// OS tokens understood by [ThemeFor]. The front-end derives these from
// navigator.platform / userAgentData.
const (
	OSMac     = "mac"
	OSLinux   = "linux"
	OSWindows = "windows"
)

// ThemeFor returns the palette matching os (one of the OS* tokens) in the
// requested light/dark variant. Unknown platforms fall back to the toolkit
// default theme.
func ThemeFor(os string, dark bool) *toolkit.Theme {
	switch os {
	case OSMac:
		if dark {
			return toolkit.WhiteSurDark()
		}
		return toolkit.WhiteSurLight()
	case OSLinux:
		if dark {
			return adwaitaDark()
		}
		return adwaitaLight()
	case OSWindows:
		if dark {
			return fluentDark()
		}
		return fluentLight()
	default:
		if dark {
			return toolkit.DefaultDark()
		}
		return toolkit.DefaultLight()
	}
}

// ResolveTheme maps a persisted theme name ("system"|"light"|"dark") plus the
// host OS and the system dark preference to a concrete palette.
func ResolveTheme(name, os string, prefersDark bool) *toolkit.Theme {
	switch name {
	case "light":
		return ThemeFor(os, false)
	case "dark":
		return ThemeFor(os, true)
	default: // "system"
		return ThemeFor(os, prefersDark)
	}
}

// rgb builds an opaque colour from a 0xRRGGBB literal.
func rgb(v uint32) toolkit.RGBA {
	return toolkit.RGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xFF}
}

// withOnAccent tags a theme with the label colour used on accent fills (the
// topbar text), which the scene looks up via Extra["OnAccent"].
func withOnAccent(t *toolkit.Theme, onAccent toolkit.RGBA) *toolkit.Theme {
	if t.Extra == nil {
		t.Extra = map[string]toolkit.RGBA{}
	}
	t.Extra["OnAccent"] = onAccent
	return t
}

// WithAccent overrides a theme's accent colour with a harvested system accent
// (e.g. macOS -[NSColor controlAccentColor]) and recomputes the on-accent label
// colour so topbar text stays legible whatever hue the user picked.
func WithAccent(t *toolkit.Theme, r, g, b uint8) *toolkit.Theme {
	t.Accent = toolkit.RGBA{R: r, G: g, B: b, A: 0xFF}
	return withOnAccent(t, onAccentFor(t.Accent))
}

// onAccentFor returns black or white for text drawn on c, whichever contrasts
// better, using a perceived-luminance (Rec. 601) threshold.
func onAccentFor(c toolkit.RGBA) toolkit.RGBA {
	lum := (299*int(c.R) + 587*int(c.G) + 114*int(c.B)) / 1000
	if lum > 150 {
		return toolkit.RGBA{A: 0xFF} // dark text on a light accent
	}
	return toolkit.RGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
}

// adwaitaLight / adwaitaDark approximate GNOME's libadwaita default palette.
func adwaitaLight() *toolkit.Theme {
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

func adwaitaDark() *toolkit.Theme {
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

// fluentLight / fluentDark approximate Windows 11's Fluent palette.
func fluentLight() *toolkit.Theme {
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

func fluentDark() *toolkit.Theme {
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
