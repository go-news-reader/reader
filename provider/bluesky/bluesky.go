// Package bluesky adapts the standalone github.com/go-atproto/atproto client to
// the aggregator's source.Provider contract.
//
// The query channel selects what to fetch:
//   - "@handle" or "handle" → that actor's author feed
//   - "#<query>"            → a post search for <query>
//
// An empty channel is an error (Bluesky has no anonymous global feed here).
package bluesky

import (
	"context"
	"errors"
	"net/http"
	"strings"

	goat "github.com/go-atproto/atproto"

	"github.com/go-news-reader/reader/source"
)

// ErrNoChannel is returned when Feed is called without an actor or search query.
var ErrNoChannel = errors.New("bluesky: specify an actor handle or a #query to search")

// client is the slice of *goat.Client the adapter uses; an interface for tests.
type client interface {
	AuthorFeed(ctx context.Context, actor string, limit int, cursor string) (*goat.Feed, error)
	SearchPosts(ctx context.Context, q string, limit int, cursor string) (*goat.Feed, error)
}

// Provider fetches Bluesky posts as normalized items.
type Provider struct {
	client client
}

// New returns a provider backed by the public Bluesky AppView (anonymous reads).
func New() *Provider { return &Provider{client: goat.New()} }

// NewWithHTTPClient returns a provider whose reads go through hc (e.g. the
// shared, request-logging client so the Network log captures Bluesky's traffic).
func NewWithHTTPClient(hc *http.Client) *Provider {
	return &Provider{client: goat.New(goat.WithHTTPClient(hc))}
}

// NewWithClient wraps a preconfigured client (or a fake in tests).
func NewWithClient(c client) *Provider { return &Provider{client: c} }

// Kind reports source.Bluesky.
func (p *Provider) Kind() source.Kind { return source.Bluesky }

// Feed returns a page of posts for the query's channel.
func (p *Provider) Feed(ctx context.Context, q source.Query) (source.Result, error) {
	ch := strings.TrimSpace(q.Channel)

	var feed *goat.Feed
	var err error
	switch {
	case strings.HasPrefix(ch, "#"):
		feed, err = p.client.SearchPosts(ctx, strings.TrimPrefix(ch, "#"), q.Limit, q.Cursor)
	case ch != "":
		feed, err = p.client.AuthorFeed(ctx, strings.TrimPrefix(ch, "@"), q.Limit, q.Cursor)
	default:
		return source.Result{}, ErrNoChannel
	}
	if err != nil {
		return source.Result{}, err
	}

	items := make([]source.Item, 0, len(feed.Posts))
	for _, post := range feed.Posts {
		items = append(items, mapPost(q.Channel, post))
	}
	return source.Result{Items: items, Cursor: feed.Cursor}, nil
}

func mapPost(channel string, p goat.Post) source.Item {
	rkey := p.URI
	if i := strings.LastIndex(rkey, "/"); i >= 0 {
		rkey = rkey[i+1:]
	}
	it := source.Item{
		ID:        rkey,
		Source:    source.Bluesky,
		Channel:   channel,
		Author:    p.Author.Handle,
		Body:      p.Text,
		Permalink: "https://bsky.app/profile/" + p.Author.Handle + "/post/" + rkey,
		Score:     p.LikeCount,
		Comments:  p.ReplyCount,
		Created:   p.CreatedAt.Unix(),
	}
	for _, img := range p.Images {
		if img.Thumb != "" {
			it.Media = append(it.Media, source.Media{URL: img.Thumb, Kind: source.MediaThumbnail})
		}
		if img.Fullsize != "" {
			it.Media = append(it.Media, source.Media{URL: img.Fullsize, Kind: source.MediaImage})
		}
	}
	return it
}
