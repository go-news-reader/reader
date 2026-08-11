package instagram

import (
	"context"

	"github.com/go-news-reader/reader/source"
)

// followMaxPages bounds how many following-list pages MyFollows walks, so a
// pathological or looping cursor cannot drive an unbounded fetch. 40 pages at
// Instagram's default page size covers far more follows than a typical account.
const followMaxPages = 40

// MyFollows returns every account the connected Instagram user follows as a
// ready subscription, so the aggregator's generic "import my subscriptions"
// action adds them all. It resolves the viewer's own id (from the ds_user_id
// cookie the imported session carried, else the private current_user endpoint),
// then pages the friendships following list, returning each followed account as
// its username — the channel form [Provider.Feed] resolves to that account's
// public posts.
//
// It satisfies [source.FollowImporter]. A sessionid is required; without one, or
// when Instagram refuses the private endpoint (302→login / 401 / 403), it returns
// a typed [source.AuthError] prompting a fresh sign-in rather than a raw error.
func (p *Provider) MyFollows(ctx context.Context) ([]source.Subscription, error) {
	if p.sessionID == "" {
		return nil, source.NeedsAuth(source.Instagram,
			"sign in to Instagram and import your session cookie to import your following list")
	}

	userID := p.userID
	if userID == "" {
		id, err := p.follow.CurrentUserID(ctx)
		if err != nil {
			return nil, mapFollowErr(err)
		}
		userID = id
	}

	var out []source.Subscription
	maxID := ""
	for page := 0; page < followMaxPages; page++ {
		pg, err := p.follow.Following(ctx, userID, maxID)
		if err != nil {
			return nil, mapFollowErr(err)
		}
		for _, u := range pg.Users {
			if u.Username == "" {
				continue
			}
			out = append(out, source.Subscription{Source: source.Instagram, Channel: u.Username})
		}
		if pg.NextMaxID == "" {
			break
		}
		maxID = pg.NextMaxID
	}
	return out, nil
}

// mapFollowErr promotes Instagram's "blocking you" statuses (401/403, folded into
// the client's error text) to a typed auth prompt, leaving genuine transient
// failures unchanged.
func mapFollowErr(err error) error {
	if source.ErrHasAuthStatus(err) {
		return source.NeedsAuth(source.Instagram,
			"Instagram refused your following list; re-import a fresh sessionid + csrftoken")
	}
	return err
}

// Provider implements the follow-import capability.
var _ source.FollowImporter = (*Provider)(nil)
