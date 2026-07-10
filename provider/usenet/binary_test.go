package usenet

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"testing"
	"time"

	goimages "github.com/go-images/images"
	gonntp "github.com/go-newsgroups/nntp"
	"github.com/go-newsgroups/newznab"
	"github.com/go-newsgroups/par2"
	"github.com/go-newsgroups/yenc"

	"github.com/go-news-reader/reader/source"
)

// ---- search ----

type fakeSearcher struct {
	res *newznab.SearchResult
	err error
	got newznab.SearchOptions
}

func (f *fakeSearcher) Search(_ context.Context, o newznab.SearchOptions) (*newznab.SearchResult, error) {
	f.got = o
	return f.res, f.err
}

func TestNewWithSearch(t *testing.T) {
	if NewWithSearch("news:119", false, "https://indexer", "key").search == nil {
		t.Fatal("search client not set")
	}
}

func TestSearchFeed(t *testing.T) {
	fs := &fakeSearcher{res: &newznab.SearchResult{Items: []newznab.Item{{
		Title: "Cool Release", GUID: "g1", NZBURL: "https://x/1.nzb", Category: "Movies",
		Poster: "poster@x", Group: "alt.binaries.movies", Grabs: 12,
		PublishDate: time.Unix(1700000000, 0),
	}}}}
	p := NewWithDial(nil)
	p.search = fs

	res, err := p.Feed(context.Background(), source.Query{Channel: "search:cool", Limit: 25})
	if err != nil {
		t.Fatal(err)
	}
	if fs.got.Query != "cool" || fs.got.Limit != 25 {
		t.Fatalf("search opts %+v", fs.got)
	}
	it := res.Items[0]
	if it.ID != "g1" || it.Source != source.Usenet || it.Channel != "alt.binaries.movies" ||
		it.Title != "Cool Release" || it.Author != "poster@x" || it.Link != "https://x/1.nzb" ||
		it.Permalink != "https://x/1.nzb" || it.Score != 12 || it.Created != 1700000000 ||
		len(it.Tags) != 1 || it.Tags[0] != "Movies" {
		t.Fatalf("mapped item %+v", it)
	}
}

func TestSearchFeedNoCategoryTag(t *testing.T) {
	fs := &fakeSearcher{res: &newznab.SearchResult{Items: []newznab.Item{{GUID: "g", Category: ""}}}}
	p := NewWithDial(nil)
	p.search = fs
	res, _ := p.Feed(context.Background(), source.Query{Channel: "search:x"})
	if len(res.Items[0].Tags) != 0 {
		t.Fatalf("empty category should yield no tags: %v", res.Items[0].Tags)
	}
}

func TestSearchNoIndexer(t *testing.T) {
	if _, err := NewWithDial(nil).Feed(context.Background(), source.Query{Channel: "search:x"}); !errors.Is(err, ErrNoSearch) {
		t.Fatalf("want ErrNoSearch, got %v", err)
	}
}

func TestSearchError(t *testing.T) {
	p := NewWithDial(nil)
	p.search = &fakeSearcher{err: errors.New("boom")}
	if _, err := p.Feed(context.Background(), source.Query{Channel: "search:x"}); err == nil {
		t.Fatal("want search error")
	}
}

// ---- FetchNZB ----

type binaryFakeConn struct {
	articles map[string]*gonntp.Article
	artErr   error
	closed   bool
}

func (c *binaryFakeConn) Group(string) (*gonntp.Group, error)     { return nil, nil }
func (c *binaryFakeConn) Over(int, int) ([]gonntp.Overview, error) { return nil, nil }
func (c *binaryFakeConn) Article(id string) (*gonntp.Article, error) {
	if c.artErr != nil {
		return nil, c.artErr
	}
	return c.articles[id], nil
}
func (c *binaryFakeConn) Close() error { c.closed = true; return nil }

const nzbDoc = `<?xml version="1.0"?>
<nzb xmlns="http://www.newzbin.com/DTD/2003/nzb">
 <file poster="me" date="1700000000" subject="myfile">
  <groups><group>alt.binaries.test</group></groups>
  <segments><segment bytes="100" number="1">abc@host</segment></segments>
 </file>
</nzb>`

func TestFetchNZB(t *testing.T) {
	payload := []byte("hello binary world")
	body := yenc.Encode("myfile.bin", payload, 128)
	fc := &binaryFakeConn{articles: map[string]*gonntp.Article{"<abc@host>": {Body: string(body)}}}
	p := NewWithDial(func(context.Context) (conn, error) { return fc, nil })

	files, err := p.FetchNZB(context.Background(), []byte(nzbDoc))
	if err != nil {
		t.Fatal(err)
	}
	if got := files["myfile.bin"]; !bytes.Equal(got, payload) {
		t.Fatalf("payload = %q", got)
	}
	if !fc.closed {
		t.Fatal("conn not closed")
	}
}

func TestFetchNZBParseError(t *testing.T) {
	p := NewWithDial(func(context.Context) (conn, error) { return &binaryFakeConn{}, nil })
	if _, err := p.FetchNZB(context.Background(), []byte("not xml <")); err == nil {
		t.Fatal("want parse error")
	}
}

func TestFetchNZBDialError(t *testing.T) {
	p := NewWithDial(func(context.Context) (conn, error) { return nil, errors.New("dial") })
	if _, err := p.FetchNZB(context.Background(), []byte(nzbDoc)); err == nil {
		t.Fatal("want dial error")
	}
}

func TestFetchNZBDownloadError(t *testing.T) {
	fc := &binaryFakeConn{artErr: errors.New("430 no article")}
	p := NewWithDial(func(context.Context) (conn, error) { return fc, nil })
	if _, err := p.FetchNZB(context.Background(), []byte(nzbDoc)); err == nil {
		t.Fatal("want download error")
	}
}

// ---- AutoPAR ----

func makeSet(t *testing.T) (*par2.RecoverySet, map[string][]byte) {
	t.Helper()
	data := map[string][]byte{
		"a.bin": bytes.Repeat([]byte("A"), 16),
		"b.bin": bytes.Repeat([]byte("B"), 16),
	}
	rs, err := par2.Create(8, data, 2)
	if err != nil {
		t.Fatalf("par2.Create: %v", err)
	}
	return rs, data
}

func TestAutoPARComplete(t *testing.T) {
	rs, data := makeSet(t)
	out, vr, err := AutoPAR(rs, data)
	if err != nil {
		t.Fatal(err)
	}
	if !vr.Complete {
		t.Fatal("expected complete")
	}
	if len(out) != 2 {
		t.Fatalf("out files %d", len(out))
	}
}

func TestAutoPARRepair(t *testing.T) {
	rs, data := makeSet(t)
	delete(data, "a.bin") // one file missing -> repairable with 2 recovery slices
	out, vr, err := autoPAR(rs, data)
	if err != nil {
		t.Fatal(err)
	}
	if !vr.Complete {
		t.Fatalf("expected complete after repair, vr=%+v", vr)
	}
	if !bytes.Equal(out["a.bin"], bytes.Repeat([]byte("A"), 16)) {
		t.Fatalf("a.bin not repaired: %q", out["a.bin"])
	}
}

func TestAutoPARNotRepairable(t *testing.T) {
	rs, data := makeSet(t)
	delete(data, "a.bin")
	delete(data, "b.bin") // 4 slices missing > 2 recovery
	out, vr, err := autoPAR(rs, data)
	if err != nil {
		t.Fatal(err)
	}
	if vr.Complete || vr.Repairable {
		t.Fatalf("expected not-repairable, vr=%+v", vr)
	}
	if len(out) != 0 {
		t.Fatalf("data returned unchanged, got %d files", len(out))
	}
}

type fakeRS struct {
	vr        []*par2.VerifyResult
	vErr      []error
	repaired  map[string][]byte
	repairErr error
	n         int
}

func (f *fakeRS) Verify(map[string][]byte) (*par2.VerifyResult, error) {
	i := f.n
	f.n++
	var vr *par2.VerifyResult
	if i < len(f.vr) {
		vr = f.vr[i]
	}
	var e error
	if i < len(f.vErr) {
		e = f.vErr[i]
	}
	return vr, e
}
func (f *fakeRS) Repair(map[string][]byte) (map[string][]byte, error) {
	return f.repaired, f.repairErr
}

func TestAutoPARVerifyError(t *testing.T) {
	rs := &fakeRS{vErr: []error{errors.New("verify")}}
	if _, _, err := autoPAR(rs, nil); err == nil {
		t.Fatal("want verify error")
	}
}

func TestAutoPARRepairError(t *testing.T) {
	rs := &fakeRS{vr: []*par2.VerifyResult{{Complete: false, Repairable: true}}, repairErr: errors.New("repair")}
	if _, _, err := autoPAR(rs, nil); err == nil {
		t.Fatal("want repair error")
	}
}

func TestAutoPARFinalVerifyError(t *testing.T) {
	rs := &fakeRS{
		vr:       []*par2.VerifyResult{{Complete: false, Repairable: true}, nil},
		vErr:     []error{nil, errors.New("final")},
		repaired: map[string][]byte{},
	}
	if _, _, err := autoPAR(rs, nil); err == nil {
		t.Fatal("want final verify error")
	}
}

// ---- SplitPAR2 ----

func TestSplitPAR2(t *testing.T) {
	blobs, data := SplitPAR2(map[string][]byte{
		"movie.mkv":     []byte("video"),
		"movie.par2":    []byte("par2a"),
		"movie.vol0.PAR2": []byte("par2b"),
	})
	if len(blobs) != 2 {
		t.Fatalf("par2 blobs = %d", len(blobs))
	}
	if len(data) != 1 || string(data["movie.mkv"]) != "video" {
		t.Fatalf("data = %v", data)
	}
}

// ---- Thumbnail ----

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 128, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestThumbnailLandscape(t *testing.T) {
	out, err := Thumbnail(pngBytes(t, 100, 50), 20)
	if err != nil {
		t.Fatal(err)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width != 20 || cfg.Height != 10 {
		t.Fatalf("thumb dims = %dx%d, want 20x10", cfg.Width, cfg.Height)
	}
}

func TestThumbnailPortraitAndNoUpscale(t *testing.T) {
	out, err := Thumbnail(pngBytes(t, 50, 100), 20)
	if err != nil {
		t.Fatal(err)
	}
	cfg, _, _ := image.DecodeConfig(bytes.NewReader(out))
	if cfg.Width != 10 || cfg.Height != 20 {
		t.Fatalf("portrait dims = %dx%d, want 10x20", cfg.Width, cfg.Height)
	}
	// Small image is not upscaled.
	out2, err := Thumbnail(pngBytes(t, 8, 8), 20)
	if err != nil {
		t.Fatal(err)
	}
	cfg2, _, _ := image.DecodeConfig(bytes.NewReader(out2))
	if cfg2.Width != 8 || cfg2.Height != 8 {
		t.Fatalf("no-upscale dims = %dx%d, want 8x8", cfg2.Width, cfg2.Height)
	}
}

func TestThumbnailDecodeError(t *testing.T) {
	if _, err := Thumbnail([]byte("not an image"), 20); err == nil {
		t.Fatal("want decode error")
	}
}

func TestThumbnailResizeError(t *testing.T) {
	orig := resizeImage
	resizeImage = func(image.Image, int, int, goimages.ResizeMode) (*image.RGBA, error) {
		return nil, errors.New("resize")
	}
	defer func() { resizeImage = orig }()
	if _, err := Thumbnail(pngBytes(t, 40, 40), 20); err == nil {
		t.Fatal("want resize error")
	}
}

func TestThumbnailEncodeError(t *testing.T) {
	orig := encodeJPEG
	encodeJPEG = func(*bytes.Buffer, image.Image) error { return errors.New("encode") }
	defer func() { encodeJPEG = orig }()
	if _, err := Thumbnail(pngBytes(t, 40, 40), 20); err == nil {
		t.Fatal("want encode error")
	}
}

func TestThumbDimsClamp(t *testing.T) {
	if w, h := thumbDims(1000, 1, 10); w != 10 || h != 1 {
		t.Fatalf("wide clamp = %dx%d, want 10x1", w, h)
	}
	if w, h := thumbDims(1, 1000, 10); w != 1 || h != 10 {
		t.Fatalf("tall clamp = %dx%d, want 1x10", w, h)
	}
}
