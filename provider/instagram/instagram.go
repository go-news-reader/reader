// Package instagram adapts the standalone github.com/go-instagram/instagram
// best-effort client to the aggregator's source.Provider contract. A public
// account username as the query channel fetches that account's recent posts; the
// reserved channels "home" and "following" fetch the logged-in user's own home
// (following) timeline through Instagram's private web feed endpoint.
//
// This provider is inherently fragile: without a valid sessionid cookie
// Instagram frequently returns 401/403/429, surfaced here as errors. The home
// timeline additionally needs the csrftoken cookie (sent as X-CSRFToken); import
// the full Instagram cookie string ("sessionid=…; csrftoken=…") so both are
// present, otherwise the endpoint answers 302→login and the read is reported as a
// sign-in prompt.
package instagram

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-browserhttp/browserhttp"
	goig "github.com/go-instagram/instagram"

	"github.com/go-news-reader/reader/source"
)

// ErrNoChannel is returned when Feed is called without a username.
var ErrNoChannel = errors.New("instagram: Query.Channel must be a username")

// defaultTimeout bounds a read on the client this package builds for itself.
const defaultTimeout = 30 * time.Second

// client is the slice of *goig.Client the adapter uses; an interface for tests.
// UserPosts fetches an account's posts by its numeric id, skipping the
// web_profile_info round-trip UserProfile makes to resolve that id — the endpoint
// Instagram rate-limits at scale — for accounts whose id is already cached.
type client interface {
	UserProfile(ctx context.Context, username string) (*goig.Profile, error)
	UserPosts(ctx context.Context, userID, profileUser string) ([]goig.Post, error)
}

// follower lists the accounts the authenticated user follows, paging the private
// friendships endpoint. The real *goig.Client satisfies it; tests supply a fake.
type follower interface {
	CurrentUserID(ctx context.Context) (string, error)
	Following(ctx context.Context, userID, maxID string) (*goig.FollowingPage, error)
}

// Provider fetches a public Instagram profile's recent posts as items, and — for
// the reserved "home"/"following" channels — the authenticated home timeline.
type Provider struct {
	client  client
	hasCred bool // a sessionid was configured

	// Home-timeline plumbing. hc performs the raw private-endpoint request,
	// sessionID + csrf authenticate it, appID is the x-ig-app-id header, and
	// homeBase is the request origin (overridden in tests to a local server).
	hc        *http.Client
	sessionID string
	csrf      string
	appID     string
	homeBase  string

	// Follow-import plumbing. follow lists the authenticated user's follows, and
	// userID is the viewer's own id lifted from the ds_user_id cookie when the
	// imported session carried one (else resolved through follow.CurrentUserID).
	follow follower
	userID string

	// idCache memoizes a followed account's screen name → its permanent numeric
	// id, so a refresh resolves each account through web_profile_info (which
	// Instagram rate-limits at scale) only once, then reads its posts straight
	// from the feed endpoint. Guarded by idMu.
	idMu    sync.Mutex
	idCache map[string]string
}

// New returns a provider. session is an optional Instagram session for
// authenticated reads: a bare sessionid cookie value, or a full cookie string
// ("sessionid=…; csrftoken=…") — the latter also unlocks the home timeline.
// Empty means anonymous, best-effort.
func New(session string) *Provider { return newWith(nil, session) }

// NewWithHTTPClient returns a provider whose reads go through hc (e.g. the
// shared, request-logging client so the Network log captures Instagram traffic).
func NewWithHTTPClient(hc *http.Client, session string) *Provider {
	return newWith(hc, session)
}

// newWith builds the provider, wiring hc when non-nil. It splits session into a
// bare sessionid (sent as the sessionid cookie on every request) and, when a full
// cookie string was supplied, the csrftoken the home endpoint requires.
func newWith(hc *http.Client, session string) *Provider {
	sessionID, csrf := splitSession(session)
	if hc == nil {
		hc = browserhttp.NewClient(defaultTimeout)
	}
	opts := []goig.Option{goig.WithHTTPClient(hc)}
	if sessionID != "" {
		opts = append(opts, goig.WithSessionID(sessionID))
	}
	if csrf != "" {
		opts = append(opts, goig.WithCSRFToken(csrf))
	}
	gc := goig.New(opts...)
	return &Provider{
		client:    gc,
		hasCred:   sessionID != "",
		hc:        hc,
		sessionID: sessionID,
		csrf:      csrf,
		appID:     goig.DefaultAppID,
		homeBase:  goig.DefaultBaseURL,
		follow:    gc,
		userID:    cookieValue(session, "ds_user_id"),
		idCache:   map[string]string{},
	}
}

// NewWithClient wraps a preconfigured client (or a fake in tests).
func NewWithClient(c client) *Provider {
	return &Provider{client: c, idCache: map[string]string{}}
}

// splitSession separates an Instagram session field into its sessionid and
// csrftoken. A value containing "=" is treated as a full cookie string and both
// cookies are lifted from it; anything else is a bare sessionid with no csrf.
func splitSession(s string) (sessionID, csrf string) {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "=") {
		return cookieValue(s, "sessionid"), cookieValue(s, "csrftoken")
	}
	return s, ""
}

// cookieValue extracts the value of the named cookie from a "; "-separated cookie
// string, or "" when it is absent.
func cookieValue(cookieStr, name string) string {
	for _, part := range strings.Split(cookieStr, ";") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) == 2 && kv[0] == name {
			return kv[1]
		}
	}
	return ""
}

// Kind reports source.Instagram.
func (p *Provider) Kind() source.Kind { return source.Instagram }

// Feed returns items for the query. The reserved channels "home" and "following"
// fetch the authenticated user's home (following) timeline; any other channel is
// a public account username whose recent posts are returned (not paginated, so
// Result.Cursor is empty).
func (p *Provider) Feed(ctx context.Context, q source.Query) (source.Result, error) {
	if isHomeChannel(q.Channel) {
		return p.homeFeed(ctx, q)
	}
	user := strings.TrimPrefix(strings.TrimSpace(q.Channel), "@")
	if user == "" {
		return source.Result{}, ErrNoChannel
	}
	posts, err := p.userPosts(ctx, user)
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
	items := make([]source.Item, 0, len(posts))
	for _, post := range posts {
		if limit > 0 && len(items) >= limit {
			break
		}
		items = append(items, mapPost(user, post))
	}
	return source.Result{Items: items}, nil
}

// userPosts returns a public account's recent posts. Once the account's numeric
// id is known it reads the posts straight from the feed endpoint (one request,
// via UserPosts); until then it resolves the id through UserProfile — the
// web_profile_info round-trip Instagram rate-limits at scale — and caches it. A
// cached-id read that fails re-resolves through UserProfile, so a rotated id or a
// transient blip self-heals rather than sticking.
func (p *Provider) userPosts(ctx context.Context, user string) ([]goig.Post, error) {
	if id := p.cachedID(user); id != "" {
		if posts, err := p.client.UserPosts(ctx, id, user); err == nil {
			return posts, nil
		}
		p.forgetID(user)
	}
	prof, err := p.client.UserProfile(ctx, user)
	if err != nil {
		return nil, err
	}
	if prof.ID != "" {
		p.cacheID(user, prof.ID)
	}
	return prof.Posts, nil
}

// cachedID returns the memoized numeric id for user, or "".
func (p *Provider) cachedID(user string) string {
	p.idMu.Lock()
	defer p.idMu.Unlock()
	return p.idCache[user]
}

// cacheID memoizes user's numeric id. Every constructor initializes idCache, so
// it is never nil here.
func (p *Provider) cacheID(user, id string) {
	p.idMu.Lock()
	defer p.idMu.Unlock()
	p.idCache[user] = id
}

// forgetID drops a cached id after a read with it failed, so the next fetch
// re-resolves through UserProfile.
func (p *Provider) forgetID(user string) {
	p.idMu.Lock()
	defer p.idMu.Unlock()
	delete(p.idCache, user)
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
		Created:   source.UnixOrZero(p.Timestamp),
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
