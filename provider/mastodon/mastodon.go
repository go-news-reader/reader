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
	HomeTimeline(ctx context.Context, opts gomasto.TimelineOptions) (*gomasto.Timeline, error)
	VerifyCredentials(ctx context.Context) (*gomasto.Account, error)
	Following(ctx context.Context, accountID string, opts gomasto.TimelineOptions) (*gomasto.FollowingPage, error)
}

// homeChannel is the reserved subscription channel that maps to the
// authenticated user's home timeline (the statuses of the accounts they
// follow). An empty channel also maps to home when a token is configured;
// without a token an empty channel is the instance public timeline.
const homeChannel = "home"

// Provider fetches Mastodon statuses as normalized items.
type Provider struct {
	client client
	// authed reports whether a bearer token was configured, which gates the
	// home timeline and enables the empty-channel→home default.
	authed bool
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
	return &Provider{client: gomasto.New(instance, opts...), authed: token != ""}
}

// NewWithClient wraps a preconfigured client (or a fake in tests). The client
// is treated as unauthenticated (no home-timeline default); use
// [NewWithClientAuthed] to wrap a token-carrying client.
func NewWithClient(c client) *Provider { return &Provider{client: c} }

// NewWithClientAuthed wraps a preconfigured client that carries a bearer token,
// enabling the home timeline and the empty-channel→home default. Tests use it to
// exercise the authenticated paths without a network.
func NewWithClientAuthed(c client) *Provider { return &Provider{client: c, authed: true} }

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
	case ch == homeChannel || (ch == "" && p.authed):
		// The home timeline needs a token; a "home" channel without one is a
		// typed prompt to connect an account rather than a silent fallback.
		if !p.authed {
			return source.Result{}, source.NeedsAuth(source.Mastodon, "sign in to view your Mastodon home timeline")
		}
		tl, err = p.client.HomeTimeline(ctx, opts)
	default:
		tl, err = p.client.PublicTimeline(ctx, opts)
	}
	if err != nil {
		// A 401/403 from the instance means the read needs (or was refused) an
		// access token; surface it as a typed prompt instead of a raw status.
		if source.ErrHasAuthStatus(err) {
			return source.Result{}, source.NeedsAuth(source.Mastodon, "access token required/invalid")
		}
		return source.Result{}, err
	}

	items := make([]source.Item, 0, len(tl.Statuses))
	for _, s := range tl.Statuses {
		items = append(items, mapStatus(q.Channel, s))
	}
	return source.Result{Items: items, Cursor: tl.MaxID}, nil
}

// followMaxPages bounds MyFollows' pagination so a misbehaving instance that
// always returns a next cursor cannot loop forever; 40 pages of 80 covers 3200
// follows, beyond any typical account.
const followMaxPages = 40

// mapErr promotes a 401/403 (folded into the client's error text) to a typed
// auth prompt, leaving transient failures unchanged.
func mapErr(err error) error {
	if source.ErrHasAuthStatus(err) {
		return source.NeedsAuth(source.Mastodon, "access token required/invalid")
	}
	return err
}

// MyFollows returns every account the connected Mastodon user follows as a
// ready subscription, so the aggregator's generic "import my subscriptions"
// action adds them all. It verifies the token to learn the account id, then
// pages the account's following list, returning each followed account as an
// "@<acct>" channel — the handle form [Provider.Feed] resolves to that account's
// own timeline. It satisfies [source.FollowImporter]; a token is required and a
// 401/403 is surfaced as a typed auth prompt.
func (p *Provider) MyFollows(ctx context.Context) ([]source.Subscription, error) {
	me, err := p.client.VerifyCredentials(ctx)
	if err != nil {
		return nil, mapErr(err)
	}
	var out []source.Subscription
	opts := gomasto.TimelineOptions{Limit: 80}
	for page := 0; page < followMaxPages; page++ {
		pg, err := p.client.Following(ctx, me.ID, opts)
		if err != nil {
			return nil, mapErr(err)
		}
		for _, a := range pg.Accounts {
			if a.Acct == "" {
				continue
			}
			out = append(out, source.Subscription{Source: source.Mastodon, Channel: "@" + a.Acct})
		}
		if pg.MaxID == "" {
			break
		}
		opts.MaxID = pg.MaxID
	}
	return out, nil
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
		Created:   source.UnixOrZero(s.CreatedAt),
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
