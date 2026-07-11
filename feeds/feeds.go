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
	"github.com/go-news-reader/reader/provider/syndication"
	"github.com/go-news-reader/reader/provider/tiktok"
	"github.com/go-news-reader/reader/provider/twitter"
	"github.com/go-news-reader/reader/provider/usenet"
	"github.com/go-news-reader/reader/source"
)

// Options configures which providers get registered and with what credentials.
type Options struct {
	// MastodonInstance (e.g. "https://mastodon.social") enables the Mastodon
	// provider; MastodonToken optionally authenticates it.
	MastodonInstance string
	MastodonToken    string

	// LemmyInstance (e.g. "https://lemmy.world") enables the Lemmy provider.
	LemmyInstance string

	// UsenetAddr ("host:port") enables the Usenet provider; UsenetTLS selects
	// implicit TLS. UsenetIndexerURL + UsenetIndexerAPIKey additionally enable
	// Newznab "search:" queries (direct indexer or NZBHydra2).
	UsenetAddr          string
	UsenetTLS           bool
	UsenetIndexerURL    string
	UsenetIndexerAPIKey string

	// Optional best-effort credentials.
	InstagramSession string
	TikTokMSToken    string
	TikTokSession    string
	TwitterToken     string

	// Recorder, when set, captures every provider's HTTP exchanges so the app's
	// Network log can display them. Nil disables logging (each provider keeps its
	// own default client).
	Recorder *httplog.Recorder
}

// Registry builds a source.Registry with every applicable provider registered
// according to opts.
func Registry(opts Options) *source.Registry {
	r := source.NewRegistry()
	hc := loggedClient(opts.Recorder) // nil when no recorder is configured

	// Anonymous, always available.
	r.Register(newReddit(hc))
	r.Register(newHackerNews(hc))
	r.Register(newBluesky(hc))
	r.Register(newSyndication(hc))

	// Best-effort scrapers; credentials optional.
	r.Register(newInstagram(hc, opts.InstagramSession))
	r.Register(newTikTok(hc, opts.TikTokMSToken, opts.TikTokSession))
	r.Register(newTwitter(hc, opts.TwitterToken))

	// Require mandatory endpoint config.
	if opts.MastodonInstance != "" {
		r.Register(newMastodon(hc, opts.MastodonInstance, opts.MastodonToken))
	}
	if opts.LemmyInstance != "" {
		r.Register(newLemmy(hc, opts.LemmyInstance))
	}
	if opts.UsenetAddr != "" {
		if opts.UsenetIndexerURL != "" {
			r.Register(newUsenetSearch(hc, opts.UsenetAddr, opts.UsenetTLS, opts.UsenetIndexerURL, opts.UsenetIndexerAPIKey))
		} else {
			r.Register(usenet.New(opts.UsenetAddr, opts.UsenetTLS))
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

// newX registers provider X on the shared logged client hc when present, else on
// the provider's own default constructor (unchanged behaviour).

func newReddit(hc *http.Client) source.Provider {
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

func newTwitter(hc *http.Client, token string) source.Provider {
	if hc != nil {
		return twitter.NewWithHTTPClient(hc, token)
	}
	return twitter.New(token)
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

func newUsenetSearch(hc *http.Client, addr string, useTLS bool, indexerURL, apiKey string) source.Provider {
	if hc != nil {
		return usenet.NewWithSearchClient(hc, addr, useTLS, indexerURL, apiKey)
	}
	return usenet.NewWithSearch(addr, useTLS, indexerURL, apiKey)
}
