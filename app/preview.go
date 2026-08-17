package app

import (
	"bytes"
	"context"
	"image"
	"image/draw"
	"sort"
	"strings"

	// Decoders for the reconstructed binary bytes (and the Thumbnail JPEG).
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"github.com/go-widgets/toolkit"

	"github.com/go-news-reader/reader/provider/usenet"
	"github.com/go-news-reader/reader/source"
)

// previewMaxDim caps the decoded preview image's longest side (memory + blit
// cost); the pane scales it down to fit anyway.
const previewMaxDim = 1200

// SelectPreview shows it in the right preview pane. For a Usenet item that
// carries an image and has not been fetched yet, it kicks off an async
// reconstruct → decode → SetThumb so the picture appears in the pane.
func (a *App) SelectPreview(it source.Item) { a.selectPreview(it, false) }

// selectPreview shows it in the pane. debounceWeb defers a web-page render until
// the selection settles (rapid keyboard navigation), rather than firing one per
// item; a direct click passes false and fetches immediately.
func (a *App) selectPreview(it source.Item, debounceWeb bool) {
	a.scene.SelectPreview(it)
	// Lay out the pane now so the embedded browser has its real content width
	// before a web open reads it. Without this the first open of a page fires at
	// the stale (often zero) bounds left by the prior frame, so the page is
	// rendered and cached under width 0; revisiting it later — now measured at the
	// real width — would miss that entry and re-fetch. See TestSelectFirstWebOpenRealWidth.
	a.scene.LayoutPreview()
	a.webArmed = false // a new selection cancels any pending debounced open
	a.maybeFetchComments(it)
	if a.wantsPreviewImage(it) {
		a.scene.SetPreviewLoading(true)
		a.previewFetch(it.ID, singleArticleParts(it))
		return
	}
	// A non-Usenet item that links out: open its target page in the embedded
	// browser (which marks the tab loading and fires OnNavigate → the async
	// render). Re-selecting the page already shown is a no-op (no re-render).
	if url := a.scene.WebPreviewURL(it); url != "" && a.scene.Browser().CurrentURL() != url {
		if debounceWeb {
			a.webArmItem, a.webArmURL = it, url
			a.webArmAt, a.webArmed = a.frameCount, true
		} else {
			a.openWebPreview(it, url)
		}
	}
}

// openWebPreview opens url in the embedded browser (firing the render) and warms
// the render cache for the selection's likely-next neighbours. Called once the
// selection has settled (a direct click, or a debounced open).
func (a *App) openWebPreview(it source.Item, url string) {
	a.scene.Browser().Open(url, it.Title)
	a.prefetchNeighbors(it)
}

// currentPostCreated returns the creation time (Unix seconds, UTC) of the post
// currently shown in the preview pane, or 0 when nothing is previewed or the
// item carries no known date. It is read on the UI thread (the OnNavigate seam)
// so the media a page turns out to hold can be cached under the post's date.
func (a *App) currentPostCreated() int64 {
	if it, ok := a.scene.PreviewItem(); ok {
		return it.Created
	}
	return 0
}

// tickWebDebounce advances the frame clock and opens an armed web page once the
// selection has held for webDebounceFrames ticks. Called once per Frame on the
// render thread (the only thread that arms/reads these fields).
func (a *App) tickWebDebounce() {
	a.frameCount++
	if a.webArmed && a.frameCount-a.webArmAt >= a.webDebounceFrames {
		a.webArmed = false
		a.openWebPreview(a.webArmItem, a.webArmURL)
	}
}

// SelectAdjacent moves the feed selection one card up (dir<0) or down (dir>0)
// through the virtual.CardList and previews the newly selected post — the
// keyboard equivalent of clicking it, so its image is fetched and it scrolls
// into view. The preview itself is driven by the feed select hook (installed in
// New), which the CardList fires on the cursor move; the web-page render is
// debounced there so holding an arrow key does not render every item passed over.
func (a *App) SelectAdjacent(dir int) {
	code := "ArrowDown"
	if dir < 0 {
		code = "ArrowUp"
	}
	a.scene.FeedEvent(toolkit.Event{Kind: toolkit.EventKeyDown, Code: code})
}

// previewGroupIfShown previews the Usenet multipart post whose release base is
// `base` — reconstructing its image into the pane — when that group is currently
// shown in the feed, and reports whether it did. A base that is not a shown group
// is left to the standalone-item preview path.
func (a *App) previewGroupIfShown(base string) bool {
	if _, ok := a.scene.GroupPreviewItem(base); !ok {
		return false
	}
	a.PreviewGroup(base)
	return true
}

// OpenSelected opens the currently previewed post in the full reading view (the
// keyboard equivalent of the pane's Open button). A no-op when nothing is
// selected.
func (a *App) OpenSelected() {
	if it, ok := a.scene.PreviewItem(); ok {
		a.vm.OpenDetail(it)
	}
}

// SetPreviewFetchHook overrides the async image fetch (tests use a synchronous
// variant for determinism).
func (a *App) SetPreviewFetchHook(f func(id string, parts []usenet.ReconstructPart)) {
	a.previewFetch = f
}

// PreviewGroup previews a Usenet multipart post (a group card, which is not a
// single Item): it synthesises a preview item and reconstructs the post's image
// into the pane.
func (a *App) PreviewGroup(base string) {
	it, ok := a.scene.GroupPreviewItem(base)
	if !ok {
		return
	}
	a.scene.SelectPreview(it)
	// GroupPreviewItem already confirmed the group is shown, so GroupParts returns
	// its member articles.
	parts, _ := a.scene.GroupParts(base)
	a.scene.SetPreviewLoading(true)
	a.previewFetch(base, toReconstructParts(parts))
}

// wantsPreviewImage reports whether a preview image should be fetched for it: a
// Usenet item that declares an image and has not already been decoded. Runs on
// the UI thread (SelectPreview), same thread that drains SetThumb, so the
// HasThumb read is race-free.
func (a *App) wantsPreviewImage(it source.Item) bool {
	if it.Source != source.Usenet || it.ID == "" || a.scene.HasThumb(it.ID) {
		return false
	}
	for _, med := range it.Media {
		if med.Kind == source.MediaImage {
			return true
		}
	}
	// A binary post whose subject named an image file is worth trying even without
	// an explicit media entry (the standalone-article path).
	return looksLikeImageName(it.Title)
}

// singleArticleParts builds the one-part reconstruct request for a standalone
// Usenet article (a non-grouped binary post): its bare message-id, letting the
// yEnc header supply the filename.
func singleArticleParts(it source.Item) []usenet.ReconstructPart {
	id := strings.Trim(strings.TrimPrefix(it.Permalink, "news:"), "<>")
	if id == "" {
		return nil
	}
	return []usenet.ReconstructPart{{MessageID: id}}
}

// loadPreviewImage reconstructs the parts, finds the first file that decodes as
// an image, and hands it to the scene keyed by id. Loading is cleared whether or
// not an image was found. All scene writes go through post (render thread).
func (a *App) loadPreviewImage(ctx context.Context, id string, parts []usenet.ReconstructPart) {
	img := a.reconstructPreviewImage(ctx, parts)
	a.post(func() { a.scene.FinishPreviewImage(id, img) })
}

// reconstructPreviewImage downloads+reassembles the parts and returns the first
// file that decodes as an image (bounded to previewMaxDim), or nil.
func (a *App) reconstructPreviewImage(ctx context.Context, parts []usenet.ReconstructPart) *image.RGBA {
	if len(parts) == 0 {
		return nil
	}
	prov, ok := a.reg.Get(source.Usenet)
	if !ok {
		return nil
	}
	rc, ok := prov.(reconstructor)
	if !ok {
		return nil
	}
	files, _, err := rc.Reconstruct(ctx, usenet.ReconstructRequest{Parts: parts})
	if err != nil {
		return nil
	}
	return firstImage(files)
}

// decodeImage is a seam over image.Decode so tests can drive firstImage's
// otherwise-untriggerable "thumbnail encoded but re-decode failed" branch.
var decodeImage = image.Decode

// firstImage returns the first file (by name, for determinism) whose bytes
// decode as an image, as a bounded *image.RGBA. nil when none decode.
func firstImage(files map[string][]byte) *image.RGBA {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		// usenet.Thumbnail decodes + bounds the image (reusing go-images' resize)
		// and re-encodes JPEG; a non-image simply errors and is skipped.
		jpg, err := usenet.Thumbnail(files[name], previewMaxDim)
		if err != nil {
			continue
		}
		img, _, err := decodeImage(bytes.NewReader(jpg))
		if err != nil {
			continue
		}
		return toRGBA(img)
	}
	return nil
}

// toRGBA returns img as an *image.RGBA (itself when already one, else a copy).
func toRGBA(img image.Image) *image.RGBA {
	if r, ok := img.(*image.RGBA); ok {
		return r
	}
	b := img.Bounds()
	r := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(r, r.Bounds(), img, b.Min, draw.Src)
	return r
}

// looksLikeImageName reports whether a subject/filename names a common image.
func looksLikeImageName(s string) bool {
	s = strings.ToLower(s)
	for _, ext := range []string{".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp"} {
		if strings.Contains(s, ext) {
			return true
		}
	}
	return false
}
