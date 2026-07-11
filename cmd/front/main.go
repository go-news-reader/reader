// Command front is the WebAssembly front-end: it renders the aggregator's
// go-widgets UI ([ui.Scene]) into a browser <canvas> and fetches the merged
// feed over same-origin /api/feed. The Scene itself is pure Go and covered by
// native tests; this file is the thin syscall/js glue (GOOS=js) that a browser
// — or the macOS WKWebView host — runs. It is not part of the coverage gate.
//
//go:build js && wasm

package main

import (
	"encoding/json"
	"net/http"
	"syscall/js"

	"github.com/go-news-reader/reader/source"
	"github.com/go-news-reader/reader/ui"
)

// feedPayload mirrors the JSON served by /api/feed.
type feedPayload struct {
	Items []source.Item     `json:"items"`
	Subs  []ui.Subscription `json:"subs"`
	OS    string            `json:"os"`
	Dark  bool              `json:"dark"`
}

func main() {
	doc := js.Global().Get("document")
	canvas := doc.Call("getElementById", "screen")
	if !canvas.Truthy() {
		js.Global().Get("console").Call("error", "front: no #screen canvas")
		select {}
	}
	ctx := canvas.Call("getContext", "2d")

	scene := ui.New(1000, 700, ui.ThemeFor(detectOS(), prefersDark()))

	var buf []byte
	lastRev := -1
	lastW, lastH := 0, 0

	render := func() {
		dpr := js.Global().Get("devicePixelRatio").Float()
		if dpr <= 0 {
			dpr = 1
		}
		cw := clientSize(canvas, "clientWidth", 1000)
		ch := clientSize(canvas, "clientHeight", 700)
		w := int(float64(cw) * dpr)
		h := int(float64(ch) * dpr)
		sizeChanged := w != lastW || h != lastH
		if sizeChanged {
			canvas.Set("width", w)
			canvas.Set("height", h)
			scene.SetScale(dpr)
			scene.Resize(w, h)
			lastW, lastH = w, h
		}
		if !sizeChanged && scene.Rev() == lastRev {
			return // damage-gated: nothing changed, skip the blit
		}
		if len(buf) != scene.W*scene.H*4 {
			buf = make([]byte, scene.W*scene.H*4)
		}
		scene.Draw(buf)
		imageData := ctx.Call("createImageData", scene.W, scene.H)
		js.CopyBytesToJS(imageData.Get("data"), buf)
		ctx.Call("putImageData", imageData, 0, 0)
		lastRev = scene.Rev()
	}

	// requestAnimationFrame loop; render only repaints on damage.
	var raf js.Func
	raf = js.FuncOf(func(js.Value, []js.Value) any {
		render()
		js.Global().Call("requestAnimationFrame", raf)
		return nil
	})
	js.Global().Call("requestAnimationFrame", raf)

	// --- input ---
	canvas.Call("addEventListener", "mousedown", js.FuncOf(func(_ js.Value, args []js.Value) any {
		x, y := eventXY(canvas, args)
		switch hit := scene.HitTest(x, y); hit.Kind {
		case ui.HitItem:
			url := hit.Item.Permalink
			if url == "" {
				url = hit.Item.Link
			}
			if url != "" {
				js.Global().Call("open", url, "_blank")
			}
			scene.FocusSearch(false)
		case ui.HitSub:
			scene.SetActive(hit.Sub)
			scene.FocusSearch(false)
		case ui.HitSearch:
			scene.FocusSearch(true)
		default:
			scene.FocusSearch(false)
		}
		return nil
	}))

	canvas.Call("addEventListener", "wheel", js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 {
			args[0].Call("preventDefault")
			scene.Scroll(int(args[0].Get("deltaY").Float()))
		}
		return nil
	}), map[string]any{"passive": false})

	js.Global().Call("addEventListener", "keydown", js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 || !scene.SearchFocused() {
			return nil
		}
		key := args[0].Get("key").String()
		switch key {
		case "Backspace":
			scene.Backspace()
		case "Escape":
			scene.FocusSearch(false)
		case "Enter":
			scene.FocusSearch(false)
		default:
			if len([]rune(key)) == 1 { // a single printable character
				scene.TypeRune([]rune(key)[0])
			}
		}
		return nil
	}))

	loadFeed(scene)
	select {} // keep the wasm module alive
}

// loadFeed fetches /api/feed and populates the scene.
func loadFeed(scene *ui.Scene) {
	go func() {
		scene.Status = "Loading…"
		resp, err := http.Get("/api/feed")
		if err != nil {
			scene.Status = "Fetch error: " + err.Error()
			return
		}
		defer resp.Body.Close()
		var p feedPayload
		if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
			scene.Status = "Decode error: " + err.Error()
			return
		}
		if p.OS != "" {
			scene.SetTheme(ui.ThemeFor(p.OS, p.Dark))
		}
		scene.SetSubs(p.Subs)
		scene.SetItems(p.Items)
		scene.Status = ""
	}()
}

func clientSize(canvas js.Value, prop string, def int) int {
	if v := canvas.Get(prop).Int(); v > 0 {
		return v
	}
	return def
}

// eventXY converts a mouse event's client coordinates to canvas-local device
// pixels, accounting for CSS scaling of the canvas element.
func eventXY(canvas js.Value, args []js.Value) (int, int) {
	if len(args) == 0 {
		return 0, 0
	}
	e := args[0]
	rect := canvas.Call("getBoundingClientRect")
	cw := rect.Get("width").Float()
	ch := rect.Get("height").Float()
	if cw <= 0 || ch <= 0 {
		return 0, 0
	}
	sx := float64(canvas.Get("width").Int()) / cw
	sy := float64(canvas.Get("height").Int()) / ch
	x := (e.Get("clientX").Float() - rect.Get("left").Float()) * sx
	y := (e.Get("clientY").Float() - rect.Get("top").Float()) * sy
	return int(x), int(y)
}

func prefersDark() bool {
	mql := js.Global().Call("matchMedia", "(prefers-color-scheme: dark)")
	return mql.Truthy() && mql.Get("matches").Bool()
}

func detectOS() string {
	nav := js.Global().Get("navigator")
	if !nav.Truthy() {
		return ui.OSMac
	}
	p := nav.Get("platform").String()
	switch {
	case containsAny(p, "Mac", "iP"):
		return ui.OSMac
	case containsAny(p, "Win"):
		return ui.OSWindows
	default:
		return ui.OSLinux
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if len(sub) <= len(s) {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}
