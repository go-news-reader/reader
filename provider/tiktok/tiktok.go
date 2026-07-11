// Package tiktok adapts the standalone github.com/go-tiktok/tiktok best-effort
// client to the aggregator's source.Provider contract. The query channel is a
// user's secUid (TikTok's opaque per-account id, obtained once out of band).
//
// This provider is inherently fragile: TikTok's web API usually needs a
// msToken/sessionid and a signed param, and often returns 403/429 or empty
// anti-bot bodies — surfaced here as errors or empty results.
package tiktok

import (
	"context"
	"errors"
	"net/http"
	"strings"

	gott "github.com/go-tiktok/tiktok"

	"github.com/go-news-reader/reader/source"
)

// ErrNoChannel is returned when Feed is called without a secUid.
var ErrNoChannel = errors.New("tiktok: Query.Channel must be a user secUid")

// defaultCount caps a page when Query.Limit is unset.
const defaultCount = 30

// client is the slice of *gott.Client the adapter uses; an interface for tests.
type client interface {
	UserPosts(ctx context.Context, secUid string, count int, cursor string) (*gott.UserFeed, error)
}

// Provider fetches a TikTok user's recent videos as items.
type Provider struct {
	client  client
	hasCred bool // an msToken or sessionid was configured
}

// New returns a provider. msToken and sessionID are optional credentials for
// authenticated reads (empty for anonymous, best-effort).
func New(msToken, sessionID string) *Provider { return newWith(nil, msToken, sessionID) }

// NewWithHTTPClient returns a provider whose reads go through hc (e.g. the
// shared, request-logging client so the Network log captures TikTok traffic).
func NewWithHTTPClient(hc *http.Client, msToken, sessionID string) *Provider {
	return newWith(hc, msToken, sessionID)
}

// newWith builds the provider, wiring hc when non-nil.
func newWith(hc *http.Client, msToken, sessionID string) *Provider {
	var opts []gott.Option
	if hc != nil {
		opts = append(opts, gott.WithHTTPClient(hc))
	}
	if msToken != "" {
		opts = append(opts, gott.WithMSToken(msToken))
	}
	if sessionID != "" {
		opts = append(opts, gott.WithSessionID(sessionID))
	}
	return &Provider{client: gott.New(opts...), hasCred: msToken != "" || sessionID != ""}
}

// NewWithClient wraps a preconfigured client (or a fake in tests).
func NewWithClient(c client) *Provider { return &Provider{client: c} }

// Kind reports source.TikTok.
func (p *Provider) Kind() source.Kind { return source.TikTok }

// Feed returns a page of the user's videos. Query.Channel is the secUid;
// Query.Cursor pages through results.
func (p *Provider) Feed(ctx context.Context, q source.Query) (source.Result, error) {
	secUID := strings.TrimSpace(q.Channel)
	if secUID == "" {
		return source.Result{}, ErrNoChannel
	}
	count := q.Limit
	if count <= 0 {
		count = defaultCount
	}
	feed, err := p.client.UserPosts(ctx, secUID, count, q.Cursor)
	if err != nil {
		// Heuristic for this best-effort scraper: without an msToken/sessionid
		// TikTok's web API returns 403/429 or empty anti-bot bodies, so any
		// failure with no credential configured (or an explicit 401/403) is
		// really "give me a session token". Transient errors with a credential
		// set pass through untouched.
		if !p.hasCred || source.ErrHasAuthStatus(err) {
			return source.Result{}, source.NeedsAuth(source.TikTok, "session/token required")
		}
		return source.Result{}, err
	}

	items := make([]source.Item, 0, len(feed.Videos))
	for _, v := range feed.Videos {
		items = append(items, mapVideo(feed.Username, v))
	}
	cursor := ""
	if feed.HasMore {
		cursor = feed.Cursor
	}
	return source.Result{Items: items, Cursor: cursor}, nil
}

func mapVideo(username string, v gott.Video) source.Item {
	author := v.Author
	if author == "" {
		author = username
	}
	it := source.Item{
		ID:        v.ID,
		Source:    source.TikTok,
		Channel:   username,
		Author:    author,
		Body:      v.Description,
		Permalink: v.Permalink,
		Score:     v.Likes,
		Comments:  v.Comments,
		Created:   v.CreateTime.Unix(),
	}
	if v.CoverURL != "" {
		it.Media = append(it.Media, source.Media{URL: v.CoverURL, Kind: source.MediaThumbnail})
	}
	if v.PlayURL != "" {
		it.Media = append(it.Media, source.Media{URL: v.PlayURL, Kind: source.MediaVideo})
	}
	return it
}
