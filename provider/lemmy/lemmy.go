// Package lemmy adapts the standalone github.com/go-lemmy/lemmy client to the
// aggregator's source.Provider contract. The query channel is a community name
// (e.g. "technology" or "technology@lemmy.world"); an empty channel fetches the
// instance-wide listing. Query.Cursor is a 1-based page number.
package lemmy

import (
	"context"
	"net/http"
	"strconv"

	golem "github.com/go-lemmy/lemmy"

	"github.com/go-news-reader/reader/source"
)

// client is the slice of *golem.Client the adapter uses; an interface for tests.
type client interface {
	Posts(ctx context.Context, opts golem.PostsOptions) (*golem.PostList, error)
}

// Provider fetches Lemmy posts as normalized items.
type Provider struct {
	client client
}

// New returns a provider for the given instance (e.g. "https://lemmy.world").
// An optional token is currently unused for anonymous reads but reserved for
// future authenticated listings.
func New(instance string) *Provider {
	return &Provider{client: golem.New(instance)}
}

// NewWithHTTPClient returns a provider whose reads go through hc (e.g. the
// shared, request-logging client so the Network log captures Lemmy traffic).
func NewWithHTTPClient(hc *http.Client, instance string) *Provider {
	return &Provider{client: golem.New(instance, golem.WithHTTPClient(hc))}
}

// NewWithClient wraps a preconfigured client (or a fake in tests).
func NewWithClient(c client) *Provider { return &Provider{client: c} }

// Kind reports source.Lemmy.
func (p *Provider) Kind() source.Kind { return source.Lemmy }

// Feed returns a page of posts for the query's community.
func (p *Provider) Feed(ctx context.Context, q source.Query) (source.Result, error) {
	page := 1
	if q.Cursor != "" {
		if n, err := strconv.Atoi(q.Cursor); err == nil && n > 0 {
			page = n
		}
	}
	list, err := p.client.Posts(ctx, golem.PostsOptions{
		Community: q.Channel,
		Sort:      q.Sort,
		Limit:     q.Limit,
		Page:      page,
	})
	if err != nil {
		return source.Result{}, err
	}

	items := make([]source.Item, 0, len(list.Posts))
	for _, post := range list.Posts {
		items = append(items, mapPost(post))
	}
	cursor := ""
	if len(items) > 0 {
		cursor = strconv.Itoa(page + 1)
	}
	return source.Result{Items: items, Cursor: cursor}, nil
}

func mapPost(p golem.Post) source.Item {
	it := source.Item{
		ID:        strconv.Itoa(p.ID),
		Source:    source.Lemmy,
		Channel:   p.Community,
		Title:     p.Title,
		Author:    p.Creator,
		Body:      p.Body,
		Permalink: p.Permalink,
		Link:      p.URL,
		Score:     p.Score,
		Comments:  p.Comments,
		Created:   p.Published.Unix(),
		NSFW:      p.NSFW,
	}
	if p.ThumbnailURL != "" {
		it.Media = append(it.Media, source.Media{URL: p.ThumbnailURL, Kind: source.MediaThumbnail})
	}
	return it
}
