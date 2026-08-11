package reddit

import (
	"context"
	"errors"
	"testing"

	goreddit "github.com/go-reddit/reddit"

	"github.com/go-news-reader/reader/provider/redgifs"
	"github.com/go-news-reader/reader/source"
)

// fakeRedgifs is a RedgifsResolver seam that returns a canned gif/error and
// records the id it was asked for, so the dispatch is testable without network.
type fakeRedgifs struct {
	gif   *redgifs.Gif
	err   error
	sawID string
	calls int
}

func (f *fakeRedgifs) GifByID(_ context.Context, id string) (*redgifs.Gif, error) {
	f.sawID = id
	f.calls++
	return f.gif, f.err
}

func TestImgurDirect(t *testing.T) {
	const img = source.MediaImage
	const vid = source.MediaVideo
	cases := []struct {
		name string
		in   string
		want string
		kind source.MediaKind
		ok   bool
	}{
		{"gifv on page host", "https://imgur.com/abc123.gifv", "https://i.imgur.com/abc123.gif", img, true},
		{"gifv on cdn host", "https://i.imgur.com/abc123.gifv", "https://i.imgur.com/abc123.gif", img, true},
		{"cdn jpg kept", "https://i.imgur.com/abc123.jpg", "https://i.imgur.com/abc123.jpg", img, true},
		{"cdn png kept", "https://i.imgur.com/abc123.png", "https://i.imgur.com/abc123.png", img, true},
		{"cdn mp4 kept", "https://i.imgur.com/abc123.mp4", "https://i.imgur.com/abc123.mp4", vid, true},
		{"cdn no extension", "https://i.imgur.com/abc123", "", "", false},
		{"bare id to jpg", "https://imgur.com/abc123", "https://i.imgur.com/abc123.jpg", img, true},
		{"bare id with png", "https://imgur.com/abc123.png", "https://i.imgur.com/abc123.png", img, true},
		{"bare id with mp4", "https://imgur.com/abc123.mp4", "https://i.imgur.com/abc123.mp4", vid, true},
		{"bare id other ext", "https://imgur.com/abc123.pdf", "", "", false},
		{"www host bare id", "https://www.imgur.com/abc123", "https://i.imgur.com/abc123.jpg", img, true},
		{"m host bare id", "https://m.imgur.com/abc123", "https://i.imgur.com/abc123.jpg", img, true},
		{"album not mapped", "https://imgur.com/a/xyz", "", "", false},
		{"gallery not mapped", "https://imgur.com/gallery/xyz", "", "", false},
		{"empty path", "https://imgur.com/", "", "", false},
		{"non-imgur host", "https://example.com/abc123.jpg", "", "", false},
		{"empty host", "/relative/abc123.jpg", "", "", false},
		{"parse error", "https://%gg", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, kind, ok := imgurDirect(tc.in)
			if got != tc.want || kind != tc.kind || ok != tc.ok {
				t.Fatalf("imgurDirect(%q) = (%q,%q,%v), want (%q,%q,%v)",
					tc.in, got, kind, ok, tc.want, tc.kind, tc.ok)
			}
		})
	}
}

func TestResolveImgur(t *testing.T) {
	// A match sets Link and appends the media.
	it := source.Item{}
	if !resolveImgur(&it, "https://imgur.com/xyz.gifv") {
		t.Fatal("expected imgur match")
	}
	if it.Link != "https://i.imgur.com/xyz.gif" {
		t.Fatalf("Link = %q", it.Link)
	}
	if len(it.Media) != 1 || it.Media[0].Kind != source.MediaImage || it.Media[0].URL != "https://i.imgur.com/xyz.gif" {
		t.Fatalf("media = %+v", it.Media)
	}
	// A non-match leaves the item untouched.
	other := source.Item{}
	if resolveImgur(&other, "https://example.com/x") {
		t.Fatal("non-imgur should not match")
	}
	if other.Link != "" || len(other.Media) != 0 {
		t.Fatalf("item mutated: %+v", other)
	}
}

func TestRedditAudioURL(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"modern mp4 stream", "https://v.redd.it/abc/DASH_720.mp4?source=fallback", "https://v.redd.it/abc/DASH_AUDIO_128.mp4"},
		{"legacy bare stream", "https://v.redd.it/abc/DASH_4_8_M", "https://v.redd.it/abc/audio"},
		{"host case-insensitive", "https://V.REDD.IT/abc/DASH_360.mp4", "https://V.REDD.IT/abc/DASH_AUDIO_128.mp4"},
		{"not v.redd.it", "https://example.com/abc/DASH_720.mp4", ""},
		{"no stream segment", "https://v.redd.it/onlyid", ""},
		{"parse error", "https://%gg", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := redditAudioURL(tc.in); got != tc.want {
				t.Fatalf("redditAudioURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestRedgifsID(t *testing.T) {
	cases := []struct {
		name, in, want string
		ok             bool
	}{
		{"watch", "https://redgifs.com/watch/CoolId", "coolid", true},
		{"ifr on www", "https://www.redgifs.com/ifr/AbC", "abc", true},
		{"media subdomain", "https://media.redgifs.com/watch/xyz", "xyz", true},
		{"empty id", "https://redgifs.com/watch/", "", false},
		{"unknown segment", "https://redgifs.com/users/bob", "", false},
		{"too short", "https://redgifs.com/watch", "", false},
		{"wrong host", "https://example.com/watch/x", "", false},
		{"parse error", "https://%gg", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := redgifsID(tc.in)
			if got != tc.want || ok != tc.ok {
				t.Fatalf("redgifsID(%q) = (%q,%v), want (%q,%v)", tc.in, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestResolveRedgifs(t *testing.T) {
	base := goreddit.Post{URL: "https://redgifs.com/watch/SomeId"}

	t.Run("hd and poster", func(t *testing.T) {
		f := &fakeRedgifs{gif: &redgifs.Gif{
			URLs:  redgifs.GifURLs{HD: "https://m/hd.mp4", Poster: "https://m/p.jpg"},
			Width: 720, Height: 1280,
		}}
		pr := NewWithClient(&fakeFetcher{}).WithRedgifs(f)
		it := source.Item{}
		if !pr.resolveRedgifs(context.Background(), &it, base) {
			t.Fatal("expected resolution")
		}
		if f.sawID != "someid" {
			t.Errorf("id passed = %q, want lower-cased", f.sawID)
		}
		if it.Link != "https://m/p.jpg" {
			t.Errorf("Link = %q, want the poster (mp4 is unplayable)", it.Link)
		}
		if !hasMedia(it, source.MediaVideo, "https://m/hd.mp4") || !hasMedia(it, source.MediaImage, "https://m/p.jpg") {
			t.Errorf("media = %+v", it.Media)
		}
		if it.Media[0].Width != 720 || it.Media[0].Height != 1280 {
			t.Errorf("dims = %dx%d", it.Media[0].Width, it.Media[0].Height)
		}
	})

	t.Run("sd fallback and thumbnail fallback", func(t *testing.T) {
		f := &fakeRedgifs{gif: &redgifs.Gif{
			URLs: redgifs.GifURLs{SD: "https://m/sd.mp4", Thumbnail: "https://m/t.jpg"},
		}}
		pr := NewWithClient(&fakeFetcher{}).WithRedgifs(f)
		it := source.Item{}
		if !pr.resolveRedgifs(context.Background(), &it, base) {
			t.Fatal("expected resolution")
		}
		if it.Link != "https://m/t.jpg" {
			t.Errorf("Link = %q, want the thumbnail poster", it.Link)
		}
		if !hasMedia(it, source.MediaImage, "https://m/t.jpg") {
			t.Errorf("poster should fall back to thumbnail: %+v", it.Media)
		}
	})

	t.Run("no poster", func(t *testing.T) {
		f := &fakeRedgifs{gif: &redgifs.Gif{URLs: redgifs.GifURLs{HD: "https://m/hd.mp4"}}}
		pr := NewWithClient(&fakeFetcher{}).WithRedgifs(f)
		it := source.Item{}
		if !pr.resolveRedgifs(context.Background(), &it, base) {
			t.Fatal("expected resolution")
		}
		if len(it.Media) != 1 || it.Media[0].Kind != source.MediaVideo {
			t.Errorf("media = %+v, want video only", it.Media)
		}
	})

	t.Run("poster only, no video", func(t *testing.T) {
		f := &fakeRedgifs{gif: &redgifs.Gif{URLs: redgifs.GifURLs{Poster: "https://m/p.jpg"}}}
		pr := NewWithClient(&fakeFetcher{}).WithRedgifs(f)
		it := source.Item{}
		if !pr.resolveRedgifs(context.Background(), &it, base) {
			t.Fatal("a poster-only gif should still resolve (show the poster)")
		}
		if it.Link != "https://m/p.jpg" {
			t.Errorf("Link = %q, want the poster", it.Link)
		}
		if len(it.Media) != 1 || it.Media[0].Kind != source.MediaImage {
			t.Errorf("media = %+v, want image (poster) only", it.Media)
		}
	})

	t.Run("no poster and no video", func(t *testing.T) {
		f := &fakeRedgifs{gif: &redgifs.Gif{URLs: redgifs.GifURLs{}}}
		pr := NewWithClient(&fakeFetcher{}).WithRedgifs(f)
		it := source.Item{}
		if pr.resolveRedgifs(context.Background(), &it, base) {
			t.Fatal("nothing to show should not resolve (keep Reddit's static poster)")
		}
	})

	t.Run("lookup error", func(t *testing.T) {
		f := &fakeRedgifs{err: errors.New("boom")}
		pr := NewWithClient(&fakeFetcher{}).WithRedgifs(f)
		it := source.Item{}
		if pr.resolveRedgifs(context.Background(), &it, base) {
			t.Fatal("a lookup error should not resolve")
		}
	})

	t.Run("nil gif", func(t *testing.T) {
		f := &fakeRedgifs{} // gif nil, err nil
		pr := NewWithClient(&fakeFetcher{}).WithRedgifs(f)
		it := source.Item{}
		if pr.resolveRedgifs(context.Background(), &it, base) {
			t.Fatal("a nil gif should not resolve")
		}
	})

	t.Run("non-redgifs url", func(t *testing.T) {
		f := &fakeRedgifs{gif: &redgifs.Gif{URLs: redgifs.GifURLs{HD: "https://m/hd.mp4"}}}
		pr := NewWithClient(&fakeFetcher{}).WithRedgifs(f)
		it := source.Item{}
		if pr.resolveRedgifs(context.Background(), &it, goreddit.Post{URL: "https://example.com/x"}) {
			t.Fatal("non-redgifs url should not resolve")
		}
		if f.calls != 0 {
			t.Errorf("resolver called %d times for a non-redgifs url", f.calls)
		}
	})

	t.Run("nil resolver", func(t *testing.T) {
		pr := NewWithClient(&fakeFetcher{}) // no WithRedgifs
		it := source.Item{}
		if pr.resolveRedgifs(context.Background(), &it, base) {
			t.Fatal("a nil resolver should not resolve")
		}
	})
}

// TestMapPostImgurDispatch drives an imgur post through the full Feed pipeline
// and asserts the imgur resolver won (and the redgifs resolver was not called).
func TestMapPostImgurDispatch(t *testing.T) {
	f := &fakeRedgifs{gif: &redgifs.Gif{URLs: redgifs.GifURLs{HD: "https://should/not.mp4"}}}
	p := postFromJSON(t, `{"id":"im","subreddit":"pics","title":"i","post_hint":"image",
		"url":"https://imgur.com/abc123","permalink":"/r/pics/comments/im/i/"}`)
	it := feedOne(t, p, f)
	if it.Link != "https://i.imgur.com/abc123.jpg" {
		t.Fatalf("Link = %q, want the resolved imgur image", it.Link)
	}
	if !hasMedia(it, source.MediaImage, "https://i.imgur.com/abc123.jpg") {
		t.Fatalf("media = %+v", it.Media)
	}
	if f.calls != 0 {
		t.Errorf("redgifs resolver called %d times for an imgur post", f.calls)
	}
}

// TestMapPostRedgifsDispatch drives a redgifs post through Feed and asserts the
// redgifs resolver replaced Reddit's static poster with the real mp4 + poster.
func TestMapPostRedgifsDispatch(t *testing.T) {
	f := &fakeRedgifs{gif: &redgifs.Gif{URLs: redgifs.GifURLs{HD: "https://m/hd.mp4", Poster: "https://m/p.jpg"}}}
	p := postFromJSON(t, `{"id":"rg","subreddit":"gifs","title":"g","post_hint":"rich:video",
		"url":"https://redgifs.com/watch/xyz","permalink":"/r/gifs/comments/rg/g/",
		"preview":{"images":[{"source":{"url":"https://external-preview.redd.it/rg.jpg"}}]}}`)
	it := feedOne(t, p, f)
	if it.Link != "https://m/p.jpg" {
		t.Fatalf("Link = %q, want the resolved redgifs poster (mp4 is unplayable)", it.Link)
	}
	if !hasMedia(it, source.MediaVideo, "https://m/hd.mp4") || !hasMedia(it, source.MediaImage, "https://m/p.jpg") {
		t.Fatalf("media = %+v", it.Media)
	}
	if f.sawID != "xyz" {
		t.Errorf("resolver id = %q", f.sawID)
	}
}

// TestMapPostRedgifsFallsThrough asserts that when the redgifs lookup fails the
// generic path still shows Reddit's poster (graceful degradation).
func TestMapPostRedgifsFallsThrough(t *testing.T) {
	f := &fakeRedgifs{err: errors.New("offline")}
	p := postFromJSON(t, `{"id":"rg","subreddit":"gifs","title":"g","post_hint":"rich:video",
		"url":"https://redgifs.com/watch/xyz","permalink":"/r/gifs/comments/rg/g/",
		"preview":{"images":[{"source":{"url":"https://external-preview.redd.it/rg.jpg"}}]}}`)
	it := feedOne(t, p, f)
	if it.Link != "https://external-preview.redd.it/rg.jpg" {
		t.Fatalf("Link = %q, want the reddit poster fallback", it.Link)
	}
}

// TestMapVideoPostExposesAudio asserts a reddit-hosted video also exposes its
// separate DASH audio track alongside the video stream and poster.
func TestMapVideoPostExposesAudio(t *testing.T) {
	p := postFromJSON(t, `{"id":"v1","subreddit":"videos","title":"clip",
		"url":"https://v.redd.it/v1","permalink":"/r/videos/comments/v1/clip/",
		"is_video":true,"post_hint":"hosted:video",
		"media":{"reddit_video":{"fallback_url":"https://v.redd.it/v1/DASH_720.mp4?source=fallback"}},
		"preview":{"images":[{"source":{"url":"https://external-preview.redd.it/v1.jpg"}}]}}`)
	it := mapOne(t, p)
	if !hasMedia(it, source.MediaVideo, "https://v.redd.it/v1/DASH_720.mp4?source=fallback") {
		t.Errorf("video stream missing: %+v", it.Media)
	}
	if !hasMedia(it, source.MediaAudio, "https://v.redd.it/v1/DASH_AUDIO_128.mp4") {
		t.Errorf("audio track missing: %+v", it.Media)
	}
}

// feedOne runs a single post through the full Feed pipeline with a fake RedGIFs
// resolver attached, returning the mapped item.
func feedOne(t *testing.T, p goreddit.Post, rg RedgifsResolver) source.Item {
	t.Helper()
	f := &fakeFetcher{page: &goreddit.Page{Posts: []goreddit.Post{p}}}
	pr := NewWithClient(f).WithRedgifs(rg)
	res, err := pr.Feed(context.Background(), source.Query{Channel: "r/x"})
	if err != nil {
		t.Fatalf("Feed: %v", err)
	}
	if len(res.Items) != 1 {
		t.Fatalf("got %d items", len(res.Items))
	}
	return res.Items[0]
}
