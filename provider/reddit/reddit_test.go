package reddit

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
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

// cookieRT records the Cookie / Authorization headers and the host of a
// listing request, serving canned listing JSON so the session-cookie path can
// be exercised offline.
type cookieRT struct {
	cookie   string
	authHdr  string
	listHost string
}

func (rt *cookieRT) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.cookie = req.Header.Get("Cookie")
	rt.authHdr = req.Header.Get("Authorization")
	rt.listHost = req.URL.Host
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(`{"data":{"after":"","children":[{"kind":"t3","data":{"id":"c1","title":"Hi","subreddit":"golang"}}]}}`)),
		Header:     make(http.Header),
	}, nil
}

func TestNewWithCookie(t *testing.T) {
	rt := &cookieRT{}
	p := NewWithCookie(&http.Client{Transport: rt}, "abc123")

	res, err := p.Feed(context.Background(), source.Query{Channel: "golang", Limit: 5})
	if err != nil {
		t.Fatalf("Feed: %v", err)
	}
	if len(res.Items) != 1 || res.Items[0].ID != "c1" {
		t.Fatalf("items = %+v", res.Items)
	}
	// The reddit_session cookie authenticates the request against the anonymous
	// www ".json" host, with no Bearer token.
	if rt.cookie != "reddit_session=abc123" {
		t.Fatalf("Cookie = %q, want reddit_session=abc123", rt.cookie)
	}
	if rt.authHdr != "" {
		t.Fatalf("Authorization = %q, want none for cookie auth", rt.authHdr)
	}
	if rt.listHost != "www.reddit.com" {
		t.Fatalf("listing host = %q, want www.reddit.com", rt.listHost)
	}
}

func TestNewWithCookieNilClient(t *testing.T) {
	// A nil client falls back to the browser-fingerprint client and still wires
	// the provider (no network here).
	if p := NewWithCookie(nil, "abc123"); p.client == nil {
		t.Fatal("nil client should fall back and still build the provider")
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

func TestFeedAuthErrorMapping(t *testing.T) {
	// A 403 (anonymous data-center block) or 401 (stale session cookie) becomes a
	// typed source.AuthError so the UI prompts the user to sign in.
	for _, code := range []int{401, 403} {
		f := &fakeFetcher{err: &goreddit.APIError{StatusCode: code, Status: "forbidden"}}
		_, err := NewWithClient(f).Feed(context.Background(), source.Query{Channel: "golang"})
		ae, ok := source.AsAuthError(err)
		if !ok || ae.Kind != source.Reddit {
			t.Fatalf("status %d not mapped to Reddit AuthError: %v", code, err)
		}
	}
	// A non-auth APIError (e.g. 500) passes through untouched.
	f := &fakeFetcher{err: &goreddit.APIError{StatusCode: 500, Status: "server error"}}
	_, err := NewWithClient(f).Feed(context.Background(), source.Query{Channel: "golang"})
	if err == nil {
		t.Fatal("want error")
	}
	if _, ok := source.AsAuthError(err); ok {
		t.Fatalf("500 misclassified as auth: %v", err)
	}
}

// postFromJSON builds a goreddit.Post from a Reddit-shaped JSON body (the media
// sub-structs use unexported types, so a composite literal can't set them —
// unmarshalling exercises the same decode path the client uses).
func postFromJSON(t *testing.T, body string) goreddit.Post {
	t.Helper()
	var p goreddit.Post
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		t.Fatalf("unmarshal test post: %v", err)
	}
	return p
}

func mapOne(t *testing.T, p goreddit.Post) source.Item {
	t.Helper()
	f := &fakeFetcher{page: &goreddit.Page{Posts: []goreddit.Post{p}}}
	res, err := NewWithClient(f).Feed(context.Background(), source.Query{Channel: "r/x"})
	if err != nil {
		t.Fatalf("Feed: %v", err)
	}
	if len(res.Items) != 1 {
		t.Fatalf("got %d items", len(res.Items))
	}
	return res.Items[0]
}

func hasMedia(it source.Item, kind source.MediaKind, url string) bool {
	for _, m := range it.Media {
		if m.Kind == kind && m.URL == url {
			return true
		}
	}
	return false
}

// A gallery post shows its first image (a resolved gallery item), not the
// gallery permalink.
func TestMapGalleryPost(t *testing.T) {
	p := postFromJSON(t, `{"id":"g1","subreddit":"pics","title":"gallery",
		"url":"https://www.reddit.com/gallery/g1","permalink":"/r/pics/comments/g1/gallery/",
		"is_gallery":true,
		"gallery_data":{"items":[{"media_id":"m1"},{"media_id":"m2"}]},
		"media_metadata":{"m1":{"status":"valid","s":{"u":"https://i.redd.it/m1.jpg?s=a&amp;t=b"}},
		                  "m2":{"status":"valid","s":{"u":"https://i.redd.it/m2.jpg"}}}}`)
	it := mapOne(t, p)
	if it.Link != "https://i.redd.it/m1.jpg?s=a&t=b" {
		t.Errorf("gallery Link = %q, want the first gallery image", it.Link)
	}
	if !hasMedia(it, source.MediaImage, "https://i.redd.it/m1.jpg?s=a&t=b") {
		t.Errorf("gallery media missing first image: %+v", it.Media)
	}
}

// A reddit-hosted video shows its poster and exposes the video stream.
func TestMapVideoPost(t *testing.T) {
	p := postFromJSON(t, `{"id":"v1","subreddit":"videos","title":"clip",
		"url":"https://v.redd.it/v1","permalink":"/r/videos/comments/v1/clip/",
		"is_video":true,"post_hint":"hosted:video",
		"media":{"reddit_video":{"fallback_url":"https://v.redd.it/v1/DASH_720.mp4?source=fallback"}},
		"preview":{"images":[{"source":{"url":"https://external-preview.redd.it/v1.jpg?width=640"}}]}}`)
	it := mapOne(t, p)
	if it.Link != "https://external-preview.redd.it/v1.jpg?width=640" {
		t.Errorf("video Link = %q, want the poster image", it.Link)
	}
	if !hasMedia(it, source.MediaImage, "https://external-preview.redd.it/v1.jpg?width=640") {
		t.Errorf("video poster missing: %+v", it.Media)
	}
	if !hasMedia(it, source.MediaVideo, "https://v.redd.it/v1/DASH_720.mp4?source=fallback") {
		t.Errorf("video stream missing: %+v", it.Media)
	}
}

// An image post whose URL is not a direct image (post_hint=image, preview only)
// still shows the resolved preview image.
func TestMapImagePostFromPreview(t *testing.T) {
	p := postFromJSON(t, `{"id":"i1","subreddit":"art","title":"art","post_hint":"image",
		"url":"https://example.com/view/i1","permalink":"/r/art/comments/i1/art/",
		"preview":{"images":[{"source":{"url":"https://preview.redd.it/i1.png?width=1024&amp;crop=smart"}}]}}`)
	it := mapOne(t, p)
	if it.Link != "https://preview.redd.it/i1.png?width=1024&crop=smart" {
		t.Errorf("image Link = %q, want the preview image", it.Link)
	}
}

// A plain external article link (no media) web-renders its article and carries
// no image media.
func TestMapExternalLink(t *testing.T) {
	p := postFromJSON(t, `{"id":"l1","subreddit":"news","title":"story",
		"url":"https://example.com/article","permalink":"/r/news/comments/l1/story/","post_hint":"link"}`)
	it := mapOne(t, p)
	if it.Link != "https://example.com/article" {
		t.Errorf("link Link = %q", it.Link)
	}
	for _, m := range it.Media {
		if m.Kind == source.MediaImage {
			t.Errorf("external link should map no image media: %+v", it.Media)
		}
	}
}
