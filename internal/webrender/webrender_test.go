package webrender

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-webengine/engine"
)

// testEngine returns an Engine whose HTTP client is the plain default (no TLS
// fingerprinting), so it can talk to an httptest plain-HTTP server hermetically.
func testEngine() *Engine {
	e := engine.New()
	e.Client = &http.Client{}
	return WithEngine(e)
}

func TestRenderServesPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><head><title>T</title></head><body><h1>Hello</h1><p>Article <a href="/next">body</a> text.</p></body></html>`))
	}))
	defer srv.Close()

	r := testEngine()
	img, links, final, err := r.Render(context.Background(), srv.URL, 400)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if img == nil {
		t.Fatal("nil image")
	}
	if final != srv.URL {
		t.Errorf("finalURL = %q, want %q", final, srv.URL)
	}
	if len(links) != 1 || links[0].Href == "" {
		t.Errorf("links = %+v, want one anchor", links)
	}
	if got := img.Bounds().Dx(); got != 400 {
		t.Errorf("width = %d, want 400", got)
	}
	if img.Bounds().Dy() <= 0 {
		t.Errorf("height = %d, want > 0", img.Bounds().Dy())
	}
}

func TestRenderProgressive(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><head><title>T</title></head><body><h1>Hi</h1><p>Body <a href="/n">link</a>.</p></body></html>`))
	}))
	defer srv.Close()

	var frames []Frame
	err := testEngine().RenderProgressive(context.Background(), srv.URL, 400, func(f Frame) {
		frames = append(frames, f)
	})
	if err != nil {
		t.Fatalf("RenderProgressive: %v", err)
	}
	if len(frames) == 0 {
		t.Fatal("expected at least one frame")
	}
	last := frames[len(frames)-1]
	if !last.Final {
		t.Fatal("last frame must be Final")
	}
	if last.Img == nil || last.Img.Bounds().Dx() != 400 || last.Img.Bounds().Dy() <= 0 {
		t.Fatalf("final frame image wrong: %+v", last.Img.Bounds())
	}
	if last.FinalURL != srv.URL {
		t.Errorf("finalURL = %q, want %q", last.FinalURL, srv.URL)
	}
	if len(last.Links) != 1 || last.Links[0].Href == "" {
		t.Errorf("final links = %+v, want one anchor", last.Links)
	}
	for i, f := range frames[:len(frames)-1] {
		if f.Final {
			t.Fatalf("frame %d is Final before the last", i)
		}
	}

	// Default width (<=0) path.
	var gotDefault bool
	_ = testEngine().RenderProgressive(context.Background(), srv.URL, 0, func(f Frame) {
		if f.Final && f.Img.Bounds().Dx() == defaultWidth {
			gotDefault = true
		}
	})
	if !gotDefault {
		t.Fatalf("default-width progressive final not %d wide", defaultWidth)
	}

	// A cancelled context errors before any frame.
	cctx, cancel := context.WithCancel(context.Background())
	cancel()
	n := 0
	if err := testEngine().RenderProgressive(cctx, "http://127.0.0.1:9/nope", 300, func(Frame) { n++ }); err == nil {
		t.Fatal("expected error on cancelled fetch")
	}
	if n != 0 {
		t.Fatalf("no frames expected on fetch error, got %d", n)
	}
}

func TestRenderDefaultWidth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html><body>x</body></html>"))
	}))
	defer srv.Close()

	img, _, _, err := testEngine().Render(context.Background(), srv.URL, 0) // width<=0 → default
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if got := img.Bounds().Dx(); got != defaultWidth {
		t.Errorf("width = %d, want default %d", got, defaultWidth)
	}
}

func TestRenderError(t *testing.T) {
	// A cancelled context makes the fetch fail → Render returns the error, nil img.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	img, _, _, err := testEngine().Render(ctx, "http://127.0.0.1:9/nope", 300)
	if err == nil {
		t.Fatal("expected an error for a cancelled fetch")
	}
	if img != nil {
		t.Fatal("expected nil image on error")
	}
}

func TestConstructors(t *testing.T) {
	if New() == nil {
		t.Fatal("New returned nil")
	}
	if WithEngine(nil) == nil {
		t.Fatal("WithEngine(nil) should fall back to a usable engine")
	}
	if WithEngine(engine.New()) == nil {
		t.Fatal("WithEngine(e) returned nil")
	}
}
