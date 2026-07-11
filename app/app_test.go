package app

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"io"
	"testing"

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

func TestSetTheme(t *testing.T) {
	a := New(Config{Registry: newReg(), OS: ui.OSLinux})
	light := a.Scene()
	a.SetTheme(ui.OSWindows, true)
	if a.Scene() != light {
		t.Fatal("SetTheme should not replace the scene")
	}
}
