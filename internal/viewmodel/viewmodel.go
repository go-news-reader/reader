// Package viewmodel holds the news reader's core UI state as go-widgets/mvvm
// primitives — the "ViewModel" layer of an MVVM split. It owns the merged feed,
// the live loading/pending state, the search text, the current view mode, the
// status line and the in-feed sign-in prompts as Observables/ObservableLists,
// plus a small set of parameter-less Commands for the chrome actions (refresh,
// toggle-sidebar, open settings/accounts/log, close-to-feed).
//
// It is rendering-agnostic: it imports [ui] only for the small view-facing enum
// and value types ([ui.Mode], [ui.AuthPrompt]) and never touches the painter or
// the scene. The app drives these observables; a binder subscribes the scene to
// them (app logic → ViewModel → subscription → Scene), so the ui package stays
// view-only and never imports this one.
package viewmodel

import (
	"fmt"

	"github.com/go-widgets/mvvm"

	"github.com/go-news-reader/reader/source"
	"github.com/go-news-reader/reader/ui"
)

// LoadState is the live aggregation progress: whether a refresh is in flight and
// how many sources have returned so far. It is comparable, so it rides in a
// plain [mvvm.Observable] whose equal-value Set is a no-op.
type LoadState struct {
	Active      bool
	Done, Total int
}

// Text renders the "Loading N/M sources…" label for a progress indicator, or ""
// when no refresh is in flight.
func (ls LoadState) Text() string {
	if !ls.Active {
		return ""
	}
	return fmt.Sprintf("Loading %d/%d sources…", ls.Done, ls.Total)
}

// Actions are the app-supplied side effects the parameter-less commands invoke.
// A nil action makes its command a safe no-op (Execute guards on a nil exec).
type Actions struct {
	// Refresh re-aggregates the active profile's subscriptions.
	Refresh func()
	// ToggleSidebar collapses/expands the sidebar chrome.
	ToggleSidebar func()
}

// ViewModel is the observable core state of the reader plus its chrome commands.
type ViewModel struct {
	// Items is the merged, newest-first feed.
	Items *mvvm.ObservableList[source.Item]
	// Load is the streaming-refresh progress (drives the loading indicator).
	Load *mvvm.Observable[LoadState]
	// Pending is the set of subscriptions whose fetch is still outstanding.
	Pending *mvvm.ObservableList[source.Subscription]
	// Search is the topbar filter text.
	Search *mvvm.Observable[string]
	// Mode selects which view the scene renders.
	Mode *mvvm.Observable[ui.Mode]
	// Detail is the item opened in the reading view (valid while Mode==ModeDetail).
	Detail *mvvm.Observable[source.Item]
	// Status is the status-line message (non-auth failures).
	Status *mvvm.Observable[string]
	// AuthPrompts are the in-feed "needs sign-in" banners.
	AuthPrompts *mvvm.ObservableList[ui.AuthPrompt]

	// Refresh re-aggregates the feed; disabled while a load is in flight.
	Refresh *mvvm.Command
	// ToggleSidebar collapses/expands the sidebar.
	ToggleSidebar *mvvm.Command
	// OpenSettings / OpenAccounts / OpenLog switch Mode into the matching view.
	OpenSettings *mvvm.Command
	OpenAccounts *mvvm.Command
	OpenLog      *mvvm.Command
	// CloseView returns to the feed.
	CloseView *mvvm.Command
}

// New builds a ViewModel seeded to an empty feed view. The commands drive the
// observables (open/close switch Mode) or the injected actions (refresh,
// toggle-sidebar). Refresh's CanExecute is bound to Load, so a bound button
// re-greys itself while an aggregation is in flight.
func New(act Actions) *ViewModel {
	vm := &ViewModel{
		Items:       mvvm.NewObservableList[source.Item](),
		Load:        mvvm.NewObservable(LoadState{}),
		Pending:     mvvm.NewObservableList[source.Subscription](),
		Search:      mvvm.NewObservable(""),
		Mode:        mvvm.NewObservable(ui.ModeFeed),
		Detail:      mvvm.NewObservableEq(source.Item{}, sameItem),
		Status:      mvvm.NewObservable(""),
		AuthPrompts: mvvm.NewObservableList[ui.AuthPrompt](),
	}
	vm.Refresh = mvvm.NewCommand(act.Refresh, func() bool { return !vm.Load.Get().Active })
	mvvm.BindCanExecute(vm.Refresh, vm.Load)
	vm.ToggleSidebar = mvvm.NewCommand(act.ToggleSidebar, nil)
	vm.OpenSettings = mvvm.NewCommand(func() { vm.Mode.Set(ui.ModeSettings) }, nil)
	vm.OpenAccounts = mvvm.NewCommand(func() { vm.Mode.Set(ui.ModeAccounts) }, nil)
	vm.OpenLog = mvvm.NewCommand(func() { vm.Mode.Set(ui.ModeLog) }, nil)
	vm.CloseView = mvvm.NewCommand(func() { vm.Mode.Set(ui.ModeFeed) }, nil)
	return vm
}

// sameItem is the Detail change test: two items are the same reading target when
// their source-scoped identity matches (== cannot apply — Item has slices).
func sameItem(a, b source.Item) bool { return a.Source == b.Source && a.ID == b.ID }

// SetItems replaces the whole feed (the caller has already merged/sorted it).
func (vm *ViewModel) SetItems(items []source.Item) {
	vm.Items.Clear()
	vm.Items.Append(items...)
}

// SetLoad publishes the current aggregation progress.
func (vm *ViewModel) SetLoad(active bool, done, total int) {
	vm.Load.Set(LoadState{Active: active, Done: done, Total: total})
}

// SetPending replaces the outstanding-subscription set.
func (vm *ViewModel) SetPending(subs []source.Subscription) {
	vm.Pending.Clear()
	vm.Pending.Append(subs...)
}

// ClearPending removes the first outstanding subscription matching source+channel
// once it has returned; an unknown source+channel is a no-op.
func (vm *ViewModel) ClearPending(k source.Kind, ch string) {
	for i := 0; i < vm.Pending.Len(); i++ {
		if s := vm.Pending.At(i); s.Source == k && s.Channel == ch {
			vm.Pending.RemoveAt(i)
			return
		}
	}
}

// SetStatus publishes the status-line message.
func (vm *ViewModel) SetStatus(s string) { vm.Status.Set(s) }

// SetAuthPrompts replaces the in-feed sign-in prompts.
func (vm *ViewModel) SetAuthPrompts(p []ui.AuthPrompt) {
	vm.AuthPrompts.Clear()
	vm.AuthPrompts.Append(p...)
}

// OpenDetail opens the reading view for it (sets the Detail item, then switches
// Mode to ModeDetail).
func (vm *ViewModel) OpenDetail(it source.Item) {
	vm.Detail.Set(it)
	vm.Mode.Set(ui.ModeDetail)
}
