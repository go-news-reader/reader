// Package feeds assembles a source.Registry from user configuration, wiring
// every provider adapter into one place. The application builds a registry once
// and then drives it via source.Registry.Aggregate over the user's
// subscriptions.
//
// Providers that work anonymously (Reddit, Hacker News, Bluesky, RSS/Atom) are
// always registered. Best-effort scrapers (Instagram, TikTok) register too but
// take optional credentials. Providers that require mandatory endpoint config
// (Mastodon instance, Usenet server) register only when that config is present.
package feeds

import (
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
	// implicit TLS.
	UsenetAddr string
	UsenetTLS  bool

	// Optional best-effort credentials.
	InstagramSession string
	TikTokMSToken    string
	TikTokSession    string
	TwitterToken     string
}

// Registry builds a source.Registry with every applicable provider registered
// according to opts.
func Registry(opts Options) *source.Registry {
	r := source.NewRegistry()

	// Anonymous, always available.
	r.Register(reddit.New())
	r.Register(hackernews.New())
	r.Register(bluesky.New())
	r.Register(syndication.New(nil))

	// Best-effort scrapers; credentials optional.
	r.Register(instagram.New(opts.InstagramSession))
	r.Register(tiktok.New(opts.TikTokMSToken, opts.TikTokSession))
	r.Register(twitter.New(opts.TwitterToken))

	// Require mandatory endpoint config.
	if opts.MastodonInstance != "" {
		r.Register(mastodon.New(opts.MastodonInstance, opts.MastodonToken))
	}
	if opts.LemmyInstance != "" {
		r.Register(lemmy.New(opts.LemmyInstance))
	}
	if opts.UsenetAddr != "" {
		r.Register(usenet.New(opts.UsenetAddr, opts.UsenetTLS))
	}

	return r
}
