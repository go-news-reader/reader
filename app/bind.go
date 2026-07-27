package app

import (
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
func bindScene(s *ui.Scene, vm *viewmodel.ViewModel) {
	vm.Items.SubscribeChanged(func() { s.SetItems(vm.Items.Slice()) })
	vm.Load.Subscribe(func(ls viewmodel.LoadState) { s.SetLoading(ls.Active, ls.Done, ls.Total) })
	vm.Pending.SubscribeChanged(func() { s.SetPendingSources(vm.Pending.Slice()) })
	vm.Status.Subscribe(func(v string) { s.SetStatus(v) })
	vm.AuthPrompts.SubscribeChanged(func() { s.SetAuthPrompts(vm.AuthPrompts.Slice()) })
	vm.Search.Subscribe(func(v string) { s.SetSearch(v) })
	vm.Mode.Subscribe(func(m ui.Mode) { applyMode(s, m, vm.Detail.Get()) })
	vm.Detail.Subscribe(func(it source.Item) {
		// Re-open the reading view when the target item changes while already in
		// detail mode (a bare Mode.Set(ModeDetail) would be a no-op there).
		if vm.Mode.Get() == ui.ModeDetail {
			s.OpenDetail(it)
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
	default: // ui.ModeFeed
		s.CloseDetail()
	}
}
