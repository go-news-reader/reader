package lemmy

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	golem "github.com/go-lemmy/lemmy"

	"github.com/go-news-reader/reader/source"
)

type fakeClient struct {
	list        *golem.PostList
	communities *golem.CommunityList
	err         error
	got         golem.PostsOptions
	gotQuery    string
}

func (f *fakeClient) Posts(_ context.Context, opts golem.PostsOptions) (*golem.PostList, error) {
	f.got = opts
	return f.list, f.err
}

func (f *fakeClient) SearchCommunities(_ context.Context, query string, _ int) (*golem.CommunityList, error) {
	f.gotQuery = query
	return f.communities, f.err
}

func TestNewWithHTTPClient(t *testing.T) {
	if p := NewWithHTTPClient(&http.Client{}, "https://lemmy.world"); p.client == nil {
		t.Fatal("client not set from injected HTTP client")
	}
}

func TestKindAndNew(t *testing.T) {
	if New("https://lemmy.world").Kind() != source.Lemmy {
		t.Fatal("kind")
	}
}

func TestFeedMapAndPaging(t *testing.T) {
	f := &fakeClient{list: &golem.PostList{Posts: []golem.Post{{
		ID: 7, Title: "T", URL: "https://x", Body: "b", Permalink: "https://lemmy.world/post/7",
		ThumbnailURL: "https://t", Published: time.Unix(1700000000, 0), NSFW: true,
		Creator: "alice", Community: "tech", Score: 9, Comments: 4,
	}}}}
	p := NewWithClient(f)
	res, err := p.Feed(context.Background(), source.Query{Channel: "tech", Sort: "New", Limit: 20, Cursor: "3"})
	if err != nil {
		t.Fatal(err)
	}
	if f.got.Community != "tech" || f.got.Sort != "New" || f.got.Limit != 20 || f.got.Page != 3 {
		t.Fatalf("opts %+v", f.got)
	}
	if res.Cursor != "4" {
		t.Fatalf("cursor=%q, want 4", res.Cursor)
	}
	it := res.Items[0]
	if it.ID != "7" || it.Channel != "tech" || it.Author != "alice" || it.Link != "https://x" ||
		it.Permalink != "https://lemmy.world/post/7" || it.Score != 9 || it.Comments != 4 ||
		!it.NSFW || it.Created != 1700000000 {
		t.Fatalf("item %+v", it)
	}
	if len(it.Media) != 1 || it.Media[0].Kind != source.MediaThumbnail {
		t.Fatalf("media %+v", it.Media)
	}
}

func TestFeedDefaultPageAndEmptyCursor(t *testing.T) {
	f := &fakeClient{list: &golem.PostList{}} // empty -> no next cursor
	p := NewWithClient(f)
	res, err := p.Feed(context.Background(), source.Query{Channel: "tech"})
	if err != nil {
		t.Fatal(err)
	}
	if f.got.Page != 1 {
		t.Fatalf("default page = %d", f.got.Page)
	}
	if res.Cursor != "" {
		t.Fatalf("empty result should have no cursor, got %q", res.Cursor)
	}
}

func TestFeedBadCursor(t *testing.T) {
	f := &fakeClient{list: &golem.PostList{}}
	// Non-numeric and non-positive cursors fall back to page 1.
	_, _ = NewWithClient(f).Feed(context.Background(), source.Query{Cursor: "abc"})
	if f.got.Page != 1 {
		t.Fatalf("bad cursor page = %d", f.got.Page)
	}
	_, _ = NewWithClient(f).Feed(context.Background(), source.Query{Cursor: "0"})
	if f.got.Page != 1 {
		t.Fatalf("zero cursor page = %d", f.got.Page)
	}
}

func TestFeedError(t *testing.T) {
	p := NewWithClient(&fakeClient{err: errors.New("boom")})
	if _, err := p.Feed(context.Background(), source.Query{}); err == nil {
		t.Fatal("want error")
	}
}

func TestMapPostNoThumbnail(t *testing.T) {
	it := mapPost(golem.Post{ID: 1})
	if len(it.Media) != 0 {
		t.Fatalf("no thumbnail should yield no media: %+v", it.Media)
	}
}

func TestFeedAuthError(t *testing.T) {
	p := NewWithClient(&fakeClient{err: errors.New("lemmy: unexpected status 401: unauthorized")})
	_, err := p.Feed(context.Background(), source.Query{Channel: "tech"})
	if ae, ok := source.AsAuthError(err); !ok || ae.Kind != source.Lemmy {
		t.Fatalf("401 not mapped to Lemmy AuthError: %v", err)
	}
	p2 := NewWithClient(&fakeClient{err: errors.New("boom")})
	_, err = p2.Feed(context.Background(), source.Query{Channel: "tech"})
	if _, ok := source.AsAuthError(err); ok {
		t.Fatalf("transient error misclassified as auth: %v", err)
	}
}

func TestSearchChannelsMapsCommunities(t *testing.T) {
	f := &fakeClient{communities: &golem.CommunityList{Communities: []golem.Community{
		{Name: "golang", Title: "Go", Description: "gophers", Icon: "https://i/ic.png", ActorID: "https://lemmy.world/c/golang", Subscribers: 1234},
		{Name: "rust", ActorID: "https://lemmy.ml/c/rust"}, // no title → title falls back to name; different instance
		{Name: ""}, // skipped
	}}}
	p := NewWithClient(f)

	rs, err := p.SearchChannels(context.Background(), "prog")
	if err != nil {
		t.Fatalf("SearchChannels: %v", err)
	}
	if f.gotQuery != "prog" {
		t.Fatalf("query passed = %q", f.gotQuery)
	}
	if len(rs) != 2 {
		t.Fatalf("results = %d, want 2 (empty name skipped)", len(rs))
	}
	want := source.ChannelResult{Source: source.Lemmy, Channel: "golang@lemmy.world", Title: "Go", Description: "gophers", Subscribers: 1234, IconURL: "https://i/ic.png"}
	if rs[0] != want {
		t.Fatalf("result[0] = %+v, want %+v", rs[0], want)
	}
	if rs[1].Channel != "rust@lemmy.ml" || rs[1].Title != "rust" { // federated handle + title fallback
		t.Fatalf("result[1] = %+v", rs[1])
	}
	if got := rs[0].Subscription(); got.Source != source.Lemmy || got.Channel != "golang@lemmy.world" {
		t.Fatalf("Subscription() = %+v", got)
	}
}

func TestSearchChannelsBareNameWhenNoActor(t *testing.T) {
	f := &fakeClient{communities: &golem.CommunityList{Communities: []golem.Community{
		{Name: "local"}, // no actor_id → bare name
	}}}
	rs, err := NewWithClient(f).SearchChannels(context.Background(), "l")
	if err != nil || len(rs) != 1 || rs[0].Channel != "local" {
		t.Fatalf("bare-name channel failed: rs=%+v err=%v", rs, err)
	}
}

func TestSearchChannelsPropagatesError(t *testing.T) {
	f := &fakeClient{err: errors.New("boom")}
	if _, err := NewWithClient(f).SearchChannels(context.Background(), "x"); err == nil {
		t.Fatal("client error should propagate")
	}
}

func TestSearchChannelsInvalidActorFallsBackToBareName(t *testing.T) {
	f := &fakeClient{communities: &golem.CommunityList{Communities: []golem.Community{
		{Name: "weird", ActorID: "http://%zz"}, // unparseable actor_id (bad %-escape)
	}}}
	rs, err := NewWithClient(f).SearchChannels(context.Background(), "w")
	if err != nil || len(rs) != 1 || rs[0].Channel != "weird" {
		t.Fatalf("invalid actor_id should fall back to the bare name: rs=%+v err=%v", rs, err)
	}
}
