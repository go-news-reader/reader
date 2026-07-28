package usenet

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	gonntp "github.com/go-newsgroups/nntp"
	"github.com/go-newsgroups/yenc"
)

// imgArticle yEnc-encodes a decodable PNG as an article body.
func imgArticle(t *testing.T, name string, w, h int) *gonntp.Article {
	t.Helper()
	return &gonntp.Article{Body: string(yenc.Encode(name, pngBytes(t, w, h), 128))}
}

func TestPrefetchImagesParallel(t *testing.T) {
	arts := map[string]*gonntp.Article{
		"<a@h>": imgArticle(t, "a.png", 20, 20),
		"<b@h>": imgArticle(t, "b.png", 20, 20),
		"<c@h>": imgArticle(t, "c.png", 20, 20),
	}
	var dials int32
	p := NewWithDial(func(context.Context) (conn, error) {
		atomic.AddInt32(&dials, 1)
		return &reconFakeConn{articles: arts}, nil
	})
	reqs := []ImageRequest{
		{ID: "a", Parts: []ReconstructPart{{MessageID: "a@h"}}},
		{ID: "b", Parts: []ReconstructPart{{MessageID: "b@h"}}},
		{ID: "c", Parts: []ReconstructPart{{MessageID: "c@h"}}},
	}
	var mu sync.Mutex
	got := map[string]int{}
	p.PrefetchImages(context.Background(), reqs, 2, 16, func(r ImageResult) {
		mu.Lock()
		defer mu.Unlock()
		if r.Err != nil {
			t.Errorf("%s: unexpected err %v", r.ID, r.Err)
			return
		}
		got[r.ID] = len(r.JPEG)
	})
	if len(got) != 3 || got["a"] == 0 || got["b"] == 0 || got["c"] == 0 {
		t.Fatalf("results = %v, want 3 non-empty JPEGs", got)
	}
	// workers=2 over 3 posts → exactly two connections, each reused (the pool).
	if d := atomic.LoadInt32(&dials); d != 2 {
		t.Fatalf("dials = %d, want 2 (persistent worker connections)", d)
	}
}

func TestPrefetchImagesWorkerClamp(t *testing.T) {
	arts := map[string]*gonntp.Article{"<a@h>": imgArticle(t, "a.png", 8, 8)}
	var dials int32
	p := NewWithDial(func(context.Context) (conn, error) {
		atomic.AddInt32(&dials, 1)
		return &reconFakeConn{articles: arts}, nil
	})
	reqs := []ImageRequest{{ID: "a", Parts: []ReconstructPart{{MessageID: "a@h"}}}}
	// workers far exceeds the request count → clamped to len(reqs)=1.
	p.PrefetchImages(context.Background(), reqs, 99, 16, func(ImageResult) {})
	if d := atomic.LoadInt32(&dials); d != 1 {
		t.Fatalf("dials = %d, want 1 (clamped to req count)", d)
	}
	// workers<1 → clamped to 1 (and a nil callback / empty reqs are no-ops).
	atomic.StoreInt32(&dials, 0)
	p.PrefetchImages(context.Background(), reqs, 0, 16, func(ImageResult) {})
	if d := atomic.LoadInt32(&dials); d != 1 {
		t.Fatalf("workers<1 dials = %d, want 1", d)
	}
	p.PrefetchImages(context.Background(), nil, 4, 16, func(ImageResult) { t.Fatal("empty reqs must not run") })
	p.PrefetchImages(context.Background(), reqs, 4, 16, nil) // nil callback: no panic, no-op
}

func TestPrefetchImagesErrors(t *testing.T) {
	arts := map[string]*gonntp.Article{
		"<img@h>":  imgArticle(t, "p.png", 8, 8),
		"<text@h>": {Body: string(yenc.Encode("note.txt", []byte("just text"), 128))},
		"<bad@h>":  {Body: "not yenc at all"},
	}
	p := NewWithDial(func(context.Context) (conn, error) { return &reconFakeConn{articles: arts}, nil })
	reqs := []ImageRequest{
		{ID: "img", Parts: []ReconstructPart{{MessageID: "img@h"}}},
		{ID: "text", Parts: []ReconstructPart{{MessageID: "text@h"}}}, // decodes but not an image
		{ID: "bad", Parts: []ReconstructPart{{MessageID: "bad@h"}}},   // yEnc decode fails
		{ID: "missing", Parts: []ReconstructPart{{MessageID: "gone@h"}}},
		{ID: "empty", Parts: nil}, // no parts
	}
	var mu sync.Mutex
	res := map[string]error{}
	p.PrefetchImages(context.Background(), reqs, 3, 16, func(r ImageResult) {
		mu.Lock()
		res[r.ID] = r.Err
		mu.Unlock()
	})
	if res["img"] != nil {
		t.Fatalf("img err = %v, want nil", res["img"])
	}
	if !errors.Is(res["text"], ErrNoImage) {
		t.Fatalf("text err = %v, want ErrNoImage", res["text"])
	}
	if !errors.Is(res["empty"], ErrNoImage) {
		t.Fatalf("empty err = %v, want ErrNoImage", res["empty"])
	}
	for _, id := range []string{"bad", "missing"} {
		if res[id] == nil {
			t.Fatalf("%s: expected an error", id)
		}
	}
}

func TestPrefetchImagesConnectError(t *testing.T) {
	p := NewWithDial(func(context.Context) (conn, error) { return nil, errors.New("dial refused") })
	reqs := []ImageRequest{{ID: "a", Parts: []ReconstructPart{{MessageID: "a@h"}}}}
	var got ImageResult
	p.PrefetchImages(context.Background(), reqs, 1, 16, func(r ImageResult) { got = r })
	if got.ID != "a" || got.Err == nil {
		t.Fatalf("connect-failed result = %+v, want an error for a", got)
	}
}

func TestPrefetchImagesCtxCancel(t *testing.T) {
	gate := make(chan struct{})
	entered := make(chan struct{}, 1)
	p := NewWithDial(func(context.Context) (conn, error) {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-gate // block the worker inside connect
		return &reconFakeConn{articles: map[string]*gonntp.Article{}}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	reqs := []ImageRequest{{ID: "a", Parts: []ReconstructPart{{MessageID: "a@h"}}}}
	done := make(chan int, 1)
	go func() {
		n := 0
		p.PrefetchImages(ctx, reqs, 1, 16, func(ImageResult) { n++ })
		done <- n
	}()
	<-entered   // the worker is stuck in connect, so the feeder blocks on send
	cancel()    // feeder now takes the ctx.Done() branch (break feed)
	close(gate) // release connect; worker ranges the closed/empty task channel
	if n := <-done; n != 0 {
		t.Fatalf("cancelled feed delivered %d results, want 0", n)
	}
}
