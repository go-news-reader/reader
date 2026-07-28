package app

import (
	"context"

	"github.com/go-news-reader/reader/source"
)

// grouper is the Usenet-provider capability the newsgroup browser needs: it
// fetches the server's carried group list (cached) and can force a re-fetch. The
// registered usenet.Provider satisfies it structurally.
type grouper interface {
	Groups(ctx context.Context) ([]string, error)
	RefreshGroups(ctx context.Context) ([]string, error)
}

// SetLoadGroupsHook overrides the asynchronous group-list fetch trigger (tests
// use a synchronous variant for determinism).
func (a *App) SetLoadGroupsHook(f func(force bool)) { a.loadGroups = f }

// LoadGroups asynchronously fetches the configured Usenet server's full group
// list into the browse view (served from the provider cache after the first
// fetch). The front-end calls it when opening the browser.
func (a *App) LoadGroups() { a.loadGroups(false) }

// RefreshGroups asynchronously re-fetches the group list, bypassing the provider
// cache so newly-carried groups appear. The browser's Refresh control calls it.
func (a *App) RefreshGroups() { a.loadGroups(true) }

// doLoadGroups performs the group-list fetch synchronously: it resolves the
// Usenet provider, drives the shared loading indicator, fetches (or force
// re-fetches) the active group list and loads it into the scene. A missing or
// incapable provider, or a fetch failure, is surfaced in the status line; an
// authentication failure becomes an in-feed sign-in prompt.
func (a *App) doLoadGroups(ctx context.Context, force bool) {
	prov, ok := a.reg.Get(source.Usenet)
	if !ok {
		a.vmStatus("Usenet server not configured")
		return
	}
	g, ok := prov.(grouper)
	if !ok {
		a.vmStatus("Usenet provider cannot list groups")
		return
	}

	a.vmLoad(true, 0, 1)
	fetch := g.Groups
	if force {
		fetch = g.RefreshGroups
	}
	names, err := fetch(ctx)
	a.vmLoad(false, 1, 1)
	if err != nil {
		if ae, ok := source.AsAuthError(err); ok {
			a.vmDo(func() { a.vm.SetAuthPrompts(authPrompts([]error{ae})) })
			return
		}
		a.vmStatus("Group list failed: " + err.Error())
		return
	}
	a.post(func() { a.scene.SetBrowseGroups(names) })
	a.vmStatus("")
}

// SubscribeGroup adds a usenet:<group> subscription to the active profile (when
// not already present), then persists and re-aggregates so the group appears in
// the sidebar and feed. A click in the browser routes here.
func (a *App) SubscribeGroup(group string) {
	if a.scene.SubscribeActive(source.Usenet, group) {
		a.ApplySceneSettings()
	}
}

// UnsubscribeGroup removes the usenet:<group> subscription from the active
// profile (when present), then persists and re-aggregates so it leaves the
// sidebar and feed. Clicking a subscribed group's ✓ in the browser routes here.
func (a *App) UnsubscribeGroup(group string) {
	if a.scene.UnsubscribeActive(source.Usenet, group) {
		a.ApplySceneSettings()
	}
}
