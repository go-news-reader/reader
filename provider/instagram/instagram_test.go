package instagram

import (
	"context"
	"errors"
	"testing"
	"time"

	goig "github.com/go-instagram/instagram"

	"github.com/go-news-reader/reader/source"
)

type fakeClient struct {
	prof *goig.Profile
	err  error
	got  string
}

func (f *fakeClient) UserProfile(_ context.Context, username string) (*goig.Profile, error) {
	f.got = username
	return f.prof, f.err
}

func TestKindAndNew(t *testing.T) {
	if New("").Kind() != source.Instagram {
		t.Fatal("kind")
	}
	if New("sess") == nil {
		t.Fatal("session ctor nil")
	}
}

func TestFeedNoChannel(t *testing.T) {
	if _, err := NewWithClient(&fakeClient{}).Feed(context.Background(), source.Query{Channel: "@"}); !errors.Is(err, ErrNoChannel) {
		t.Fatalf("want ErrNoChannel, got %v", err)
	}
}

func TestFeedError(t *testing.T) {
	p := NewWithClient(&fakeClient{err: errors.New("403")})
	if _, err := p.Feed(context.Background(), source.Query{Channel: "nasa"}); err == nil {
		t.Fatal("want error")
	}
}

func TestFeedMapAndLimit(t *testing.T) {
	f := &fakeClient{prof: &goig.Profile{Username: "nasa", Posts: []goig.Post{
		{ID: "1", Shortcode: "abc", Caption: "moon", Owner: "nasa",
			Permalink: "https://www.instagram.com/p/abc/", DisplayURL: "img", IsVideo: true, VideoURL: "vid",
			Likes: 100, Comments: 5, Timestamp: time.Unix(1700000000, 0)},
		{ID: "2", Shortcode: "", DisplayURL: "img2"}, // no shortcode -> id, no owner -> username, image only
		{ID: "3", Shortcode: "z"},                    // dropped by limit
	}}}
	p := NewWithClient(f)
	res, err := p.Feed(context.Background(), source.Query{Channel: "@nasa", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if f.got != "nasa" {
		t.Fatalf("username=%q (@ should be stripped)", f.got)
	}
	if len(res.Items) != 2 {
		t.Fatalf("limit not applied: %d", len(res.Items))
	}
	a := res.Items[0]
	if a.ID != "abc" || a.Author != "nasa" || a.Body != "moon" || a.Score != 100 || a.Comments != 5 || a.Created != 1700000000 {
		t.Fatalf("item A %+v", a)
	}
	if len(a.Media) != 2 || a.Media[0].Kind != source.MediaImage || a.Media[1].Kind != source.MediaVideo {
		t.Fatalf("media A %+v", a.Media)
	}
	b := res.Items[1]
	if b.ID != "2" || b.Author != "nasa" || len(b.Media) != 1 {
		t.Fatalf("item B fallbacks %+v", b)
	}
}

func TestFirstNonEmpty(t *testing.T) {
	if firstNonEmpty("", "y") != "y" || firstNonEmpty("", "") != "" {
		t.Fatal("firstNonEmpty")
	}
}
