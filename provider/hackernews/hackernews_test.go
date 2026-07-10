package hackernews

import (
	"context"
	"errors"
	"testing"

	gohn "github.com/go-hackernews/hackernews"

	"github.com/go-news-reader/reader/source"
)

type fakeClient struct {
	items    []gohn.Item
	err      error
	gotKind  gohn.StoryKind
	gotLimit int
}

func (f *fakeClient) Stories(_ context.Context, kind gohn.StoryKind, limit int) ([]gohn.Item, error) {
	f.gotKind, f.gotLimit = kind, limit
	return f.items, f.err
}

func TestKindAndNew(t *testing.T) {
	if New().Kind() != source.HackerNews {
		t.Fatal("kind")
	}
}

func TestFeed(t *testing.T) {
	f := &fakeClient{items: []gohn.Item{
		{ID: 1, Title: "Story", By: "pg", Text: "t", URL: "https://x", Score: 10, Descendants: 4, Time: 1700000000},
		{ID: 2, Deleted: true},
		{ID: 3, Dead: true},
	}}
	p := NewWithClient(f)
	res, err := p.Feed(context.Background(), source.Query{Sort: "new"})
	if err != nil {
		t.Fatal(err)
	}
	if f.gotKind != gohn.Newest || f.gotLimit != defaultLimit {
		t.Fatalf("dispatch kind=%v limit=%d", f.gotKind, f.gotLimit)
	}
	if len(res.Items) != 1 {
		t.Fatalf("deleted/dead not filtered: %+v", res.Items)
	}
	it := res.Items[0]
	if it.ID != "1" || it.Title != "Story" || it.Author != "pg" || it.Link != "https://x" ||
		it.Permalink != "https://news.ycombinator.com/item?id=1" || it.Score != 10 || it.Comments != 4 || it.Created != 1700000000 {
		t.Fatalf("item %+v", it)
	}
}

func TestFeedLimitAndError(t *testing.T) {
	p := NewWithClient(&fakeClient{err: errors.New("boom")})
	if _, err := p.Feed(context.Background(), source.Query{Limit: 5}); err == nil {
		t.Fatal("want error")
	}
	f := &fakeClient{}
	_, _ = NewWithClient(f).Feed(context.Background(), source.Query{Limit: 5})
	if f.gotLimit != 5 {
		t.Fatalf("limit passthrough = %d", f.gotLimit)
	}
}

func TestParseKind(t *testing.T) {
	cases := map[string]gohn.StoryKind{
		"": gohn.Top, "TOP": gohn.Top, "garbage": gohn.Top,
		"new": gohn.Newest, "newest": gohn.Newest, "best": gohn.Best,
	}
	for in, want := range cases {
		if got := parseKind(in); got != want {
			t.Errorf("parseKind(%q)=%v want %v", in, got, want)
		}
	}
}
