// Command dmg wraps the built "News Reader.app" in the disk image it is
// distributed in: a window with the application on the left, a shortcut to
// /Applications on the right, and the reader's own icon on the volume.
//
// nr-build runs it after building and SIGNING the bundle. The order matters:
// the image is a copy of the .app as it stands, so a bundle signed afterwards
// is not the bundle inside the image. A file copy preserves a bundle's
// signature — it lives in Contents/_CodeSignature and inside the Mach-O, both
// ordinary files — and the build verifies that on the mounted copy rather
// than assuming it.
//
// Build tooling, so it is excluded from the coverage gate (like cmd/bundle and
// the example plugins).
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-macos/appbundle"
	"github.com/go-macos/appdmg"
	"github.com/go-news-reader/reader/internal/appicon"
)

func main() {
	app := flag.String("app", "", `path to the built "News Reader.app"`)
	out := flag.String("o", "", "path of the .dmg to write")
	background := flag.String("background", "", "optional picture to show behind the icons")
	flag.Parse()
	if *app == "" || *out == "" {
		fmt.Fprintln(os.Stderr, `usage: dmg -app "News Reader.app" -o "News Reader.dmg" [-background art.png]`)
		os.Exit(2)
	}

	// The volume wears the application's own icon, from the same PNG the
	// bundle's .icns is made of.
	icns, err := appbundle.ICNS(appicon.Icon)
	if err != nil {
		fmt.Fprintln(os.Stderr, "dmg: icns:", err)
		os.Exit(1)
	}
	iconFile, err := os.CreateTemp("", "volume-*.icns")
	if err != nil {
		fmt.Fprintln(os.Stderr, "dmg:", err)
		os.Exit(1)
	}
	defer os.Remove(iconFile.Name())
	if _, err := iconFile.Write(icns); err != nil {
		fmt.Fprintln(os.Stderr, "dmg:", err)
		os.Exit(1)
	}
	iconFile.Close()

	name := filepath.Base(*app)
	spec := appdmg.Spec{
		Output:           *out,
		VolumeName:       "News Reader",
		App:              *app,
		VolumeIcon:       iconFile.Name(),
		Background:       *background,
		ApplicationsLink: true,
		Positions: map[string]appdmg.Point{
			name:           {X: 160, Y: 200},
			"Applications": {X: 460, Y: 200},
		},
	}
	// With no picture behind them, nothing else knows how big the window
	// should be, and the Finder would open it at whatever size it used last.
	if *background == "" {
		spec.Window = appdmg.Window{X: 200, Y: 300, Width: 620, Height: 380}
	}
	if err := appdmg.Build(spec); err != nil {
		fmt.Fprintln(os.Stderr, "dmg:", err)
		os.Exit(1)
	}
	fmt.Println(*out)
}
