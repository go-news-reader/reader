package redgifs

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/go-news-reader/reader/source"
)

// fakeFetcher implements the fetcher seam without any network.
type fakeFetcher struct {
	page *SearchPage
	err  error

	sawQuery string
	sawOrder string
	sawPage  int
	sawCount int
}

func (f *fakeFetcher) Search(_ context.Context, query, order string, page, count int) (*SearchPage, error) {
	f.sawQuery, f.sawOrder, f.sawPage, f.sawCount = query, order, page, count
	return f.page, f.err
}

func TestProviderKindAndConstructors(t *testing.T) {
	if New().Kind() != source.Redgifs {
		t.Fatal("New().Kind() != Redgifs")
	}
	if NewWithHTTPClient(&http.Client{}).Kind() != source.Redgifs {
		t.Fatal("NewWithHTTPClient().Kind() != Redgifs")
	}
	if NewWithClient(&fakeFetcher{}).Kind() != source.Redgifs {
		t.Fatal("NewWithClient().Kind() != Redgifs")
	}
}

func TestFeedEmptyChannelBrowsesTrending(t *testing.T) {
	f := &fakeFetcher{page: &SearchPage{Page: 1, Pages: 1}}
	p := NewWithClient(f)
	// A non-trending sort hint must be overridden to trending for a browse feed.
	if _, err := p.Feed(context.Background(), source.Query{Channel: "  ", Sort: "latest", Limit: 0}); err != nil {
		t.Fatal(err)
	}
	if f.sawQuery != "" {
		t.Fatalf("query = %q, want empty", f.sawQuery)
	}
	if f.sawOrder != defaultOrder {
		t.Fatalf("order = %q, want %q", f.sawOrder, defaultOrder)
	}
	if f.sawCount != defaultCount {
		t.Fatalf("count = %d, want default %d", f.sawCount, defaultCount)
	}
	if f.sawPage != 1 {
		t.Fatalf("page = %d, want 1", f.sawPage)
	}
}

func TestFeedQueryDispatchAndCursor(t *testing.T) {
	f := &fakeFetcher{page: &SearchPage{Page: 2, Pages: 5}}
	p := NewWithClient(f)
	res, err := p.Feed(context.Background(), source.Query{Channel: "teen", Sort: "best", Limit: 25, Cursor: "2"})
	if err != nil {
		t.Fatal(err)
	}
	if f.sawQuery != "teen" || f.sawOrder != "best" || f.sawPage != 2 || f.sawCount != 25 {
		t.Fatalf("passed q=%q order=%q page=%d count=%d", f.sawQuery, f.sawOrder, f.sawPage, f.sawCount)
	}
	// page 2 of 5 → next cursor "3".
	if res.Cursor != "3" {
		t.Fatalf("next cursor = %q, want 3", res.Cursor)
	}
}

func TestFeedCursorExhaustionAndBadCursor(t *testing.T) {
	// Last page: cursor exhausts.
	f := &fakeFetcher{page: &SearchPage{Page: 5, Pages: 5}}
	p := NewWithClient(f)
	res, err := p.Feed(context.Background(), source.Query{Channel: "x", Cursor: "bogus"})
	if err != nil {
		t.Fatal(err)
	}
	// A non-numeric cursor falls back to page 1.
	if f.sawPage != 1 {
		t.Fatalf("bad cursor page = %d, want 1", f.sawPage)
	}
	if res.Cursor != "" {
		t.Fatalf("cursor = %q, want empty on last page", res.Cursor)
	}
	// A non-positive cursor also falls back to page 1.
	f2 := &fakeFetcher{page: &SearchPage{Page: 1, Pages: 1}}
	if _, err := NewWithClient(f2).Feed(context.Background(), source.Query{Channel: "x", Cursor: "0"}); err != nil {
		t.Fatal(err)
	}
	if f2.sawPage != 1 {
		t.Fatalf("cursor 0 page = %d, want 1", f2.sawPage)
	}
}

func TestFeedErrorPropagates(t *testing.T) {
	f := &fakeFetcher{err: errors.New("boom")}
	if _, err := NewWithClient(f).Feed(context.Background(), source.Query{Channel: "x"}); err == nil {
		t.Fatal("want error propagated")
	}
}

func TestFeedMapsItems(t *testing.T) {
	f := &fakeFetcher{page: &SearchPage{
		Page: 1, Pages: 1,
		Gifs: []Gif{{
			ID: "abc", CreateDate: 100, Description: "a cat", UserName: "u",
			Width: 10, Height: 20, Tags: []string{"t1", "t2"},
			URLs: GifURLs{HD: "hd.mp4", Poster: "p.jpg", HTML: "https://www.redgifs.com/ifr/abc"},
		}},
	}}
	res, err := NewWithClient(f).Feed(context.Background(), source.Query{Channel: "kitty"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 1 {
		t.Fatalf("items = %d", len(res.Items))
	}
	it := res.Items[0]
	if it.ID != "abc" || it.Source != source.Redgifs || it.Channel != "kitty" ||
		it.Title != "a cat" || it.Author != "u" || it.Created != 100 || !it.NSFW ||
		it.Score != -1 || it.Comments != -1 || it.Link != "https://www.redgifs.com/ifr/abc" ||
		it.Permalink != "https://www.redgifs.com/ifr/abc" {
		t.Fatalf("mapped item wrong: %+v", it)
	}
	if len(it.Tags) != 2 {
		t.Fatalf("tags = %v", it.Tags)
	}
	if len(it.Media) != 2 || it.Media[0].Kind != source.MediaVideo || it.Media[0].URL != "hd.mp4" ||
		it.Media[0].Width != 10 || it.Media[0].Height != 20 ||
		it.Media[1].Kind != source.MediaImage || it.Media[1].URL != "p.jpg" {
		t.Fatalf("media wrong: %+v", it.Media)
	}
}

func TestMapGifTitleFallbacks(t *testing.T) {
	// Empty description → tags joined.
	g := Gif{ID: "id1", Tags: []string{"Pussy", "Teen"}, URLs: GifURLs{Thumbnail: "th.jpg"}}
	if it := mapGif(g, "ch"); it.Title != "Pussy, Teen" {
		t.Fatalf("tags-title = %q, want 'Pussy, Teen'", it.Title)
	}
	// Empty description + no tags → id.
	g2 := Gif{ID: "id2"}
	it2 := mapGif(g2, "ch")
	if it2.Title != "id2" {
		t.Fatalf("id-title = %q, want id2", it2.Title)
	}
	// No HD, no poster, thumbnail present → single image entry using the thumbnail.
	itTh := mapGif(g, "ch")
	if len(itTh.Media) != 1 || itTh.Media[0].Kind != source.MediaImage || itTh.Media[0].URL != "th.jpg" {
		t.Fatalf("thumbnail media wrong: %+v", itTh.Media)
	}
	// No HD, no poster, no thumbnail → no media at all.
	if it := mapGif(g2, "ch"); len(it.Media) != 0 {
		t.Fatalf("expected no media, got %+v", it.Media)
	}
}
