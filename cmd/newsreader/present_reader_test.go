package main

import (
	"bytes"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-news-reader/reader/app"
	"github.com/go-news-reader/reader/feeds"
	"github.com/go-news-reader/reader/internal/settings"
	"github.com/go-news-reader/reader/source"
	application "github.com/go-widgets/application"
)

// TestEmitWindowActivatesAfterFirstFrame proves the ordering the feature exists
// for: the vault-reading activation runs only AFTER a frame is on screen, via the
// onReady the window fires. ActivateAfterVault reads the (in-memory) vault on a
// goroutine and applies its state on the render thread via post, so the stub
// window drives Frame until that posted work has drained.
func TestEmitWindowActivatesAfterFirstFrame(t *testing.T) {
	// An in-memory vault so ActivateAfterVault's keychain read never touches the
	// host's (#174).
	st := &settings.Store{Path: filepath.Join(t.TempDir(), "s.json"), Secrets: settings.NewMemorySecrets()}
	// Biometric unlock defaults ON, which would gate the vault read behind a real
	// LocalAuthentication prompt; turn it off so this activation-ordering test stays
	// hermetic (the gate's own behaviour is covered by app.TestActivateAfterVaultBiometric*).
	set := settings.Default()
	off := false
	set.BiometricUnlock = &off
	a := app.New(app.Config{Registry: source.NewRegistry(), Settings: set, Store: st, Width: 400, Height: 300})
	a.SetRefreshHook(func() {}) // refreshFeed must not reach the network

	// rebuildRegistry runs on the render thread inside the posted activation; it
	// signals here, proving the activation drained. The inner onLoaded (go
	// refreshFeed) runs right after it in the same posted closure.
	rebuilt := make(chan struct{}, 1)
	a.SetRegistryBuilder(func(feeds.Options) *source.Registry {
		select {
		case rebuilt <- struct{}{}:
		default:
		}
		return source.NewRegistry()
	})

	origOpen := openWindow
	var readyBeforeSecondFrame bool
	openWindow = func(_ application.Spec, _ application.Config, h application.Handler, onReady func()) error {
		h.Frame() // first frame: the activation must NOT have run yet
		select {
		case <-rebuilt:
			readyBeforeSecondFrame = true
		default:
		}
		onReady() // fire the deferred activation (goroutine + post)
		deadline := time.Now().Add(2 * time.Second)
		for {
			h.Frame() // drain the render thread
			select {
			case <-rebuilt:
				return nil
			default:
			}
			if time.Now().After(deadline) {
				t.Fatal("the deferred activation never drained")
			}
			time.Sleep(time.Millisecond)
		}
	}
	t.Cleanup(func() { openWindow = origOpen })

	var out, errb bytes.Buffer
	if code := emitWindow(a, config{w: 400, h: 300}, &out, &errb); code != 0 {
		t.Fatalf("emitWindow code=%d err=%s", code, errb.String())
	}
	if readyBeforeSecondFrame {
		t.Fatal("the activation ran before the first frame was shown")
	}
}

// TestPresentWindowSpecAndTrayQuit covers the Spec the reader hands
// go-widgets/application — its identity and its menu-bar tray — and the Quit
// item's action (persist + exit, with osExit stubbed so the test survives).
func TestPresentWindowSpecAndTrayQuit(t *testing.T) {
	a := app.New(app.Config{Registry: source.NewRegistry(), Width: 400, Height: 300})
	a.SetRefreshHook(func() {})

	origExit := osExit
	exited := -1
	osExit = func(c int) { exited = c }
	t.Cleanup(func() { osExit = origExit })

	origOpen := openWindow
	var gotName, gotID string
	var gotIcon, gotTitle int
	openWindow = func(s application.Spec, c application.Config, h application.Handler, onReady func()) error {
		gotName, gotID, gotIcon = s.Name, s.Identifier, len(s.Icon)
		gotTitle = len(c.Title)
		if h == nil || onReady == nil {
			t.Fatal("presentWindow passed a nil handler or onReady")
		}
		menu := s.Tray() // build the menu-bar menu (covers the Tray closure)
		if len(menu.Items) != 1 || menu.Items[0].Label != "Quit News Reader" {
			t.Fatalf("tray menu = %+v", menu.Items)
		}
		menu.Items[0].OnClick() // the Quit action: persist + exit
		return nil
	}
	t.Cleanup(func() { openWindow = origOpen })

	if err := presentWindow(a, config{w: 400, h: 300}, func() {}); err != nil {
		t.Fatalf("presentWindow err = %v", err)
	}
	if gotName != "News Reader" || gotID != "com.gonewsreader.reader" || gotIcon == 0 || gotTitle == 0 {
		t.Fatalf("spec name=%q id=%q iconbytes=%d titlelen=%d", gotName, gotID, gotIcon, gotTitle)
	}
	if exited != 0 {
		t.Fatalf("Quit should exit 0, got %d", exited)
	}
}
