package tiktok

import (
	"context"

	"github.com/go-news-reader/reader/source"
)

// followMaxPages bounds how many user-list pages MyFollows walks.
const followMaxPages = 40

// MyFollows attempts to import the connected TikTok user's following list, but
// TikTok gates the web user-list endpoint behind its request-signing wall
// (X-Bogus / _signature, derived from the viewer's own secUid) that a pure-Go
// client cannot forge — the same wall that blocked TikTok's home feed in the
// authenticated-home work. It therefore gates on a configured session and
// reports a typed [source.AuthError] describing the wall, rather than crashing
// or fabricating a follow list. It satisfies [source.FollowImporter].
//
// The blocker is twofold: the list keys on the viewer's own secUid, which is not
// carried by any cookie and cannot be derived without the signed endpoints, and
// the request itself must carry a valid signed parameter. Both are unreachable
// from pure Go, so in practice this always returns the wall error.
func (p *Provider) MyFollows(ctx context.Context) ([]source.Subscription, error) {
	if !p.hasCred {
		return nil, source.NeedsAuth(source.TikTok,
			"sign in to TikTok and import your ms_token + session cookie to import your following list")
	}
	if p.followSecUID == "" {
		// No viewer secUid: TikTok exposes it only through the signed endpoints
		// this pure-Go client cannot reach. Report the wall rather than guessing.
		return nil, source.NeedsAuth(source.TikTok,
			"TikTok's following list needs your account secUid and a signed request "+
				"(X-Bogus/_signature), which this pure-Go client cannot forge")
	}

	var out []source.Subscription
	cursor := "0"
	for page := 0; page < followMaxPages; page++ {
		list, err := p.follow.Following(ctx, p.followSecUID, defaultCount, cursor)
		if err != nil {
			return nil, followWallErr(err)
		}
		for _, u := range list.Users {
			if u.SecUID == "" {
				continue
			}
			out = append(out, source.Subscription{Source: source.TikTok, Channel: u.SecUID})
		}
		if !list.HasMore || list.MaxCursor == "" {
			break
		}
		cursor = list.MaxCursor
	}
	return out, nil
}

// followWallErr reports any user-list failure as the typed anti-bot/sign-in wall
// it is: the following list is unreachable without TikTok's request-signing, so
// every refusal (non-2xx, empty body, non-zero statusCode) is really "a signed
// request is required" — surfaced the same way the home feed reports the wall.
func followWallErr(err error) error {
	return source.NeedsAuth(source.TikTok,
		"TikTok refused the following list (anti-bot signing wall); a signed request "+
			"this pure-Go client cannot forge is required: "+err.Error())
}

// Provider implements the follow-import capability.
var _ source.FollowImporter = (*Provider)(nil)
