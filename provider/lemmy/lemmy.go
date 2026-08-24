// Package lemmy adapts the standalone github.com/go-lemmy/lemmy client to the
// aggregator's source.Provider contract. The query channel is a community name
// (e.g. "technology" or "technology@lemmy.world"); an empty channel fetches the
// instance-wide listing. Query.Cursor is a 1-based page number.
package lemmy

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	golem "github.com/go-lemmy/lemmy"

	"github.com/go-news-reader/reader/source"
)

// client is the slice of *golem.Client the adapter uses; an interface for tests.
type client interface {
	Posts(ctx context.Context, opts golem.PostsOptions) (*golem.PostList, error)
	SearchCommunities(ctx context.Context, query string, limit int) (*golem.CommunityList, error)
}

// Provider implements source.Searcher (community discovery).
var _ source.Searcher = (*Provider)(nil)

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

// SearchChannels implements source.Searcher: it discovers communities matching
// query (via the public /api/v3/search) and maps each onto a source.ChannelResult.
// The search federates, so results span instances; each subscribes by its
// unambiguous "<name>@<instance>" handle (derived from the community's actor_id).
func (p *Provider) SearchChannels(ctx context.Context, query string) ([]source.ChannelResult, error) {
	list, err := p.client.SearchCommunities(ctx, query, 0)
	if err != nil {
		return nil, err
	}
	out := make([]source.ChannelResult, 0, len(list.Communities))
	for _, c := range list.Communities {
		if c.Name == "" {
			continue
		}
		channel := c.Name
		if host := actorHost(c.ActorID); host != "" {
			channel = c.Name + "@" + host
		}
		title := c.Title
		if title == "" {
			title = c.Name
		}
		out = append(out, source.ChannelResult{
			Source:      source.Lemmy,
			Channel:     channel,
			Title:       title,
			Description: c.Description,
			Subscribers: int64(c.Subscribers),
			NSFW:        c.NSFW,
			IconURL:     c.Icon,
		})
	}
	return out, nil
}

// actorHost extracts the instance host from a Lemmy community actor_id URL
// ("https://lemmy.world/c/golang" → "lemmy.world"); "" when unparseable.
func actorHost(actorID string) string {
	u, err := url.Parse(actorID)
	if err != nil {
		return ""
	}
	return u.Host
}

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
		// A 401/403 means the instance refuses anonymous reads and needs a
		// logged-in session; surface a typed prompt rather than a raw status.
		if source.ErrHasAuthStatus(err) {
			return source.Result{}, source.NeedsAuth(source.Lemmy, "instance requires a signed-in session")
		}
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
		Created:   source.UnixOrZero(p.Published),
		NSFW:      p.NSFW,
	}
	if p.ThumbnailURL != "" {
		it.Media = append(it.Media, source.Media{URL: p.ThumbnailURL, Kind: source.MediaThumbnail})
	}
	return it
}
