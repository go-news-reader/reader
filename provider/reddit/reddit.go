// Package reddit adapts the standalone github.com/go-reddit/reddit client to
// the aggregator's source.Provider contract. Anonymous fetches go through the
// shared browserhttp uTLS client so Reddit's TLS-fingerprint anti-bot 403 does
// not fire; a session-cookie-authenticated client can be supplied instead (the
// user's own logged-in reddit_session), since Reddit's self-serve OAuth app
// registration is effectively closed to new personal projects.
package reddit

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-browserhttp/browserhttp"
	goreddit "github.com/go-reddit/reddit"

	"github.com/go-news-reader/reader/source"
)

// fetcher is the slice of *goreddit.Client the provider needs. Declaring it as
// an interface lets tests inject a fake without any network.
type fetcher interface {
	Subreddit(ctx context.Context, name string, sort goreddit.Sort, opts goreddit.ListingOptions) (*goreddit.Page, error)
	Frontpage(ctx context.Context, sort goreddit.Sort, opts goreddit.ListingOptions) (*goreddit.Page, error)
	Comments(ctx context.Context, subreddit, id string, opts goreddit.ListingOptions) (*goreddit.PostWithComments, error)
}

// Provider fetches Reddit posts as normalized source items.
type Provider struct {
	client fetcher
}

// New returns an anonymous Reddit provider backed by the portable browser
// fingerprint HTTP client (pure Go, CGO=0, no host web view).
func New() *Provider {
	return NewWithHTTPClient(browserhttp.NewClient(30 * time.Second))
}

// NewWithHTTPClient returns an anonymous Reddit provider driving hc (e.g. the
// shared, request-logging client the aggregator builds so the Network log can
// show Reddit's traffic). The browser User-Agent is kept so the fingerprint
// still matches.
func NewWithHTTPClient(hc *http.Client) *Provider {
	c := goreddit.NewClient(
		goreddit.WithHTTPClient(hc),
		goreddit.WithUserAgent(browserhttp.DefaultUserAgent),
	)
	return &Provider{client: c}
}

// NewWithCookie returns a Reddit provider that authenticates reads with the
// user's own logged-in browser reddit_session cookie, driving hc (the shared,
// request-logging client the aggregator builds). Reddit's self-serve OAuth
// registration is effectively closed to new personal projects, so supplying the
// cookie from an already-signed-in browser is the practical path for
// individual, read-only use — the same session-cookie pattern the reader
// already offers for Instagram and TikTok. The cookie keeps the anonymous
// www ".json" endpoints (no OAuth). A nil hc falls back to the portable
// browser-fingerprint client so the constructor is safe on its own.
func NewWithCookie(hc *http.Client, sessionCookie string) *Provider {
	if hc == nil {
		hc = browserhttp.NewClient(30 * time.Second)
	}
	c := goreddit.NewClient(
		goreddit.WithHTTPClient(hc),
		goreddit.WithUserAgent(browserhttp.DefaultUserAgent),
		goreddit.WithSessionCookie(sessionCookie),
	)
	return &Provider{client: c}
}

// NewWithClient wraps an already-configured reddit client — e.g. a
// session-cookie (logged-in) client, or a fake in tests.
func NewWithClient(c fetcher) *Provider { return &Provider{client: c} }

// Kind reports source.Reddit.
func (p *Provider) Kind() source.Kind { return source.Reddit }

// Feed returns a page of posts. An empty Query.Channel fetches the front page;
// otherwise it fetches r/<Channel>. Query.Cursor is the reddit "after" token.
func (p *Provider) Feed(ctx context.Context, q source.Query) (source.Result, error) {
	opts := goreddit.ListingOptions{Limit: q.Limit, After: q.Cursor}
	sort := parseSort(q.Sort)

	var page *goreddit.Page
	var err error
	if strings.TrimSpace(q.Channel) == "" {
		page, err = p.client.Frontpage(ctx, sort, opts)
	} else {
		page, err = p.client.Subreddit(ctx, strings.TrimPrefix(q.Channel, "r/"), sort, opts)
	}
	if err != nil {
		return source.Result{}, mapErr(err)
	}

	items := make([]source.Item, 0, len(page.Posts))
	for _, post := range page.Posts {
		items = append(items, mapPost(post))
	}
	return source.Result{Items: items, Cursor: page.After}, nil
}

// Comment-thread bounds: a very active post can carry tens of thousands of
// comments nested dozens deep, which would explode the pane's memory and layout
// time. Comments flattens at most maxComments nodes and stops descending past
// maxCommentDepth levels, so the preview stays bounded however large the thread.
const (
	maxComments     = 200
	maxCommentDepth = 8
)

// Comments fetches a post's comment thread and flattens it into display order
// with a per-comment Depth (0 = top-level reply). It goes through the same
// (anonymous or session-cookie) client as the feed, so the user's auth applies.
// The tree is walked depth-first, preserving the reddit ordering; "load more"
// frontier markers are already dropped by the client (only "t1" things survive),
// and deleted/empty-body nodes are skipped here while their replies are kept, so
// a live subtree under a removed parent is not lost.
func (p *Provider) Comments(ctx context.Context, postID, subreddit string) ([]source.Comment, error) {
	pwc, err := p.client.Comments(ctx, subreddit, postID, goreddit.ListingOptions{})
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]source.Comment, 0, len(pwc.Comments))
	flattenComments(pwc.Comments, 0, &out)
	return out, nil
}

// flattenComments walks the nested comment tree depth-first, appending each
// non-empty comment to out with its nesting depth. It stops descending once
// depth reaches maxCommentDepth and stops appending once out reaches
// maxComments, so a huge thread stays bounded. A deleted/removed comment (empty
// body) is dropped from the output but its replies are still visited (at the
// next depth), so real replies below a removed parent survive.
func flattenComments(nodes []goreddit.Comment, depth int, out *[]source.Comment) {
	if depth >= maxCommentDepth || len(*out) >= maxComments {
		return
	}
	for _, c := range nodes {
		if len(*out) >= maxComments {
			return
		}
		if strings.TrimSpace(c.Body) != "" {
			*out = append(*out, source.Comment{
				Author:  c.Author,
				Body:    c.Body,
				Score:   c.Score,
				Created: int64(c.CreatedUTC),
				Depth:   depth,
			})
		}
		flattenComments(c.Replies, depth+1, out)
	}
}

// mapErr translates a Reddit client failure into a typed source.AuthError when
// it is a 401/403 — the anonymous ".json" endpoints 403 from data-center IPs, and
// a stale/rejected session cookie returns 401 — so the UI can prompt the user to
// sign in instead of showing a raw status. Other failures (network, 429, 5xx)
// pass through unchanged as transient errors.
func mapErr(err error) error {
	var apiErr *goreddit.APIError
	if errors.As(err, &apiErr) && source.HTTPAuthStatus(apiErr.StatusCode) {
		return source.NeedsAuth(source.Reddit, "log into Reddit in your browser, then import the session cookie")
	}
	return err
}

// parseSort maps a generic sort hint onto reddit's vocabulary, defaulting to
// "hot" for empty or unrecognized values.
func parseSort(s string) goreddit.Sort {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "new":
		return goreddit.SortNew
	case "top":
		return goreddit.SortTop
	case "rising":
		return goreddit.SortRising
	case "controversial", "controvers":
		return goreddit.SortControvers
	case "best":
		return goreddit.SortBest
	default:
		return goreddit.SortHot
	}
}

// mapPost projects a reddit Post onto the normalized Item.
func mapPost(p goreddit.Post) source.Item {
	it := source.Item{
		ID:        p.ID,
		Source:    source.Reddit,
		Channel:   p.Subreddit,
		Title:     p.Title,
		Author:    p.Author,
		Body:      p.SelfText,
		Permalink: "https://www.reddit.com" + p.Permalink,
		Score:     p.Score,
		Comments:  p.NumComments,
		Created:   int64(p.CreatedUTC),
		NSFW:      p.Over18,
		Pinned:    p.Stickied,
	}
	if p.Flair != "" {
		it.Tags = []string{p.Flair}
	}
	if isThumbURL(p.Thumbnail) {
		it.Media = append(it.Media, source.Media{URL: p.Thumbnail, Kind: source.MediaThumbnail})
	}
	if p.IsSelf {
		return it // a text post: its content is Body, no link/media to resolve
	}

	// A media post — an image, a gallery, or a reddit-hosted video — shows a
	// resolved picture: the image itself, the gallery's first image, or the
	// video's poster. Point Link at that direct image (the preview pane renders
	// it) rather than at a gallery / v.redd.it permalink, which is a JavaScript
	// page that would paint blank. A plain external link falls through to its
	// article page. p.PreviewImage() prefers Reddit's resolved preview; it is a
	// direct image URL even when p.URL is a permalink.
	display := p.PreviewImage()
	isMedia := p.IsGallery || p.IsVideo || p.PostHint == "image" || isImageURL(p.URL)
	if isMedia && display != "" {
		it.Link = display
		it.Media = append(it.Media, source.Media{URL: display, Kind: source.MediaImage})
	} else {
		// A plain external article (or a media post whose image could not be
		// resolved): render the linked page. A real image URL is always caught
		// above (PreviewImage resolves it), so nothing is mapped as media here.
		it.Link = p.URL
	}
	// Expose a reddit-hosted video's stream as media (the poster shows now; a
	// future front-end could offer playback).
	if v := p.VideoURL(); v != "" {
		it.Media = append(it.Media, source.Media{URL: v, Kind: source.MediaVideo})
	}
	return it
}

// isThumbURL reports whether a reddit thumbnail field is a real image URL and
// not one of the sentinel strings reddit uses ("self", "default", "nsfw", …).
func isThumbURL(s string) bool {
	switch s {
	case "", "self", "default", "nsfw", "spoiler", "image":
		return false
	}
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// isImageURL reports whether u points at an image by extension or known host.
func isImageURL(u string) bool {
	l := strings.ToLower(u)
	for _, ext := range []string{".jpg", ".jpeg", ".png", ".gif", ".webp"} {
		if strings.HasSuffix(l, ext) {
			return true
		}
	}
	return strings.Contains(l, "i.redd.it/") || strings.Contains(l, "i.imgur.com/")
}
