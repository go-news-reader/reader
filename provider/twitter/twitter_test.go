package twitter

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
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

// feedOne runs one tweet through the provider and returns the mapped item.
func feedOne(t *testing.T, tw gotw.Tweet, channel string) source.Item {
	t.Helper()
	p := NewWithClient(&fakeClient{tl: &gotw.Timeline{Tweets: []gotw.Tweet{tw}}})
	res, err := p.Feed(context.Background(), source.Query{Channel: channel})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(res.Items))
	}
	return res.Items[0]
}

func TestNewWithHTTPClient(t *testing.T) {
	if p := NewWithHTTPClient(&http.Client{}); p.client == nil {
		t.Fatal("client not set from injected HTTP client")
	}
}

func TestKindAndNew(t *testing.T) {
	if New().Kind() != source.Twitter {
		t.Fatal("kind")
	}
	if New() == nil {
		t.Fatal("ctor nil")
	}
}

// TestNewBuildsFingerprintClient checks the no-client constructor does not fall
// back to net/http's default: the endpoint 429s the stock transport whatever the
// quota says, so that fallback was a guaranteed failure, not a degraded path.
func TestNewBuildsFingerprintClient(t *testing.T) {
	p := New()
	c, ok := p.client.(*gotw.Client)
	if !ok {
		t.Fatalf("client = %T, want *gotw.Client", p.client)
	}
	if c.HTTPClient == nil || c.HTTPClient == http.DefaultClient {
		t.Fatalf("HTTPClient = %v, want a dedicated fingerprinting client", c.HTTPClient)
	}
	if c.HTTPClient.Timeout != defaultTimeout {
		t.Fatalf("timeout = %v, want %v", c.HTTPClient.Timeout, defaultTimeout)
	}
	if c.AuthToken != "" {
		t.Fatalf("token = %q, want none (public reads use no auth)", c.AuthToken)
	}
}

func TestFeedNoChannel(t *testing.T) {
	if _, err := NewWithClient(&fakeClient{}).Feed(context.Background(), source.Query{Channel: "@"}); !errors.Is(err, ErrNoChannel) {
		t.Fatalf("want ErrNoChannel, got %v", err)
	}
}

func TestFeedMap(t *testing.T) {
	f := &fakeClient{tl: &gotw.Timeline{Tweets: []gotw.Tweet{{
		ID: "1", Text: "hi", Author: "jack", Permalink: "https://twitter.com/jack/status/1",
		User:      gotw.User{ScreenName: "jack", Name: "Jack D."},
		CreatedAt: time.Unix(1700000000, 0), Likes: 5, Replies: 2, Sensitive: true,
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
	if it.ID != "1" || it.Author != "Jack D." || it.Channel != "jack" || it.Body != "hi" ||
		it.Score != 5 || it.Comments != 2 || it.Created != 1700000000 || !it.NSFW {
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

// TestFeedExpandsLinks checks t.co URLs are resolved in the body and the first
// external destination becomes Item.Link, which is what drives the in-app web
// preview — it used to stay empty, so a tweet never previewed anything.
func TestFeedExpandsLinks(t *testing.T) {
	it := feedOne(t, gotw.Tweet{
		ID: "1", Text: "watch https://t.co/AAA now https://t.co/BBB",
		User: gotw.User{ScreenName: "nasa", Name: "NASA"},
		Links: []gotw.Link{
			{URL: "https://t.co/AAA", Expanded: "https://x.com/nasa/status/1/photo/1"},
			{URL: "https://t.co/BBB", Expanded: "http://nasa.gov/live"},
		},
	}, "nasa")

	if strings.Contains(it.Body, "t.co") {
		t.Fatalf("body still carries a shortened link: %q", it.Body)
	}
	if !strings.Contains(it.Body, "http://nasa.gov/live") {
		t.Fatalf("body = %q, want the expanded destination", it.Body)
	}
	// The self-link is skipped in favour of the real article.
	if it.Link != "http://nasa.gov/live" {
		t.Fatalf("link = %q", it.Link)
	}
}

// TestFeedUnwrapsRetweets is the substance of the retweet fix: the wrapper is a
// truncated "RT @x: …" stub with zero likes and zero replies, which is what the
// feed used to display. The item must carry the ORIGINAL tweet's content,
// author, media, counts and permalink — but keep the retweet's own timestamp, so
// it sorts where it appeared in the timeline.
func TestFeedUnwrapsRetweets(t *testing.T) {
	original := gotw.Tweet{
		ID: "inner", Text: "the real content", Permalink: "https://twitter.com/spox/status/inner",
		User:      gotw.User{ScreenName: "spox", Name: "Bethany Stevens"},
		CreatedAt: time.Unix(1600000000, 0), Likes: 510, Replies: 42,
		Media: []gotw.Media{{URL: "https://pbs/thumb.jpg", Type: "video"}},
	}
	rt := gotw.Tweet{
		ID: "outer", Text: "RT @spox: the real conte…", Permalink: "https://twitter.com/nasa/status/outer",
		User:      gotw.User{ScreenName: "nasa", Name: "NASA"},
		CreatedAt: time.Unix(1700000000, 0), Likes: 0, Replies: 0,
		Retweeted: &original,
	}
	it := feedOne(t, rt, "nasa")

	if it.ID != "outer" {
		t.Fatalf("ID = %q, want the timeline entry's own id", it.ID)
	}
	if it.Body != "the real content" {
		t.Fatalf("body = %q, want the original's text", it.Body)
	}
	if it.Author != "Bethany Stevens" {
		t.Fatalf("author = %q, want the original author", it.Author)
	}
	if it.Channel != "nasa" {
		t.Fatalf("channel = %q, want the subscribed account", it.Channel)
	}
	if it.Score != 510 || it.Comments != 42 {
		t.Fatalf("counts = %d/%d, want the original's 510/42 (the stub reports zeroes)", it.Score, it.Comments)
	}
	if it.Permalink != original.Permalink {
		t.Fatalf("permalink = %q, want the original tweet", it.Permalink)
	}
	if len(it.Media) != 1 || it.Media[0].Kind != source.MediaVideo {
		t.Fatalf("media = %+v, want the original's video", it.Media)
	}
	if it.Created != 1700000000 {
		t.Fatalf("created = %d, want the retweet's own time (timeline order)", it.Created)
	}
}

// TestFeedAppendsQuotedTweet checks a quote is not left dangling without the
// thing it quotes.
func TestFeedAppendsQuotedTweet(t *testing.T) {
	quoted := gotw.Tweet{ID: "q", Text: "the quoted claim", User: gotw.User{ScreenName: "someone"}}
	it := feedOne(t, gotw.Tweet{
		ID: "1", Text: "this is wrong\n", User: gotw.User{ScreenName: "nasa", Name: "NASA"},
		Quoted: &quoted,
	}, "nasa")

	if !strings.Contains(it.Body, "this is wrong") || !strings.Contains(it.Body, "@someone: the quoted claim") {
		t.Fatalf("body = %q, want the quote appended", it.Body)
	}
	if strings.Contains(it.Body, "wrong\n\n\n") {
		t.Fatalf("body = %q, trailing newlines should be trimmed before appending", it.Body)
	}
}

func TestDisplayNameFallsBackToHandle(t *testing.T) {
	if got := displayName(gotw.User{ScreenName: "jack", Name: "  "}); got != "jack" {
		t.Fatalf("got %q, want the handle when there is no name", got)
	}
	if got := displayName(gotw.User{ScreenName: "jack", Name: "Jack"}); got != "Jack" {
		t.Fatalf("got %q, want the display name", got)
	}
}

// TestFeedErrorClassification is the diagnosis fix: this provider used to report
// EVERY failure as "session/token required", sending the user off to find a
// credential that would not have helped. Only a non-public account is actually a
// sign-in problem.
func TestFeedErrorClassification(t *testing.T) {
	authCase := func(t *testing.T, err error, wantAuth bool, wantText string) {
		t.Helper()
		p := NewWithClient(&fakeClient{err: err})
		_, got := p.Feed(context.Background(), source.Query{Channel: "jack"})
		ae, isAuth := source.AsAuthError(got)
		if isAuth != wantAuth {
			t.Fatalf("%v → auth=%v, want %v (mapped: %v)", err, isAuth, wantAuth, got)
		}
		if wantAuth && ae.Kind != source.Twitter {
			t.Fatalf("auth error kind = %v", ae.Kind)
		}
		if !strings.Contains(got.Error(), wantText) {
			t.Fatalf("%v → %q, want it to mention %q", err, got, wantText)
		}
	}

	t.Run("protected is a sign-in problem", func(t *testing.T) {
		authCase(t, fmt.Errorf("%w: @jack", gotw.ErrProtected), true, "not public")
	})
	t.Run("fingerprint refusal is not", func(t *testing.T) {
		authCase(t, fmt.Errorf("%w (@jack)", gotw.ErrFingerprinted), false, "fingerprints")
	})
	t.Run("unknown account is not", func(t *testing.T) {
		authCase(t, fmt.Errorf("%w: @jack", gotw.ErrNotFound), false, "no such account")
	})
	t.Run("anything else passes through untouched", func(t *testing.T) {
		authCase(t, errors.New("twitter: __NEXT_DATA__ not found"), false, "__NEXT_DATA__")
	})
}

// TestFeedCarriesTheVideoNotJustItsThumbnail is the gap this closes. X reports
// only the still frame in media_url_https and keeps the encodings in video_info,
// so mapping the attachment to one entry threw the playable URL away at the
// provider boundary — a consumer reading Item.Media could reach the preview
// image and nothing else.
func TestFeedCarriesTheVideoNotJustItsThumbnail(t *testing.T) {
	it := feedOne(t, gotw.Tweet{
		ID: "v", User: gotw.User{ScreenName: "nasa", Name: "NASA"},
		Media: []gotw.Media{{
			URL: "https://pbs/still.jpg", Type: "video", Width: 1280, Height: 720,
			Variants: []gotw.VideoVariant{
				{URL: "https://v/hls.m3u8", ContentType: "application/x-mpegURL"},
				{URL: "https://v/low.mp4", ContentType: "video/mp4", Bitrate: 288000},
				{URL: "https://v/high.mp4", ContentType: "video/mp4", Bitrate: 2176000},
			},
		}},
	}, "nasa")

	if len(it.Media) != 2 {
		t.Fatalf("media = %+v, want the still AND the playable video", it.Media)
	}
	still, video := it.Media[0], it.Media[1]
	if still.URL != "https://pbs/still.jpg" || still.Kind != source.MediaThumbnail {
		t.Fatalf("still = %+v, want the preview frame as a thumbnail", still)
	}
	if still.Width != 1280 || still.Height != 720 {
		t.Fatalf("still = %+v, want the reported dimensions", still)
	}
	if video.URL != "https://v/high.mp4" || video.Kind != source.MediaVideo {
		t.Fatalf("video = %+v, want the highest-bitrate progressive MP4", video)
	}
	// The dimensions describe the media, so they belong on the playable entry
	// too: a consumer that picks it must still know the aspect.
	if video.Width != 1280 || video.Height != 720 {
		t.Fatalf("video = %+v, want the same dimensions as the still", video)
	}
}

// TestFeedPhotoStaysOneEntry checks the split is video-only: a photo is already
// its own full-resolution URL, so doubling it would be noise.
func TestFeedPhotoStaysOneEntry(t *testing.T) {
	it := feedOne(t, gotw.Tweet{
		ID: "p", User: gotw.User{ScreenName: "nasa"},
		Media: []gotw.Media{{URL: "https://pbs/photo.jpg", Type: "photo", Width: 1080, Height: 1080}},
	}, "nasa")

	if len(it.Media) != 1 {
		t.Fatalf("media = %+v, want a single entry", it.Media)
	}
	if got := it.Media[0]; got.Kind != source.MediaImage || got.Width != 1080 || got.Height != 1080 {
		t.Fatalf("media = %+v", got)
	}
}

// TestFeedStreamOnlyVideoKeepsItsKind checks the honest degradation: with no
// progressive MP4 to hand a plain decoder, the entry stays a video rather than
// claiming to be a thumbnail with nothing behind it.
func TestFeedStreamOnlyVideoKeepsItsKind(t *testing.T) {
	it := feedOne(t, gotw.Tweet{
		ID: "s", User: gotw.User{ScreenName: "nasa"},
		Media: []gotw.Media{{
			URL: "https://pbs/still.jpg", Type: "video",
			Variants: []gotw.VideoVariant{{URL: "https://v/only.m3u8", ContentType: "application/x-mpegURL"}},
		}},
	}, "nasa")

	if len(it.Media) != 1 {
		t.Fatalf("media = %+v, want one entry when nothing is playable", it.Media)
	}
	if got := it.Media[0]; got.Kind != source.MediaVideo || got.URL != "https://pbs/still.jpg" {
		t.Fatalf("media = %+v, want the preview frame still marked a video", got)
	}
}

// TestFeedAnimatedGIFSplitsToo checks X's GIFs — which it delivers as MP4 — get
// the same treatment, keeping the GIF kind on the playable entry.
func TestFeedAnimatedGIFSplitsToo(t *testing.T) {
	it := feedOne(t, gotw.Tweet{
		ID: "g", User: gotw.User{ScreenName: "nasa"},
		Media: []gotw.Media{{
			URL: "https://pbs/gif.jpg", Type: "animated_gif",
			Variants: []gotw.VideoVariant{{URL: "https://v/gif.mp4", ContentType: "video/mp4", Bitrate: 1}},
		}},
	}, "nasa")

	if len(it.Media) != 2 {
		t.Fatalf("media = %+v, want the still and the playable GIF", it.Media)
	}
	if it.Media[0].Kind != source.MediaThumbnail || it.Media[1].Kind != source.MediaGIF {
		t.Fatalf("media = %+v", it.Media)
	}
	if it.Media[1].URL != "https://v/gif.mp4" {
		t.Fatalf("playable = %q", it.Media[1].URL)
	}
}
