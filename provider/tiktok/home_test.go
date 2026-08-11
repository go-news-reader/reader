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

func TestIsHomeChannel(t *testing.T) {
	if !isHomeChannel("home") || !isHomeChannel(" Following ") {
		t.Fatal("home/following should be home channels")
	}
	if isHomeChannel("secUid123") || isHomeChannel("") {
		t.Fatal("non-reserved channel misclassified")
	}
}

func TestFirstNonEmpty(t *testing.T) {
	if firstNonEmpty("", "b", "c") != "b" || firstNonEmpty("", "") != "" {
		t.Fatal("firstNonEmpty")
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

// TestFollowingMapsSignsAndSendsCreds drives the home feed through the library's
// signed request: the httptest server stands in for TikTok, and the provider is
// expected to sign (X-Bogus), send the imported msToken/sessionid, and map the
// returned videos.
func TestFollowingMapsSignsAndSendsCreds(t *testing.T) {
	var gotQuery, gotCookies, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		gotCookies = r.Header.Get("Cookie")
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(followingJSON))
	}))
	defer srv.Close()

	p := &Provider{hasCred: true, msToken: "MSTOK", session: "SID", homeBase: srv.URL, hc: srv.Client()}
	res, err := p.Feed(context.Background(), source.Query{Channel: "home", Limit: 5, Cursor: "20"})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/following/item_list/" {
		t.Fatalf("path = %q", gotPath)
	}
	if !strings.Contains(gotQuery, "X-Bogus=") {
		t.Fatalf("request not signed: %q", gotQuery)
	}
	if !strings.Contains(gotQuery, "msToken=MSTOK") {
		t.Fatalf("msToken param missing: %q", gotQuery)
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

// TestFollowingNoMoreCursor also covers the empty-msToken/empty-session branches
// of gottClient (hasCred forced without importing tokens).
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

func TestFollowingEmptyBodyWall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()
	p := &Provider{hasCred: true, homeBase: srv.URL, hc: srv.Client()}
	_, err := p.followingFeed(context.Background(), source.Query{})
	ae, ok := source.AsAuthError(err)
	if !ok || ae.Kind != source.TikTok {
		t.Fatalf("empty body should map to AuthError, got %v", err)
	}
	if !strings.Contains(ae.Reason, "msToken") {
		t.Errorf("reason = %q, want the msToken wall", ae.Reason)
	}
}

func TestFollowingStatusCodeWall(t *testing.T) {
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

// TestFollowingTransportWall also covers the empty-homeBase branch of gottClient
// (the default origin is used, but the injected transport never hits network).
func TestFollowingTransportWall(t *testing.T) {
	p := &Provider{hasCred: true, homeBase: "", hc: &http.Client{Transport: errRT{}}}
	_, err := p.followingFeed(context.Background(), source.Query{})
	if _, ok := source.AsAuthError(err); !ok {
		t.Fatalf("transport error should map to the wall AuthError, got %v", err)
	}
}
