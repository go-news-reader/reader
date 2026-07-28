package app

import (
	"github.com/go-widgets/mvvm"

	"github.com/go-news-reader/reader/internal/viewmodel"
	"github.com/go-news-reader/reader/source"
	"github.com/go-news-reader/reader/ui"
)

// bindScene is the MVVM adapter seam: it subscribes the scene to the view-model
// so every vm.*.Set / list mutation flows through to the rendered scene. Data
// flows app logic → ViewModel (Observable/List) → subscription → Scene, which is
// why the ui package never imports the view-model. In a later phase the scene's
// views become toolkit widgets bound with mvvm's pointer binders; for now the
// scene keeps its own rendering and simply receives its data through these
// subscriptions to the existing setters.
//
// mvvm's Observable/ObservableList are single-goroutine by contract, and the
// scene is read by the render thread, so each subscription runs on the goroutine
// that fired the observable (the aggregation goroutine, for a streaming
// refresh): there it snapshots the observable's value — safe, same goroutine as
// the write — and hands the resulting scene write to a.post. a.post applies it
// inline (single-threaded CLI/tests) or marshals it to the render thread
// (the window front-end), so the scene is never mutated concurrently with Draw.
func bindScene(a *App) {
	s, vm := a.scene, a.vm
	vm.Items.SubscribeChanged(func() { items := vm.Items.Slice(); a.post(func() { s.SetItems(items) }) })
	vm.Load.Subscribe(func(ls viewmodel.LoadState) { a.post(func() { s.SetLoading(ls.Active, ls.Done, ls.Total) }) })
	vm.Pending.SubscribeChanged(func() { subs := vm.Pending.Slice(); a.post(func() { s.SetPendingSources(subs) }) })
	vm.Status.Subscribe(func(v string) { a.post(func() { s.SetStatus(v) }) })
	vm.AuthPrompts.SubscribeChanged(func() { p := vm.AuthPrompts.Slice(); a.post(func() { s.SetAuthPrompts(p) }) })
	// Search is a real two-way widget binding: mvvm.BindField seeds the scene's
	// toolkit.SearchEntry from vm.Search, composes vm.Search.Set into the widget's
	// OnChange (so a keystroke routed through the widget flows to the VM), and
	// pushes vm.Search → SearchEntry.Text on change. Unlike the other reflections
	// this writes the widget field directly rather than through a.post: vm.Search
	// is only ever Set from the input/render thread (a topbar keystroke or a
	// programmatic Set on that thread), never from the background aggregation
	// goroutines, so the write cannot race Draw. InvalidateSearch requests the
	// repaint (the topbar sprite cache keys on the widget text).
	mvvm.BindField(vm.Search, &s.SearchEntry().Text, &s.SearchEntry().OnChange, s.InvalidateSearch)
	// The newsgroup browser's regexp filter is bound the same way: BrowseFilter is
	// only ever Set from the input/render thread, so it too writes the widget field
	// directly rather than through a.post.
	mvvm.BindField(vm.BrowseFilter, &s.BrowseEntry().Text, &s.BrowseEntry().OnChange, s.InvalidateBrowse)
	vm.SearchFocus.Subscribe(func(v bool) { a.post(func() { s.FocusSearch(v) }) })
	vm.Account.Subscribe(func(k source.Kind) { a.post(func() { s.SelectAccount(k) }) })
	vm.Mode.Subscribe(func(m ui.Mode) { d := vm.Detail.Get(); a.post(func() { applyMode(s, m, d) }) })
	vm.Detail.Subscribe(func(it source.Item) {
		// Re-open the reading view when the target item changes while already in
		// detail mode (a bare Mode.Set(ModeDetail) would be a no-op there).
		if vm.Mode.Get() == ui.ModeDetail {
			a.post(func() { s.OpenDetail(it) })
		}
	})
}

// applyMode reflects a view-model Mode change onto the scene by invoking the
// scene's existing view-switch methods (which carry the per-view reset side
// effects). Every close resolves to ModeFeed, and the scene's Close* methods are
// all equivalent (mode ← ModeFeed), so CloseDetail stands in for the generic
// return-to-feed.
func applyMode(s *ui.Scene, m ui.Mode, detail source.Item) {
	switch m {
	case ui.ModeDetail:
		s.OpenDetail(detail)
	case ui.ModeSettings:
		s.OpenSettings()
	case ui.ModeLog:
		s.OpenLog()
	case ui.ModeAccounts:
		s.OpenAccounts()
	case ui.ModeBrowse:
		s.OpenBrowse()
	default: // ui.ModeFeed
		s.CloseDetail()
	}
}
