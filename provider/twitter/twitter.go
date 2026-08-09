// Package twitter adapts the standalone github.com/go-birdsite/twitter
// best-effort client to the aggregator's source.Provider contract. The query
// channel is a public account screen name (with or without a leading "@").
//
// This provider is inherently fragile: Twitter/X locks these endpoints and
// changes their shape without notice. It does NOT need an auth token for public
// timelines — it needs a browser-fingerprinting HTTP client, which is what the
// endpoint actually checks.
package twitter

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	gotw "github.com/go-birdsite/twitter"
	"github.com/go-browserhttp/browserhttp"

	"github.com/go-news-reader/reader/source"
)

// ErrNoChannel is returned when Feed is called without a screen name.
var ErrNoChannel = errors.New("twitter: Query.Channel must be a screen name")

// defaultTimeout bounds a timeline read on the client this package builds for
// itself (when the caller supplies none).
const defaultTimeout = 30 * time.Second

// client is the slice of *gotw.Client the adapter uses; an interface for tests.
type client interface {
	UserTweets(ctx context.Context, screenName string) (*gotw.Timeline, error)
}

// Provider fetches a public account's tweets as normalized items.
type Provider struct {
	client client
}

// New returns a provider. Public timelines need no auth token — the reader
// reads them through the syndication endpoint (see newWith).
func New() *Provider { return newWith(nil) }

// NewWithHTTPClient returns a provider whose reads go through hc (e.g. the
// shared, request-logging client so the Network log captures Twitter/X traffic).
func NewWithHTTPClient(hc *http.Client) *Provider {
	return newWith(hc)
}

// newWith builds the provider. When the caller supplies no client this builds a
// browser-fingerprint one rather than falling back to net/http's default: the
// syndication endpoint answers 429 to Go's stock transport whatever the account
// quota says, because it fingerprints the TLS/HTTP2 handshake. The stock client
// is not a degraded path here, it is a guaranteed failure.
func newWith(hc *http.Client) *Provider {
	if hc == nil {
		hc = browserhttp.NewClient(defaultTimeout)
	}
	return &Provider{client: gotw.New(gotw.WithHTTPClient(hc))}
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
		return source.Result{}, mapErr(name, err)
	}

	items := make([]source.Item, 0, len(tl.Tweets))
	for _, tw := range tl.Tweets {
		items = append(items, mapTweet(tw, name))
	}
	return source.Result{Items: items}, nil
}

// mapErr translates a client failure into what the user should actually do
// about it. Only a genuinely non-public account is a sign-in problem; a
// fingerprint refusal and a missing account are not, and reporting either as
// "session/token required" (as this provider used to, for every error) sends the
// user off to find a credential that would not have helped.
func mapErr(name string, err error) error {
	switch {
	case errors.Is(err, gotw.ErrProtected):
		return source.NeedsAuth(source.Twitter, "@"+name+" is not public")
	case errors.Is(err, gotw.ErrNotFound):
		return errors.New("twitter: no such account @" + name)
	case errors.Is(err, gotw.ErrFingerprinted):
		return errors.New("twitter: X refused the request (429) — it fingerprints the HTTP client; " +
			"this provider must run on a browser-fingerprinting client")
	default:
		return err
	}
}

// mapTweet normalizes one tweet into a feed item, keyed to the subscribed
// account name.
//
// A retweet is unwrapped: its content, author, media, counts and permalink all
// come from the ORIGINAL tweet, because the wrapper carries only a truncated
// "RT @x: …" stub with zero likes and zero replies — which is what the feed used
// to show. The timestamp stays the retweet's own, so the item sorts where it
// actually appeared in the timeline, exactly as a Twitter client orders it.
func mapTweet(tw gotw.Tweet, channel string) source.Item {
	o := tw.Original()
	it := source.Item{
		ID:        tw.ID,
		Source:    source.Twitter,
		Channel:   channel,
		Author:    displayName(o.User),
		Body:      body(o),
		Permalink: o.Permalink,
		Link:      o.PrimaryLink(),
		Score:     o.Likes,
		Comments:  o.Replies,
		Created:   source.UnixOrZero(tw.CreatedAt),
		NSFW:      o.Sensitive,
	}
	for _, m := range o.Media {
		it.Media = append(it.Media, mapMedia(m)...)
	}
	return it
}

// mapMedia turns one attachment into the feed's media entries.
//
// A photo is one entry. A video or animated GIF is TWO: the still preview frame
// as a thumbnail, and the playable MP4 as the video. X reports only the still in
// media_url_https and keeps the encodings in video_info, so emitting the still
// alone — as this used to — threw the playable URL away at the provider
// boundary, and a consumer reading Item.Media had no way to reach the video at
// all. The two-entry shape is what provider/tiktok already does for exactly the
// same reason.
func mapMedia(m gotw.Media) []source.Media {
	kind := mediaKind(m.Type)
	still := source.Media{URL: m.URL, Kind: kind, Width: m.Width, Height: m.Height, AltText: m.AltText}
	if kind == source.MediaImage {
		return []source.Media{still}
	}
	v, ok := m.BestVariant()
	if !ok {
		// Offered only as an adaptive stream, with no progressive MP4 to hand a
		// plain decoder. Keep the preview frame and keep calling it a video,
		// rather than promising a thumbnail with nothing behind it.
		return []source.Media{still}
	}
	// original_info describes the MEDIA, so it sizes the video as much as the
	// frame lifted out of it; carrying it on both entries means a consumer that
	// picks the playable one still knows its aspect.
	playable := source.Media{URL: v.URL, Kind: kind, Width: m.Width, Height: m.Height, AltText: m.AltText}
	still.Kind = source.MediaThumbnail
	return []source.Media{still, playable}
}

// body is the tweet's readable text: every t.co resolved to its destination,
// with a quoted tweet appended so the reply is not left dangling without the
// thing it replies to.
func body(t gotw.Tweet) string {
	out := t.ExpandedText()
	if q := t.Quoted; q != nil {
		out = strings.TrimRight(out, "\n") + "\n\n— @" + q.User.ScreenName + ": " + q.ExpandedText()
	}
	return out
}

// displayName prefers the author's real name over the handle, falling back to
// the handle when the payload carries no name.
func displayName(u gotw.User) string {
	if n := strings.TrimSpace(u.Name); n != "" {
		return n
	}
	return u.ScreenName
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
