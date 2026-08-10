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
	page        *goreddit.Page
	err         error
	sawFront    bool
	sawSub      string
	sawUser     string
	sawSort     goreddit.Sort
	sawOptions  goreddit.ListingOptions
	pwc         *goreddit.PostWithComments
	commentsErr error
	sawCmtSub   string
	sawCmtID    string
	mySubs      []goreddit.SubredditInfo
	mySubsErr   error

	// Search seams.
	subPage       *goreddit.SubredditPage
	subSearchErr  error
	sawSubQuery   string
	postSearchErr error
	sawSearchQ    string
	sawSearchSub  string
	sawSearchSort goreddit.SearchSort
}

func (f *fakeFetcher) SearchSubreddits(_ context.Context, query string, _ goreddit.ListingOptions) (*goreddit.SubredditPage, error) {
	f.sawSubQuery = query
	return f.subPage, f.subSearchErr
}

func (f *fakeFetcher) SearchPosts(_ context.Context, query, subreddit string, sort goreddit.SearchSort, _ goreddit.ListingOptions) (*goreddit.Page, error) {
	f.sawSearchQ, f.sawSearchSub, f.sawSearchSort = query, subreddit, sort
	return f.page, f.err
}

func (f *fakeFetcher) MySubreddits(_ context.Context) ([]goreddit.SubredditInfo, error) {
	return f.mySubs, f.mySubsErr
}

func (f *fakeFetcher) Subreddit(_ context.Context, name string, sort goreddit.Sort, opts goreddit.ListingOptions) (*goreddit.Page, error) {
	f.sawSub, f.sawSort, f.sawOptions = name, sort, opts
	return f.page, f.err
}

func (f *fakeFetcher) UserPosts(_ context.Context, name string, sort goreddit.Sort, opts goreddit.ListingOptions) (*goreddit.Page, error) {
	f.sawUser, f.sawSort, f.sawOptions = name, sort, opts
	return f.page, f.err
}

func (f *fakeFetcher) Frontpage(_ context.Context, sort goreddit.Sort, opts goreddit.ListingOptions) (*goreddit.Page, error) {
	f.sawFront, f.sawSort, f.sawOptions = true, sort, opts
	return f.page, f.err
}

func (f *fakeFetcher) Comments(_ context.Context, subreddit, id string, _ goreddit.ListingOptions) (*goreddit.PostWithComments, error) {
	f.sawCmtSub, f.sawCmtID = subreddit, id
	return f.pwc, f.commentsErr
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
	// The provider forwards the channel verbatim; goreddit's Subreddit strips the
	// r/ itself, so the fake sees the prefixed form.
	if f.sawSub != "r/pics" {
		t.Fatalf("subreddit = %q, want r/pics", f.sawSub)
	}
	if f.sawSort != goreddit.SortHot {
		t.Fatalf("default sort = %v, want hot", f.sawSort)
	}
	it := res.Items[0]
	// Items are tagged with the subscription's own (prefixed) channel so the
	// feed's per-subscription filter matches.
	if it.Channel != "r/pics" {
		t.Fatalf("item channel = %q, want r/pics (the subscription form)", it.Channel)
	}
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

// TestFeedUserRoutesToUserPosts: a "u/" channel fetches the redditor's
// submissions (not a subreddit) and tags each item with the u/ channel so the
// per-subscription feed filter matches even though the posts span subreddits.
func TestFeedUserRoutesToUserPosts(t *testing.T) {
	f := &fakeFetcher{page: &goreddit.Page{Posts: []goreddit.Post{{
		ID: "u1", Subreddit: "AskReddit", Title: "A question", Author: "spez",
		Permalink: "/r/AskReddit/comments/u1/a_question/", IsSelf: true,
	}}}}
	p := NewWithClient(f)

	res, err := p.Feed(context.Background(), source.Query{Channel: "u/spez", Sort: "new"})
	if err != nil {
		t.Fatalf("Feed: %v", err)
	}
	if f.sawFront || f.sawSub != "" {
		t.Fatalf("u/ channel must not hit Frontpage or Subreddit (front=%v sub=%q)", f.sawFront, f.sawSub)
	}
	if f.sawUser != "u/spez" {
		t.Fatalf("user = %q, want u/spez (goreddit strips u/ itself)", f.sawUser)
	}
	if it := res.Items[0]; it.Channel != "u/spez" {
		t.Fatalf("item channel = %q, want u/spez (the subscription form, not r/AskReddit)", it.Channel)
	}
}

func TestFeedError(t *testing.T) {
	f := &fakeFetcher{err: errors.New("403")}
	if _, err := NewWithClient(f).Feed(context.Background(), source.Query{Channel: "x"}); err == nil {
		t.Fatal("want error propagated")
	}
}

func TestMySubscriptions(t *testing.T) {
	f := &fakeFetcher{mySubs: []goreddit.SubredditInfo{
		{Name: "golang"},
		{Name: ""}, // skipped: empty display name
		{Name: "rust"},
	}}
	got, err := NewWithClient(f).MySubscriptions(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"r/golang", "r/rust"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestMySubscriptionsError(t *testing.T) {
	f := &fakeFetcher{mySubsErr: &goreddit.APIError{StatusCode: 403, Status: "forbidden"}}
	_, err := NewWithClient(f).MySubscriptions(context.Background())
	if err == nil {
		t.Fatal("want error propagated")
	}
	// mapErr promotes a 403 to a typed auth error.
	var ae *source.AuthError
	if !errors.As(err, &ae) {
		t.Fatalf("want mapErr AuthError, got %T: %v", err, err)
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

// cmt is a terse constructor for a comment subtree in the flatten tests. The
// go-reddit Comment type has exported fields (including the Replies slice), so a
// nested tree can be built with composite literals directly.
func cmt(author, body string, score int, replies ...goreddit.Comment) goreddit.Comment {
	return goreddit.Comment{Author: author, Body: body, Score: score, CreatedUTC: 1710000000, Replies: replies}
}

// TestCommentsFlattenDepth builds a nested thread and asserts it flattens
// depth-first in order, carrying each comment's Depth, and that the subreddit +
// id are passed through to the client (the "t3_" prefix stripped by the client,
// but the provider forwards the raw id).
func TestCommentsFlattenDepth(t *testing.T) {
	f := &fakeFetcher{pwc: &goreddit.PostWithComments{Comments: []goreddit.Comment{
		cmt("alice", "top one", 10,
			cmt("bob", "reply to alice", 5,
				cmt("carol", "deep reply", 2),
			),
		),
		cmt("dave", "top two", 8),
	}}}
	p := NewWithClient(f)

	got, err := p.Comments(context.Background(), "t3_abc", "r/golang")
	if err != nil {
		t.Fatalf("Comments: %v", err)
	}
	if f.sawCmtSub != "r/golang" || f.sawCmtID != "t3_abc" {
		t.Fatalf("client saw sub=%q id=%q", f.sawCmtSub, f.sawCmtID)
	}
	want := []source.Comment{
		{Author: "alice", Body: "top one", Score: 10, Created: 1710000000, Depth: 0},
		{Author: "bob", Body: "reply to alice", Score: 5, Created: 1710000000, Depth: 1},
		{Author: "carol", Body: "deep reply", Score: 2, Created: 1710000000, Depth: 2},
		{Author: "dave", Body: "top two", Score: 8, Created: 1710000000, Depth: 0},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d comments, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("comment %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestCommentsSkipsDeletedKeepsReplies drops an empty-/whitespace-body node from
// the output but still visits its replies (a live reply under a removed parent
// survives), and the reply keeps its true depth.
func TestCommentsSkipsDeletedKeepsReplies(t *testing.T) {
	f := &fakeFetcher{pwc: &goreddit.PostWithComments{Comments: []goreddit.Comment{
		cmt("", "   ", 0, // deleted/removed parent: empty body
			cmt("eve", "still here", 3),
		),
	}}}
	got, err := NewWithClient(f).Comments(context.Background(), "abc", "")
	if err != nil {
		t.Fatalf("Comments: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 surviving reply, got %d: %+v", len(got), got)
	}
	if got[0].Author != "eve" || got[0].Depth != 1 {
		t.Fatalf("surviving reply = %+v, want eve at depth 1", got[0])
	}
}

// TestCommentsCountCap caps the flattened output at maxComments however many the
// thread carries.
func TestCommentsCountCap(t *testing.T) {
	var nodes []goreddit.Comment
	for i := 0; i < maxComments+50; i++ {
		nodes = append(nodes, cmt("u", "body", 1))
	}
	got, err := NewWithClient(&fakeFetcher{pwc: &goreddit.PostWithComments{Comments: nodes}}).
		Comments(context.Background(), "abc", "golang")
	if err != nil {
		t.Fatalf("Comments: %v", err)
	}
	if len(got) != maxComments {
		t.Fatalf("count cap: got %d, want %d", len(got), maxComments)
	}
}

// TestCommentsCountCapMidTree stops appending mid-tree: a first subtree that
// fills the cap leaves no room for a following top-level comment.
func TestCommentsCountCapMidTree(t *testing.T) {
	// A top-level comment with maxComments direct replies (1 + 200 candidates):
	// the parent + the first (maxComments-1) replies fill the cap.
	var replies []goreddit.Comment
	for i := 0; i < maxComments; i++ {
		replies = append(replies, cmt("r", "reply", 1))
	}
	nodes := []goreddit.Comment{cmt("p", "parent", 1, replies...), cmt("after", "later top", 1)}
	got, err := NewWithClient(&fakeFetcher{pwc: &goreddit.PostWithComments{Comments: nodes}}).
		Comments(context.Background(), "abc", "golang")
	if err != nil {
		t.Fatalf("Comments: %v", err)
	}
	if len(got) != maxComments {
		t.Fatalf("count cap mid-tree: got %d, want %d", len(got), maxComments)
	}
	for _, c := range got {
		if c.Author == "after" {
			t.Fatal("a comment past the count cap was included")
		}
	}
}

// TestCommentsDepthCap stops descending past maxCommentDepth levels: a chain
// deeper than the cap yields exactly maxCommentDepth comments (depths 0..cap-1).
func TestCommentsDepthCap(t *testing.T) {
	// Build a single chain 2*maxCommentDepth deep.
	leaf := cmt("d", "deepest", 1)
	node := leaf
	for i := 0; i < 2*maxCommentDepth; i++ {
		node = cmt("d", "body", 1, node)
	}
	got, err := NewWithClient(&fakeFetcher{pwc: &goreddit.PostWithComments{Comments: []goreddit.Comment{node}}}).
		Comments(context.Background(), "abc", "golang")
	if err != nil {
		t.Fatalf("Comments: %v", err)
	}
	if len(got) != maxCommentDepth {
		t.Fatalf("depth cap: got %d comments, want %d", len(got), maxCommentDepth)
	}
	for i, c := range got {
		if c.Depth != i {
			t.Fatalf("comment %d depth = %d, want %d", i, c.Depth, i)
		}
	}
}

// TestCommentsError maps a client failure through mapErr (an auth status becomes
// a typed AuthError; a plain error propagates).
func TestCommentsError(t *testing.T) {
	f := &fakeFetcher{commentsErr: &goreddit.APIError{StatusCode: 403, Status: "forbidden"}}
	_, err := NewWithClient(f).Comments(context.Background(), "abc", "golang")
	if ae, ok := source.AsAuthError(err); !ok || ae.Kind != source.Reddit {
		t.Fatalf("403 not mapped to Reddit AuthError: %v", err)
	}

	f2 := &fakeFetcher{commentsErr: errors.New("network down")}
	if _, err := NewWithClient(f2).Comments(context.Background(), "abc", "golang"); err == nil {
		t.Fatal("want error propagated")
	}
}

// A RedGIFs (or other embedded external video) post is tagged post_hint
// "rich:video" with the poster only in preview; it must show that poster image
// (rendered + zoomable via the web preview), not web-render the redgifs page.
func TestMapRichVideoEmbedPost(t *testing.T) {
	p := postFromJSON(t, `{"id":"rv1","subreddit":"gifs","title":"clip",
		"url":"https://www.redgifs.com/watch/somegif","permalink":"/r/gifs/comments/rv1/clip/",
		"post_hint":"rich:video","is_video":false,
		"preview":{"images":[{"source":{"url":"https://external-preview.redd.it/rv1.jpg?width=640&amp;auto=webp"}}]}}`)
	it := mapOne(t, p)
	if it.Link != "https://external-preview.redd.it/rv1.jpg?width=640&auto=webp" {
		t.Errorf("rich:video Link = %q, want the poster image (not the redgifs page)", it.Link)
	}
	if !hasMedia(it, source.MediaImage, "https://external-preview.redd.it/rv1.jpg?width=640&auto=webp") {
		t.Errorf("rich:video poster missing from media: %+v", it.Media)
	}
}

// A reddit-hosted video via post_hint "hosted:video" (belt-and-braces with the
// is_video flag) also resolves to its poster.
func TestMapHostedVideoHintPost(t *testing.T) {
	p := postFromJSON(t, `{"id":"hv1","subreddit":"videos","title":"v",
		"url":"https://v.redd.it/hv1","permalink":"/r/videos/comments/hv1/v/","post_hint":"hosted:video",
		"preview":{"images":[{"source":{"url":"https://external-preview.redd.it/hv1.jpg"}}]}}`)
	it := mapOne(t, p)
	if it.Link != "https://external-preview.redd.it/hv1.jpg" {
		t.Errorf("hosted:video Link = %q, want the poster", it.Link)
	}
}

// --- Search: subreddit discovery + post search + the search: feed channel ---

func TestSearchSubredditsMapsResults(t *testing.T) {
	f := &fakeFetcher{subPage: &goreddit.SubredditPage{
		After: "t5_next",
		Subreddits: []goreddit.SubredditInfo{
			{Name: "golang", Title: "The Go Programming Language", PublicDescription: "Gophers", Subscribers: 250000, Over18: false},
			{Name: "", Title: "skip me"}, // an empty name is skipped
			{Name: "nsfwsub", Title: "Spicy", Subscribers: 10, Over18: true},
		},
	}}
	p := NewWithClient(f)

	rs, err := p.SearchSubreddits(context.Background(), "  go  ")
	if err != nil {
		t.Fatalf("SearchSubreddits: %v", err)
	}
	if f.sawSubQuery != "go" {
		t.Fatalf("query passed to client = %q, want trimmed 'go'", f.sawSubQuery)
	}
	if len(rs) != 2 {
		t.Fatalf("results = %d, want 2 (empty name skipped)", len(rs))
	}
	if rs[0] != (source.SubredditResult{Name: "golang", Title: "The Go Programming Language", Description: "Gophers", Subscribers: 250000, NSFW: false}) {
		t.Fatalf("result[0] = %+v", rs[0])
	}
	if !rs[1].NSFW || rs[1].Name != "nsfwsub" {
		t.Fatalf("result[1] = %+v, want the NSFW sub", rs[1])
	}
}

func TestSearchSubredditsBlankQuery(t *testing.T) {
	p := NewWithClient(&fakeFetcher{})
	if _, err := p.SearchSubreddits(context.Background(), "   "); err == nil {
		t.Fatal("blank query should error")
	}
}

func TestSearchSubredditsPropagatesError(t *testing.T) {
	f := &fakeFetcher{subSearchErr: errors.New("boom")}
	if _, err := NewWithClient(f).SearchSubreddits(context.Background(), "go"); err == nil {
		t.Fatal("client error should propagate")
	}
}

func TestSearchPostsMapsAndScopes(t *testing.T) {
	f := &fakeFetcher{page: &goreddit.Page{Posts: []goreddit.Post{{
		ID: "s1", Subreddit: "golang", Title: "Generics", Author: "gopher",
		Permalink: "/r/golang/comments/s1/generics/", IsSelf: true,
	}}}}
	p := NewWithClient(f)

	// Site-wide (empty subreddit).
	items, err := p.SearchPosts(context.Background(), "generics", "")
	if err != nil {
		t.Fatalf("SearchPosts: %v", err)
	}
	if f.sawSearchQ != "generics" || f.sawSearchSub != "" || f.sawSearchSort != goreddit.SearchRelevance {
		t.Fatalf("client saw q=%q sub=%q sort=%q", f.sawSearchQ, f.sawSearchSub, f.sawSearchSort)
	}
	if len(items) != 1 || items[0].ID != "s1" || items[0].Channel != "golang" {
		t.Fatalf("mapped items = %+v", items)
	}

	// Restricted to a subreddit.
	if _, err := p.SearchPosts(context.Background(), "generics", "r/golang"); err != nil {
		t.Fatalf("SearchPosts(sub): %v", err)
	}
	if f.sawSearchSub != "r/golang" {
		t.Fatalf("subreddit passed = %q, want r/golang", f.sawSearchSub)
	}
}

func TestSearchPostsPropagatesError(t *testing.T) {
	f := &fakeFetcher{err: errors.New("nope")}
	if _, err := NewWithClient(f).SearchPosts(context.Background(), "go", ""); err == nil {
		t.Fatal("client error should propagate")
	}
}

// TestFeedSearchChannelRoutesToPostSearch: a "search:<query>" channel routes the
// feed to a site-wide post search and tags each item with the search: channel so
// the per-subscription feed filter matches.
func TestFeedSearchChannelRoutesToPostSearch(t *testing.T) {
	f := &fakeFetcher{page: &goreddit.Page{After: "t3_next", Posts: []goreddit.Post{{
		ID: "sc1", Subreddit: "golang", Title: "hit", Permalink: "/r/golang/comments/sc1/hit/", IsSelf: true,
	}}}}
	p := NewWithClient(f)

	res, err := p.Feed(context.Background(), source.Query{Channel: "search:go generics", Limit: 25})
	if err != nil {
		t.Fatalf("Feed: %v", err)
	}
	if f.sawFront || f.sawSub != "" || f.sawUser != "" {
		t.Fatalf("search channel must not hit Frontpage/Subreddit/UserPosts")
	}
	if f.sawSearchQ != "go generics" || f.sawSearchSub != "" || f.sawSearchSort != goreddit.SearchRelevance {
		t.Fatalf("search args wrong: q=%q sub=%q sort=%q", f.sawSearchQ, f.sawSearchSub, f.sawSearchSort)
	}
	if len(res.Items) != 1 {
		t.Fatalf("items = %d", len(res.Items))
	}
	// Tagged with the subscription's own search: channel (not the post's r/golang).
	if res.Items[0].Channel != "search:go generics" {
		t.Fatalf("item channel = %q, want the search: subscription form", res.Items[0].Channel)
	}
	if res.Cursor != "t3_next" {
		t.Fatalf("cursor = %q", res.Cursor)
	}
}

// A capitalised "Search:" prefix is accepted case-insensitively too.
func TestFeedSearchChannelCaseInsensitive(t *testing.T) {
	f := &fakeFetcher{page: &goreddit.Page{}}
	if _, err := NewWithClient(f).Feed(context.Background(), source.Query{Channel: "Search:foo"}); err != nil {
		t.Fatalf("Feed: %v", err)
	}
	if f.sawSearchQ != "foo" {
		t.Fatalf("case-insensitive search prefix not routed: q=%q", f.sawSearchQ)
	}
}
