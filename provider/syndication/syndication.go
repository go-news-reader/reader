// Package syndication adapts the standalone github.com/go-syndication/feed
// parser (RSS/Atom/JSONFeed) to the aggregator's source.Provider contract. The
// query channel is the feed URL to fetch.
package syndication

import (
	"context"
	"errors"
	"net/http"
	"strings"

	gofeed "github.com/go-syndication/feed"

	"github.com/go-news-reader/reader/source"
)

// ErrNoChannel is returned when Feed is called without a feed URL.
var ErrNoChannel = errors.New("syndication: Query.Channel must be a feed URL")

// fetcher is the slice of the feed package the adapter uses; a var-injected
// seam for network-free tests.
type fetcher func(ctx context.Context, client *http.Client, url string) (*gofeed.Feed, error)

// Provider fetches and normalizes RSS/Atom/JSONFeed feeds.
type Provider struct {
	client *http.Client
	fetch  fetcher
}

// New returns a syndication provider using client (or http.DefaultClient when nil).
func New(client *http.Client) *Provider {
	return &Provider{client: client, fetch: gofeed.Fetch}
}

// Kind reports source.Syndication.
func (p *Provider) Kind() source.Kind { return source.Syndication }

// Feed fetches the feed at Query.Channel and returns its entries. Feeds are not
// paginated, so Result.Cursor is always empty.
func (p *Provider) Feed(ctx context.Context, q source.Query) (source.Result, error) {
	url := strings.TrimSpace(q.Channel)
	if url == "" {
		return source.Result{}, ErrNoChannel
	}
	f, err := p.fetch(ctx, p.client, url)
	if err != nil {
		return source.Result{}, err
	}

	limit := q.Limit
	items := make([]source.Item, 0, len(f.Entries))
	for _, e := range f.Entries {
		if limit > 0 && len(items) >= limit {
			break
		}
		items = append(items, mapEntry(url, f.Title, e))
	}
	return source.Result{Items: items}, nil
}

func mapEntry(channel, feedTitle string, e gofeed.Entry) source.Item {
	body := e.Content
	if body == "" {
		body = e.Summary
	}
	author := e.Author
	if author == "" {
		author = feedTitle
	}
	it := source.Item{
		ID:        firstNonEmpty(e.ID, e.Link),
		Source:    source.Syndication,
		Channel:   channel,
		Title:     e.Title,
		Author:    author,
		Body:      body,
		Permalink: e.Link,
		Link:      e.Link,
		Comments:  -1, // feeds carry no comment count
		Score:     -1, // feeds carry no score
		Created:   e.Published.Unix(),
	}
	for _, m := range e.Media {
		it.Media = append(it.Media, source.Media{URL: m.URL, Kind: mediaKind(m.Type)})
	}
	return it
}

func mediaKind(mime string) source.MediaKind {
	switch {
	case strings.HasPrefix(mime, "video/"):
		return source.MediaVideo
	case strings.HasPrefix(mime, "audio/"):
		return source.MediaAudio
	case mime == "image/gif":
		return source.MediaGIF
	default:
		return source.MediaImage
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
