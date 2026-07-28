package app

import (
	"errors"
	"os"
	"testing"

	"github.com/go-news-reader/reader/source"
	"github.com/go-news-reader/reader/ui"
)

func TestFileDownloadToggleSavesAndClears(t *testing.T) {
	saved := map[string][]byte{}
	orig := writeFile
	writeFile = func(name string, data []byte, _ os.FileMode) error { saved[name] = data; return nil }
	defer func() { writeFile = orig }()

	fu := &fakeUsenet{files: map[string][]byte{"release.tar.zst": []byte("payload")}}
	a := New(Config{Registry: newReg(fu)})
	a.fdl.async = false // run synchronously
	a.scene.SetSubs(nil)
	a.scene.SetItems(usenetPost()) // groupable base "release"
	a.scene.SetCachePath("/tmp/dl")

	a.ToggleDownload("release")
	a.drainScene()
	dls := a.scene.Downloads()
	if len(dls) != 1 || dls[0].ID != "release" || dls[0].Status != ui.DLDone {
		t.Fatalf("download = %+v, want one DLDone release", dls)
	}
	if len(saved) == 0 {
		t.Fatal("reconstructed files were not saved to the cache")
	}
	// Toggling an already-finished post is ignored (no cancel).
	a.ToggleDownload("release")
	if len(a.scene.Downloads()) != 1 {
		t.Fatal("finished download should be left alone")
	}
	// Unknown base is a no-op.
	a.ToggleDownload("does-not-exist")
	// Clear drops the finished row.
	a.ClearDownloads()
	a.drainScene()
	if len(a.scene.Downloads()) != 0 {
		t.Fatal("Clear should drop finished downloads")
	}
}

func TestFileDownloadCancelQueuedAndIgnoreActive(t *testing.T) {
	a := New(Config{Registry: newReg()})
	// A still-queued item is cancelled by toggling it again; a second item is kept
	// (exercises removeOrderLocked's keep branch).
	a.fdl.items["x"] = &ui.DownloadItem{ID: "x", Status: ui.DLQueued}
	a.fdl.items["z"] = &ui.DownloadItem{ID: "z", Status: ui.DLActive}
	a.fdl.order = []string{"x", "z"}
	a.fdl.toggle("x", "x", nil, "/tmp") // queued -> cancel branch
	a.drainScene()
	if got := a.scene.Downloads(); len(got) != 1 || got[0].ID != "z" {
		t.Fatalf("cancelling x should keep z, got %+v", got)
	}
	// An active item is left alone.
	a.fdl.items["y"] = &ui.DownloadItem{ID: "y", Status: ui.DLActive}
	a.fdl.order = []string{"y"}
	a.fdl.toggle("y", "y", nil, "/tmp")
	a.fdl.mu.Lock()
	_, stillActive := a.fdl.items["y"]
	a.fdl.mu.Unlock()
	if !stillActive {
		t.Fatal("active download must not be cancelled/removed")
	}
	// Clear keeps the still-active y (only finished rows are dropped).
	a.ClearDownloads()
	a.drainScene()
	if !a.scene.IsDownloadQueued("y") {
		t.Fatal("Clear should keep an active download")
	}
}

func TestFileDownloadFailures(t *testing.T) {
	// Provider is not a reconstructor → Failed.
	a := New(Config{Registry: newReg(fakeProv{kind: source.Usenet})})
	a.fdl.async = false
	a.scene.SetSubs(nil)
	a.scene.SetItems(usenetPost())
	a.ToggleDownload("release")
	a.drainScene()
	if a.scene.Downloads()[0].Status != ui.DLFailed {
		t.Fatal("non-reconstructor provider → DLFailed")
	}

	// Reconstruct error → Failed.
	a2 := New(Config{Registry: newReg(&fakeUsenet{err: errors.New("boom")})})
	a2.fdl.async = false
	a2.scene.SetSubs(nil)
	a2.scene.SetItems(usenetPost())
	a2.ToggleDownload("release")
	a2.drainScene()
	if a2.scene.Downloads()[0].Status != ui.DLFailed {
		t.Fatal("reconstruct error → DLFailed")
	}

	// Save error → Failed.
	orig := writeFile
	writeFile = func(string, []byte, os.FileMode) error { return errors.New("disk full") }
	defer func() { writeFile = orig }()
	a3 := New(Config{Registry: newReg(&fakeUsenet{files: map[string][]byte{"f": []byte("x")}})})
	a3.fdl.async = false
	a3.scene.SetSubs(nil)
	a3.scene.SetItems(usenetPost())
	a3.scene.SetCachePath("/tmp/x")
	a3.ToggleDownload("release")
	a3.drainScene()
	if a3.scene.Downloads()[0].Status != ui.DLFailed {
		t.Fatal("save error → DLFailed")
	}
}

func TestFileDownloadAsync(t *testing.T) {
	a := New(Config{Registry: newReg(&fakeUsenet{files: map[string][]byte{"f": []byte("x")}})})
	a.DeferSceneWrites()
	a.scene.SetSubs(nil)
	a.scene.SetItems(usenetPost())
	a.scene.SetCachePath(t.TempDir())
	a.ToggleDownload("release") // default async: spawns a worker goroutine
	done := false
	for i := 0; i < 1_000_000 && !done; i++ {
		a.drainScene()
		dls := a.scene.Downloads()
		done = len(dls) == 1 && dls[0].Status == ui.DLDone
	}
	if !done {
		t.Fatal("async download did not reach DLDone")
	}
}
