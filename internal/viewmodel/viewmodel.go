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
	// Search is the topbar filter text. It is two-way bound to the scene's
	// [toolkit.SearchEntry] widget (see the app's search binder), so typing in the
	// widget flows here and a programmatic Set flows back to the widget.
	Search *mvvm.Observable[string]
	// BrowseFilter is the newsgroup browser's regexp filter text, two-way bound to
	// the scene's browse [toolkit.SearchEntry] widget exactly like Search.
	BrowseFilter *mvvm.Observable[string]
	// SearchFocus is whether the topbar search field holds keyboard focus. Input
	// drives it; the binder reflects it onto the scene's focus state.
	SearchFocus *mvvm.Observable[bool]
	// Mode selects which view the scene renders.
	Mode *mvvm.Observable[ui.Mode]
	// Detail is the item opened in the reading view (valid while Mode==ModeDetail).
	Detail *mvvm.Observable[source.Item]
	// Account is the provider the accounts editor operates on. Input sets it
	// (selecting a provider, or fixing a failed sign-in); the binder reflects it
	// onto the scene's accounts editor.
	Account *mvvm.Observable[source.Kind]
	// Profile is the active profile index. Input switches it through the app,
	// which keeps this observable in step so a bound profile-tab widget (a later
	// migration) reflects the selection.
	Profile *mvvm.Observable[int]
	// Status is the status-line message (non-auth failures).
	Status *mvvm.Observable[string]
	// AuthPrompts are the in-feed "needs sign-in" banners.
	AuthPrompts *mvvm.ObservableList[ui.AuthPrompt]

	// Refresh re-aggregates the feed; disabled while a load is in flight.
	Refresh *mvvm.Command
	// ToggleSidebar collapses/expands the sidebar.
	ToggleSidebar *mvvm.Command
	// OpenSettings / OpenAccounts / OpenLog / OpenBrowse switch Mode into the
	// matching view.
	OpenSettings *mvvm.Command
	OpenAccounts *mvvm.Command
	OpenLog      *mvvm.Command
	OpenBrowse   *mvvm.Command
	// CloseView returns to the feed.
	CloseView *mvvm.Command
}

// New builds a ViewModel seeded to an empty feed view. The commands drive the
// observables (open/close switch Mode) or the injected actions (refresh,
// toggle-sidebar). Refresh's CanExecute is bound to Load, so a bound button
// re-greys itself while an aggregation is in flight.
func New(act Actions) *ViewModel {
	vm := &ViewModel{
		Items:        mvvm.NewObservableList[source.Item](),
		Load:         mvvm.NewObservable(LoadState{}),
		Pending:      mvvm.NewObservableList[source.Subscription](),
		Search:       mvvm.NewObservable(""),
		BrowseFilter: mvvm.NewObservable(""),
		SearchFocus:  mvvm.NewObservable(false),
		Mode:         mvvm.NewObservable(ui.ModeFeed),
		Detail:       mvvm.NewObservableEq(source.Item{}, sameItem),
		Account:      mvvm.NewObservable(source.Kind("")),
		Profile:      mvvm.NewObservable(0),
		Status:       mvvm.NewObservable(""),
		AuthPrompts:  mvvm.NewObservableList[ui.AuthPrompt](),
	}
	vm.Refresh = mvvm.NewCommand(act.Refresh, func() bool { return !vm.Load.Get().Active })
	mvvm.BindCanExecute(vm.Refresh, vm.Load)
	vm.ToggleSidebar = mvvm.NewCommand(act.ToggleSidebar, nil)
	vm.OpenSettings = mvvm.NewCommand(func() { vm.Mode.Set(ui.ModeSettings) }, nil)
	vm.OpenAccounts = mvvm.NewCommand(func() { vm.Mode.Set(ui.ModeAccounts) }, nil)
	vm.OpenLog = mvvm.NewCommand(func() { vm.Mode.Set(ui.ModeLog) }, nil)
	vm.OpenBrowse = mvvm.NewCommand(func() { vm.Mode.Set(ui.ModeBrowse) }, nil)
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

// FocusSearch gives (or removes) keyboard focus to the topbar search field.
func (vm *ViewModel) FocusSearch(v bool) { vm.SearchFocus.Set(v) }

// SelectAccount picks which provider the accounts editor operates on.
func (vm *ViewModel) SelectAccount(k source.Kind) { vm.Account.Set(k) }

// FixAuth opens the accounts editor pre-selected on the provider that needs
// signing in (the click target of an in-feed "needs sign-in" banner): it records
// the provider, then switches Mode to the accounts editor.
func (vm *ViewModel) FixAuth(k source.Kind) {
	vm.Account.Set(k)
	vm.Mode.Set(ui.ModeAccounts)
}

// SelectProfile records the active profile index. The app calls it while
// switching profiles so the observable tracks the live selection.
func (vm *ViewModel) SelectProfile(i int) { vm.Profile.Set(i) }
