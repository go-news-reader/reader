package bluesky

import (
	"context"
	"errors"
	"testing"
	"time"

	goat "github.com/go-atproto/atproto"

	"github.com/go-news-reader/reader/source"
)

type fakeClient struct {
	feed    *goat.Feed
	err     error
	called  string
	gotArg  string
	gotCur  string
	gotLim  int
}

func (f *fakeClient) AuthorFeed(_ context.Context, actor string, limit int, cursor string) (*goat.Feed, error) {
	f.called, f.gotArg, f.gotLim, f.gotCur = "author", actor, limit, cursor
	return f.feed, f.err
}
func (f *fakeClient) SearchPosts(_ context.Context, q string, limit int, cursor string) (*goat.Feed, error) {
	f.called, f.gotArg = "search", q
	return f.feed, f.err
}

func TestKindAndNew(t *testing.T) {
	if New().Kind() != source.Bluesky {
		t.Fatal("kind")
	}
}

func TestFeedAuthor(t *testing.T) {
	f := &fakeClient{feed: &goat.Feed{Cursor: "next", Posts: []goat.Post{{
		URI: "at://did:plc:xyz/app.bsky.feed.post/3kabc", Text: "hello",
		Author: goat.Author{Handle: "alice.bsky.social"}, CreatedAt: time.Unix(1700000000, 0),
		LikeCount: 7, ReplyCount: 3,
		Images: []goat.Image{{Thumb: "t1", Fullsize: "f1", Alt: "a"}, {Fullsize: "f2"}, {Thumb: "t3"}},
	}}}}
	p := NewWithClient(f)
	res, err := p.Feed(context.Background(), source.Query{Channel: "@alice.bsky.social", Limit: 5, Cursor: "c0"})
	if err != nil {
		t.Fatal(err)
	}
	if f.called != "author" || f.gotArg != "alice.bsky.social" || f.gotLim != 5 || f.gotCur != "c0" {
		t.Fatalf("dispatch %+v", f)
	}
	if res.Cursor != "next" || len(res.Items) != 1 {
		t.Fatalf("res %+v", res)
	}
	it := res.Items[0]
	if it.ID != "3kabc" || it.Author != "alice.bsky.social" || it.Body != "hello" ||
		it.Permalink != "https://bsky.app/profile/alice.bsky.social/post/3kabc" ||
		it.Score != 7 || it.Comments != 3 || it.Created != 1700000000 {
		t.Fatalf("item %+v", it)
	}
	// img1: thumb+full (2), img2: full only (1), img3: thumb only (1) => 4
	if len(it.Media) != 4 {
		t.Fatalf("media %+v", it.Media)
	}
}

func TestFeedSearch(t *testing.T) {
	f := &fakeClient{feed: &goat.Feed{}}
	if _, err := NewWithClient(f).Feed(context.Background(), source.Query{Channel: "#golang"}); err != nil {
		t.Fatal(err)
	}
	if f.called != "search" || f.gotArg != "golang" {
		t.Fatalf("search dispatch %+v", f)
	}
}

func TestFeedNoChannel(t *testing.T) {
	if _, err := NewWithClient(&fakeClient{}).Feed(context.Background(), source.Query{}); !errors.Is(err, ErrNoChannel) {
		t.Fatalf("want ErrNoChannel, got %v", err)
	}
}

func TestFeedError(t *testing.T) {
	p := NewWithClient(&fakeClient{err: errors.New("boom")})
	if _, err := p.Feed(context.Background(), source.Query{Channel: "bob"}); err == nil {
		t.Fatal("want error")
	}
}

func TestMapPostURIWithoutSlash(t *testing.T) {
	// URI with no slash exercises the LastIndex<0 branch.
	it := mapPost("x", goat.Post{URI: "bareid", Author: goat.Author{Handle: "h"}})
	if it.ID != "bareid" {
		t.Fatalf("id=%q", it.ID)
	}
}
