// Package instagram adapts the standalone github.com/go-instagram/instagram
// best-effort client to the aggregator's source.Provider contract. The query
// channel is a public account username.
//
// This provider is inherently fragile: without a valid sessionid cookie
// Instagram frequently returns 401/403/429, surfaced here as errors.
package instagram

import (
	"context"
	"errors"
	"net/http"
	"strings"

	goig "github.com/go-instagram/instagram"

	"github.com/go-news-reader/reader/source"
)

// ErrNoChannel is returned when Feed is called without a username.
var ErrNoChannel = errors.New("instagram: Query.Channel must be a username")

// client is the slice of *goig.Client the adapter uses; an interface for tests.
type client interface {
	UserProfile(ctx context.Context, username string) (*goig.Profile, error)
}

// Provider fetches a public Instagram profile's recent posts as items.
type Provider struct {
	client  client
	hasCred bool // a sessionid was configured
}

// New returns a provider. sessionID is an optional sessionid cookie for
// authenticated reads (empty for anonymous, best-effort).
func New(sessionID string) *Provider { return newWith(nil, sessionID) }

// NewWithHTTPClient returns a provider whose reads go through hc (e.g. the
// shared, request-logging client so the Network log captures Instagram traffic).
func NewWithHTTPClient(hc *http.Client, sessionID string) *Provider {
	return newWith(hc, sessionID)
}

// newWith builds the provider, wiring hc when non-nil.
func newWith(hc *http.Client, sessionID string) *Provider {
	var opts []goig.Option
	if hc != nil {
		opts = append(opts, goig.WithHTTPClient(hc))
	}
	if sessionID != "" {
		opts = append(opts, goig.WithSessionID(sessionID))
	}
	return &Provider{client: goig.New(opts...), hasCred: sessionID != ""}
}

// NewWithClient wraps a preconfigured client (or a fake in tests).
func NewWithClient(c client) *Provider { return &Provider{client: c} }

// Kind reports source.Instagram.
func (p *Provider) Kind() source.Kind { return source.Instagram }

// Feed returns the recent posts of the account named by Query.Channel. Not
// paginated here, so Result.Cursor is always empty.
func (p *Provider) Feed(ctx context.Context, q source.Query) (source.Result, error) {
	user := strings.TrimPrefix(strings.TrimSpace(q.Channel), "@")
	if user == "" {
		return source.Result{}, ErrNoChannel
	}
	prof, err := p.client.UserProfile(ctx, user)
	if err != nil {
		// Heuristic for this best-effort scraper: without a sessionid it cannot
		// work at all, and Instagram also answers a blocked read with 401/403 (or
		// a login redirect). Either case is really "give me a session token", so
		// map it to a typed prompt; genuine transient errors (with a session
		// configured) pass through untouched.
		if !p.hasCred || source.ErrHasAuthStatus(err) {
			return source.Result{}, source.NeedsAuth(source.Instagram, "session/token required")
		}
		return source.Result{}, err
	}

	limit := q.Limit
	items := make([]source.Item, 0, len(prof.Posts))
	for _, post := range prof.Posts {
		if limit > 0 && len(items) >= limit {
			break
		}
		items = append(items, mapPost(prof.Username, post))
	}
	return source.Result{Items: items}, nil
}

func mapPost(username string, p goig.Post) source.Item {
	author := p.Owner
	if author == "" {
		author = username
	}
	it := source.Item{
		ID:        firstNonEmpty(p.Shortcode, p.ID),
		Source:    source.Instagram,
		Channel:   username,
		Author:    author,
		Body:      p.Caption,
		Permalink: p.Permalink,
		Score:     p.Likes,
		Comments:  p.Comments,
		Created:   p.Timestamp.Unix(),
	}
	if p.DisplayURL != "" {
		it.Media = append(it.Media, source.Media{URL: p.DisplayURL, Kind: source.MediaImage})
	}
	if p.IsVideo && p.VideoURL != "" {
		it.Media = append(it.Media, source.Media{URL: p.VideoURL, Kind: source.MediaVideo})
	}
	return it
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
