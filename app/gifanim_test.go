package app

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// gifPalette indexes: 0 transparent, 1 red, 2 green, 3 blue.
var gifPalette = color.Palette{
	color.RGBA{0, 0, 0, 0},
	color.RGBA{255, 0, 0, 255},
	color.RGBA{0, 255, 0, 255},
	color.RGBA{0, 0, 255, 255},
}

// palFrame builds a paletted frame of bounds r, every pixel set to index idx.
func palFrame(r image.Rectangle, idx uint8) *image.Paletted {
	p := image.NewPaletted(r, gifPalette)
	for i := range p.Pix {
		p.Pix[i] = idx
	}
	return p
}

// encodeAnimatedGIF EncodeAll-s the frames/delays into GIF bytes with a WxH
// logical screen (delays in hundredths of a second).
func encodeAnimatedGIF(t *testing.T, w, h int, frames []*image.Paletted, delays []int) []byte {
	t.Helper()
	var buf bytes.Buffer
	g := &gif.GIF{Image: frames, Delay: delays, Config: image.Config{ColorModel: gifPalette, Width: w, Height: h}}
	if err := gif.EncodeAll(&buf, g); err != nil {
		t.Fatalf("EncodeAll: %v", err)
	}
	return buf.Bytes()
}

// threeFrameGIF is a 2x2 animated GIF of 3 full-canvas frames (red, green, blue)
// each shown 50ms.
func threeFrameGIF(t *testing.T) []byte {
	r := image.Rect(0, 0, 2, 2)
	return encodeAnimatedGIF(t, 2, 2,
		[]*image.Paletted{palFrame(r, 1), palFrame(r, 2), palFrame(r, 3)},
		[]int{5, 5, 5})
}

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func TestLooksLikeGIFURL(t *testing.T) {
	cases := []struct {
		url  string
		want bool
	}{
		{"https://ex/a.gif", true},
		{"https://ex/A.GIF?w=1&h=2", true}, // case-insensitive, query ignored
		{"https://ex/a.gifv", false},       // imgur video container, not a GIF
		{"https://ex/page.html", false},
		{"https://ex/", false},
		{"http://foo\nbar/a.gif", false}, // url.Parse rejects the control char
	}
	for _, c := range cases {
		if got := looksLikeGIFURL(c.url); got != c.want {
			t.Errorf("looksLikeGIFURL(%q) = %v, want %v", c.url, got, c.want)
		}
	}
}

// TestCompositeGIFDisposalAndTransparency drives every disposal method, a
// partial-bounds frame and palette transparency through compositeGIF, asserting
// exact composited pixels and the clamped per-frame delays.
func TestCompositeGIFDisposalAndTransparency(t *testing.T) {
	full := image.Rect(0, 0, 2, 2)
	topLeft := image.Rect(0, 0, 1, 1)
	botRight := image.Rect(1, 1, 2, 2)
	// f3 is a full-canvas frame that is red at (0,0) and transparent elsewhere.
	f3 := image.NewPaletted(full, gifPalette)
	f3.SetColorIndex(0, 0, 1) // red; the other three stay index 0 (transparent)

	g := &gif.GIF{
		Image: []*image.Paletted{
			palFrame(full, 1),     // f0: all red
			palFrame(topLeft, 2),  // f1: green at (0,0)
			palFrame(botRight, 3), // f2: blue at (1,1)
			f3,                    // f3: red at (0,0), transparent elsewhere
		},
		Delay:    []int{100, 0, 5, 3}, // 1s, floored, 50ms, 30ms
		Disposal: []byte{gif.DisposalNone, gif.DisposalBackground, gif.DisposalPrevious, gif.DisposalNone},
		Config:   image.Config{ColorModel: gifPalette, Width: 2, Height: 2},
	}
	frames, delays := compositeGIF(g)
	if len(frames) != 4 || len(delays) != 4 {
		t.Fatalf("frames=%d delays=%d, want 4/4", len(frames), len(delays))
	}

	red := color.RGBA{255, 0, 0, 255}
	green := color.RGBA{0, 255, 0, 255}
	blue := color.RGBA{0, 0, 255, 255}
	clear := color.RGBA{0, 0, 0, 0}
	at := func(fi, x, y int) color.RGBA { return frames[fi].RGBAAt(x, y) }

	// f0: all red.
	for _, p := range [][2]int{{0, 0}, {1, 0}, {0, 1}, {1, 1}} {
		if at(0, p[0], p[1]) != red {
			t.Fatalf("f0 (%d,%d) = %v, want red", p[0], p[1], at(0, p[0], p[1]))
		}
	}
	// f1: green at (0,0), red elsewhere (composited over f0).
	if at(1, 0, 0) != green || at(1, 1, 0) != red || at(1, 0, 1) != red || at(1, 1, 1) != red {
		t.Fatalf("f1 = %v %v %v %v", at(1, 0, 0), at(1, 1, 0), at(1, 0, 1), at(1, 1, 1))
	}
	// f2: (0,0) cleared to transparent by f1's Background disposal; (1,1) blue.
	if at(2, 0, 0) != clear || at(2, 1, 1) != blue || at(2, 1, 0) != red || at(2, 0, 1) != red {
		t.Fatalf("f2 = %v %v %v %v", at(2, 0, 0), at(2, 1, 0), at(2, 0, 1), at(2, 1, 1))
	}
	// f3: f2's Previous disposal restored (1,1) to red before f3 drew red at (0,0)
	// (transparent pixels keep the canvas beneath) → all red.
	for _, p := range [][2]int{{0, 0}, {1, 0}, {0, 1}, {1, 1}} {
		if at(3, p[0], p[1]) != red {
			t.Fatalf("f3 (%d,%d) = %v, want red", p[0], p[1], at(3, p[0], p[1]))
		}
	}
	// Delays: 100→1s, 0→floored, 5→50ms, 3→30ms.
	want := []time.Duration{time.Second, gifFrameFloor, 50 * time.Millisecond, 30 * time.Millisecond}
	for i, w := range want {
		if delays[i] != w {
			t.Fatalf("delays[%d] = %v, want %v", i, delays[i], w)
		}
	}
}

// TestCompositeGIFDefaults covers the fallbacks: a zero Config (union-of-frames
// logical screen), and missing Delay/Disposal slices (unspecified → keep, delay
// floored).
func TestCompositeGIFDefaults(t *testing.T) {
	r := image.Rect(0, 0, 2, 2)
	g := &gif.GIF{Image: []*image.Paletted{palFrame(r, 1), palFrame(r, 2)}} // no Config/Delay/Disposal
	frames, delays := compositeGIF(g)
	if len(frames) != 2 {
		t.Fatalf("frames = %d, want 2 (union fallback)", len(frames))
	}
	if b := frames[0].Bounds(); b.Dx() != 2 || b.Dy() != 2 {
		t.Fatalf("union bounds = %v, want 2x2", b)
	}
	red := color.RGBA{255, 0, 0, 255}
	green := color.RGBA{0, 255, 0, 255}
	if frames[0].RGBAAt(1, 1) != red || frames[1].RGBAAt(1, 1) != green {
		t.Fatalf("frames = %v %v", frames[0].RGBAAt(1, 1), frames[1].RGBAAt(1, 1))
	}
	for i, d := range delays {
		if d != gifFrameFloor {
			t.Fatalf("delays[%d] = %v, want floor (missing delay)", i, d)
		}
	}
	// nil and single-frame GIFs animate nothing.
	if f, _ := compositeGIF(nil); f != nil {
		t.Fatal("nil GIF should composite to nil")
	}
	if f, _ := compositeGIF(&gif.GIF{Image: []*image.Paletted{palFrame(r, 1)}}); f != nil {
		t.Fatal("single-frame GIF should composite to nil")
	}
}

// TestPlayAnimatedGIFDrivesFrames is the end-to-end interception: selecting a
// web item whose link is an animated .gif decodes+plays it (bypassing the page
// renderer), keeps the scene animating, advances frames on the render-thread
// clock (wrapping to loop), and clears cleanly when the preview navigates away.
func TestPlayAnimatedGIFDrivesFrames(t *testing.T) {
	a := New(Config{Registry: newReg(), Width: 900, Height: 600})
	a.SetWebRenderer(&fakeRenderer{img: image.NewRGBA(image.Rect(0, 0, 40, 40))})
	a.gifFetch = func(context.Context, string) ([]byte, error) { return threeFrameGIF(t), nil }
	clock := &fakeClock{t: time.Unix(1000, 0)}
	a.now = clock.now
	syncFetch(a)

	a.SelectPreview(webItem("g", "https://ex/anim.gif"))
	if !a.Scene().GIFPlaying() {
		t.Fatal("selecting an animated GIF should mark the scene GIF-playing")
	}
	a.gifMu.Lock()
	pg := a.activeGIF
	a.gifMu.Unlock()
	if pg == nil || len(pg.frames) != 3 || pg.idx != 0 {
		t.Fatalf("active GIF = %+v", pg)
	}
	if pg.w != 2 || pg.h != 2 {
		t.Fatalf("gif dims = %dx%d, want 2x2", pg.w, pg.h)
	}
	// The renderer was NOT invoked — the GIF path bypassed it.
	if a.Scene().Browser().Loading() {
		t.Fatal("a delivered GIF frame should clear the loading state")
	}

	idx := func() int { a.gifMu.Lock(); defer a.gifMu.Unlock(); return a.activeGIF.idx }
	// Not yet due: the frame holds.
	a.tickGIF()
	if idx() != 0 {
		t.Fatalf("frame advanced before its delay: idx=%d", idx())
	}
	// Each 50ms tick steps one frame; the third wraps back to 0 (the loop).
	for _, want := range []int{1, 2, 0} {
		clock.advance(50 * time.Millisecond)
		a.tickGIF()
		if idx() != want {
			t.Fatalf("after advance: idx=%d, want %d", idx(), want)
		}
	}

	// Navigating to a non-GIF page clears the active GIF and the playing flag.
	a.SelectPreview(webItem("h", "https://ex/plain"))
	if a.Scene().GIFPlaying() {
		t.Fatal("navigating away should clear the GIF-playing flag")
	}
	a.gifMu.Lock()
	still := a.activeGIF
	a.gifMu.Unlock()
	if still != nil {
		t.Fatal("navigating away should drop the active GIF")
	}
	a.tickGIF() // no active GIF → safe no-op
}

// TestPlayAnimatedGIFFalsePaths covers the branches that fall through to the
// static render path: a non-GIF URL, a fetch error, undecodable bytes, and a
// single-frame GIF.
func TestPlayAnimatedGIFFalsePaths(t *testing.T) {
	a := New(Config{Registry: newReg()})
	ctx := context.Background()

	if a.playAnimatedGIF(ctx, "https://ex/page.html", 800) {
		t.Fatal("a non-GIF URL must not be intercepted")
	}
	a.gifFetch = func(context.Context, string) ([]byte, error) { return nil, errors.New("boom") }
	if a.playAnimatedGIF(ctx, "https://ex/x.gif", 800) {
		t.Fatal("a fetch error must fall through")
	}
	a.gifFetch = func(context.Context, string) ([]byte, error) { return []byte("not a gif"), nil }
	if a.playAnimatedGIF(ctx, "https://ex/x.gif", 800) {
		t.Fatal("undecodable bytes must fall through")
	}
	single := encodeAnimatedGIF(t, 2, 2, []*image.Paletted{palFrame(image.Rect(0, 0, 2, 2), 1)}, []int{5})
	a.gifFetch = func(context.Context, string) ([]byte, error) { return single, nil }
	if a.playAnimatedGIF(ctx, "https://ex/x.gif", 800) {
		t.Fatal("a single-frame GIF must fall through to the static render")
	}
}

// TestFetchGIFBytes covers the default gifFetch seam: a successful GET, a
// non-200 (errBadGIFStatus), a transport error, and a request-build error.
func TestFetchGIFBytes(t *testing.T) {
	a := New(Config{Registry: newReg()})
	ctx := context.Background()
	body := threeFrameGIF(t)

	var hits int32
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Write(body)
	}))
	defer ok.Close()
	a.mediaClient = ok.Client()
	// Go through the default gifFetch closure (the New-wired wrapper over
	// fetchGIFBytes) so that seam is exercised too.
	got, err := a.gifFetch(ctx, ok.URL)
	if err != nil || !bytes.Equal(got, body) {
		t.Fatalf("fetch ok: err=%v len=%d want=%d", err, len(got), len(body))
	}
	// A second fetch of the same URL is served from the on-disk media cache — no
	// network — so re-opening a GIF post is instant.
	got2, err := a.fetchGIFBytes(ctx, ok.URL)
	if err != nil || !bytes.Equal(got2, body) {
		t.Fatalf("cache hit: err=%v len=%d want=%d", err, len(got2), len(body))
	}
	if n := atomic.LoadInt32(&hits); n != 1 {
		t.Fatalf("second fetch hit the network (%d requests); should be served from cache", n)
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer bad.Close()
	a.mediaClient = bad.Client()
	if _, err := a.fetchGIFBytes(ctx, bad.URL); err == nil || err.Error() != "gif fetch: non-200 status" {
		t.Fatalf("non-200: err=%v, want errBadGIFStatus", err)
	}

	// A truncated body (declared longer than sent, then the connection drops)
	// makes io.ReadAll error after the request succeeded.
	short := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("truncated"))
	}))
	defer short.Close()
	a.mediaClient = short.Client()
	if _, err := a.fetchGIFBytes(ctx, short.URL+"/short.gif"); err == nil {
		t.Fatal("a truncated body should error on read")
	}

	// Transport error: a server that is already closed.
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := dead.URL
	client := dead.Client()
	dead.Close()
	a.mediaClient = client
	if _, err := a.fetchGIFBytes(ctx, url); err == nil {
		t.Fatal("a dead server should error")
	}

	// Request-build error: a URL with a control character.
	if _, err := a.fetchGIFBytes(ctx, "http://foo\nbar/a.gif"); err == nil {
		t.Fatal("a malformed URL should error before the request")
	}
}
