// Package reddit adapts the standalone github.com/go-reddit/reddit client to
// the aggregator's source.Provider contract. Anonymous fetches go through the
// shared browserhttp uTLS client so Reddit's TLS-fingerprint anti-bot 403 does
// not fire; an OAuth-authenticated client can be supplied instead.
package reddit

import (
	"context"
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

// NewOAuth returns an authenticated Reddit provider driving hc (the shared,
// request-logging client the aggregator builds). clientID + clientSecret enable
// application-only ("client_credentials") OAuth against oauth.reddit.com — which
// reads public listings and, crucially, works from IPs where Reddit 403s the
// anonymous ".json" endpoints. When username and password are both supplied, the
// per-user "script" grant is used instead. A nil hc falls back to the portable
// browser-fingerprint client (so the constructor is safe on its own).
func NewOAuth(hc *http.Client, clientID, clientSecret, username, password string) *Provider {
	if hc == nil {
		hc = browserhttp.NewClient(30 * time.Second)
	}
	opts := []goreddit.Option{
		goreddit.WithHTTPClient(hc),
		goreddit.WithUserAgent(browserhttp.DefaultUserAgent),
		goreddit.WithOAuth(clientID, clientSecret),
	}
	if username != "" && password != "" {
		opts = append(opts, goreddit.WithOAuthScript(clientID, clientSecret, username, password))
	}
	return &Provider{client: goreddit.NewClient(opts...)}
}

// NewWithClient wraps an already-configured reddit client — e.g. an OAuth
// (logged-in) client, or a fake in tests.
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
		return source.Result{}, err
	}

	items := make([]source.Item, 0, len(page.Posts))
	for _, post := range page.Posts {
		items = append(items, mapPost(post))
	}
	return source.Result{Items: items, Cursor: page.After}, nil
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
	if !p.IsSelf {
		it.Link = p.URL
	}
	if p.Flair != "" {
		it.Tags = []string{p.Flair}
	}
	if isThumbURL(p.Thumbnail) {
		it.Media = append(it.Media, source.Media{URL: p.Thumbnail, Kind: source.MediaThumbnail})
	}
	if !p.IsSelf && isImageURL(p.URL) {
		it.Media = append(it.Media, source.Media{URL: p.URL, Kind: source.MediaImage})
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
