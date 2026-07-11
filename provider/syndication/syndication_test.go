package syndication

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	gofeed "github.com/go-syndication/feed"

	"github.com/go-news-reader/reader/source"
)

func TestKindAndNew(t *testing.T) {
	if New(nil).Kind() != source.Syndication {
		t.Fatal("kind")
	}
	if New(http.DefaultClient).fetch == nil {
		t.Fatal("fetch seam unset")
	}
}

func TestFeedNoChannel(t *testing.T) {
	if _, err := New(nil).Feed(context.Background(), source.Query{}); !errors.Is(err, ErrNoChannel) {
		t.Fatalf("want ErrNoChannel, got %v", err)
	}
}

func TestFeedError(t *testing.T) {
	p := New(nil)
	p.fetch = func(context.Context, *http.Client, string) (*gofeed.Feed, error) {
		return nil, errors.New("boom")
	}
	if _, err := p.Feed(context.Background(), source.Query{Channel: "https://x/f.xml"}); err == nil {
		t.Fatal("want error")
	}
}

func TestFeedMapAndLimit(t *testing.T) {
	p := New(nil)
	var gotURL string
	p.fetch = func(_ context.Context, _ *http.Client, url string) (*gofeed.Feed, error) {
		gotURL = url
		return &gofeed.Feed{Title: "Site", Entries: []gofeed.Entry{
			{ID: "e1", Title: "A", Author: "auth", Content: "full", Summary: "sum",
				Link: "https://x/a", Published: time.Unix(1700000000, 0),
				Media: []gofeed.Enclosure{{URL: "i", Type: "image/png"}, {URL: "v", Type: "video/mp4"}, {URL: "au", Type: "audio/mpeg"}, {URL: "g", Type: "image/gif"}, {URL: "o", Type: "application/pdf"}}},
			{ID: "", Title: "B", Link: "https://x/b", Summary: "onlysummary"}, // no content, no author, no id
			{Title: "C", Link: "https://x/c"},                                 // dropped by limit
		}}, nil
	}
	res, err := p.Feed(context.Background(), source.Query{Channel: "https://x/f.xml", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if gotURL != "https://x/f.xml" {
		t.Fatalf("url=%q", gotURL)
	}
	if len(res.Items) != 2 {
		t.Fatalf("limit not applied: %d", len(res.Items))
	}
	a := res.Items[0]
	if a.ID != "e1" || a.Author != "auth" || a.Body != "full" || a.Score != -1 || a.Comments != -1 || a.Created != 1700000000 {
		t.Fatalf("item A %+v", a)
	}
	wantKinds := []source.MediaKind{source.MediaImage, source.MediaVideo, source.MediaAudio, source.MediaGIF, source.MediaImage}
	for i, k := range wantKinds {
		if a.Media[i].Kind != k {
			t.Fatalf("media[%d]=%v want %v", i, a.Media[i].Kind, k)
		}
	}
	b := res.Items[1]
	if b.ID != "https://x/b" || b.Author != "Site" || b.Body != "onlysummary" {
		t.Fatalf("item B fallbacks %+v", b)
	}
}

func TestFirstNonEmpty(t *testing.T) {
	if firstNonEmpty("", "", "x") != "x" {
		t.Fatal("firstNonEmpty pick")
	}
	if firstNonEmpty("", "") != "" {
		t.Fatal("firstNonEmpty empty")
	}
}
