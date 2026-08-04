package app

import (
	"context"

	"github.com/go-news-reader/reader/source"
)

// groupStatter is the Usenet provider capability that samples a newsgroup's
// content mix. The provider registry's Usenet entry implements it.
type groupStatter interface {
	GroupStats(ctx context.Context, name string, sample int) (source.GroupStats, error)
}

// ScanBrowseGroup kicks off a sampled content-mix scan for the newsgroup under
// the browser's keyboard selection, unless one is already cached. A no-op when
// the selected row isn't a group. Called after browse-tree navigation.
func (a *App) ScanBrowseGroup() {
	name, ok := a.scene.SelectedBrowseGroup()
	if !ok || a.scene.HasGroupStats(name) {
		return
	}
	a.groupStatsFetch(name)
}

// loadGroupStats scans the group via the Usenet provider and delivers the
// estimate to the browser on the UI thread. Provider/registry gaps and scan
// errors are swallowed (the row just keeps showing its post count). The default
// groupStatsFetch runs this on its own goroutine; tests call it directly.
func (a *App) loadGroupStats(ctx context.Context, name string) {
	prov, ok := a.reg.Get(source.Usenet)
	if !ok {
		return
	}
	gs, ok := prov.(groupStatter)
	if !ok {
		return
	}
	st, err := gs.GroupStats(ctx, name, 0)
	if err != nil {
		return
	}
	a.post(func() { a.scene.SetGroupStats(name, st) })
}

// SetGroupStatsFetchHook overrides the async group scan (tests use a synchronous
// variant for determinism).
func (a *App) SetGroupStatsFetchHook(f func(name string)) { a.groupStatsFetch = f }
