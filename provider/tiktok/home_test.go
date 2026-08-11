package tiktok

import (
	"context"
	"errors"
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
	if isHomeChannel("secUid123") || isHomeChannel("") {
		t.Fatal("non-reserved channel misclassified")
	}
}

func TestBoolNumber(t *testing.T) {
	cases := map[string]bool{`true`: true, `false`: false, `"1"`: true, `0`: false, `"true"`: true}
	for in, want := range cases {
		var n boolNumber
		if err := n.UnmarshalJSON([]byte(in)); err != nil {
			t.Fatal(err)
		}
		if bool(n) != want {
			t.Fatalf("%s -> %v, want %v", in, bool(n), want)
		}
	}
}

func TestFirstNonEmpty(t *testing.T) {
	if firstNonEmpty("", "b", "c") != "b" || firstNonEmpty("", "") != "" {
		t.Fatal("firstNonEmpty")
	}
}

func TestSnippet(t *testing.T) {
	if snippet([]byte("short")) != "short" {
		t.Fatal("short snippet")
	}
	long := strings.Repeat("x", 200)
	if s := snippet([]byte(long)); !strings.HasSuffix(s, "…") || len(s) >= 200 {
		t.Fatalf("long snippet not truncated: %d", len(s))
	}
}

func TestFollowingNeedsCred(t *testing.T) {
	p := NewWithClient(&fakeClient{})
	_, err := p.Feed(context.Background(), source.Query{Channel: "following"})
	if ae, ok := source.AsAuthError(err); !ok || ae.Kind != source.TikTok {
		t.Fatalf("want TikTok AuthError, got %v", err)
	}
}

const followingJSON = `{
  "statusCode": 0, "cursor": "40", "hasMore": true,
  "itemList": [
    {"id": "v1", "desc": "hi", "author": {"uniqueId": "creator"},
     "video": {"cover": "cov.jpg", "playAddr": "play.mp4"},
     "stats": {"diggCount": 9, "commentCount": 2}, "createTime": 1700000000},
    {"id": "", "author": {"uniqueId": ""}, "video": {}}
  ]
}`

func TestFollowingMapsAndSendsCreds(t *testing.T) {
	var gotMsParam, gotCookies string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMsParam = r.URL.Query().Get("msToken")
		gotCookies = r.Header.Get("Cookie")
		_, _ = w.Write([]byte(followingJSON))
	}))
	defer srv.Close()

	p := &Provider{hasCred: true, msToken: "MSTOK", session: "SID", homeBase: srv.URL, hc: srv.Client()}
	res, err := p.Feed(context.Background(), source.Query{Channel: "home", Limit: 5, Cursor: "20"})
	if err != nil {
		t.Fatal(err)
	}
	if gotMsParam != "MSTOK" {
		t.Fatalf("msToken param = %q", gotMsParam)
	}
	if !strings.Contains(gotCookies, "msToken=MSTOK") || !strings.Contains(gotCookies, "sessionid=SID") {
		t.Fatalf("cookies = %q", gotCookies)
	}
	if res.Cursor != "40" {
		t.Fatalf("cursor = %q", res.Cursor)
	}
	if len(res.Items) != 2 {
		t.Fatalf("items = %d", len(res.Items))
	}
	a := res.Items[0]
	if a.ID != "v1" || a.Channel != "home" || a.Author != "creator" || a.Body != "hi" || a.Score != 9 ||
		a.Comments != 2 || a.Created != 1700000000 || a.Permalink != "https://www.tiktok.com/@creator/video/v1" {
		t.Fatalf("item A = %+v", a)
	}
	if len(a.Media) != 2 || a.Media[0].Kind != source.MediaThumbnail || a.Media[1].Kind != source.MediaVideo {
		t.Fatalf("item A media = %+v", a.Media)
	}
	b := res.Items[1]
	if b.Permalink != "" || len(b.Media) != 0 {
		t.Fatalf("item B should have no permalink/media: %+v", b)
	}
}

func TestFollowingNoMoreCursor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"statusCode":0,"cursor":"40","hasMore":false,"itemList":[]}`))
	}))
	defer srv.Close()
	p := &Provider{hasCred: true, homeBase: srv.URL, hc: srv.Client()}
	res, err := p.followingFeed(context.Background(), source.Query{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Cursor != "" {
		t.Fatalf("cursor should be empty when hasMore=false, got %q", res.Cursor)
	}
}

func TestFollowingNon2xxWall(t *testing.T) {
	body := `{"statusCode":10201,"statusMsg":"missing required fields ` + strings.Repeat("x", 200) + `"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	p := &Provider{hasCred: true, homeBase: srv.URL, hc: srv.Client()}
	_, err := p.followingFeed(context.Background(), source.Query{})
	if _, ok := source.AsAuthError(err); !ok {
		t.Fatalf("400 wall should map to AuthError, got %v", err)
	}
}

func TestFollowingEmptyBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()
	p := &Provider{hasCred: true, homeBase: srv.URL, hc: srv.Client()}
	_, err := p.followingFeed(context.Background(), source.Query{})
	if _, ok := source.AsAuthError(err); !ok {
		t.Fatalf("empty body should map to AuthError, got %v", err)
	}
}

func TestFollowingStatusCodeNonZero(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"statusCode":10201,"statusMsg":"blocked","itemList":[]}`))
	}))
	defer srv.Close()
	p := &Provider{hasCred: true, homeBase: srv.URL, hc: srv.Client()}
	_, err := p.followingFeed(context.Background(), source.Query{})
	if _, ok := source.AsAuthError(err); !ok {
		t.Fatalf("statusCode!=0 should map to AuthError, got %v", err)
	}
}

func TestFollowingBadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()
	p := &Provider{hasCred: true, homeBase: srv.URL, hc: srv.Client()}
	if _, err := p.followingFeed(context.Background(), source.Query{}); err == nil {
		t.Fatal("want decode error")
	}
}

func TestFollowingRequestBuildError(t *testing.T) {
	p := &Provider{hasCred: true, homeBase: "http://\x7f-bad", hc: http.DefaultClient}
	if _, err := p.followingFeed(context.Background(), source.Query{}); err == nil {
		t.Fatal("want request-build error")
	}
}

func TestFollowingTransportError(t *testing.T) {
	p := &Provider{hasCred: true, homeBase: "http://example.invalid", hc: &http.Client{Transport: errRT{}}}
	if _, err := p.followingFeed(context.Background(), source.Query{}); err == nil {
		t.Fatal("want transport error")
	}
}

func TestFollowingBodyReadError(t *testing.T) {
	p := &Provider{hasCred: true, homeBase: "http://example.invalid", hc: &http.Client{Transport: errBodyRT{}}}
	if _, err := p.followingFeed(context.Background(), source.Query{}); err == nil {
		t.Fatal("want body read error")
	}
}
