package app

import (
	"errors"
	"testing"

	"github.com/go-news-reader/reader/feeds"
	"github.com/go-news-reader/reader/source"
)

// TestLoadPluginsRegisters verifies discovered plugin providers are registered
// into the live registry.
func TestLoadPluginsRegisters(t *testing.T) {
	a := New(Config{Registry: newReg(), Width: 400, Height: 300})
	a.pluginLoader = func(string) ([]source.Provider, func() error, error) {
		return []source.Provider{fakeProv{kind: "example"}}, func() error { return nil }, nil
	}
	a.loadPlugins()

	if got := len(a.pluginProviders); got != 1 {
		t.Fatalf("pluginProviders = %d, want 1", got)
	}
	if _, ok := a.reg.Get("example"); !ok {
		t.Fatal("example plugin provider was not registered")
	}
}

// TestLoadPluginsError verifies a discovery error leaves the reader running with
// no plugin providers recorded.
func TestLoadPluginsError(t *testing.T) {
	a := New(Config{Registry: newReg(), Width: 400, Height: 300})
	a.pluginLoader = func(string) ([]source.Provider, func() error, error) {
		return nil, nil, errors.New("bad plugins dir")
	}
	a.loadPlugins()

	if a.pluginProviders != nil {
		t.Fatalf("pluginProviders = %v, want nil after error", a.pluginProviders)
	}
	if _, ok := a.reg.Get("example"); ok {
		t.Fatal("no provider should be registered after a discovery error")
	}
}

// TestPluginsSurviveRegistryRebuild verifies the discovered plugins are
// re-registered when ApplyAccounts rebuilds the registry (so a running plugin is
// not lost, and its subprocess is not respawned).
func TestPluginsSurviveRegistryRebuild(t *testing.T) {
	a := New(Config{Registry: newReg(), Width: 400, Height: 300})
	a.pluginLoader = func(string) ([]source.Provider, func() error, error) {
		return []source.Provider{fakeProv{kind: "example"}}, func() error { return nil }, nil
	}
	a.loadPlugins()

	// Rebuild the registry from scratch (a fresh registry with only built-ins).
	a.SetRegistryBuilder(func(feeds.Options) *source.Registry { return newReg() })
	a.rebuildRegistry()

	if _, ok := a.reg.Get("example"); !ok {
		t.Fatal("example plugin provider was lost across a registry rebuild")
	}
}
