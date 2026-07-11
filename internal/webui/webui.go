// Package webui serves the interactive aggregator: the WebAssembly front-end
// (cmd/front) blitting the ui.Scene into a browser <canvas>, plus /api/feed
// which aggregates the subscriptions and returns the merged items as JSON. The
// same handler backs `newsreader -ui` and can be hosted by a native WKWebView.
package webui

import (
	"context"
	_ "embed"
	"encoding/json"
	"net/http"

	"github.com/go-news-reader/reader/source"
	"github.com/go-news-reader/reader/ui"
)

//go:generate sh -c "cd ../.. && GOOS=js GOARCH=wasm CGO_ENABLED=0 go build -o internal/webui/assets/reader.wasm ./cmd/front"

//go:embed assets/index.html
var indexHTML []byte

//go:embed assets/reader.wasm
var wasmBin []byte

//go:embed assets/wasm_exec.js
var wasmExec []byte

// feedPayload is the JSON shape served at /api/feed (mirrored by cmd/front).
type feedPayload struct {
	Items []source.Item     `json:"items"`
	Subs  []ui.Subscription `json:"subs"`
	OS    string            `json:"os"`
	Dark  bool              `json:"dark"`
}

// App is the slice of *app.App the web UI needs; an interface so it stays
// testable without a real provider registry.
type App interface {
	Refresh(context.Context) []error
	Items() []source.Item
	Scene() *ui.Scene
}

// Handler serves the front-end assets and the /api/feed endpoint for a. osName
// (ui.OSMac|OSLinux|OSWindows) and dark seed the initial theme.
func Handler(a App, osName string, dark bool) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/feed", func(w http.ResponseWriter, r *http.Request) {
		a.Refresh(r.Context())
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(feedPayload{
			Items: a.Items(),
			Subs:  a.Scene().Subs,
			OS:    osName,
			Dark:  dark,
		})
	})
	mux.HandleFunc("/reader.wasm", asset(wasmBin, "application/wasm"))
	mux.HandleFunc("/wasm_exec.js", asset(wasmExec, "text/javascript; charset=utf-8"))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(indexHTML)
	})
	return mux
}

func asset(data []byte, contentType string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.Write(data)
	}
}
