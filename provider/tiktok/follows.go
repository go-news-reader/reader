package tiktok

import (
	"context"

	"github.com/go-news-reader/reader/source"
)

// followMaxPages bounds how many user-list pages MyFollows walks.
const followMaxPages = 40

// viewerResolver resolves the authenticated viewer's own secUid. The real
// *gott.Client satisfies it (via ViewerSecUID, which parses the embedded
// rehydration JSON of an authenticated page); tests supply a fake.
type viewerResolver interface {
	ViewerSecUID(ctx context.Context) (string, error)
}

// MyFollows imports the connected TikTok user's following list. It resolves the
// viewer's own secUid (from the session-authenticated web app) and then pages
// the library's signed /api/user/list/ endpoint. It satisfies
// [source.FollowImporter].
//
// Honest scope: the signed request needs a browser-minted msToken (and the
// fingerprint-derived X-Gnarly token) that a pure-Go client cannot forge, so
// against live TikTok the user-list refusal is surfaced as a typed
// [source.AuthError] rather than a fabricated list. When a valid msToken/session
// is supplied and TikTok returns real users, they are mapped to subscriptions.
func (p *Provider) MyFollows(ctx context.Context) ([]source.Subscription, error) {
	if !p.hasCred {
		return nil, source.NeedsAuth(source.TikTok,
			"sign in to TikTok and import your ms_token + session cookie to import your following list")
	}
	sec := p.followSecUID
	if sec == "" {
		if p.viewer == nil {
			// No viewer resolver and no preset secUid: nothing to key the list on.
			return nil, source.NeedsAuth(source.TikTok,
				"TikTok's following list needs your account secUid and a signed request, "+
					"which this pure-Go client cannot forge")
		}
		var err error
		sec, err = p.viewer.ViewerSecUID(ctx)
		if err != nil {
			return nil, followWallErr(err)
		}
	}

	var out []source.Subscription
	cursor := "0"
	for page := 0; page < followMaxPages; page++ {
		list, err := p.follow.Following(ctx, sec, defaultCount, cursor)
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
