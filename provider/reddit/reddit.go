// Package reddit adapts the standalone github.com/go-reddit/reddit client to
// the aggregator's source.Provider contract. Anonymous fetches go through the
// shared browserhttp uTLS client so Reddit's TLS-fingerprint anti-bot 403 does
// not fire; a session-cookie-authenticated client can be supplied instead (the
// user's own logged-in reddit_session), since Reddit's self-serve OAuth app
// registration is effectively closed to new personal projects.
package reddit

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-browserhttp/browserhttp"
	goreddit "github.com/go-reddit/reddit"

	"github.com/go-news-reader/reader/provider/redgifs"
	"github.com/go-news-reader/reader/source"
)

// fetcher is the slice of *goreddit.Client the provider needs. Declaring it as
// an interface lets tests inject a fake without any network.
type fetcher interface {
	Subreddit(ctx context.Context, name string, sort goreddit.Sort, opts goreddit.ListingOptions) (*goreddit.Page, error)
	UserPosts(ctx context.Context, name string, sort goreddit.Sort, opts goreddit.ListingOptions) (*goreddit.Page, error)
	Frontpage(ctx context.Context, sort goreddit.Sort, opts goreddit.ListingOptions) (*goreddit.Page, error)
	Comments(ctx context.Context, subreddit, id string, opts goreddit.ListingOptions) (*goreddit.PostWithComments, error)
	MySubreddits(ctx context.Context) ([]goreddit.SubredditInfo, error)
	Friends(ctx context.Context) ([]goreddit.Friend, error)
	SearchSubreddits(ctx context.Context, query string, opts goreddit.ListingOptions) (*goreddit.SubredditPage, error)
	SearchPosts(ctx context.Context, query, subreddit string, sort goreddit.SearchSort, opts goreddit.ListingOptions) (*goreddit.Page, error)
}

// RedgifsResolver resolves a RedGIFs gif id to its media variant URLs. The
// reddit provider uses it to turn a redgifs.com/watch/<id> or /ifr/<id> link
// pasted into a post into the actual playable mp4 + poster, instead of the
// static poster Reddit embeds. *redgifs.Client satisfies it; tests inject a fake
// so no live network is touched.
type RedgifsResolver interface {
	GifByID(ctx context.Context, id string) (*redgifs.Gif, error)
}

// Provider fetches Reddit posts as normalized source items.
type Provider struct {
	client fetcher
	// redgifs resolves RedGIFs links embedded in posts to direct media; nil
	// disables that resolution (the post keeps Reddit's static poster).
	redgifs RedgifsResolver
}

// New returns an anonymous Reddit provider backed by the portable browser
// fingerprint HTTP client (pure Go, CGO=0, no host web view).
func New() *Provider {
	return NewWithHTTPClient(browserhttp.NewClient(30 * time.Second))
}

// NewWithHTTPClient returns an anonymous Reddit provider driving hc (e.g. the
// shared, request-logging client the aggregator builds so the Network log can
// show Reddit's traffic). The browser User-Agent is kept so the fingerprint
// still matches.
func NewWithHTTPClient(hc *http.Client) *Provider {
	c := goreddit.NewClient(
		goreddit.WithHTTPClient(hc),
		goreddit.WithUserAgent(browserhttp.DefaultUserAgent),
	)
	return &Provider{client: c, redgifs: redgifs.NewClientWithHTTPClient(hc)}
}

// NewWithCookie returns a Reddit provider that authenticates reads with the
// user's own logged-in browser reddit_session cookie, driving hc (the shared,
// request-logging client the aggregator builds). Reddit's self-serve OAuth
// registration is effectively closed to new personal projects, so supplying the
// cookie from an already-signed-in browser is the practical path for
// individual, read-only use — the same session-cookie pattern the reader
// already offers for Instagram and TikTok. The cookie keeps the anonymous
// www ".json" endpoints (no OAuth). A nil hc falls back to the portable
// browser-fingerprint client so the constructor is safe on its own.
func NewWithCookie(hc *http.Client, sessionCookie string) *Provider {
	if hc == nil {
		hc = browserhttp.NewClient(30 * time.Second)
	}
	c := goreddit.NewClient(
		goreddit.WithHTTPClient(hc),
		goreddit.WithUserAgent(browserhttp.DefaultUserAgent),
		goreddit.WithSessionCookie(sessionCookie),
	)
	return &Provider{client: c, redgifs: redgifs.NewClientWithHTTPClient(hc)}
}

// NewWithClient wraps an already-configured reddit client — e.g. a
// session-cookie (logged-in) client, or a fake in tests. It has no RedGIFs
// resolver; attach one with [Provider.WithRedgifs].
func NewWithClient(c fetcher) *Provider { return &Provider{client: c} }

// WithRedgifs sets the resolver that turns RedGIFs links inside posts into
// direct media and returns the provider for chaining. The network constructors
// wire a real *redgifs.Client automatically; this is for [NewWithClient] and for
// injecting a fake in tests.
func (p *Provider) WithRedgifs(r RedgifsResolver) *Provider {
	p.redgifs = r
	return p
}

// Kind reports source.Reddit.
func (p *Provider) Kind() source.Kind { return source.Reddit }

// Feed returns a page of posts. An empty Query.Channel fetches the front page;
// otherwise it fetches r/<Channel>. Query.Cursor is the reddit "after" token.
func (p *Provider) Feed(ctx context.Context, q source.Query) (source.Result, error) {
	opts := goreddit.ListingOptions{Limit: q.Limit, After: q.Cursor}
	sort := parseSort(q.Sort)

	// A subscription channel distinguishes a subreddit ("r/<name>" or a bare
	// name) from a redditor ("u/<name>"): u/ routes to the person's submissions,
	// everything else to a subreddit; an empty channel is the front page. goreddit
	// strips the r/ or u/ prefix itself.
	ch := strings.TrimSpace(q.Channel)
	var page *goreddit.Page
	var err error
	switch {
	case ch == "":
		page, err = p.client.Frontpage(ctx, sort, opts)
	case strings.HasPrefix(strings.ToLower(ch), searchPrefix):
		// A saved keyword search ("search:<query>"): a live subscription whose feed
		// is Reddit's site-wide post search for the query, so fresh matches keep
		// flowing into the aggregated feed. The default relevance order applies.
		query := strings.TrimSpace(ch[len(searchPrefix):])
		page, err = p.client.SearchPosts(ctx, query, "", goreddit.SearchRelevance, opts)
	case strings.HasPrefix(strings.ToLower(ch), "u/"):
		page, err = p.client.UserPosts(ctx, ch, sort, opts)
	default:
		page, err = p.client.Subreddit(ctx, ch, sort, opts)
	}
	if err != nil {
		return source.Result{}, mapErr(err)
	}

	items := make([]source.Item, 0, len(page.Posts))
	for _, post := range page.Posts {
		it := p.mapPost(ctx, post)
		// Tag every item with the subscription's own channel (its r/ or u/ form) so
		// the feed's per-subscription filter matches — a redditor's posts span many
		// subreddits, so their intrinsic subreddit would never equal the u/ channel.
		if ch != "" {
			it.Channel = ch
		}
		items = append(items, it)
	}
	return source.Result{Items: items, Cursor: page.After}, nil
}

// Comment-thread bounds: a very active post can carry tens of thousands of
// comments nested dozens deep, which would explode the pane's memory and layout
// time. Comments flattens at most maxComments nodes and stops descending past
// maxCommentDepth levels, so the preview stays bounded however large the thread.
const (
	maxComments     = 200
	maxCommentDepth = 8
)

// Comments fetches a post's comment thread and flattens it into display order
// with a per-comment Depth (0 = top-level reply). It goes through the same
// (anonymous or session-cookie) client as the feed, so the user's auth applies.
// The tree is walked depth-first, preserving the reddit ordering; "load more"
// frontier markers are already dropped by the client (only "t1" things survive),
// and deleted/empty-body nodes are skipped here while their replies are kept, so
// a live subtree under a removed parent is not lost.
func (p *Provider) Comments(ctx context.Context, postID, subreddit string) ([]source.Comment, error) {
	pwc, err := p.client.Comments(ctx, subreddit, postID, goreddit.ListingOptions{})
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]source.Comment, 0, len(pwc.Comments))
	flattenComments(pwc.Comments, 0, &out)
	return out, nil
}

// flattenComments walks the nested comment tree depth-first, appending each
// non-empty comment to out with its nesting depth. It stops descending once
// depth reaches maxCommentDepth and stops appending once out reaches
// maxComments, so a huge thread stays bounded. A deleted/removed comment (empty
// body) is dropped from the output but its replies are still visited (at the
// next depth), so real replies below a removed parent survive.
func flattenComments(nodes []goreddit.Comment, depth int, out *[]source.Comment) {
	if depth >= maxCommentDepth || len(*out) >= maxComments {
		return
	}
	for _, c := range nodes {
		if len(*out) >= maxComments {
			return
		}
		if strings.TrimSpace(c.Body) != "" {
			*out = append(*out, source.Comment{
				Author:  c.Author,
				Body:    c.Body,
				Score:   c.Score,
				Created: int64(c.CreatedUTC),
				Depth:   depth,
			})
		}
		flattenComments(c.Replies, depth+1, out)
	}
}

// MySubscriptions returns the r/<name> channels of every subreddit the
// authenticated account follows, ready to add as subscriptions. It requires a
// session-cookie (logged-in) client; an anonymous client yields an error.
func (p *Provider) MySubscriptions(ctx context.Context) ([]string, error) {
	subs, err := p.client.MySubreddits(ctx)
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]string, 0, len(subs))
	for _, s := range subs {
		if s.Name == "" {
			continue
		}
		out = append(out, "r/"+s.Name)
	}
	return out, nil
}

// MyFollows returns the connected Reddit account's follows as ready
// subscriptions: every subreddit it is subscribed to (r/<name>) plus every
// redditor it follows (u/<name>). It satisfies [source.FollowImporter], so the
// aggregator's generic "import my subscriptions" action pulls both in one call.
// A session-cookie (logged-in) client is required; an anonymous client yields a
// typed auth error.
func (p *Provider) MyFollows(ctx context.Context) ([]source.Subscription, error) {
	channels, err := p.MySubscriptions(ctx)
	if err != nil {
		return nil, err // already mapped by MySubscriptions
	}
	out := make([]source.Subscription, 0, len(channels))
	for _, ch := range channels {
		out = append(out, source.Subscription{Source: source.Reddit, Channel: ch})
	}
	// Friends (followed u/ users) sit behind an OAuth-only endpoint a cookie
	// session cannot reach, so a failure here must not sink the whole import: the
	// subscribed subreddits gathered above are the point. Fold friends in when the
	// call happens to succeed, and otherwise return the subscriptions alone.
	if friends, ferr := p.client.Friends(ctx); ferr == nil {
		for _, f := range friends {
			if f.Name == "" {
				continue
			}
			out = append(out, source.Subscription{Source: source.Reddit, Channel: "u/" + f.Name})
		}
	}
	return out, nil
}

// searchPrefix marks a subscription channel as a saved keyword search
// ("search:<query>"): [Provider.Feed] routes it to a site-wide post search
// instead of a subreddit fetch, and [normalizeChannel] leaves it verbatim (it is
// not an r/ subreddit). The comparison is done in lower-case, so any casing of
// the prefix is accepted.
const searchPrefix = "search:"

// SearchSubreddits discovers subreddits whose name, title or description matches
// query, mapping Reddit's search results onto the aggregator's [source.SubredditResult]
// discovery type. A blank query is rejected. The caller may narrow the returned
// names/descriptions further with a local regexp filter (Reddit itself cannot
// enumerate subreddits, so this search is the only discovery path).
func (p *Provider) SearchSubreddits(ctx context.Context, query string) ([]source.SubredditResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("reddit: empty search query")
	}
	page, err := p.client.SearchSubreddits(ctx, query, goreddit.ListingOptions{})
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]source.SubredditResult, 0, len(page.Subreddits))
	for _, si := range page.Subreddits {
		if si.Name == "" {
			continue
		}
		out = append(out, source.SubredditResult{
			Name:        si.Name,
			Title:       si.Title,
			Description: si.PublicDescription,
			Subscribers: si.Subscribers,
			NSFW:        si.Over18,
		})
	}
	return out, nil
}

// SearchPosts searches Reddit for posts matching query and maps them onto the
// normalized feed items (through the same mapPost the feed uses). When subreddit
// is non-empty the search is restricted to it; otherwise it is site-wide. The
// default relevance order applies. A blank query is rejected by the underlying
// client.
func (p *Provider) SearchPosts(ctx context.Context, query, subreddit string) ([]source.Item, error) {
	page, err := p.client.SearchPosts(ctx, query, subreddit, goreddit.SearchRelevance, goreddit.ListingOptions{})
	if err != nil {
		return nil, mapErr(err)
	}
	items := make([]source.Item, 0, len(page.Posts))
	for _, post := range page.Posts {
		items = append(items, p.mapPost(ctx, post))
	}
	return items, nil
}

// mapErr translates a Reddit client failure into a typed source.AuthError when
// it is a 401/403 — the anonymous ".json" endpoints 403 from data-center IPs, and
// a stale/rejected session cookie returns 401 — so the UI can prompt the user to
// sign in instead of showing a raw status. Other failures (network, 429, 5xx)
// pass through unchanged as transient errors.
func mapErr(err error) error {
	var apiErr *goreddit.APIError
	if errors.As(err, &apiErr) && source.HTTPAuthStatus(apiErr.StatusCode) {
		return source.NeedsAuth(source.Reddit, "log into Reddit in your browser, then import the session cookie")
	}
	return err
}

// parseSort maps a generic sort hint onto reddit's vocabulary, defaulting to
// "hot" for empty or unrecognized values.
func parseSort(s string) goreddit.Sort {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "new":
		return goreddit.SortNew
	case "top":
		return goreddit.SortTop
	case "rising":
		return goreddit.SortRising
	case "controversial", "controvers":
		return goreddit.SortControvers
	case "best":
		return goreddit.SortBest
	default:
		return goreddit.SortHot
	}
}

// mapPost projects a reddit Post onto the normalized Item, resolving the
// external media host the post embeds where it can so the reader shows the
// actual media instead of a blank page or a bare link.
func (pr *Provider) mapPost(ctx context.Context, p goreddit.Post) source.Item {
	it := source.Item{
		ID:        p.ID,
		Source:    source.Reddit,
		Channel:   p.Subreddit,
		Title:     p.Title,
		Author:    p.Author,
		Body:      p.SelfText,
		Permalink: "https://www.reddit.com" + p.Permalink,
		Score:     p.Score,
		Comments:  p.NumComments,
		Created:   int64(p.CreatedUTC),
		NSFW:      p.Over18,
		Pinned:    p.Stickied,
	}
	if p.Flair != "" {
		it.Tags = []string{p.Flair}
	}
	if isThumbURL(p.Thumbnail) {
		it.Media = append(it.Media, source.Media{URL: p.Thumbnail, Kind: source.MediaThumbnail})
	}
	if p.IsSelf {
		return it // a text post: its content is Body, no link/media to resolve
	}

	// Host-specific resolution: an imgur link resolves to its direct image/mp4,
	// and a redgifs link resolves — via the RedGIFs API — to the real mp4 + poster
	// (Reddit only embeds a static poster for these). Each fully handles the media
	// for the posts it matches, so return once one does.
	if resolveImgur(&it, p.URL) {
		return it
	}
	if pr.resolveRedgifs(ctx, &it, p) {
		return it
	}

	// The generic path: reddit's own image/gallery/hosted-video, plus the
	// separate v.redd.it audio track when the post is a reddit-hosted video.
	resolveGeneric(&it, p)
	return it
}

// resolveGeneric maps a reddit post's own media the way the reader always has: a
// media post (image, gallery, hosted/rich video) points Link at Reddit's
// resolved display image — the picture, the gallery's first image, or the
// video's poster — which the preview pane renders and zooms, rather than at a
// media-page URL (a gallery permalink or a JS embed page would paint blank). A
// plain external article falls through to its page. On top of that a
// reddit-hosted video also exposes its playable stream and, derived from it, the
// separate DASH audio track (see [redditAudioURL]) so a future player can mux
// the two — Reddit serves video and audio as distinct files.
func resolveGeneric(it *source.Item, p goreddit.Post) {
	display := p.PreviewImage()
	isMedia := p.IsGallery || p.IsVideo || isImageURL(p.URL) ||
		p.PostHint == "image" || p.PostHint == "rich:video" || p.PostHint == "hosted:video"
	if isMedia && display != "" {
		it.Link = display
		it.Media = append(it.Media, source.Media{URL: display, Kind: source.MediaImage})
	} else {
		// A plain external article (or a media post whose image could not be
		// resolved): render the linked page. A real image URL is always caught
		// above (PreviewImage resolves it), so nothing is mapped as media here.
		it.Link = p.URL
	}
	if v := p.VideoURL(); v != "" {
		it.Media = append(it.Media, source.Media{URL: v, Kind: source.MediaVideo})
		if a := redditAudioURL(v); a != "" {
			it.Media = append(it.Media, source.Media{URL: a, Kind: source.MediaAudio})
		}
	}
}

// isThumbURL reports whether a reddit thumbnail field is a real image URL and
// not one of the sentinel strings reddit uses ("self", "default", "nsfw", …).
func isThumbURL(s string) bool {
	switch s {
	case "", "self", "default", "nsfw", "spoiler", "image":
		return false
	}
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// isImageURL reports whether u points at an image by extension or known host.
func isImageURL(u string) bool {
	l := strings.ToLower(u)
	for _, ext := range []string{".jpg", ".jpeg", ".png", ".gif", ".webp"} {
		if strings.HasSuffix(l, ext) {
			return true
		}
	}
	return strings.Contains(l, "i.redd.it/") || strings.Contains(l, "i.imgur.com/")
}
