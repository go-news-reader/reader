package app

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/gif"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// gifFrameFloor is the minimum per-frame delay used for playback. Many GIFs
// encode a 0 (or tiny) delay meaning "as fast as the client can go"; browsers
// clamp those to a few hundredths of a second so playback is smooth rather than
// a CPU-melting blur. 20ms (~50fps) matches that convention.
const gifFrameFloor = 20 * time.Millisecond

// maxGIFBytes bounds how much of a remote GIF is read before giving up, so a
// mislabelled or hostile resource cannot exhaust memory.
const maxGIFBytes = 32 << 20

// previewGIF is an animated GIF the app is driving into the embedded browser
// itself. frames are full-canvas composited RGBA frames (origin 0,0); delays[i]
// is how long frame i shows; w,h are the frame dimensions. idx is the frame on
// screen and lastAdvance is when it was shown (per App.now). All fields are read
// and written under App.gifMu.
type previewGIF struct {
	target      string
	frames      []*image.RGBA
	delays      []time.Duration
	w, h        int
	idx         int
	lastAdvance time.Time
}

// looksLikeGIFURL reports whether target's path names a .gif resource. It is the
// cheap gate that keeps the GIF interception from downloading (and re-parsing)
// every ordinary HTML page: only a URL that actually points at a .gif is
// speculatively fetched. Query strings and case are ignored; ".gifv" (an imgur
// video container, not a GIF) is deliberately excluded.
func looksLikeGIFURL(target string) bool {
	u, err := url.Parse(target)
	if err != nil {
		return false
	}
	return strings.HasSuffix(strings.ToLower(u.Path), ".gif")
}

// playAnimatedGIF attempts to intercept target as an animated GIF: it fetches
// the bytes (through the media-client seam), decodes every frame, and — when the
// GIF has more than one frame — composites them, stores the result as the active
// preview GIF, delivers frame 0 to the browser and marks the scene GIF-playing,
// then reports true so the caller skips the engine render (and the render cache).
// It returns false for a non-GIF URL, a fetch/decode failure, or a single-frame
// GIF, so the caller falls back to the normal static render path.
func (a *App) playAnimatedGIF(ctx context.Context, target string, width int, created int64) bool {
	if !looksLikeGIFURL(target) {
		return false
	}
	data, err := a.gifFetch(ctx, target, created)
	if err != nil {
		return false
	}
	g, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil {
		return false
	}
	frames, delays := compositeGIF(g)
	if len(frames) <= 1 {
		return false // single-frame (or empty) GIF: let the static renderer handle it
	}
	pg := &previewGIF{
		target: target,
		frames: frames,
		delays: delays,
		w:      frames[0].Bounds().Dx(),
		h:      frames[0].Bounds().Dy(),
		idx:    0,
	}
	a.gifMu.Lock()
	pg.lastAdvance = a.now()
	a.activeGIF = pg
	a.gifMu.Unlock()
	// Deliver frame 0 and start the present loop ticking, on the render thread.
	f0 := frames[0]
	a.post(func() {
		a.scene.SetGIFPlaying(true)
		b := a.scene.Browser()
		b.DeliverStage(target, f0.Pix, pg.w, pg.h, width, nil, b.ActiveTitle(), true)
	})
	return true
}

// tickGIF advances the active animated-GIF preview to its due frame. Called once
// per Frame on the render thread: if a GIF is playing and its current frame has
// been shown for at least its delay, it steps to the next frame (wrapping at the
// end for a continuous loop) and delivers it into the browser. A single delivery
// per due tick keeps the cost proportional to the GIF's own frame rate, not the
// display refresh rate. A no-op when no GIF is active.
func (a *App) tickGIF() {
	a.gifMu.Lock()
	pg := a.activeGIF
	if pg == nil {
		a.gifMu.Unlock()
		return
	}
	now := a.now()
	if now.Sub(pg.lastAdvance) < pg.delays[pg.idx] {
		a.gifMu.Unlock()
		return
	}
	pg.idx = (pg.idx + 1) % len(pg.frames)
	pg.lastAdvance = now
	frame := pg.frames[pg.idx]
	target, w, h := pg.target, pg.w, pg.h
	a.gifMu.Unlock()

	b := a.scene.Browser()
	b.DeliverStage(target, frame.Pix, w, h, w, nil, b.ActiveTitle(), true)
}

// clearActiveGIF stops any animated-GIF playback: it drops the active GIF and
// clears the scene's GIF-playing flag so the present loop can go idle. Called at
// the start of every preview navigation (a new/non-GIF target supersedes it) and
// safe to call when nothing is playing.
func (a *App) clearActiveGIF() {
	a.gifMu.Lock()
	had := a.activeGIF != nil
	a.activeGIF = nil
	a.gifMu.Unlock()
	if had {
		a.post(func() { a.scene.SetGIFPlaying(false) })
	}
}

// fetchGIFBytes GETs url through the shared browser-fingerprint media client
// (so the Network log records it and fingerprinting hosts serve it) and returns
// the raw body, bounded to maxGIFBytes. created is the Unix-second creation time
// of the post the GIF belongs to (0 when unknown): a freshly downloaded GIF is
// cached with the file's mtime stamped to it, so the on-disk cache reflects post
// chronology. It is the default gifFetch seam.
func (a *App) fetchGIFBytes(ctx context.Context, url string, created int64) ([]byte, error) {
	// Serve a previously-fetched GIF straight from the on-disk media cache: no
	// network, so re-opening a GIF post is instant (and survives restarts). Only
	// the download path pays the cost, once.
	if data, ok := a.mediaCache.Get(url); ok {
		return data, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := a.mediaClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errBadGIFStatus
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxGIFBytes))
	if err != nil {
		return nil, err
	}
	a.cacheMedia(url, data, created)
	return data, nil
}

// errBadGIFStatus is returned by fetchGIFBytes for a non-200 response, so a
// dead or forbidden GIF URL falls through to the static render path.
var errBadGIFStatus = errorString("gif fetch: non-200 status")

// errorString is a tiny sentinel error type (avoids pulling errors.New into a
// package var for one message).
type errorString string

func (e errorString) Error() string { return string(e) }

// compositeGIF flattens a decoded multi-frame GIF into full-canvas RGBA frames,
// honouring each frame's sub-rect placement, palette transparency and disposal
// method (see image/gif). The canvas is the logical screen; each frame is drawn
// onto it and a snapshot emitted. Disposal then prepares the canvas for the next
// frame: DisposalBackground clears the just-drawn rect to transparent, and
// DisposalPrevious restores the canvas to its pre-frame state; DisposalNone (and
// an unspecified 0) keep the composited pixels as the base. Per-frame delays
// (hundredths of a second) become time.Durations clamped up to gifFrameFloor. It
// returns nil for a nil or single-frame GIF (nothing to animate — the caller
// renders it statically).
func compositeGIF(g *gif.GIF) (frames []*image.RGBA, delays []time.Duration) {
	if g == nil || len(g.Image) <= 1 {
		return nil, nil
	}
	w, h := g.Config.Width, g.Config.Height
	if w == 0 || h == 0 {
		// No explicit logical screen: fall back to the union of the frame extents.
		b := image.Rectangle{}
		for _, fr := range g.Image {
			b = b.Union(fr.Bounds())
		}
		w, h = b.Max.X, b.Max.Y
	}
	canvas := image.NewRGBA(image.Rect(0, 0, w, h))
	for i, fr := range g.Image {
		// Snapshot the canvas first when this frame is to be reverted afterwards.
		var prev *image.RGBA
		if disposalAt(g, i) == gif.DisposalPrevious {
			prev = cloneRGBA(canvas)
		}
		drawPaletted(canvas, fr)
		frames = append(frames, cloneRGBA(canvas))
		delays = append(delays, frameDelay(g, i))
		switch disposalAt(g, i) {
		case gif.DisposalBackground:
			clearRect(canvas, fr.Bounds())
		case gif.DisposalPrevious:
			copy(canvas.Pix, prev.Pix)
		}
	}
	return frames, delays
}

// disposalAt returns frame i's disposal method, or 0 (unspecified → keep) when
// the GIF omits a disposal byte for it.
func disposalAt(g *gif.GIF, i int) byte {
	if i < len(g.Disposal) {
		return g.Disposal[i]
	}
	return 0
}

// frameDelay returns frame i's on-screen duration: its GIF delay (hundredths of
// a second) as a time.Duration, floored at gifFrameFloor. A missing delay is
// treated as 0 and thus floored.
func frameDelay(g *gif.GIF, i int) time.Duration {
	d := time.Duration(0)
	if i < len(g.Delay) {
		d = time.Duration(g.Delay[i]) * 10 * time.Millisecond
	}
	if d < gifFrameFloor {
		d = gifFrameFloor
	}
	return d
}

// drawPaletted composites a paletted GIF frame onto the RGBA canvas at the
// frame's own bounds, leaving transparent pixels untouched so the canvas beneath
// shows through. image/gif's decoder sets a frame's transparent palette entry to
// a zero-alpha colour, so an alpha of 0 marks "transparent"; every other pixel
// is written fully opaque (GIF has no partial alpha).
func drawPaletted(dst *image.RGBA, src *image.Paletted) {
	b := src.Bounds().Intersect(dst.Bounds())
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, gg, bb, aa := src.At(x, y).RGBA()
			if aa == 0 {
				continue // transparent: keep the canvas pixel beneath
			}
			dst.SetRGBA(x, y, color.RGBA{R: uint8(r >> 8), G: uint8(gg >> 8), B: uint8(bb >> 8), A: 0xFF})
		}
	}
}

// clearRect resets r (clipped to the canvas) to transparent — the
// DisposalBackground effect, so the next frame composites over cleared pixels.
func clearRect(dst *image.RGBA, r image.Rectangle) {
	r = r.Intersect(dst.Bounds())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			dst.SetRGBA(x, y, color.RGBA{})
		}
	}
}

// cloneRGBA returns a deep copy of src (same bounds, independent pixels).
func cloneRGBA(src *image.RGBA) *image.RGBA {
	dst := image.NewRGBA(src.Bounds())
	copy(dst.Pix, src.Pix)
	return dst
}
