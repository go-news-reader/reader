package twitter

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-news-reader/reader/source"
)

type errRT struct{}

func (errRT) RoundTrip(*http.Request) (*http.Response, error) { return nil, errors.New("boom") }

type errBody struct{}

func (errBody) Read([]byte) (int, error) { return 0, errors.New("read fail") }
func (errBody) Close() error             { return nil }

type errBodyRT struct{}

func (errBodyRT) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Body: errBody{}, Header: http.Header{}}, nil
}

func TestIsHomeChannel(t *testing.T) {
	if !isHomeChannel("home") || !isHomeChannel(" Following ") {
		t.Fatal("home/following should be home channels")
	}
	if isHomeChannel("nasa") || isHomeChannel("") {
		t.Fatal("public channel misclassified")
	}
}

func TestCookieValue(t *testing.T) {
	s := "auth_token=AT; ct0=CSRF; guest_id=9"
	if cookieValue(s, "auth_token") != "AT" || cookieValue(s, "ct0") != "CSRF" {
		t.Fatal("cookieValue extract")
	}
	if cookieValue(s, "missing") != "" || cookieValue("novalue; k=v", "k") != "v" {
		t.Fatal("cookieValue edge cases")
	}
}

func TestNewWithSession(t *testing.T) {
	p := NewWithSession(&http.Client{}, "auth_token=AT; ct0=CSRF")
	if p.authToken != "AT" || p.csrf != "CSRF" {
		t.Fatalf("session not parsed: %+v", p)
	}
}

func TestHomeNeedsSession(t *testing.T) {
	// Neither cookie present.
	p := NewWithClient(&fakeClient{})
	if _, err := p.Feed(context.Background(), source.Query{Channel: "home"}); func() bool {
		_, ok := source.AsAuthError(err)
		return !ok
	}() {
		t.Fatal("no cookies should map to AuthError")
	}
	// auth_token present but ct0 missing.
	p2 := &Provider{authToken: "AT"}
	if _, err := p2.homeFeed(context.Background(), source.Query{}); func() bool {
		_, ok := source.AsAuthError(err)
		return !ok
	}() {
		t.Fatal("missing ct0 should map to AuthError")
	}
}

// homeJSON exercises: a Top cursor (ignored), a Bottom cursor (captured), a photo
// tweet, a video tweet, an animated-gif tweet with only an adaptive stream, a
// visibility-wrapped tweet whose author name is blank (screen_name fallback), and
// a null-result entry (skipped).
const homeJSON = `{"data":{"home":{"home_timeline_urt":{"instructions":[
  {"type":"TimelineAddEntries","entries":[
    {"entryId":"cursor-top","content":{"entryType":"TimelineTimelineCursor","cursorType":"Top","value":"TOP"}},
    {"entryId":"t1","content":{"entryType":"TimelineTimelineItem","itemContent":{"tweet_results":{"result":{
      "rest_id":"1","core":{"user_results":{"result":{"legacy":{"screen_name":"alice","name":"Alice"}}}},
      "legacy":{"full_text":"a photo","favorite_count":5,"reply_count":2,"possibly_sensitive":true,
        "created_at":"Wed Oct 10 20:19:24 +0000 2018",
        "extended_entities":{"media":[{"type":"photo","media_url_https":"p.jpg","ext_alt_text":"alt","original_info":{"width":100,"height":80}}]}}}}}}},
    {"entryId":"t2","content":{"entryType":"TimelineTimelineItem","itemContent":{"tweet_results":{"result":{
      "rest_id":"2","core":{"user_results":{"result":{"legacy":{"screen_name":"bob","name":"Bob"}}}},
      "legacy":{"full_text":"a video","created_at":"bad-date",
        "extended_entities":{"media":[{"type":"video","media_url_https":"still.jpg",
          "video_info":{"variants":[{"bitrate":256,"content_type":"video/mp4","url":"low.mp4"},{"bitrate":999,"content_type":"video/mp4","url":"hi.mp4"},{"content_type":"application/x-mpegURL","url":"stream.m3u8"}]}}]}}}}}}},
    {"entryId":"t3","content":{"entryType":"TimelineTimelineItem","itemContent":{"tweet_results":{"result":{
      "rest_id":"3","core":{"user_results":{"result":{"legacy":{"screen_name":"carol","name":"Carol"}}}},
      "legacy":{"full_text":"a gif",
        "extended_entities":{"media":[{"type":"animated_gif","media_url_https":"gif.jpg",
          "video_info":{"variants":[{"content_type":"application/x-mpegURL","url":"stream.m3u8"}]}}]}}}}}}},
    {"entryId":"t4","content":{"entryType":"TimelineTimelineItem","itemContent":{"tweet_results":{"result":{
      "__typename":"TweetWithVisibilityResults","tweet":{
        "rest_id":"4","core":{"user_results":{"result":{"legacy":{"screen_name":"dave","name":""}}}},
        "legacy":{"full_text":"wrapped"}}}}}}},
    {"entryId":"nostatus","content":{"entryType":"TimelineTimelineItem","itemContent":{"tweet_results":{"result":null}}}},
    {"entryId":"cursor-bottom","content":{"entryType":"TimelineTimelineCursor","cursorType":"Bottom","value":"BOTTOM"}}
  ]}
]}}}}`

func TestHomeMapsAndSendsAuth(t *testing.T) {
	var gotAuth, gotCSRF, gotCookie, gotMethod, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotAuth = r.Header.Get("Authorization")
		gotCSRF = r.Header.Get("x-csrf-token")
		gotCookie = r.Header.Get("Cookie")
		buf, _ := io.ReadAll(r.Body)
		gotBody = string(buf)
		_, _ = w.Write([]byte(homeJSON))
	}))
	defer srv.Close()

	p := &Provider{authToken: "AT", csrf: "CSRF", homeBase: srv.URL, hc: srv.Client()}
	res, err := p.Feed(context.Background(), source.Query{Channel: "home", Cursor: "PAGE2"})
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("method = %s", gotMethod)
	}
	if !strings.HasPrefix(gotAuth, "Bearer ") || gotCSRF != "CSRF" {
		t.Fatalf("auth headers: %q / %q", gotAuth, gotCSRF)
	}
	if !strings.Contains(gotCookie, "auth_token=AT") || !strings.Contains(gotCookie, "ct0=CSRF") {
		t.Fatalf("cookie = %q", gotCookie)
	}
	if !strings.Contains(gotBody, homeTimelineQueryID) || !strings.Contains(gotBody, "PAGE2") {
		t.Fatalf("body = %q", gotBody)
	}
	if res.Cursor != "BOTTOM" {
		t.Fatalf("cursor = %q", res.Cursor)
	}
	if len(res.Items) != 4 {
		t.Fatalf("items = %d, want 4", len(res.Items))
	}
	photo := res.Items[0]
	if photo.ID != "1" || photo.Author != "Alice" || photo.Body != "a photo" || photo.Score != 5 ||
		photo.Comments != 2 || !photo.NSFW || photo.Created != 1539202764 || photo.Channel != "home" ||
		photo.Permalink != "https://x.com/alice/status/1" {
		t.Fatalf("photo tweet = %+v", photo)
	}
	if len(photo.Media) != 1 || photo.Media[0].Kind != source.MediaImage || photo.Media[0].AltText != "alt" ||
		photo.Media[0].Width != 100 {
		t.Fatalf("photo media = %+v", photo.Media)
	}
	video := res.Items[1]
	if video.Created != 0 { // "bad-date" -> unknown
		t.Fatalf("bad date should yield Created 0, got %d", video.Created)
	}
	if len(video.Media) != 2 || video.Media[0].Kind != source.MediaThumbnail ||
		video.Media[1].Kind != source.MediaVideo || video.Media[1].URL != "hi.mp4" {
		t.Fatalf("video media = %+v", video.Media)
	}
	gif := res.Items[2]
	if len(gif.Media) != 1 || gif.Media[0].Kind != source.MediaGIF { // adaptive-only -> still kept as gif
		t.Fatalf("gif media = %+v", gif.Media)
	}
	wrapped := res.Items[3]
	if wrapped.ID != "4" || wrapped.Author != "dave" || wrapped.Permalink != "https://x.com/dave/status/4" {
		t.Fatalf("wrapped tweet (screen_name fallback) = %+v", wrapped)
	}
}

func TestHomeLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(homeJSON))
	}))
	defer srv.Close()
	p := &Provider{authToken: "AT", csrf: "CSRF", homeBase: srv.URL, hc: srv.Client()}
	res, err := p.homeFeed(context.Background(), source.Query{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 2 {
		t.Fatalf("limit not applied: %d", len(res.Items))
	}
}

func TestHomePermalinkEmptyScreenName(t *testing.T) {
	if homePermalink("", "9") != "" {
		t.Fatal("empty screen name should yield empty permalink")
	}
}

func TestHomeNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	p := &Provider{authToken: "AT", csrf: "CSRF", homeBase: srv.URL, hc: srv.Client()}
	if _, err := p.homeFeed(context.Background(), source.Query{}); func() bool {
		_, ok := source.AsAuthError(err)
		return !ok
	}() {
		t.Fatal("403 should map to AuthError")
	}
}

func TestHomeBadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html>login</html>"))
	}))
	defer srv.Close()
	p := &Provider{authToken: "AT", csrf: "CSRF", homeBase: srv.URL, hc: srv.Client()}
	if _, err := p.homeFeed(context.Background(), source.Query{}); func() bool {
		_, ok := source.AsAuthError(err)
		return !ok
	}() {
		t.Fatal("non-JSON body should map to AuthError")
	}
}

func TestHomeRequestBuildError(t *testing.T) {
	p := &Provider{authToken: "AT", csrf: "CSRF", homeBase: "http://\x7f-bad", hc: http.DefaultClient}
	if _, err := p.homeFeed(context.Background(), source.Query{}); err == nil {
		t.Fatal("want request-build error")
	}
}

func TestHomeTransportError(t *testing.T) {
	p := &Provider{authToken: "AT", csrf: "CSRF", homeBase: "http://example.invalid", hc: &http.Client{Transport: errRT{}}}
	if _, err := p.homeFeed(context.Background(), source.Query{}); err == nil {
		t.Fatal("want transport error")
	}
}

func TestHomeBodyReadError(t *testing.T) {
	p := &Provider{authToken: "AT", csrf: "CSRF", homeBase: "http://example.invalid", hc: &http.Client{Transport: errBodyRT{}}}
	if _, err := p.homeFeed(context.Background(), source.Query{}); err == nil {
		t.Fatal("want body read error")
	}
}
