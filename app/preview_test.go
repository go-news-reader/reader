package app

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"testing"

	"github.com/go-news-reader/reader/provider/usenet"
	"github.com/go-news-reader/reader/source"
)

// pngBytes encodes a solid w×h PNG for the image-decode paths.
func pngBytes(w, h int) []byte {
	im := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(im.Pix); i += 4 {
		im.Pix[i], im.Pix[i+1], im.Pix[i+2], im.Pix[i+3] = 0x20, 0x80, 0xC0, 0xFF
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, im)
	return buf.Bytes()
}

func TestLooksLikeImageName(t *testing.T) {
	for _, s := range []string{"a.JPG", "b.png", "[1/1] c.gif yEnc", "d.webp"} {
		if !looksLikeImageName(s) {
			t.Fatalf("%q should look like an image", s)
		}
	}
	if looksLikeImageName("release.tar.zst") {
		t.Fatal("archive should not look like an image")
	}
}

func TestSingleArticleParts(t *testing.T) {
	p := singleArticleParts(source.Item{Permalink: "news:<abc@x>"})
	if len(p) != 1 || p[0].MessageID != "abc@x" {
		t.Fatalf("parts = %+v", p)
	}
	if singleArticleParts(source.Item{}) != nil {
		t.Fatal("no permalink → no parts")
	}
}

func TestWantsPreviewImage(t *testing.T) {
	a := New(Config{Registry: newReg()})
	img := source.Item{ID: "1", Source: source.Usenet, Media: []source.Media{{Kind: source.MediaImage}}}
	if !a.wantsPreviewImage(img) {
		t.Fatal("usenet image item should want a fetch")
	}
	// Named image without an explicit media entry.
	if !a.wantsPreviewImage(source.Item{ID: "2", Source: source.Usenet, Title: "pic.jpg"}) {
		t.Fatal("named-image usenet item should want a fetch")
	}
	// Non-usenet, empty id, already-cached → no fetch.
	if a.wantsPreviewImage(source.Item{ID: "3", Source: source.Reddit, Media: img.Media}) {
		t.Fatal("non-usenet must not fetch")
	}
	if a.wantsPreviewImage(source.Item{Source: source.Usenet}) {
		t.Fatal("empty id must not fetch")
	}
	a.scene.SetThumb("1", image.NewRGBA(image.Rect(0, 0, 2, 2)))
	if a.wantsPreviewImage(img) {
		t.Fatal("already-cached id must not refetch")
	}
	// Usenet, no media, non-image title → no fetch.
	if a.wantsPreviewImage(source.Item{ID: "4", Source: source.Usenet, Title: "just text"}) {
		t.Fatal("text usenet item must not fetch")
	}
}

func TestFirstImageAndToRGBA(t *testing.T) {
	files := map[string][]byte{
		"z-notes.txt": []byte("not an image"),
		"a-pic.png":   pngBytes(8, 6),
	}
	img := firstImage(files)
	if img == nil || img.Bounds().Dx() == 0 {
		t.Fatal("firstImage should decode the PNG")
	}
	if firstImage(map[string][]byte{"x": []byte("nope")}) != nil {
		t.Fatal("no decodable file → nil")
	}
	// Thumbnail succeeds but the re-decode fails (fault-injected seam) → skipped.
	orig := decodeImage
	decodeImage = func(io.Reader) (image.Image, string, error) { return nil, "", errors.New("decode boom") }
	if firstImage(map[string][]byte{"a.png": pngBytes(4, 4)}) != nil {
		t.Fatal("decode failure should skip the file")
	}
	decodeImage = orig
	// toRGBA passes an *image.RGBA through and converts others.
	r := image.NewRGBA(image.Rect(0, 0, 2, 2))
	if toRGBA(r) != r {
		t.Fatal("RGBA should pass through")
	}
	g := image.NewGray(image.Rect(0, 0, 3, 2))
	g.Set(0, 0, color.Gray{Y: 200})
	if got := toRGBA(g); got.Bounds().Dx() != 3 || got.Bounds().Dy() != 2 {
		t.Fatalf("converted dims = %v", got.Bounds())
	}
}

func TestReconstructPreviewImage(t *testing.T) {
	ctx := context.Background()
	parts := []usenet.ReconstructPart{{MessageID: "m1"}}

	// Success: provider returns image bytes.
	a := New(Config{Registry: newReg(&fakeUsenet{files: map[string][]byte{"p.png": pngBytes(4, 4)}})})
	if a.reconstructPreviewImage(ctx, parts) == nil {
		t.Fatal("expected an image")
	}
	// No parts → nil (no fetch).
	if a.reconstructPreviewImage(ctx, nil) != nil {
		t.Fatal("no parts → nil")
	}
	// Provider error → nil.
	aErr := New(Config{Registry: newReg(&fakeUsenet{err: errors.New("boom")})})
	if aErr.reconstructPreviewImage(ctx, parts) != nil {
		t.Fatal("provider error → nil")
	}
	// No usenet provider → nil.
	if New(Config{Registry: newReg()}).reconstructPreviewImage(ctx, parts) != nil {
		t.Fatal("no provider → nil")
	}
	// Provider present but not a reconstructor → nil.
	aPlain := New(Config{Registry: newReg(fakeProv{kind: source.Usenet})})
	if aPlain.reconstructPreviewImage(ctx, parts) != nil {
		t.Fatal("non-reconstructor → nil")
	}
}

func TestSelectPreviewFetchesAndFinishes(t *testing.T) {
	a := New(Config{Registry: newReg(&fakeUsenet{files: map[string][]byte{"pic.png": pngBytes(10, 8)}})})
	// Synchronous fetch hook: reconstruct inline so the thumb lands deterministically.
	a.SetPreviewFetchHook(func(id string, parts []usenet.ReconstructPart) {
		a.loadPreviewImage(context.Background(), id, parts)
	})
	it := source.Item{ID: "img1", Source: source.Usenet, Permalink: "news:m1", Media: []source.Media{{Kind: source.MediaImage}}}
	a.SelectPreview(it)
	if sel, ok := a.scene.PreviewItem(); !ok || sel.ID != "img1" {
		t.Fatalf("preview item = %+v ok=%v", sel, ok)
	}
	if !a.scene.HasThumb("img1") {
		t.Fatal("preview image not stored after fetch")
	}

	// A non-image item selects but launches no fetch.
	fetched := false
	a.SetPreviewFetchHook(func(string, []usenet.ReconstructPart) { fetched = true })
	a.SelectPreview(source.Item{ID: "t", Source: source.HackerNews, Title: "text"})
	if fetched {
		t.Fatal("non-usenet item must not fetch")
	}
}

func TestPreviewGroup(t *testing.T) {
	a := New(Config{Registry: newReg(&fakeUsenet{files: map[string][]byte{"pic.png": pngBytes(6, 6)}})})
	a.scene.SetSubs(nil)
	a.scene.SetItems(usenetPost()) // groupable base "release"
	var gotParts []usenet.ReconstructPart
	a.SetPreviewFetchHook(func(id string, parts []usenet.ReconstructPart) {
		gotParts = parts
		a.loadPreviewImage(context.Background(), id, parts)
	})
	a.PreviewGroup("release")
	if sel, ok := a.scene.PreviewItem(); !ok || sel.ID != "release" {
		t.Fatalf("group preview item = %+v ok=%v", sel, ok)
	}
	if len(gotParts) != 2 {
		t.Fatalf("group parts = %d, want 2", len(gotParts))
	}
	if !a.scene.HasThumb("release") {
		t.Fatal("group image not stored")
	}
	// Unknown base is a no-op.
	a.SetPreviewFetchHook(func(string, []usenet.ReconstructPart) { t.Fatal("must not fetch for unknown base") })
	a.PreviewGroup("does-not-exist")
}

func TestDefaultPreviewFetchHook(t *testing.T) {
	// The default hook is installed (async); just assert it is non-nil and does not
	// panic when a Usenet image item is selected (the goroutine hits no provider).
	a := New(Config{Registry: newReg()})
	a.SelectPreview(source.Item{ID: "x", Source: source.Usenet, Permalink: "news:m", Media: []source.Media{{Kind: source.MediaImage}}})
	if _, ok := a.scene.PreviewItem(); !ok {
		t.Fatal("item should be selected even if the async fetch finds no provider")
	}
}
