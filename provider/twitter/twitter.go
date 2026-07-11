// Package twitter adapts the standalone github.com/go-birdsite/twitter
// best-effort client to the aggregator's source.Provider contract. The query
// channel is a public account screen name (with or without a leading "@").
//
// This provider is inherently fragile: Twitter/X locks these endpoints and
// often needs an auth token; blocked requests surface as errors.
package twitter

import (
	"context"
	"errors"
	"net/http"
	"strings"

	gotw "github.com/go-birdsite/twitter"

	"github.com/go-news-reader/reader/source"
)

// ErrNoChannel is returned when Feed is called without a screen name.
var ErrNoChannel = errors.New("twitter: Query.Channel must be a screen name")

// client is the slice of *gotw.Client the adapter uses; an interface for tests.
type client interface {
	UserTweets(ctx context.Context, screenName string) (*gotw.Timeline, error)
}

// Provider fetches a public account's tweets as normalized items.
type Provider struct {
	client  client
	hasCred bool // an auth token was configured
}

// New returns a provider. authToken is an optional bearer token for
// authenticated reads (empty for anonymous, best-effort).
func New(authToken string) *Provider { return newWith(nil, authToken) }

// NewWithHTTPClient returns a provider whose reads go through hc (e.g. the
// shared, request-logging client so the Network log captures Twitter/X traffic).
func NewWithHTTPClient(hc *http.Client, authToken string) *Provider {
	return newWith(hc, authToken)
}

// newWith builds the provider, wiring hc when non-nil.
func newWith(hc *http.Client, authToken string) *Provider {
	var opts []gotw.Option
	if hc != nil {
		opts = append(opts, gotw.WithHTTPClient(hc))
	}
	if authToken != "" {
		opts = append(opts, gotw.WithAuthToken(authToken))
	}
	return &Provider{client: gotw.New(opts...), hasCred: authToken != ""}
}

// NewWithClient wraps a preconfigured client (or a fake in tests).
func NewWithClient(c client) *Provider { return &Provider{client: c} }

// Kind reports source.Twitter.
func (p *Provider) Kind() source.Kind { return source.Twitter }

// Feed returns the recent tweets of the account named by Query.Channel. Not
// paginated here, so Result.Cursor is always empty.
func (p *Provider) Feed(ctx context.Context, q source.Query) (source.Result, error) {
	name := strings.TrimPrefix(strings.TrimSpace(q.Channel), "@")
	if name == "" {
		return source.Result{}, ErrNoChannel
	}
	tl, err := p.client.UserTweets(ctx, name)
	if err != nil {
		// Heuristic for this best-effort scraper: X locks these endpoints and
		// usually needs an auth token, so any failure with no token configured
		// (or an explicit 401/403) is really "give me a token". Transient errors
		// with a token set pass through untouched.
		if !p.hasCred || source.ErrHasAuthStatus(err) {
			return source.Result{}, source.NeedsAuth(source.Twitter, "session/token required")
		}
		return source.Result{}, err
	}

	items := make([]source.Item, 0, len(tl.Tweets))
	for _, tw := range tl.Tweets {
		items = append(items, mapTweet(tw))
	}
	return source.Result{Items: items}, nil
}

func mapTweet(tw gotw.Tweet) source.Item {
	it := source.Item{
		ID:        tw.ID,
		Source:    source.Twitter,
		Channel:   tw.Author,
		Author:    tw.Author,
		Body:      tw.Text,
		Permalink: tw.Permalink,
		Score:     tw.Likes,
		Comments:  tw.Replies,
		Created:   tw.CreatedAt.Unix(),
	}
	for _, m := range tw.Media {
		it.Media = append(it.Media, source.Media{URL: m.URL, Kind: mediaKind(m.Type)})
	}
	return it
}

func mediaKind(t string) source.MediaKind {
	switch t {
	case "video":
		return source.MediaVideo
	case "animated_gif":
		return source.MediaGIF
	default:
		return source.MediaImage
	}
}
