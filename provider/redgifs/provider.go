package redgifs

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-news-reader/reader/source"
)

// fetcher is the slice of *Client the provider needs. Declaring it as an
// interface lets tests inject a fake without any network.
type fetcher interface {
	Search(ctx context.Context, query, order string, page, count int) (*SearchPage, error)
}

// Provider fetches RedGIFs videos as normalized source items.
type Provider struct {
	client fetcher
}

// New returns a RedGIFs provider backed by the portable browser-fingerprint
// HTTP client (pure Go, CGO=0, no host web view).
func New() *Provider { return &Provider{client: NewClient()} }

// NewWithHTTPClient returns a RedGIFs provider driving hc (e.g. the shared,
// request-logging client the aggregator builds so the Network log can show
// RedGIFs' traffic).
func NewWithHTTPClient(hc *http.Client) *Provider {
	return &Provider{client: NewClientWithHTTPClient(hc)}
}

// NewWithClient wraps an already-configured client — or a fake in tests.
func NewWithClient(c fetcher) *Provider { return &Provider{client: c} }

// Kind reports source.Redgifs.
func (p *Provider) Kind() source.Kind { return source.Redgifs }

// Feed returns a page of gifs. Query.Channel is the search query; an empty
// channel browses the trending feed. Query.Sort is the RedGIFs ordering hint
// (trending|latest|best|top28|oldest); Query.Cursor is the 1-based page number
// as a string ("" starts at page 1); Query.Limit is the per-page count.
func (p *Provider) Feed(ctx context.Context, q source.Query) (source.Result, error) {
	ch := strings.TrimSpace(q.Channel)
	order := q.Sort
	if ch == "" {
		// A browse feed: force trending regardless of any stale sort hint.
		order = defaultOrder
	}

	page := 1
	if cur := strings.TrimSpace(q.Cursor); cur != "" {
		if n, err := strconv.Atoi(cur); err == nil && n > 0 {
			page = n
		}
	}
	count := q.Limit
	if count <= 0 {
		count = defaultCount
	}

	sp, err := p.client.Search(ctx, ch, order, page, count)
	if err != nil {
		return source.Result{}, err
	}

	items := make([]source.Item, 0, len(sp.Gifs))
	for _, g := range sp.Gifs {
		items = append(items, mapGif(g, ch))
	}

	// Advance the cursor to the next page, or exhaust it on the last page.
	next := ""
	if sp.Page < sp.Pages {
		next = strconv.Itoa(sp.Page + 1)
	}
	return source.Result{Items: items, Cursor: next}, nil
}

// mapGif projects a RedGIFs Gif onto the normalized Item. channel is the
// subscription's own search query, tagged on every item so the feed's
// per-subscription filter matches. RedGIFs is adult content, so NSFW is always
// set. RedGIFs exposes no score/comment counts, so both are the -1 "unknown"
// sentinel other providers use.
func mapGif(g Gif, channel string) source.Item {
	title := strings.TrimSpace(g.Description)
	if title == "" {
		if len(g.Tags) > 0 {
			title = strings.Join(g.Tags, ", ")
		} else {
			title = g.ID
		}
	}
	it := source.Item{
		ID:        g.ID,
		Source:    source.Redgifs,
		Channel:   channel,
		Title:     title,
		Author:    g.UserName,
		Permalink: g.URLs.HTML,
		Link:      g.URLs.HTML,
		Score:     -1,
		Comments:  -1,
		Created:   g.CreateDate,
		NSFW:      true,
		Tags:      g.Tags,
	}
	if g.URLs.HD != "" {
		it.Media = append(it.Media, source.Media{
			URL:    g.URLs.HD,
			Kind:   source.MediaVideo,
			Width:  g.Width,
			Height: g.Height,
		})
	}
	poster := g.URLs.Poster
	if poster == "" {
		poster = g.URLs.Thumbnail
	}
	if poster != "" {
		it.Media = append(it.Media, source.Media{URL: poster, Kind: source.MediaImage})
	}
	return it
}
