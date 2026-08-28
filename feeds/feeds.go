// Package feeds assembles a source.Registry from user configuration, wiring
// every provider adapter into one place. The application builds a registry once
// and then drives it via source.Registry.Aggregate over the user's
// subscriptions.
//
// Providers that work anonymously (Reddit, Hacker News, Bluesky, RSS/Atom) are
// always registered. Best-effort scrapers (Instagram, TikTok) register too but
// take optional credentials. Providers that require mandatory endpoint config
// (Mastodon instance, Usenet server) register only when that config is present.
//
// When Options.Recorder is set, one shared request-logging *http.Client is built
// (a browser-fingerprint client whose transport records every round trip into
// the recorder) and threaded through every HTTP provider, so the in-app Network
// log can show exactly what each provider fetched. The Usenet NNTP transport is
// not HTTP and is not logged; only its Newznab indexer path (HTTP) is.
package feeds

import (
	"net/http"
	"time"

	"github.com/go-browserhttp/browserhttp"

	"github.com/go-news-reader/reader/internal/httplog"
	"github.com/go-news-reader/reader/provider/bluesky"
	"github.com/go-news-reader/reader/provider/hackernews"
	"github.com/go-news-reader/reader/provider/instagram"
	"github.com/go-news-reader/reader/provider/lemmy"
	"github.com/go-news-reader/reader/provider/mastodon"
	"github.com/go-news-reader/reader/provider/reddit"
	"github.com/go-news-reader/reader/provider/redgifs"
	"github.com/go-news-reader/reader/provider/syndication"
	"github.com/go-news-reader/reader/provider/tiktok"
	"github.com/go-news-reader/reader/provider/twitter"
	"github.com/go-news-reader/reader/provider/usenet"
	"github.com/go-news-reader/reader/source"
)

// Options configures which providers get registered and with what credentials.
type Options struct {
	// RedditSessionCookie authenticates Reddit reads with the user's own
	// logged-in browser reddit_session cookie (bare value or a full
	// "reddit_session=...; ..." Cookie string). Reddit's self-serve OAuth
	// registration is effectively closed to new personal projects, so the cookie
	// — which the reader can import straight from Firefox — is the practical path
	// for individual read-only use. Empty falls back to the anonymous ".json"
	// endpoints.
	RedditSessionCookie string

	// MastodonInstance (e.g. "https://mastodon.social") enables the Mastodon
	// provider; MastodonToken optionally authenticates it.
	MastodonInstance string
	MastodonToken    string

	// LemmyInstance (e.g. "https://lemmy.world") enables the Lemmy provider.
	LemmyInstance string

	// UsenetAddr ("host:port") enables the Usenet provider; UsenetTLS selects
	// implicit TLS. UsenetUsername + UsenetPassword supply AUTHINFO credentials
	// for modern servers (Eternal-September text, XSUsenet binary); leave empty to
	// connect anonymously to a legacy binary server (Free). UsenetIndexerURL +
	// UsenetIndexerAPIKey additionally enable Newznab "search:" queries (direct
	// indexer or NZBHydra2).
	UsenetAddr          string
	UsenetTLS           bool
	UsenetUsername      string
	UsenetPassword      string
	UsenetIndexerURL    string
	UsenetIndexerAPIKey string

	// Optional best-effort credentials.
	InstagramSession string
	TikTokMSToken    string
	TikTokSession    string
	// TwitterSession is the user's logged-in X cookie string
	// ("auth_token=…; ct0=…"), which unlocks the authenticated home timeline
	// (the "home"/"following" channels). Public timelines need no credential.
	TwitterSession string

	// Recorder, when set, captures every provider's HTTP exchanges so the app's
	// Network log can display them. Nil disables logging (each provider keeps its
	// own default client).
	Recorder *httplog.Recorder

	// SourceConcurrency caps how many subscriptions fetch at once during an
	// aggregate refresh. 0 lets the registry pick its default. It exists so a
	// profile with many subscriptions does not open one HTTP request per
	// subscription simultaneously.
	SourceConcurrency int
}

// feedCacheTTL is how long a fetched subscription result is served from cache
// before a re-fetch — long enough that navigating and refreshing a large profile
// re-uses results instead of re-hitting the APIs, short enough that the feed still
// freshens within a normal session.
const feedCacheTTL = 10 * time.Minute

// maxFetchPerView caps how many accounts a single view (an aggregate refresh)
// network-fetches; the rest are served from cache, so a follow list of thousands
// is never walked at once. Roughly the number of feeds a person actually reads in
// a sitting.
const maxFetchPerView = 40

// socialFetchInterval paces the scraped social sources: a minimum gap between two
// fetches to the same one, so hundreds of subscriptions drain as a human-paced
// trickle rather than a burst. The public APIs (Reddit, HN, RSS, …) are not
// paced (return 0).
func socialFetchInterval(k source.Kind) time.Duration {
	switch k {
	case source.Instagram, source.Twitter, source.TikTok:
		return 2 * time.Second
	}
	return 0
}

// Registry builds a source.Registry with every applicable provider registered
// according to opts.
func Registry(opts Options) *source.Registry {
	r := source.NewRegistry()
	r.MaxConcurrent = opts.SourceConcurrency
	// Cache each subscription's result and pace the scraped social sources, so a
	// profile with hundreds/thousands of X/Instagram/TikTok subscriptions does not
	// re-fetch what it just fetched, and never bursts those APIs — the behaviour
	// that trips their bot detection. A fetch that rate-limits (429) backs that
	// source off, and the feed keeps showing its last good posts.
	r.Cache = source.NewFeedCache(feedCacheTTL, socialFetchInterval)
	// Cap how many accounts one view actually fetches: past this many, a profile
	// that follows thousands of accounts shows the rest from cache rather than
	// walking the whole list — so the reader's traffic looks like a person reading
	// tens of feeds, not a bot scraping thousands.
	r.MaxFetchPerAggregate = maxFetchPerView
	hc := loggedClient(opts.Recorder) // nil when no recorder is configured

	// Reddit: authenticated with the user's session cookie when present, else anonymous.
	r.Register(newReddit(hc, opts))
	r.Register(newHackerNews(hc))
	r.Register(newBluesky(hc))
	r.Register(newSyndication(hc))
	r.Register(newRedgifs(hc))

	// Best-effort scrapers; credentials optional.
	r.Register(newInstagram(hc, opts.InstagramSession))
	r.Register(newTikTok(hc, opts.TikTokMSToken, opts.TikTokSession))
	r.Register(newTwitter(hc, opts.TwitterSession))

	// Require mandatory endpoint config.
	if opts.MastodonInstance != "" {
		r.Register(newMastodon(hc, opts.MastodonInstance, opts.MastodonToken))
	}
	if opts.LemmyInstance != "" {
		r.Register(newLemmy(hc, opts.LemmyInstance))
	}
	if opts.UsenetAddr != "" {
		if opts.UsenetIndexerURL != "" {
			r.Register(newUsenetSearch(hc, opts))
		} else {
			r.Register(usenet.New(opts.UsenetAddr, opts.UsenetTLS).WithAuth(opts.UsenetUsername, opts.UsenetPassword))
		}
	}

	return r
}

// loggedClient returns a shared browser-fingerprint HTTP client whose transport
// records every round trip into rec, preserving the cookie jar and timeout. It
// returns nil when rec is nil, so callers keep each provider's own default.
func loggedClient(rec *httplog.Recorder) *http.Client {
	if rec == nil {
		return nil
	}
	hc := browserhttp.NewClient(30 * time.Second)
	hc.Transport = rec.Transport(hc.Transport)
	return hc
}

// MediaClient returns the HTTP client to fetch media bytes with (feed
// thumbnails), logging into rec when non-nil. It is deliberately the same
// browser-fingerprint client the providers use: media hosts fingerprint the TLS
// handshake exactly as the API hosts do, so a stock http.Client would silently
// fetch nothing. Unlike loggedClient it always returns a usable client, because
// a caller without a recorder still has to download.
//
// Its transport is wrapped so every media request carries a desktop-browser
// User-Agent (browserUATransport): browserhttp impersonates Chrome's TLS
// handshake but, by its own documented contract, leaves the User-Agent to the
// caller, and media hosts that gate on it — reddit's preview.redd.it and
// i.redd.it among them — answer a stock Go client's default UA with 403, so the
// thumbnail silently never loads. Wrapping here fixes every media fetch at once
// (card thumbnails and animated GIFs alike) rather than at each call site, and,
// because the wrap sits outside the logging transport, the Network log records
// the real UA that went out.
func MediaClient(rec *httplog.Recorder) *http.Client {
	hc := loggedClient(rec)
	if hc == nil {
		hc = browserhttp.NewClient(30 * time.Second)
	}
	hc.Transport = browserUATransport{base: hc.Transport}
	return hc
}

// browserUATransport sets a desktop-browser User-Agent on every request that
// does not already carry one, then defers to base. A request that already sets
// its own User-Agent is left untouched. It clones before mutating, honouring the
// RoundTripper contract that the argument request must not be modified.
type browserUATransport struct{ base http.RoundTripper }

func (t browserUATransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Header.Get("User-Agent") == "" {
		req = req.Clone(req.Context())
		req.Header.Set("User-Agent", browserhttp.DefaultUserAgent)
	}
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req)
}

// newX registers provider X on the shared logged client hc when present, else on
// the provider's own default constructor (unchanged behaviour).

func newReddit(hc *http.Client, opts Options) source.Provider {
	if opts.RedditSessionCookie != "" {
		return reddit.NewWithCookie(hc, opts.RedditSessionCookie)
	}
	if hc != nil {
		return reddit.NewWithHTTPClient(hc)
	}
	return reddit.New()
}

func newHackerNews(hc *http.Client) source.Provider {
	if hc != nil {
		return hackernews.NewWithHTTPClient(hc)
	}
	return hackernews.New()
}

func newBluesky(hc *http.Client) source.Provider {
	if hc != nil {
		return bluesky.NewWithHTTPClient(hc)
	}
	return bluesky.New()
}

func newSyndication(hc *http.Client) source.Provider {
	// The syndication provider already takes an *http.Client (nil => default).
	return syndication.New(hc)
}

func newRedgifs(hc *http.Client) source.Provider {
	if hc != nil {
		return redgifs.NewWithHTTPClient(hc)
	}
	return redgifs.New()
}

func newInstagram(hc *http.Client, session string) source.Provider {
	if hc != nil {
		return instagram.NewWithHTTPClient(hc, session)
	}
	return instagram.New(session)
}

func newTikTok(hc *http.Client, msToken, session string) source.Provider {
	if hc != nil {
		return tiktok.NewWithHTTPClient(hc, msToken, session)
	}
	return tiktok.New(msToken, session)
}

func newTwitter(hc *http.Client, session string) source.Provider {
	if session != "" {
		return twitter.NewWithSession(hc, session)
	}
	if hc != nil {
		return twitter.NewWithHTTPClient(hc)
	}
	return twitter.New()
}

func newMastodon(hc *http.Client, instance, token string) source.Provider {
	if hc != nil {
		return mastodon.NewWithHTTPClient(hc, instance, token)
	}
	return mastodon.New(instance, token)
}

func newLemmy(hc *http.Client, instance string) source.Provider {
	if hc != nil {
		return lemmy.NewWithHTTPClient(hc, instance)
	}
	return lemmy.New(instance)
}

func newUsenetSearch(hc *http.Client, opts Options) source.Provider {
	var p *usenet.Provider
	if hc != nil {
		p = usenet.NewWithSearchClient(hc, opts.UsenetAddr, opts.UsenetTLS, opts.UsenetIndexerURL, opts.UsenetIndexerAPIKey)
	} else {
		p = usenet.NewWithSearch(opts.UsenetAddr, opts.UsenetTLS, opts.UsenetIndexerURL, opts.UsenetIndexerAPIKey)
	}
	return p.WithAuth(opts.UsenetUsername, opts.UsenetPassword)
}
