package app

import (
	"testing"

	"github.com/go-news-reader/reader/internal/viewmodel"
	"github.com/go-news-reader/reader/source"
	"github.com/go-news-reader/reader/ui"
)

// TestVMDrivesSceneData proves the data flow app → view-model → scene: mutating
// the observables reflects onto the scene through the bindScene subscriptions.
func TestVMDrivesSceneData(t *testing.T) {
	a := New(Config{Registry: newReg(), Width: 400, Height: 300})
	vm, s := a.VM(), a.Scene()
	if vm == nil {
		t.Fatal("VM() is nil")
	}

	vm.SetItems([]source.Item{{ID: "a", Source: source.Reddit}, {ID: "b", Source: source.Reddit}})
	if len(s.Items) != 2 || s.Items[0].ID != "a" {
		t.Fatalf("items not reflected: %+v", s.Items)
	}

	vm.SetLoad(true, 1, 3)
	if !s.Loading() {
		t.Fatal("loading not reflected")
	}
	if d, tot := s.LoadingProgress(); d != 1 || tot != 3 {
		t.Fatalf("progress = %d/%d", d, tot)
	}

	vm.SetPending([]source.Subscription{{Source: source.Reddit, Channel: "golang"}})
	if s.PendingCount() != 1 || !s.IsPendingSub(source.Reddit, "golang") {
		t.Fatalf("pending not reflected: count=%d", s.PendingCount())
	}
	vm.ClearPending(source.Reddit, "golang")
	if s.PendingCount() != 0 {
		t.Fatalf("clear-pending not reflected: count=%d", s.PendingCount())
	}
	vm.ClearPending(source.Reddit, "absent") // no-op path

	vm.SetStatus("hello")
	if s.Status != "hello" {
		t.Fatalf("status = %q", s.Status)
	}

	vm.SetAuthPrompts([]ui.AuthPrompt{{Kind: source.Mastodon, Reason: "token"}})
	if p := s.AuthPrompts(); len(p) != 1 || p[0].Kind != source.Mastodon {
		t.Fatalf("prompts not reflected: %+v", p)
	}

	vm.Search.Set("golang")
	if s.Search() != "golang" {
		t.Fatalf("search = %q", s.Search())
	}
}

// TestVMDrivesSceneMode proves the Mode observable and the open/close commands
// drive the scene's view-switch across every mode (applyMode branches).
func TestVMDrivesSceneMode(t *testing.T) {
	a := New(Config{Registry: newReg(), Width: 400, Height: 300})
	vm, s := a.VM(), a.Scene()

	vm.OpenSettings.Execute()
	if s.Mode() != ui.ModeSettings {
		t.Fatalf("mode = %v, want settings", s.Mode())
	}
	vm.OpenLog.Execute()
	if s.Mode() != ui.ModeLog {
		t.Fatalf("mode = %v, want log", s.Mode())
	}
	vm.OpenAccounts.Execute()
	if s.Mode() != ui.ModeAccounts {
		t.Fatalf("mode = %v, want accounts", s.Mode())
	}
	vm.CloseView.Execute() // -> ModeFeed (applyMode default branch)
	if s.Mode() != ui.ModeFeed {
		t.Fatalf("mode = %v, want feed", s.Mode())
	}

	// OpenDetail sets the item then switches Mode (applyMode ModeDetail branch).
	it := source.Item{ID: "x", Source: source.Reddit, Title: "First"}
	vm.OpenDetail(it)
	if s.Mode() != ui.ModeDetail || s.Detail().ID != "x" {
		t.Fatalf("detail not opened: mode=%v item=%+v", s.Mode(), s.Detail())
	}
	// Changing the target item while already in detail re-opens it (Detail
	// subscription's Mode==ModeDetail branch).
	it2 := source.Item{ID: "y", Source: source.Reddit, Title: "Second"}
	vm.Detail.Set(it2)
	if s.Detail().ID != "y" {
		t.Fatalf("detail item not updated in place: %+v", s.Detail())
	}
}

// TestVMCommandActions proves the injected command actions run: Refresh calls the
// app's refresh hook and ToggleSidebar flips the sidebar chrome.
func TestVMCommandActions(t *testing.T) {
	a := New(Config{Registry: newReg(fakeProv{kind: source.Reddit})})
	var refreshed int
	a.SetRefreshHook(func() { refreshed++ })

	if !a.VM().Refresh.CanExecute() {
		t.Fatal("refresh should be executable when idle")
	}
	a.VM().Refresh.Execute()
	if refreshed != 1 {
		t.Fatalf("refresh action not invoked: %d", refreshed)
	}

	// Refresh is disabled while a load is in flight (CanExecute bound to Load).
	a.VM().SetLoad(true, 0, 1)
	if a.VM().Refresh.CanExecute() {
		t.Fatal("refresh should be disabled while loading")
	}
	a.VM().Refresh.Execute() // guarded no-op
	if refreshed != 1 {
		t.Fatalf("refresh ran while loading: %d", refreshed)
	}
	a.VM().SetLoad(false, 1, 1)

	was := a.Scene().SidebarCollapsed()
	a.VM().ToggleSidebar.Execute()
	if a.Scene().SidebarCollapsed() == was {
		t.Fatal("toggle-sidebar action did not flip the sidebar")
	}
}

// TestVMTypeAssert keeps the viewmodel import meaningful (VM returns the concrete
// type) and documents the accessor's contract.
func TestVMTypeAssert(t *testing.T) {
	a := New(Config{Registry: newReg()})
	var _ *viewmodel.ViewModel = a.VM()
}
