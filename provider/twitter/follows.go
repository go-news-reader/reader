package twitter

import (
	"context"
	"errors"
	"net/url"
	"strings"

	gotw "github.com/go-birdsite/twitter"

	"github.com/go-news-reader/reader/source"
)

// followMaxPages bounds how many Following-list pages MyFollows walks, so X's
// always-present bottom cursor cannot drive an unbounded fetch. 40 pages at the
// query's page size covers far more follows than a typical account.
const followMaxPages = 40

// MyFollows returns every account the connected X user follows as a ready
// subscription, so the aggregator's generic "import my subscriptions" action
// adds them all. It pages the private GraphQL Following query — keyed on the
// viewer's own id (from the twid cookie of the imported session) — and returns
// each followed account as its screen name, the channel form [Provider.Feed]
// resolves to that account's tweets.
//
// It satisfies [source.FollowImporter]. The logged-in auth_token + ct0 cookies
// are required, and X's rotating query id / expired sessions surface as typed
// [source.AuthError] prompts (via [gotw.ErrNeedsAuth] / [gotw.ErrQueryIDRotated])
// rather than raw errors or a fabricated list.
func (p *Provider) MyFollows(ctx context.Context) ([]source.Subscription, error) {
	if p.authToken == "" || p.csrf == "" {
		return nil, source.NeedsAuth(source.Twitter,
			"sign in to X and import your session cookie (auth_token + ct0) to import your following list")
	}
	if p.followUserID == "" {
		return nil, source.NeedsAuth(source.Twitter,
			"could not determine your X account id; import your full X cookie (including twid)")
	}

	var out []source.Subscription
	cursor := ""
	for page := 0; page < followMaxPages; page++ {
		pg, err := p.follow.Following(ctx, p.followUserID, cursor)
		if err != nil {
			return nil, mapFollowErr(err)
		}
		for _, u := range pg.Users {
			if u.ScreenName == "" {
				continue
			}
			out = append(out, source.Subscription{Source: source.Twitter, Channel: u.ScreenName})
		}
		// X returns a stable bottom cursor even past the end, so stop when it is
		// empty or a page yields no users — never loop on a terminal cursor.
		if pg.Cursor == "" || len(pg.Users) == 0 {
			break
		}
		cursor = pg.Cursor
	}
	return out, nil
}

// mapFollowErr turns the client's typed Following failures into user-actionable
// sign-in prompts, leaving genuine transient errors unchanged.
func mapFollowErr(err error) error {
	switch {
	case errors.Is(err, gotw.ErrNeedsAuth):
		return source.NeedsAuth(source.Twitter,
			"X refused your following list; re-import a fresh auth_token + ct0")
	case errors.Is(err, gotw.ErrQueryIDRotated):
		return source.NeedsAuth(source.Twitter,
			"X changed its Following query id; the following-list import is temporarily unavailable")
	default:
		return err
	}
}

// viewerID extracts the viewer's numeric account id from the twid cookie of a
// full X cookie string. The cookie's value is the URL-encoded form of
// `u=<id>` (e.g. `twid=u%3D1234567890`); this unescapes it and strips the `u=`
// prefix, returning "" when the session carried no twid.
func viewerID(session string) string {
	v := cookieValue(session, "twid")
	if v == "" {
		return ""
	}
	if dec, err := url.QueryUnescape(v); err == nil {
		v = dec
	}
	v = strings.Trim(v, `"`)
	return strings.TrimPrefix(v, "u=")
}

// Provider implements the follow-import capability.
var _ source.FollowImporter = (*Provider)(nil)
