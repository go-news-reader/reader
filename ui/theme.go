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
			return toolkit.AdwaitaDark()
		}
		return toolkit.AdwaitaLight()
	case OSWindows:
		if dark {
			return toolkit.FluentDark()
		}
		return toolkit.FluentLight()
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

// themeOnAccent returns the label colour to draw on an accent fill: the theme's
// explicit Extra["OnAccent"] when it tags one, else a luminance-derived
// black/white via [onAccentFor]. The fallback matters because WhiteSur (macOS)
// and the Default themes never set Extra["OnAccent"]; defaulting to th.Background
// there painted near-black topbar text on the blue accent.
func themeOnAccent(t *toolkit.Theme) toolkit.RGBA {
	if v, ok := t.Extra["OnAccent"]; ok {
		return v
	}
	return onAccentFor(t.Accent)
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

// successColor / errorColor return the theme's success (green) / error (red)
// accent, taken from the GTK-source Extra map when the loaded theme provides one
// (success_color / error_color) and otherwise a clear built-in green / red. Used
// for the multipart group card's complete / incomplete badge.
func successColor(t *toolkit.Theme) toolkit.RGBA {
	if v, ok := t.Extra["success_color"]; ok {
		return v
	}
	return rgb(0x2FA84F)
}

func errorColor(t *toolkit.Theme) toolkit.RGBA {
	if v, ok := t.Extra["error_color"]; ok {
		return v
	}
	return rgb(0xD03030)
}
