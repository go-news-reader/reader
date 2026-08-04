// Package appicon holds the application's branded icon assets, embedded into
// the binary so the app can set its dock / taskbar icon and a menu-bar tray
// icon without shipping loose files.
//
// Icon is the full colour badge (the gocyan rounded-square "newspaper" mark),
// used as the dock / application icon. Tray is the glyph-only black variant,
// meant to be used as a macOS menu-bar template image (black + alpha, which the
// system tints for light / dark menu bars).
//
// Both are PNGs sourced from the go-news-reader brand kit.
package appicon

import _ "embed"

// Icon is the colour app badge (512×512 PNG) for the dock / application icon.
//
//go:embed icon.png
var Icon []byte

// Tray is the glyph-only black mark (32×32 PNG) for a menu-bar tray template.
//
//go:embed tray.png
var Tray []byte
