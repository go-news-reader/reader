package feeds

import (
	"testing"

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
