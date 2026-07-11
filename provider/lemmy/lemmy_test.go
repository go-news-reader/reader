package lemmy

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	golem "github.com/go-lemmy/lemmy"

	"github.com/go-news-reader/reader/source"
)

type fakeClient struct {
	list *golem.PostList
	err  error
	got  golem.PostsOptions
}

func (f *fakeClient) Posts(_ context.Context, opts golem.PostsOptions) (*golem.PostList, error) {
	f.got = opts
	return f.list, f.err
}

func TestNewWithHTTPClient(t *testing.T) {
	if p := NewWithHTTPClient(&http.Client{}, "https://lemmy.world"); p.client == nil {
		t.Fatal("client not set from injected HTTP client")
	}
}

func TestKindAndNew(t *testing.T) {
	if New("https://lemmy.world").Kind() != source.Lemmy {
		t.Fatal("kind")
	}
}

func TestFeedMapAndPaging(t *testing.T) {
	f := &fakeClient{list: &golem.PostList{Posts: []golem.Post{{
		ID: 7, Title: "T", URL: "https://x", Body: "b", Permalink: "https://lemmy.world/post/7",
		ThumbnailURL: "https://t", Published: time.Unix(1700000000, 0), NSFW: true,
		Creator: "alice", Community: "tech", Score: 9, Comments: 4,
	}}}}
	p := NewWithClient(f)
	res, err := p.Feed(context.Background(), source.Query{Channel: "tech", Sort: "New", Limit: 20, Cursor: "3"})
	if err != nil {
		t.Fatal(err)
	}
	if f.got.Community != "tech" || f.got.Sort != "New" || f.got.Limit != 20 || f.got.Page != 3 {
		t.Fatalf("opts %+v", f.got)
	}
	if res.Cursor != "4" {
		t.Fatalf("cursor=%q, want 4", res.Cursor)
	}
	it := res.Items[0]
	if it.ID != "7" || it.Channel != "tech" || it.Author != "alice" || it.Link != "https://x" ||
		it.Permalink != "https://lemmy.world/post/7" || it.Score != 9 || it.Comments != 4 ||
		!it.NSFW || it.Created != 1700000000 {
		t.Fatalf("item %+v", it)
	}
	if len(it.Media) != 1 || it.Media[0].Kind != source.MediaThumbnail {
		t.Fatalf("media %+v", it.Media)
	}
}

func TestFeedDefaultPageAndEmptyCursor(t *testing.T) {
	f := &fakeClient{list: &golem.PostList{}} // empty -> no next cursor
	p := NewWithClient(f)
	res, err := p.Feed(context.Background(), source.Query{Channel: "tech"})
	if err != nil {
		t.Fatal(err)
	}
	if f.got.Page != 1 {
		t.Fatalf("default page = %d", f.got.Page)
	}
	if res.Cursor != "" {
		t.Fatalf("empty result should have no cursor, got %q", res.Cursor)
	}
}

func TestFeedBadCursor(t *testing.T) {
	f := &fakeClient{list: &golem.PostList{}}
	// Non-numeric and non-positive cursors fall back to page 1.
	_, _ = NewWithClient(f).Feed(context.Background(), source.Query{Cursor: "abc"})
	if f.got.Page != 1 {
		t.Fatalf("bad cursor page = %d", f.got.Page)
	}
	_, _ = NewWithClient(f).Feed(context.Background(), source.Query{Cursor: "0"})
	if f.got.Page != 1 {
		t.Fatalf("zero cursor page = %d", f.got.Page)
	}
}

func TestFeedError(t *testing.T) {
	p := NewWithClient(&fakeClient{err: errors.New("boom")})
	if _, err := p.Feed(context.Background(), source.Query{}); err == nil {
		t.Fatal("want error")
	}
}

func TestMapPostNoThumbnail(t *testing.T) {
	it := mapPost(golem.Post{ID: 1})
	if len(it.Media) != 0 {
		t.Fatalf("no thumbnail should yield no media: %+v", it.Media)
	}
}

func TestFeedAuthError(t *testing.T) {
	p := NewWithClient(&fakeClient{err: errors.New("lemmy: unexpected status 401: unauthorized")})
	_, err := p.Feed(context.Background(), source.Query{Channel: "tech"})
	if ae, ok := source.AsAuthError(err); !ok || ae.Kind != source.Lemmy {
		t.Fatalf("401 not mapped to Lemmy AuthError: %v", err)
	}
	p2 := NewWithClient(&fakeClient{err: errors.New("boom")})
	_, err = p2.Feed(context.Background(), source.Query{Channel: "tech"})
	if _, ok := source.AsAuthError(err); ok {
		t.Fatalf("transient error misclassified as auth: %v", err)
	}
}
