package twitter

import (
	"context"
	"errors"
	"testing"
	"time"

	gotw "github.com/go-birdsite/twitter"

	"github.com/go-news-reader/reader/source"
)

type fakeClient struct {
	tl  *gotw.Timeline
	err error
	got string
}

func (f *fakeClient) UserTweets(_ context.Context, screenName string) (*gotw.Timeline, error) {
	f.got = screenName
	return f.tl, f.err
}

func TestKindAndNew(t *testing.T) {
	if New("").Kind() != source.Twitter {
		t.Fatal("kind")
	}
	if New("tok") == nil {
		t.Fatal("token ctor nil")
	}
}

func TestFeedNoChannel(t *testing.T) {
	if _, err := NewWithClient(&fakeClient{}).Feed(context.Background(), source.Query{Channel: "@"}); !errors.Is(err, ErrNoChannel) {
		t.Fatalf("want ErrNoChannel, got %v", err)
	}
}

func TestFeedError(t *testing.T) {
	p := NewWithClient(&fakeClient{err: errors.New("403")})
	if _, err := p.Feed(context.Background(), source.Query{Channel: "jack"}); err == nil {
		t.Fatal("want error")
	}
}

func TestFeedMap(t *testing.T) {
	f := &fakeClient{tl: &gotw.Timeline{Tweets: []gotw.Tweet{{
		ID: "1", Text: "hi", Author: "jack", Permalink: "https://twitter.com/jack/status/1",
		CreatedAt: time.Unix(1700000000, 0), Likes: 5, Replies: 2,
		Media: []gotw.Media{{URL: "p", Type: "photo"}, {URL: "v", Type: "video"}, {URL: "g", Type: "animated_gif"}, {URL: "x", Type: "other"}},
	}}}}
	p := NewWithClient(f)
	res, err := p.Feed(context.Background(), source.Query{Channel: "@jack"})
	if err != nil {
		t.Fatal(err)
	}
	if f.got != "jack" {
		t.Fatalf("screen name = %q (@ should be stripped)", f.got)
	}
	it := res.Items[0]
	if it.ID != "1" || it.Author != "jack" || it.Channel != "jack" || it.Body != "hi" ||
		it.Score != 5 || it.Comments != 2 || it.Created != 1700000000 {
		t.Fatalf("item %+v", it)
	}
	wantKinds := []source.MediaKind{source.MediaImage, source.MediaVideo, source.MediaGIF, source.MediaImage}
	if len(it.Media) != 4 {
		t.Fatalf("media %+v", it.Media)
	}
	for i, k := range wantKinds {
		if it.Media[i].Kind != k {
			t.Fatalf("media[%d]=%v want %v", i, it.Media[i].Kind, k)
		}
	}
}
