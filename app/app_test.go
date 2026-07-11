package app

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"io"
	"path/filepath"
	"testing"

	"github.com/go-news-reader/reader/internal/settings"
	"github.com/go-news-reader/reader/source"
	"github.com/go-news-reader/reader/ui"
)

type fakeProv struct {
	kind  source.Kind
	items []source.Item
	err   error
}

func (f fakeProv) Kind() source.Kind { return f.kind }
func (f fakeProv) Feed(context.Context, source.Query) (source.Result, error) {
	if f.err != nil {
		return source.Result{}, f.err
	}
	return source.Result{Items: f.items}, nil
}

func newReg(provs ...source.Provider) *source.Registry {
	r := source.NewRegistry()
	for _, p := range provs {
		r.Register(p)
	}
	return r
}

func TestNewDefaultsAndAccessors(t *testing.T) {
	reg := newReg(fakeProv{kind: source.Reddit})
	a := New(Config{
		Registry:      reg,
		Subscriptions: []source.Subscription{{Source: source.Reddit, Channel: "golang"}},
		OS:            ui.OSMac,
	})
	if a.Scene().W != 1000 || a.Scene().H != 700 {
		t.Fatalf("default size = %dx%d", a.Scene().W, a.Scene().H)
	}
	if len(a.Scene().Subs) != 1 || a.Scene().Subs[0].Channel != "golang" {
		t.Fatalf("subs not mapped: %+v", a.Scene().Subs)
	}
}

func TestRefreshSuccess(t *testing.T) {
	reg := newReg(fakeProv{kind: source.Reddit, items: []source.Item{
		{ID: "a", Source: source.Reddit, Created: 2},
		{ID: "b", Source: source.Reddit, Created: 1},
	}})
	a := New(Config{
		Registry:      reg,
		Subscriptions: []source.Subscription{{Source: source.Reddit, Channel: "golang"}},
		Width:         400, Height: 300,
	})
	errs := a.Refresh(context.Background())
	if len(errs) != 0 {
		t.Fatalf("errs = %v", errs)
	}
	if len(a.Items()) != 2 || a.Items()[0].ID != "a" {
		t.Fatalf("items = %+v", a.Items())
	}
	if a.Scene().Status != "" {
		t.Fatalf("status = %q", a.Scene().Status)
	}
}

func TestRefreshError(t *testing.T) {
	reg := newReg(fakeProv{kind: source.Reddit, err: errors.New("boom")})
	a := New(Config{
		Registry:      reg,
		Subscriptions: []source.Subscription{{Source: source.Reddit}},
	})
	errs := a.Refresh(context.Background())
	if len(errs) == 0 {
		t.Fatal("want errs")
	}
	if a.Scene().Status == "" {
		t.Fatal("status should carry the error")
	}
}

func TestRenderPNG(t *testing.T) {
	a := New(Config{Registry: newReg(), Width: 360, Height: 240})
	data, err := a.RenderPNG()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width != 360 || cfg.Height != 240 {
		t.Fatalf("png = %dx%d", cfg.Width, cfg.Height)
	}
}

func TestRenderPNGEncodeError(t *testing.T) {
	orig := encodePNG
	encodePNG = func(io.Writer, image.Image) error { return errors.New("encode") }
	defer func() { encodePNG = orig }()
	a := New(Config{Registry: newReg(), Width: 360, Height: 240})
	if _, err := a.RenderPNG(); err == nil {
		t.Fatal("want encode error")
	}
}

func TestFrameDoubleBuffer(t *testing.T) {
	a := New(Config{Registry: newReg(), Width: 360, Height: 240})
	b1, changed := a.Frame()
	if !changed || len(b1) != 360*240*4 {
		t.Fatalf("first frame changed=%v len=%d", changed, len(b1))
	}
	// No state change -> no redraw, same buffer returned.
	b2, changed := a.Frame()
	if changed {
		t.Fatal("unchanged scene produced a new frame")
	}
	if &b2[0] != &b1[0] {
		t.Fatal("front buffer changed without damage")
	}
	// Mutate -> redraw into the back buffer (the other one).
	a.Scene().Scroll(1)
	b3, changed := a.Frame()
	if !changed {
		t.Fatal("damage did not produce a frame")
	}
	if &b3[0] == &b1[0] {
		t.Fatal("expected double-buffer swap")
	}
	// Resize -> reallocate and force redraw.
	a.Scene().Resize(400, 300)
	b4, changed := a.Frame()
	if !changed || len(b4) != 400*300*4 {
		t.Fatalf("resize frame changed=%v len=%d", changed, len(b4))
	}
}

func TestSetTheme(t *testing.T) {
	a := New(Config{Registry: newReg(), OS: ui.OSLinux})
	light := a.Scene()
	a.SetTheme(ui.OSWindows, true)
	if a.Scene() != light {
		t.Fatal("SetTheme should not replace the scene")
	}
}

func TestNewWithSettings(t *testing.T) {
	set := &settings.Settings{
		Profiles: []settings.Profile{
			{Name: "Home", Subs: []source.Subscription{{Source: source.Reddit, Channel: "golang"}}},
			{Name: "Tech", Subs: []source.Subscription{{Source: source.Lemmy, Channel: "tech"}}},
		},
		Active: 1, Theme: settings.ThemeDark, CachePath: "/c",
	}
	a := New(Config{Registry: newReg(fakeProv{kind: source.Lemmy}), Settings: set, OS: ui.OSMac})
	s := a.Scene()
	if s.ActiveProfileIndex() != 1 || len(s.Subs) != 1 || s.Subs[0].Channel != "tech" {
		t.Fatalf("active profile not applied: idx=%d subs=%+v", s.ActiveProfileIndex(), s.Subs)
	}
	if s.ThemeName() != settings.ThemeDark || s.CachePath() != "/c" {
		t.Fatalf("scalars = %q %q", s.ThemeName(), s.CachePath())
	}
	if len(a.subs) != 1 || a.subs[0].Source != source.Lemmy {
		t.Fatalf("app subs = %+v", a.subs)
	}
}

func TestApplySceneSettingsPersistsAndRebuilds(t *testing.T) {
	set := &settings.Settings{
		Profiles: []settings.Profile{
			{Name: "A", Subs: []source.Subscription{{Source: source.Reddit, Channel: "a"}}},
			{Name: "B", Subs: []source.Subscription{{Source: source.Reddit, Channel: "b"}}},
		},
		Active: 0, Theme: settings.ThemeSystem,
	}
	path := filepath.Join(t.TempDir(), "s.json")
	a := New(Config{
		Registry: newReg(fakeProv{kind: source.Reddit, items: []source.Item{{ID: "x", Source: source.Reddit}}}),
		Settings: set, Store: settings.NewStore(path), OS: ui.OSMac,
	})
	var refreshed int
	a.SetRefreshHook(func() { refreshed++; a.Refresh(context.Background()) })

	a.Scene().SetActiveProfile(1) // switch to B
	a.ApplySceneSettings()

	if refreshed != 1 {
		t.Fatalf("refresh hook not called: %d", refreshed)
	}
	if len(a.subs) != 1 || a.subs[0].Channel != "b" {
		t.Fatalf("subs not rebuilt: %+v", a.subs)
	}
	// Settings were persisted with the new active index.
	loaded, err := settings.NewStore(path).Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Active != 1 {
		t.Fatalf("persisted active = %d", loaded.Active)
	}
	if len(a.Items()) != 1 {
		t.Fatalf("re-aggregate did not load items: %d", len(a.Items()))
	}
}

func TestApplySceneSettingsNoStore(t *testing.T) {
	a := New(Config{Registry: newReg(fakeProv{kind: source.Reddit})})
	a.SetRefreshHook(func() {}) // no-op, synchronous
	a.ApplySceneSettings()      // store == nil branch, must not panic
}

func TestDefaultRefreshHook(t *testing.T) {
	// Exercise the default (goroutine) refresh hook against a fake provider.
	a := New(Config{Registry: newReg(fakeProv{kind: source.Reddit})})
	a.ApplySceneSettings() // spawns go a.Refresh(...)
	// Give the goroutine a chance without asserting on its async result.
	_ = a.Items()
}
