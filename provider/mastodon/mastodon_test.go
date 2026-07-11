package mastodon

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	gomasto "github.com/go-mastodon/mastodon"

	"github.com/go-news-reader/reader/source"
)

type fakeClient struct {
	tl       *gomasto.Timeline
	err      error
	called   string
	gotTag   string
	gotAcct  string
	gotLimit int
	gotMax   string
}

func (f *fakeClient) PublicTimeline(_ context.Context, o gomasto.TimelineOptions) (*gomasto.Timeline, error) {
	f.called, f.gotLimit, f.gotMax = "public", o.Limit, o.MaxID
	return f.tl, f.err
}
func (f *fakeClient) HashtagTimeline(_ context.Context, tag string, o gomasto.TimelineOptions) (*gomasto.Timeline, error) {
	f.called, f.gotTag, f.gotLimit = "tag", tag, o.Limit
	return f.tl, f.err
}
func (f *fakeClient) AccountStatuses(_ context.Context, acct string, o gomasto.TimelineOptions) (*gomasto.Timeline, error) {
	f.called, f.gotAcct = "acct", acct
	return f.tl, f.err
}

func TestNewWithHTTPClient(t *testing.T) {
	if p := NewWithHTTPClient(&http.Client{}, "https://mastodon.social", "tok"); p.client == nil {
		t.Fatal("client not set from injected HTTP client")
	}
}

func TestKindAndNew(t *testing.T) {
	if New("https://mastodon.social", "").Kind() != source.Mastodon {
		t.Fatal("kind")
	}
	if New("https://m", "tok") == nil {
		t.Fatal("token ctor nil")
	}
}

func TestFeedPublic(t *testing.T) {
	f := &fakeClient{tl: &gomasto.Timeline{MaxID: "99", Statuses: []gomasto.Status{{
		ID: "1", URL: "https://m/@a/1", Content: "<p>hi</p>",
		CreatedAt: time.Unix(1700000000, 0), Account: gomasto.Account{Acct: "a@m"},
		Favourites: 5, Replies: 2, Sensitive: true, SpoilerText: "cw",
		Media: []gomasto.Media{{Type: "image", URL: "u1"}, {Type: "video", URL: "u2"}, {Type: "gifv", URL: "u3"}, {Type: "audio", URL: "u4"}, {Type: "other", URL: "u5"}},
		Tags:  []gomasto.Tag{{Name: "go"}},
	}}}}
	p := NewWithClient(f)
	res, err := p.Feed(context.Background(), source.Query{Limit: 10, Cursor: "50"})
	if err != nil {
		t.Fatal(err)
	}
	if f.called != "public" || f.gotLimit != 10 || f.gotMax != "50" {
		t.Fatalf("dispatch %+v", f)
	}
	if res.Cursor != "99" || len(res.Items) != 1 {
		t.Fatalf("res %+v", res)
	}
	it := res.Items[0]
	if it.Title != "cw" || it.Author != "a@m" || it.Score != 5 || it.Comments != 2 || !it.NSFW || it.Created != 1700000000 {
		t.Fatalf("item %+v", it)
	}
	wantKinds := []source.MediaKind{source.MediaImage, source.MediaVideo, source.MediaGIF, source.MediaAudio, source.MediaImage}
	if len(it.Media) != 5 {
		t.Fatalf("media %+v", it.Media)
	}
	for i, k := range wantKinds {
		if it.Media[i].Kind != k {
			t.Fatalf("media[%d]=%v want %v", i, it.Media[i].Kind, k)
		}
	}
	if len(it.Tags) != 1 || it.Tags[0] != "go" {
		t.Fatalf("tags %v", it.Tags)
	}
}

func TestFeedHashtagAndAccount(t *testing.T) {
	f := &fakeClient{tl: &gomasto.Timeline{}}
	p := NewWithClient(f)
	if _, err := p.Feed(context.Background(), source.Query{Channel: "#golang"}); err != nil {
		t.Fatal(err)
	}
	if f.called != "tag" || f.gotTag != "golang" {
		t.Fatalf("tag dispatch %+v", f)
	}
	if _, err := p.Feed(context.Background(), source.Query{Channel: "@bob@m"}); err != nil {
		t.Fatal(err)
	}
	if f.called != "acct" || f.gotAcct != "bob@m" {
		t.Fatalf("acct dispatch %+v", f)
	}
}

func TestFeedError(t *testing.T) {
	p := NewWithClient(&fakeClient{err: errors.New("boom")})
	if _, err := p.Feed(context.Background(), source.Query{}); err == nil {
		t.Fatal("want error")
	}
}

func TestAuthorName(t *testing.T) {
	if got := authorName(gomasto.Account{Acct: "x"}); got != "x" {
		t.Fatal(got)
	}
	if got := authorName(gomasto.Account{Username: "u"}); got != "u" {
		t.Fatal(got)
	}
	if got := authorName(gomasto.Account{DisplayName: "D"}); got != "D" {
		t.Fatal(got)
	}
}
