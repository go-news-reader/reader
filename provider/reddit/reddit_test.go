package reddit

import (
	"context"
	"errors"
	"net/http"
	"testing"

	goreddit "github.com/go-reddit/reddit"

	"github.com/go-news-reader/reader/source"
)

// fakeFetcher implements the fetcher seam.
type fakeFetcher struct {
	page       *goreddit.Page
	err        error
	sawFront   bool
	sawSub     string
	sawSort    goreddit.Sort
	sawOptions goreddit.ListingOptions
}

func (f *fakeFetcher) Subreddit(_ context.Context, name string, sort goreddit.Sort, opts goreddit.ListingOptions) (*goreddit.Page, error) {
	f.sawSub, f.sawSort, f.sawOptions = name, sort, opts
	return f.page, f.err
}

func (f *fakeFetcher) Frontpage(_ context.Context, sort goreddit.Sort, opts goreddit.ListingOptions) (*goreddit.Page, error) {
	f.sawFront, f.sawSort, f.sawOptions = true, sort, opts
	return f.page, f.err
}

func TestKind(t *testing.T) {
	if New().Kind() != source.Reddit {
		t.Fatal("Kind != reddit")
	}
}

func TestNewWithHTTPClient(t *testing.T) {
	// The injected client backs the reddit client; the provider is still wired.
	if p := NewWithHTTPClient(&http.Client{}); p.client == nil {
		t.Fatal("client not set from injected HTTP client")
	}
}

func TestFeedFrontpage(t *testing.T) {
	f := &fakeFetcher{page: &goreddit.Page{
		After: "t3_next",
		Posts: []goreddit.Post{{
			ID: "a1", Subreddit: "golang", Title: "Self post", Author: "gopher",
			SelfText: "body", Permalink: "/r/golang/comments/a1/self/", IsSelf: true,
			Score: 42, NumComments: 3, CreatedUTC: 1710000000, Stickied: true,
			Flair: "News", Thumbnail: "self",
		}},
	}}
	p := NewWithClient(f)

	res, err := p.Feed(context.Background(), source.Query{Sort: "top", Limit: 25, Cursor: "t3_prev"})
	if err != nil {
		t.Fatalf("Feed: %v", err)
	}
	if !f.sawFront {
		t.Fatal("empty channel should hit Frontpage")
	}
	if f.sawSort != goreddit.SortTop {
		t.Fatalf("sort = %v, want top", f.sawSort)
	}
	if f.sawOptions.Limit != 25 || f.sawOptions.After != "t3_prev" {
		t.Fatalf("options = %+v", f.sawOptions)
	}
	if res.Cursor != "t3_next" || len(res.Items) != 1 {
		t.Fatalf("result = %+v", res)
	}
	it := res.Items[0]
	if it.ID != "a1" || it.Channel != "golang" || it.Source != source.Reddit {
		t.Fatalf("item basics wrong: %+v", it)
	}
	if it.Permalink != "https://www.reddit.com/r/golang/comments/a1/self/" {
		t.Fatalf("permalink = %q", it.Permalink)
	}
	if it.Link != "" {
		t.Fatalf("self post should have no external Link, got %q", it.Link)
	}
	if it.Score != 42 || it.Comments != 3 || it.Created != 1710000000 || !it.Pinned {
		t.Fatalf("item scalars wrong: %+v", it)
	}
	if len(it.Tags) != 1 || it.Tags[0] != "News" {
		t.Fatalf("tags = %v", it.Tags)
	}
	if len(it.Media) != 0 {
		t.Fatalf("self/thumbnail 'self' should yield no media, got %v", it.Media)
	}
}

func TestFeedSubredditWithMedia(t *testing.T) {
	f := &fakeFetcher{page: &goreddit.Page{Posts: []goreddit.Post{{
		ID: "b2", Subreddit: "pics", Title: "A cat", Author: "cats",
		URL: "https://i.redd.it/abc.jpg", Permalink: "/r/pics/comments/b2/a_cat/",
		Thumbnail: "https://b.thumbs.redditmedia.com/x.jpg", Over18: true,
	}}}}
	p := NewWithClient(f)

	res, err := p.Feed(context.Background(), source.Query{Channel: "r/pics"})
	if err != nil {
		t.Fatalf("Feed: %v", err)
	}
	if f.sawSub != "pics" {
		t.Fatalf("subreddit = %q, want pics (r/ stripped)", f.sawSub)
	}
	if f.sawSort != goreddit.SortHot {
		t.Fatalf("default sort = %v, want hot", f.sawSort)
	}
	it := res.Items[0]
	if it.Link != "https://i.redd.it/abc.jpg" {
		t.Fatalf("link-post Link = %q", it.Link)
	}
	if !it.NSFW {
		t.Fatal("Over18 should map to NSFW")
	}
	// One thumbnail + one image.
	if len(it.Media) != 2 {
		t.Fatalf("media = %+v, want thumbnail+image", it.Media)
	}
	if it.Media[0].Kind != source.MediaThumbnail || it.Media[1].Kind != source.MediaImage {
		t.Fatalf("media kinds = %v", it.Media)
	}
}

func TestFeedError(t *testing.T) {
	f := &fakeFetcher{err: errors.New("403")}
	if _, err := NewWithClient(f).Feed(context.Background(), source.Query{Channel: "x"}); err == nil {
		t.Fatal("want error propagated")
	}
}

func TestParseSort(t *testing.T) {
	cases := map[string]goreddit.Sort{
		"":              goreddit.SortHot,
		"HOT":           goreddit.SortHot,
		"new":           goreddit.SortNew,
		"top":           goreddit.SortTop,
		"rising":        goreddit.SortRising,
		"controversial": goreddit.SortControvers,
		"controvers":    goreddit.SortControvers,
		"best":          goreddit.SortBest,
		"garbage":       goreddit.SortHot,
	}
	for in, want := range cases {
		if got := parseSort(in); got != want {
			t.Errorf("parseSort(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestIsThumbURL(t *testing.T) {
	yes := []string{"https://x/y.jpg", "http://a/b"}
	no := []string{"", "self", "default", "nsfw", "spoiler", "image", "notaurl"}
	for _, s := range yes {
		if !isThumbURL(s) {
			t.Errorf("isThumbURL(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if isThumbURL(s) {
			t.Errorf("isThumbURL(%q) = true, want false", s)
		}
	}
}

func TestIsImageURL(t *testing.T) {
	yes := []string{"http://x/a.JPG", "x.jpeg", "x.png", "x.gif", "x.webp",
		"https://i.redd.it/z", "https://i.imgur.com/z"}
	no := []string{"https://example.com/article", "x.mp4", ""}
	for _, s := range yes {
		if !isImageURL(s) {
			t.Errorf("isImageURL(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if isImageURL(s) {
			t.Errorf("isImageURL(%q) = true, want false", s)
		}
	}
}
