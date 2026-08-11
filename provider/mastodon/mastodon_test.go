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

	// Follow-import seams.
	me           *gomasto.Account
	meErr        error
	followPages  []*gomasto.FollowingPage // returned in sequence, one per call
	followErr    error
	followCalls  int
	gotFollowID  string
	gotFollowMax []string
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
func (f *fakeClient) HomeTimeline(_ context.Context, o gomasto.TimelineOptions) (*gomasto.Timeline, error) {
	f.called, f.gotLimit, f.gotMax = "home", o.Limit, o.MaxID
	return f.tl, f.err
}
func (f *fakeClient) VerifyCredentials(_ context.Context) (*gomasto.Account, error) {
	return f.me, f.meErr
}
func (f *fakeClient) Following(_ context.Context, id string, o gomasto.TimelineOptions) (*gomasto.FollowingPage, error) {
	f.gotFollowID = id
	f.gotFollowMax = append(f.gotFollowMax, o.MaxID)
	if f.followErr != nil {
		return nil, f.followErr
	}
	pg := f.followPages[f.followCalls]
	f.followCalls++
	return pg, nil
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

func TestFeedHomeReserved(t *testing.T) {
	// The reserved "home" channel routes to the home timeline when authed.
	f := &fakeClient{tl: &gomasto.Timeline{}}
	p := NewWithClientAuthed(f)
	if _, err := p.Feed(context.Background(), source.Query{Channel: "home", Limit: 20}); err != nil {
		t.Fatal(err)
	}
	if f.called != "home" || f.gotLimit != 20 {
		t.Fatalf("home dispatch %+v", f)
	}
}

func TestFeedEmptyChannelAuthedIsHome(t *testing.T) {
	// With a token, an empty channel defaults to the home timeline.
	f := &fakeClient{tl: &gomasto.Timeline{}}
	if _, err := NewWithClientAuthed(f).Feed(context.Background(), source.Query{}); err != nil {
		t.Fatal(err)
	}
	if f.called != "home" {
		t.Fatalf("empty+authed should be home, got %q", f.called)
	}

	// Without a token, an empty channel stays the public timeline.
	g := &fakeClient{tl: &gomasto.Timeline{}}
	if _, err := NewWithClient(g).Feed(context.Background(), source.Query{}); err != nil {
		t.Fatal(err)
	}
	if g.called != "public" {
		t.Fatalf("empty+anon should be public, got %q", g.called)
	}
}

func TestFeedHomeNeedsAuth(t *testing.T) {
	// A "home" channel without a token is a typed prompt, not a public fallback.
	f := &fakeClient{tl: &gomasto.Timeline{}}
	_, err := NewWithClient(f).Feed(context.Background(), source.Query{Channel: "home"})
	if ae, ok := source.AsAuthError(err); !ok || ae.Kind != source.Mastodon {
		t.Fatalf("home without token should NeedsAuth, got %v", err)
	}
	if f.called != "" {
		t.Fatalf("no request should have been made, called=%q", f.called)
	}
}

func TestMyFollows(t *testing.T) {
	// Two pages of following, the first carrying a cursor; blank-acct skipped.
	f := &fakeClient{
		me: &gomasto.Account{ID: "42", Acct: "me"},
		followPages: []*gomasto.FollowingPage{
			{MaxID: "cur1", Accounts: []gomasto.Account{{Acct: "a@host"}, {Acct: ""}}},
			{MaxID: "", Accounts: []gomasto.Account{{Acct: "b"}}},
		},
	}
	subs, err := NewWithClientAuthed(f).MyFollows(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if f.gotFollowID != "42" {
		t.Fatalf("following queried id %q, want 42", f.gotFollowID)
	}
	if f.followCalls != 2 {
		t.Fatalf("following calls = %d, want 2 (paginated)", f.followCalls)
	}
	// The second page must echo the first page's cursor.
	if len(f.gotFollowMax) != 2 || f.gotFollowMax[0] != "" || f.gotFollowMax[1] != "cur1" {
		t.Fatalf("cursor round-trip wrong: %v", f.gotFollowMax)
	}
	want := []source.Subscription{
		{Source: source.Mastodon, Channel: "@a@host"},
		{Source: source.Mastodon, Channel: "@b"},
	}
	if len(subs) != len(want) {
		t.Fatalf("subs = %+v, want %+v", subs, want)
	}
	for i := range want {
		if subs[i] != want[i] {
			t.Fatalf("subs[%d] = %+v, want %+v", i, subs[i], want[i])
		}
	}
}

func TestMyFollowsVerifyError(t *testing.T) {
	// A 401 from verify_credentials maps to a typed auth prompt.
	f := &fakeClient{meErr: errors.New("mastodon: GET /x: unexpected status 401: nope")}
	_, err := NewWithClientAuthed(f).MyFollows(context.Background())
	if ae, ok := source.AsAuthError(err); !ok || ae.Kind != source.Mastodon {
		t.Fatalf("verify 401 not mapped to AuthError: %v", err)
	}
}

func TestMyFollowsFollowingError(t *testing.T) {
	// verify succeeds but paging the follows fails transiently.
	f := &fakeClient{
		me:        &gomasto.Account{ID: "42"},
		followErr: errors.New("dial tcp: connection refused"),
	}
	_, err := NewWithClientAuthed(f).MyFollows(context.Background())
	if err == nil {
		t.Fatal("want error propagated from Following")
	}
	if _, ok := source.AsAuthError(err); ok {
		t.Fatalf("transient error misclassified as auth: %v", err)
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

func TestFeedAuthError(t *testing.T) {
	// A 401/403 from the instance maps to a typed source.AuthError.
	p := NewWithClient(&fakeClient{err: errors.New("mastodon: GET /x: unexpected status 403: forbidden")})
	_, err := p.Feed(context.Background(), source.Query{})
	if ae, ok := source.AsAuthError(err); !ok || ae.Kind != source.Mastodon {
		t.Fatalf("403 not mapped to Mastodon AuthError: %v", err)
	}
	// A transient error is left untouched.
	p2 := NewWithClient(&fakeClient{err: errors.New("dial tcp: connection refused")})
	_, err = p2.Feed(context.Background(), source.Query{})
	if err == nil {
		t.Fatal("want transient error")
	}
	if _, ok := source.AsAuthError(err); ok {
		t.Fatalf("transient error misclassified as auth: %v", err)
	}
}
