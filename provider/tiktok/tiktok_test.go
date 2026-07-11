package tiktok

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	gott "github.com/go-tiktok/tiktok"

	"github.com/go-news-reader/reader/source"
)

type fakeClient struct {
	feed     *gott.UserFeed
	err      error
	gotSec   string
	gotCount int
	gotCur   string
}

func (f *fakeClient) UserPosts(_ context.Context, secUid string, count int, cursor string) (*gott.UserFeed, error) {
	f.gotSec, f.gotCount, f.gotCur = secUid, count, cursor
	return f.feed, f.err
}

func TestNewWithHTTPClient(t *testing.T) {
	if p := NewWithHTTPClient(&http.Client{}, "ms", "sess"); p.client == nil {
		t.Fatal("client not set from injected HTTP client")
	}
}

func TestKindAndNew(t *testing.T) {
	if New("", "").Kind() != source.TikTok {
		t.Fatal("kind")
	}
	if New("ms", "sess") == nil {
		t.Fatal("cred ctor nil")
	}
}

func TestFeedNoChannel(t *testing.T) {
	if _, err := NewWithClient(&fakeClient{}).Feed(context.Background(), source.Query{}); !errors.Is(err, ErrNoChannel) {
		t.Fatalf("want ErrNoChannel, got %v", err)
	}
}

func TestFeedError(t *testing.T) {
	p := NewWithClient(&fakeClient{err: errors.New("429")})
	if _, err := p.Feed(context.Background(), source.Query{Channel: "sec"}); err == nil {
		t.Fatal("want error")
	}
}

func TestFeedMapAndCursor(t *testing.T) {
	f := &fakeClient{feed: &gott.UserFeed{Username: "creator", Cursor: "20", HasMore: true, Videos: []gott.Video{
		{ID: "v1", Description: "dance", Author: "creator", Permalink: "https://www.tiktok.com/@creator/video/v1",
			CoverURL: "cov", PlayURL: "play", Likes: 9, Comments: 2, CreateTime: time.Unix(1700000000, 0)},
		{ID: "v2"}, // no author -> username, no media
	}}}
	p := NewWithClient(f)
	res, err := p.Feed(context.Background(), source.Query{Channel: "sec", Cursor: "0"})
	if err != nil {
		t.Fatal(err)
	}
	if f.gotSec != "sec" || f.gotCount != defaultCount || f.gotCur != "0" {
		t.Fatalf("dispatch %+v", f)
	}
	if res.Cursor != "20" {
		t.Fatalf("cursor=%q (HasMore=true)", res.Cursor)
	}
	a := res.Items[0]
	if a.ID != "v1" || a.Author != "creator" || a.Body != "dance" || a.Score != 9 || a.Comments != 2 || a.Created != 1700000000 {
		t.Fatalf("item A %+v", a)
	}
	if len(a.Media) != 2 || a.Media[0].Kind != source.MediaThumbnail || a.Media[1].Kind != source.MediaVideo {
		t.Fatalf("media A %+v", a.Media)
	}
	if res.Items[1].Author != "creator" || len(res.Items[1].Media) != 0 {
		t.Fatalf("item B %+v", res.Items[1])
	}
}

func TestFeedNoMoreCursor(t *testing.T) {
	f := &fakeClient{feed: &gott.UserFeed{Cursor: "20", HasMore: false}}
	res, err := NewWithClient(f).Feed(context.Background(), source.Query{Channel: "sec", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if res.Cursor != "" {
		t.Fatalf("cursor should be empty when HasMore=false, got %q", res.Cursor)
	}
	if f.gotCount != 10 {
		t.Fatalf("limit passthrough=%d", f.gotCount)
	}
}

func TestFeedAuthError(t *testing.T) {
	// No credential configured: any failure prompts for a token.
	p := NewWithClient(&fakeClient{err: errors.New("tiktok: item_list request failed: status 500")})
	_, err := p.Feed(context.Background(), source.Query{Channel: "sec"})
	if ae, ok := source.AsAuthError(err); !ok || ae.Kind != source.TikTok {
		t.Fatalf("no-cred failure not mapped to AuthError: %v", err)
	}
	// With a credential, an explicit 403 still prompts.
	p2 := NewWithClient(&fakeClient{err: errors.New("tiktok: status 403")})
	p2.hasCred = true
	if _, err := p2.Feed(context.Background(), source.Query{Channel: "sec"}); func() bool {
		_, ok := source.AsAuthError(err)
		return !ok
	}() {
		t.Fatal("403 with cred not mapped to AuthError")
	}
	// With a credential, a transient error passes through.
	p3 := NewWithClient(&fakeClient{err: errors.New("tiktok: empty response body")})
	p3.hasCred = true
	_, err = p3.Feed(context.Background(), source.Query{Channel: "sec"})
	if _, ok := source.AsAuthError(err); ok {
		t.Fatalf("transient error misclassified as auth: %v", err)
	}
}
