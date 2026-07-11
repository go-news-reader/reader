// Package mastodon adapts the standalone github.com/go-mastodon/mastodon client
// to the aggregator's source.Provider contract.
//
// The query channel selects the timeline:
//   - ""          → the instance public timeline
//   - "#<tag>"    → the hashtag timeline for <tag>
//   - "@<acct>"   → a specific account's statuses
package mastodon

import (
	"context"
	"net/http"
	"strings"

	gomasto "github.com/go-mastodon/mastodon"

	"github.com/go-news-reader/reader/source"
)

// client is the slice of *gomasto.Client the adapter uses; an interface so
// tests can inject a fake with no network.
type client interface {
	PublicTimeline(ctx context.Context, opts gomasto.TimelineOptions) (*gomasto.Timeline, error)
	HashtagTimeline(ctx context.Context, tag string, opts gomasto.TimelineOptions) (*gomasto.Timeline, error)
	AccountStatuses(ctx context.Context, acct string, opts gomasto.TimelineOptions) (*gomasto.Timeline, error)
}

// Provider fetches Mastodon statuses as normalized items.
type Provider struct {
	client client
}

// New returns a provider for the given instance (e.g. "https://mastodon.social").
// An optional bearer token authenticates the reads.
func New(instance, token string) *Provider { return newWith(nil, instance, token) }

// NewWithHTTPClient returns a provider whose reads go through hc (e.g. the
// shared, request-logging client so the Network log captures Mastodon traffic).
func NewWithHTTPClient(hc *http.Client, instance, token string) *Provider {
	return newWith(hc, instance, token)
}

// newWith builds the provider, wiring hc when non-nil.
func newWith(hc *http.Client, instance, token string) *Provider {
	var opts []gomasto.Option
	if hc != nil {
		opts = append(opts, gomasto.WithHTTPClient(hc))
	}
	if token != "" {
		opts = append(opts, gomasto.WithToken(token))
	}
	return &Provider{client: gomasto.New(instance, opts...)}
}

// NewWithClient wraps a preconfigured client (or a fake in tests).
func NewWithClient(c client) *Provider { return &Provider{client: c} }

// Kind reports source.Mastodon.
func (p *Provider) Kind() source.Kind { return source.Mastodon }

// Feed returns a page of statuses for the query's channel.
func (p *Provider) Feed(ctx context.Context, q source.Query) (source.Result, error) {
	opts := gomasto.TimelineOptions{Limit: q.Limit, MaxID: q.Cursor}

	var tl *gomasto.Timeline
	var err error
	ch := strings.TrimSpace(q.Channel)
	switch {
	case strings.HasPrefix(ch, "#"):
		tl, err = p.client.HashtagTimeline(ctx, strings.TrimPrefix(ch, "#"), opts)
	case strings.HasPrefix(ch, "@"):
		tl, err = p.client.AccountStatuses(ctx, strings.TrimPrefix(ch, "@"), opts)
	default:
		tl, err = p.client.PublicTimeline(ctx, opts)
	}
	if err != nil {
		return source.Result{}, err
	}

	items := make([]source.Item, 0, len(tl.Statuses))
	for _, s := range tl.Statuses {
		items = append(items, mapStatus(q.Channel, s))
	}
	return source.Result{Items: items, Cursor: tl.MaxID}, nil
}

func mapStatus(channel string, s gomasto.Status) source.Item {
	it := source.Item{
		ID:        s.ID,
		Source:    source.Mastodon,
		Channel:   channel,
		Title:     s.SpoilerText,
		Author:    authorName(s.Account),
		Body:      s.Content,
		Permalink: s.URL,
		Score:     s.Favourites,
		Comments:  s.Replies,
		Created:   s.CreatedAt.Unix(),
		NSFW:      s.Sensitive,
	}
	for _, m := range s.Media {
		it.Media = append(it.Media, source.Media{URL: m.URL, Kind: mapMediaKind(m.Type)})
	}
	for _, t := range s.Tags {
		it.Tags = append(it.Tags, t.Name)
	}
	return it
}

// authorName prefers the fully-qualified acct, falling back to display name or
// bare username.
func authorName(a gomasto.Account) string {
	switch {
	case a.Acct != "":
		return a.Acct
	case a.Username != "":
		return a.Username
	default:
		return a.DisplayName
	}
}

func mapMediaKind(t string) source.MediaKind {
	switch t {
	case "video":
		return source.MediaVideo
	case "gifv":
		return source.MediaGIF
	case "audio":
		return source.MediaAudio
	default:
		return source.MediaImage
	}
}
