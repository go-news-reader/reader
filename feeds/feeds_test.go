package feeds

import (
	"testing"

	"github.com/go-news-reader/reader/internal/httplog"
	"github.com/go-news-reader/reader/source"
)

func has(kinds []source.Kind, k source.Kind) bool {
	for _, x := range kinds {
		if x == k {
			return true
		}
	}
	return false
}

func TestRegistryAlwaysOn(t *testing.T) {
	r := Registry(Options{})
	kinds := r.Kinds()
	// Anonymous + best-effort providers are always registered.
	for _, k := range []source.Kind{source.Reddit, source.HackerNews, source.Bluesky, source.Syndication, source.Instagram, source.TikTok, source.Twitter} {
		if !has(kinds, k) {
			t.Errorf("expected %q always registered", k)
		}
	}
	// Config-gated ones are absent without config.
	for _, k := range []source.Kind{source.Mastodon, source.Lemmy, source.Usenet} {
		if has(kinds, k) {
			t.Errorf("%q should not be registered without config", k)
		}
	}
}

func TestRegistryConfigGated(t *testing.T) {
	r := Registry(Options{
		MastodonInstance: "https://mastodon.social",
		MastodonToken:    "tok",
		LemmyInstance:    "https://lemmy.world",
		UsenetAddr:       "news.example.org:119",
		UsenetTLS:        true,
		InstagramSession: "s",
		TikTokMSToken:    "m",
		TikTokSession:    "ts",
		TwitterToken:     "tw",
	})
	kinds := r.Kinds()
	for _, k := range []source.Kind{source.Mastodon, source.Lemmy, source.Usenet} {
		if !has(kinds, k) {
			t.Errorf("expected %q registered with config", k)
		}
	}
	// All ten providers present.
	if len(kinds) != 10 {
		t.Fatalf("want 10 providers, got %d: %v", len(kinds), kinds)
	}
}

func TestRegistryUsenetSearch(t *testing.T) {
	// UsenetAddr + indexer URL registers the search-capable Usenet provider.
	r := Registry(Options{UsenetAddr: "news:119", UsenetIndexerURL: "https://indexer", UsenetIndexerAPIKey: "k"})
	if !has(r.Kinds(), source.Usenet) {
		t.Fatal("usenet not registered with indexer")
	}
}

func TestRegistryWithRecorder(t *testing.T) {
	// A recorder builds the shared logged client and routes every HTTP provider
	// (including the Newznab indexer) through it. Exercises every logged branch.
	rec := httplog.NewRecorder(8)
	r := Registry(Options{
		Recorder:            rec,
		MastodonInstance:    "https://mastodon.social",
		MastodonToken:       "tok",
		LemmyInstance:       "https://lemmy.world",
		UsenetAddr:          "news.example.org:119",
		UsenetIndexerURL:    "https://indexer",
		UsenetIndexerAPIKey: "k",
		InstagramSession:    "s",
		TikTokMSToken:       "m",
		TikTokSession:       "ts",
		TwitterToken:        "tw",
	})
	if len(r.Kinds()) != 10 {
		t.Fatalf("want 10 providers with recorder, got %d: %v", len(r.Kinds()), r.Kinds())
	}
}

func TestRegistryRedditOAuth(t *testing.T) {
	// With Reddit credentials the reddit provider is still registered (now via
	// the OAuth constructor), both with and without a shared logged client.
	r := Registry(Options{RedditClientID: "id", RedditClientSecret: "sec", RedditUsername: "u", RedditPassword: "p"})
	if !has(r.Kinds(), source.Reddit) {
		t.Fatal("reddit not registered with OAuth creds (no recorder)")
	}
	rec := httplog.NewRecorder(8)
	r2 := Registry(Options{Recorder: rec, RedditClientID: "id", RedditClientSecret: "sec"})
	if !has(r2.Kinds(), source.Reddit) {
		t.Fatal("reddit not registered with OAuth creds (with recorder)")
	}
	// A client id without a secret keeps the anonymous path (OAuth guard false).
	r3 := Registry(Options{RedditClientID: "id"})
	if !has(r3.Kinds(), source.Reddit) {
		t.Fatal("reddit should still register anonymously without a secret")
	}
}

func TestLoggedClient(t *testing.T) {
	if loggedClient(nil) != nil {
		t.Fatal("nil recorder must yield nil shared client")
	}
	hc := loggedClient(httplog.NewRecorder(2))
	if hc == nil || hc.Transport == nil {
		t.Fatal("recorder should yield a client with a logging transport")
	}
}
