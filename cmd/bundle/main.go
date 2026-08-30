// Command bundle assembles the News Reader .app around a built executable, via
// go-macos/appbundle (pure Go, CGO_ENABLED=0). nr-build runs it after building
// and signing the binary: the .app is what gives the reader a Dock tile, a name
// in the menu bar, and a WORKING menu-bar tray — a bare executable is not an
// application to AppKit and gets none of those. Build tooling, so it is excluded
// from the coverage gate (like the example plugins).
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/go-macos/appbundle"
	"github.com/go-news-reader/reader/internal/appicon"
)

func main() {
	exe := flag.String("exe", "", "path to the built executable to wrap")
	dir := flag.String("dir", "", "directory to write \"News Reader.app\" into")
	version := flag.String("version", "0.0.0", "version the bundle reports")
	flag.Parse()
	if *exe == "" || *dir == "" {
		fmt.Fprintln(os.Stderr, "usage: bundle -exe <path> -dir <out> [-version v]")
		os.Exit(2)
	}
	// icon.png is the 512x512 app icon; ICNS wraps it in the .icns macOS reads.
	icns, err := appbundle.ICNS(appicon.Icon)
	if err != nil {
		fmt.Fprintln(os.Stderr, "bundle: icns:", err)
		os.Exit(1)
	}
	b, err := appbundle.Build(appbundle.Spec{
		Dir:        *dir,
		Name:       "News Reader",
		Identifier: "com.gonewsreader.reader",
		Version:    *version,
		Executable: *exe,
		Icon:       icns,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "bundle:", err)
		os.Exit(1)
	}
	fmt.Println(b.Path)
}
