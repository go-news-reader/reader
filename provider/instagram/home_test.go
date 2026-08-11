package instagram

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

// errRT is a RoundTripper that always fails, to exercise the transport-error path.
type errRT struct{}

func (errRT) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("boom")
}

// errBodyRT returns a 200 response whose body errors on read, to exercise the
// io.ReadAll failure path.
type errBodyRT struct{}

type errBody struct{}

func (errBody) Read([]byte) (int, error) { return 0, errors.New("read fail") }
func (errBody) Close() error             { return nil }

func (errBodyRT) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Body: errBody{}, Header: http.Header{}}, nil
}

func TestIsHomeChannel(t *testing.T) {
	for _, c := range []string{"home", "Home", " following ", "FOLLOWING"} {
		if !isHomeChannel(c) {
			t.Fatalf("%q should be a home channel", c)
		}
	}
	for _, c := range []string{"", "nasa", "@bob"} {
		if isHomeChannel(c) {
			t.Fatalf("%q should not be a home channel", c)
		}
	}
}

func TestSplitSession(t *testing.T) {
	sid, csrf := splitSession(" sessionid=SID%3Aabc; csrftoken=TOK; ds_user_id=9 ")
	if sid != "SID%3Aabc" || csrf != "TOK" {
		t.Fatalf("cookie-string split: sid=%q csrf=%q", sid, csrf)
	}
	if sid, csrf := splitSession("bareSID"); sid != "bareSID" || csrf != "" {
		t.Fatalf("bare split: sid=%q csrf=%q", sid, csrf)
	}
	if v := cookieValue("a=1; b=2", "missing"); v != "" {
		t.Fatalf("cookieValue missing = %q", v)
	}
	// A malformed segment without "=" is skipped.
	if v := cookieValue("novalue; k=v", "k"); v != "v" {
		t.Fatalf("cookieValue after malformed segment = %q", v)
	}
}

func TestHomeFeedNeedsSession(t *testing.T) {
	// A provider with no sessionid (the NewWithClient fake path) prompts to sign in.
	p := NewWithClient(&fakeClient{})
	_, err := p.Feed(context.Background(), source.Query{Channel: "home"})
	if ae, ok := source.AsAuthError(err); !ok || ae.Kind != source.Instagram {
		t.Fatalf("want Instagram AuthError, got %v", err)
	}
}

// homeJSON is a canned feed/timeline response: an ad entry (skipped), an organic
// video post, and an organic image-only post.
const homeJSON = `{
  "feed_items": [
    {"suggested_users": {"x": 1}},
    {"media_or_ad": {
      "pk": "111", "code": "ABC", "caption": {"text": "a reel"},
      "user": {"username": "alice"}, "like_count": 7, "comment_count": 2,
      "taken_at": 1700000000,
      "image_versions2": {"candidates": [{"url": "cover.jpg", "width": 640, "height": 640}]},
      "video_versions": [{"url": "clip.mp4", "width": 720, "height": 1280}]
    }},
    {"media_or_ad": {
      "pk": "222", "code": "", "user": {"username": "bob"},
      "image_versions2": {"candidates": [{"url": "photo.jpg"}]}
    }}
  ],
  "next_max_id": "NEXT"
}`

func TestHomeFeedMapsAndSendsAuth(t *testing.T) {
	var gotAppID, gotCSRF, gotCookie, gotBody, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotAppID = r.Header.Get("X-IG-App-ID")
		gotCSRF = r.Header.Get("X-CSRFToken")
		gotCookie = r.Header.Get("Cookie")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_, _ = io.WriteString(w, homeJSON)
	}))
	defer srv.Close()

	p := &Provider{sessionID: "SID", csrf: "TOK", appID: "APPID", homeBase: srv.URL, hc: srv.Client()}
	res, err := p.Feed(context.Background(), source.Query{Channel: "following", Limit: 5, Cursor: "C1"})
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("method = %s", gotMethod)
	}
	if gotAppID != "APPID" {
		t.Fatalf("x-ig-app-id = %q", gotAppID)
	}
	if gotCSRF != "TOK" {
		t.Fatalf("x-csrftoken = %q", gotCSRF)
	}
	if !strings.Contains(gotCookie, "sessionid=SID") || !strings.Contains(gotCookie, "csrftoken=TOK") {
		t.Fatalf("cookie = %q", gotCookie)
	}
	if !strings.Contains(gotBody, "reason=cold_start_fetch") || !strings.Contains(gotBody, "max_id=C1") {
		t.Fatalf("body = %q", gotBody)
	}
	if res.Cursor != "NEXT" {
		t.Fatalf("cursor = %q", res.Cursor)
	}
	if len(res.Items) != 2 {
		t.Fatalf("items = %d (ad entry should be skipped)", len(res.Items))
	}
	a := res.Items[0]
	if a.ID != "ABC" || a.Author != "alice" || a.Body != "a reel" || a.Score != 7 || a.Comments != 2 ||
		a.Created != 1700000000 || a.Channel != "home" || a.Permalink != "https://www.instagram.com/p/ABC/" {
		t.Fatalf("item A = %+v", a)
	}
	if len(a.Media) != 2 || a.Media[0].Kind != source.MediaImage || a.Media[1].Kind != source.MediaVideo {
		t.Fatalf("item A media = %+v", a.Media)
	}
	b := res.Items[1]
	if b.ID != "222" || b.Author != "bob" || b.Permalink != "" || len(b.Media) != 1 ||
		b.Media[0].Kind != source.MediaImage {
		t.Fatalf("item B = %+v", b)
	}
}

func TestHomeFeedLimitAndBareSession(t *testing.T) {
	var gotCSRF, gotCookie string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCSRF = r.Header.Get("X-CSRFToken")
		gotCookie = r.Header.Get("Cookie")
		_, _ = io.WriteString(w, homeJSON)
	}))
	defer srv.Close()

	// A bare sessionid (no csrf): no X-CSRFToken header, cookie carries only sessionid.
	p := &Provider{sessionID: "SID", appID: "A", homeBase: srv.URL, hc: srv.Client()}
	res, err := p.Feed(context.Background(), source.Query{Channel: "home", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if gotCSRF != "" {
		t.Fatalf("bare session should send no csrf header, got %q", gotCSRF)
	}
	if strings.Contains(gotCookie, "csrftoken") {
		t.Fatalf("bare session cookie should omit csrftoken, got %q", gotCookie)
	}
	if len(res.Items) != 1 {
		t.Fatalf("limit not applied: %d", len(res.Items))
	}
}

func TestHomeFeedNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	p := &Provider{sessionID: "SID", homeBase: srv.URL, hc: srv.Client()}
	_, err := p.homeFeed(context.Background(), source.Query{})
	if _, ok := source.AsAuthError(err); !ok {
		t.Fatalf("403 should map to AuthError, got %v", err)
	}
}

func TestHomeFeedBadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "<html>login</html>")
	}))
	defer srv.Close()
	p := &Provider{sessionID: "SID", homeBase: srv.URL, hc: srv.Client()}
	_, err := p.homeFeed(context.Background(), source.Query{})
	if _, ok := source.AsAuthError(err); !ok {
		t.Fatalf("non-JSON body should map to AuthError, got %v", err)
	}
}

func TestHomeFeedRequestBuildError(t *testing.T) {
	p := &Provider{sessionID: "SID", homeBase: "http://\x7f-bad", hc: http.DefaultClient}
	if _, err := p.homeFeed(context.Background(), source.Query{}); err == nil {
		t.Fatal("want request-build error")
	}
}

func TestHomeFeedTransportError(t *testing.T) {
	p := &Provider{sessionID: "SID", homeBase: "http://example.invalid", hc: &http.Client{Transport: errRT{}}}
	if _, err := p.homeFeed(context.Background(), source.Query{}); err == nil {
		t.Fatal("want transport error")
	}
}

func TestHomeFeedBodyReadError(t *testing.T) {
	p := &Provider{sessionID: "SID", homeBase: "http://example.invalid", hc: &http.Client{Transport: errBodyRT{}}}
	if _, err := p.homeFeed(context.Background(), source.Query{}); err == nil {
		t.Fatal("want body read error")
	}
}
