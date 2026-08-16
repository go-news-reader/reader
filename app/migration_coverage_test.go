package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-widgets/toolkit"

	"github.com/go-news-reader/reader/internal/settings"
	"github.com/go-news-reader/reader/provider/usenet"
	"github.com/go-news-reader/reader/source"
	"github.com/go-news-reader/reader/ui"
)

// TestPersistSettings covers the exported PersistSettings live-preference seam:
// with a store it snapshots the scene and writes the file; with no store it is a
// silent no-op (the CLI paths).
func TestPersistSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	set := &settings.Settings{Profiles: []settings.Profile{{Name: "Home"}}, Active: 0, Theme: settings.ThemeSystem}
	a := New(Config{Registry: newReg(fakeProv{kind: source.Reddit}), Settings: set, Store: settings.NewStore(path), OS: ui.OSMac})
	a.PersistSettings()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("PersistSettings should write the store, stat: %v", err)
	}
	// No store configured: a no-op that must not panic.
	New(Config{Registry: newReg(fakeProv{kind: source.Reddit})}).PersistSettings()
}

// TestFeedSelectPreviewsUsenetGroup covers previewGroupIfShown (both branches)
// and the Usenet short-circuit in New's feed-select hook: selecting a grouped
// Usenet post's summary row previews the group rather than a standalone item.
func TestFeedSelectPreviewsUsenetGroup(t *testing.T) {
	a := New(Config{
		Registry: newReg(&fakeUsenet{files: map[string][]byte{"pic.png": pngBytes(6, 6)}}),
		Width:    900, Height: 600,
	})
	a.scene.SetItems(usenetPost()) // the two parts collapse into group base "release"
	// Make the group's image fetch synchronous (the default hook is async and would
	// write the scene from a goroutine, racing this test); leave New's feed-select
	// hook untouched so its Usenet short-circuit still runs.
	a.SetPreviewFetchHook(func(id string, parts []usenet.ReconstructPart) {
		a.loadPreviewImage(context.Background(), id, parts)
	})
	if _, err := a.RenderPNG(); err != nil {
		t.Fatal(err) // force a layout so the feed's group summary row exists
	}

	// previewGroupIfShown: true for the shown group base, false otherwise.
	if !a.previewGroupIfShown("release") {
		t.Fatal("previewGroupIfShown(release) = false, want true")
	}
	if a.previewGroupIfShown("nope") {
		t.Fatal("previewGroupIfShown(nope) = true, want false")
	}
	if sel, ok := a.scene.GroupPreviewItem("release"); !ok || sel.ID != "release" {
		t.Fatalf("group preview item = %+v ok=%v, want the release group", sel, ok)
	}

	// Firing a real feed selection runs New's own hook (not replaced here), whose
	// Usenet branch short-circuits to the group preview for the summary row.
	a.scene.FeedEvent(toolkit.Event{Kind: toolkit.EventClick, Y: 5})
	if sel, ok := a.scene.GroupPreviewItem("release"); !ok || sel.ID != "release" {
		t.Fatalf("after feed select, group preview = %+v ok=%v", sel, ok)
	}
}
