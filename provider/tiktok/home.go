package tiktok

import (
	"context"
	"fmt"
	"strings"

	gott "github.com/go-tiktok/tiktok"

	"github.com/go-news-reader/reader/source"
)

// homeChannels are the reserved subscription channels that select the
// authenticated following feed instead of a public account.
var homeChannels = map[string]bool{"home": true, "following": true}

// isHomeChannel reports whether ch names the following feed.
func isHomeChannel(ch string) bool {
	return homeChannels[strings.ToLower(strings.TrimSpace(ch))]
}

// gottClient builds a signing github.com/go-tiktok/tiktok client bound to the
// provider's HTTP client, origin, and imported credentials. It is the single
// place the provider constructs the underlying signer, so every request it makes
// carries the pure-Go X-Bogus signature the library computes.
func (p *Provider) gottClient() *gott.Client {
	opts := []gott.Option{gott.WithHTTPClient(p.hc)}
	if p.homeBase != "" {
		opts = append(opts, gott.WithBaseURL(p.homeBase))
	}
	if p.msToken != "" {
		opts = append(opts, gott.WithMSToken(p.msToken))
	}
	if p.session != "" {
		opts = append(opts, gott.WithSessionID(p.session))
	}
	return gott.New(opts...)
}

// followingFeed returns the authenticated viewer's following feed. It requires
// an imported credential (msToken/sessionid) and delegates to the library's
// signed /api/following/item_list/ request. When TikTok answers real videos they
// are mapped and returned; when it fires its anti-bot wall — which, absent a
// browser-minted msToken, it always does even though the X-Bogus signature is
// correct — the refusal is surfaced as a typed [source.AuthError] via
// [followFeedWall], never a fabricated feed.
func (p *Provider) followingFeed(ctx context.Context, q source.Query) (source.Result, error) {
	if !p.hasCred {
		return source.Result{}, source.NeedsAuth(source.TikTok,
			"sign in to TikTok and import your ms_token + session cookie for the following feed")
	}

	count := q.Limit
	if count <= 0 {
		count = defaultCount
	}
	feed, err := p.gottClient().FollowingFeed(ctx, count, firstNonEmpty(q.Cursor, "0"))
	if err != nil {
		return source.Result{}, followFeedWall(err)
	}

	items := make([]source.Item, 0, len(feed.Videos))
	for _, v := range feed.Videos {
		items = append(items, mapHomeVideo(v))
	}
	cursor := ""
	if feed.HasMore {
		cursor = feed.Cursor
	}
	return source.Result{Items: items, Cursor: cursor}, nil
}

// followFeedWall reports a following-feed failure as the typed anti-bot/sign-in
// wall it is. Without a browser-minted msToken (and the fingerprint-derived
// X-Gnarly token), TikTok refuses the correctly X-Bogus-signed request with an
// empty body or a non-zero in-band statusCode; every such refusal is really "a
// signed request needs credentials this pure-Go client cannot mint".
func followFeedWall(err error) error {
	return source.NeedsAuth(source.TikTok,
		"TikTok refused the following feed (a browser-minted msToken / X-Gnarly this "+
			"pure-Go client cannot forge is required): "+err.Error())
}

// mapHomeVideo projects one following-feed video onto a normalized Item under the
// reserved "home" channel.
func mapHomeVideo(v gott.Video) source.Item {
	item := source.Item{
		ID:       v.ID,
		Source:   source.TikTok,
		Channel:  "home",
		Author:   v.Author,
		Body:     v.Description,
		Score:    v.Likes,
		Comments: v.Comments,
		Created:  source.UnixOrZero(v.CreateTime),
	}
	if v.Author != "" && v.ID != "" {
		item.Permalink = fmt.Sprintf("%s/@%s/video/%s", gott.DefaultBaseURL, v.Author, v.ID)
	}
	if v.CoverURL != "" {
		item.Media = append(item.Media, source.Media{URL: v.CoverURL, Kind: source.MediaThumbnail})
	}
	if v.PlayURL != "" {
		item.Media = append(item.Media, source.Media{URL: v.PlayURL, Kind: source.MediaVideo})
	}
	return item
}

// firstNonEmpty returns the first non-empty string among its arguments.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
