package webui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-news-reader/reader/source"
	"github.com/go-news-reader/reader/ui"
)

type fakeApp struct {
	items      []source.Item
	scene      *ui.Scene
	refreshed  bool
}

func (f *fakeApp) Refresh(context.Context) []error { f.refreshed = true; return nil }
func (f *fakeApp) Items() []source.Item            { return f.items }
func (f *fakeApp) Scene() *ui.Scene                { return f.scene }

func newFake() *fakeApp {
	s := ui.New(400, 300, nil)
	s.SetSubs([]ui.Subscription{{Source: source.Reddit, Channel: "golang"}})
	return &fakeApp{
		items: []source.Item{{ID: "a", Source: source.Reddit, Title: "hi"}},
		scene: s,
	}
}

func TestFeedEndpoint(t *testing.T) {
	fa := newFake()
	h := Handler(fa, ui.OSMac, true)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/feed", nil))
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("feed = %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	if !fa.refreshed {
		t.Fatal("Refresh not called")
	}
	var p feedPayload
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if len(p.Items) != 1 || p.Items[0].ID != "a" || len(p.Subs) != 1 || p.OS != ui.OSMac || !p.Dark {
		t.Fatalf("payload = %+v", p)
	}
}

func TestAssets(t *testing.T) {
	h := Handler(newFake(), ui.OSLinux, false)
	for _, tc := range []struct{ path, ct string }{
		{"/reader.wasm", "application/wasm"},
		{"/wasm_exec.js", "text/javascript; charset=utf-8"},
		{"/", "text/html; charset=utf-8"},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", tc.path, nil))
		if rec.Code != 200 {
			t.Fatalf("%s = %d", tc.path, rec.Code)
		}
		if rec.Header().Get("Content-Type") != tc.ct {
			t.Fatalf("%s content-type = %q", tc.path, rec.Header().Get("Content-Type"))
		}
		if rec.Body.Len() == 0 {
			t.Fatalf("%s empty body", tc.path)
		}
	}
}

func TestNotFound(t *testing.T) {
	h := Handler(newFake(), ui.OSWindows, false)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("notfound = %d", rec.Code)
	}
}
