package viewmodel

import (
	"testing"

	"github.com/go-news-reader/reader/source"
	"github.com/go-news-reader/reader/ui"
)

func TestLoadStateText(t *testing.T) {
	if got := (LoadState{}).Text(); got != "" {
		t.Fatalf("idle text = %q, want empty", got)
	}
	if got := (LoadState{Active: true, Done: 2, Total: 5}).Text(); got != "Loading 2/5 sources…" {
		t.Fatalf("text = %q", got)
	}
}

func TestSetItems(t *testing.T) {
	vm := New(Actions{})
	var changes int
	vm.Items.SubscribeChanged(func() { changes++ })
	vm.SetItems([]source.Item{{ID: "a"}, {ID: "b"}})
	if vm.Items.Len() != 2 || vm.Items.At(0).ID != "a" {
		t.Fatalf("items = %+v", vm.Items.Slice())
	}
	vm.SetItems(nil) // clears
	if vm.Items.Len() != 0 {
		t.Fatalf("items not cleared: %d", vm.Items.Len())
	}
	if changes == 0 {
		t.Fatal("no change events emitted")
	}
}

func TestSetLoad(t *testing.T) {
	vm := New(Actions{})
	var seen LoadState
	vm.Load.Subscribe(func(ls LoadState) { seen = ls })
	vm.SetLoad(true, 3, 7)
	if seen != (LoadState{Active: true, Done: 3, Total: 7}) {
		t.Fatalf("load = %+v", seen)
	}
}

func TestPending(t *testing.T) {
	vm := New(Actions{})
	subs := []source.Subscription{
		{Source: source.Reddit, Channel: "golang"},
		{Source: source.Mastodon},
	}
	vm.SetPending(subs)
	if vm.Pending.Len() != 2 {
		t.Fatalf("pending len = %d", vm.Pending.Len())
	}
	vm.ClearPending(source.Reddit, "golang")
	if vm.Pending.Len() != 1 || vm.Pending.At(0).Source != source.Mastodon {
		t.Fatalf("after clear: %+v", vm.Pending.Slice())
	}
	vm.ClearPending(source.Reddit, "golang") // no match -> loop exhausts, no-op
	if vm.Pending.Len() != 1 {
		t.Fatalf("no-op clear changed the list: %d", vm.Pending.Len())
	}
}

func TestSetStatusAndAuthPrompts(t *testing.T) {
	vm := New(Actions{})
	vm.SetStatus("boom")
	if vm.Status.Get() != "boom" {
		t.Fatalf("status = %q", vm.Status.Get())
	}
	vm.SetAuthPrompts([]ui.AuthPrompt{{Kind: source.Reddit, Reason: "oauth"}})
	if vm.AuthPrompts.Len() != 1 || vm.AuthPrompts.At(0).Kind != source.Reddit {
		t.Fatalf("prompts = %+v", vm.AuthPrompts.Slice())
	}
}

func TestOpenDetailAndSameItem(t *testing.T) {
	vm := New(Actions{})
	var modes []ui.Mode
	vm.Mode.Subscribe(func(m ui.Mode) { modes = append(modes, m) })
	it := source.Item{ID: "x", Source: source.Reddit}
	vm.OpenDetail(it)
	if vm.Mode.Get() != ui.ModeDetail || vm.Detail.Get().ID != "x" {
		t.Fatalf("open detail: mode=%v detail=%+v", vm.Mode.Get(), vm.Detail.Get())
	}
	// sameItem: setting an equal-identity item is a no-op (no notify).
	var detailChanges int
	vm.Detail.SubscribeChanged(func() { detailChanges++ })
	vm.Detail.Set(source.Item{ID: "x", Source: source.Reddit, Title: "changed-title"})
	if detailChanges != 0 {
		t.Fatalf("same-identity item notified: %d", detailChanges)
	}
	// A different identity does notify.
	vm.Detail.Set(source.Item{ID: "y", Source: source.Reddit})
	if detailChanges != 1 {
		t.Fatalf("different item did not notify: %d", detailChanges)
	}
}

func TestCommands(t *testing.T) {
	var refreshed, toggled int
	vm := New(Actions{
		Refresh:       func() { refreshed++ },
		ToggleSidebar: func() { toggled++ },
	})

	vm.Refresh.Execute()
	if refreshed != 1 {
		t.Fatalf("refresh action = %d", refreshed)
	}
	// Refresh disabled while loading (CanExecute bound to Load).
	vm.SetLoad(true, 0, 1)
	if vm.Refresh.CanExecute() {
		t.Fatal("refresh should be disabled while loading")
	}
	vm.SetLoad(false, 0, 0)
	if !vm.Refresh.CanExecute() {
		t.Fatal("refresh should re-enable when idle")
	}

	vm.ToggleSidebar.Execute()
	if toggled != 1 {
		t.Fatalf("toggle action = %d", toggled)
	}

	vm.OpenSettings.Execute()
	if vm.Mode.Get() != ui.ModeSettings {
		t.Fatalf("mode = %v", vm.Mode.Get())
	}
	vm.OpenAccounts.Execute()
	if vm.Mode.Get() != ui.ModeAccounts {
		t.Fatalf("mode = %v", vm.Mode.Get())
	}
	vm.OpenLog.Execute()
	if vm.Mode.Get() != ui.ModeLog {
		t.Fatalf("mode = %v", vm.Mode.Get())
	}
	vm.CloseView.Execute()
	if vm.Mode.Get() != ui.ModeFeed {
		t.Fatalf("mode = %v", vm.Mode.Get())
	}
}

// TestInputDrivers proves the parameterized input drivers set their observables:
// search focus, account selection, fix-auth (account + accounts mode), and the
// active-profile index.
func TestInputDrivers(t *testing.T) {
	vm := New(Actions{})

	vm.FocusSearch(true)
	if !vm.SearchFocus.Get() {
		t.Fatal("FocusSearch did not set the observable")
	}

	vm.SelectAccount(source.Usenet)
	if vm.Account.Get() != source.Usenet {
		t.Fatalf("SelectAccount = %q", vm.Account.Get())
	}

	// FixAuth records the provider AND switches Mode to the accounts editor.
	var modes []ui.Mode
	vm.Mode.Subscribe(func(m ui.Mode) { modes = append(modes, m) })
	vm.FixAuth(source.Reddit)
	if vm.Account.Get() != source.Reddit || vm.Mode.Get() != ui.ModeAccounts {
		t.Fatalf("FixAuth: account=%q mode=%v", vm.Account.Get(), vm.Mode.Get())
	}
	if len(modes) != 1 || modes[0] != ui.ModeAccounts {
		t.Fatalf("FixAuth mode notifications = %v", modes)
	}

	vm.SelectProfile(2)
	if vm.Profile.Get() != 2 {
		t.Fatalf("SelectProfile = %d", vm.Profile.Get())
	}
}

// TestNilActions proves a ViewModel built without actions is safe to execute
// (the commands guard on a nil exec).
func TestNilActions(t *testing.T) {
	vm := New(Actions{})
	vm.Refresh.Execute()       // nil exec, guarded no-op
	vm.ToggleSidebar.Execute() // nil exec, guarded no-op
}
