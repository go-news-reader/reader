package feeds

import (
	"os"
	"path/filepath"
	"testing"
	"time"

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

func TestRegistryWiresFeedCache(t *testing.T) {
	r := Registry(Options{})
	if r.Cache == nil {
		t.Fatal("Registry should install a FeedCache to pace + cache social sources")
	}
	if r.MaxFetchPerAggregate <= 0 {
		t.Fatal("Registry should cap how many accounts one view fetches")
	}
	if r.Cache.TTLFor == nil {
		t.Fatal("Registry should set a per-source cache TTL")
	}
}

func TestRegistryPersistsFeedCache(t *testing.T) {
	r := Registry(Options{FeedCacheDir: t.TempDir()})
	if r.Cache == nil || r.Cache.Store == nil {
		t.Fatal("Registry with FeedCacheDir should attach a persistent store")
	}
}

func TestRegistryFeedCacheDirError(t *testing.T) {
	// A directory whose parent is a regular file cannot be created: the store is
	// skipped and the in-memory cache still works.
	f := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := Registry(Options{FeedCacheDir: filepath.Join(f, "sub")})
	if r.Cache == nil {
		t.Fatal("the in-memory cache should still exist")
	}
	if r.Cache.Store != nil {
		t.Fatal("a store that cannot be opened must be skipped")
	}
}

func TestFeedTTLFor(t *testing.T) {
	cases := map[source.Kind]time.Duration{
		source.Twitter:      10 * time.Minute,
		source.Instagram:    10 * time.Minute,
		source.TikTok:       10 * time.Minute,
		source.Bluesky:      10 * time.Minute,
		source.Mastodon:     10 * time.Minute,
		source.Reddit:       15 * time.Minute,
		source.HackerNews:   15 * time.Minute,
		source.Lemmy:        15 * time.Minute,
		source.Usenet:       45 * time.Minute,
		source.Syndication:  45 * time.Minute,
		source.Kind("nope"): feedCacheTTL, // default fallback
	}
	for k, want := range cases {
		if got := feedTTLFor(k); got != want {
			t.Errorf("feedTTLFor(%q) = %v, want %v", k, got, want)
		}
	}
}

func TestSocialFetchInterval(t *testing.T) {
	// The scraped social sources are paced; the public APIs are not.
	for _, k := range []source.Kind{source.Instagram, source.Twitter, source.TikTok} {
		if socialFetchInterval(k) <= 0 {
			t.Errorf("%q should be paced", k)
		}
	}
	for _, k := range []source.Kind{source.Reddit, source.HackerNews, source.Bluesky} {
		if socialFetchInterval(k) != 0 {
			t.Errorf("%q should not be paced", k)
		}
	}
}

func TestRegistryAlwaysOn(t *testing.T) {
	r := Registry(Options{})
	kinds := r.Kinds()
	// Anonymous + best-effort providers are always registered.
	for _, k := range []source.Kind{source.Reddit, source.HackerNews, source.Bluesky, source.Syndication, source.Instagram, source.TikTok, source.Twitter, source.Redgifs} {
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
	})
	kinds := r.Kinds()
	for _, k := range []source.Kind{source.Mastodon, source.Lemmy, source.Usenet} {
		if !has(kinds, k) {
			t.Errorf("expected %q registered with config", k)
		}
	}
	// All eleven providers present.
	if len(kinds) != 11 {
		t.Fatalf("want 11 providers, got %d: %v", len(kinds), kinds)
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
	})
	if len(r.Kinds()) != 11 {
		t.Fatalf("want 11 providers with recorder, got %d: %v", len(r.Kinds()), r.Kinds())
	}
}

func TestRegistryRedditSessionCookie(t *testing.T) {
	// A session cookie registers the (authenticated) Reddit provider both with
	// and without a shared logged client.
	r := Registry(Options{RedditSessionCookie: "reddit_session=abc"})
	if !has(r.Kinds(), source.Reddit) {
		t.Fatal("reddit not registered with a session cookie (no recorder)")
	}
	rec := httplog.NewRecorder(8)
	r2 := Registry(Options{Recorder: rec, RedditSessionCookie: "reddit_session=abc"})
	if !has(r2.Kinds(), source.Reddit) {
		t.Fatal("reddit not registered with a session cookie (with recorder)")
	}
}

func TestRegistryTwitterSession(t *testing.T) {
	// An X session cookie registers the session-aware Twitter provider, both with
	// and without a shared logged client (exercising newTwitter's session branch).
	r := Registry(Options{TwitterSession: "auth_token=at; ct0=c"})
	if !has(r.Kinds(), source.Twitter) {
		t.Fatal("twitter not registered with a session (no recorder)")
	}
	rec := httplog.NewRecorder(8)
	r2 := Registry(Options{Recorder: rec, TwitterSession: "auth_token=at; ct0=c"})
	if !has(r2.Kinds(), source.Twitter) {
		t.Fatal("twitter not registered with a session (with recorder)")
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

// TestFollowImporterProviders asserts the social providers whose accounts can be
// connected implement source.FollowImporter, so the app's syncFollowImportKinds
// offers the "import my subscriptions" action for each. Reddit and Mastodon are
// the pre-existing importers; Instagram, X/Twitter and TikTok are the ones this
// change adds. Only kinds a provider actually implements the interface for are
// listed by syncFollowImportKinds, so this is the wiring the button hangs on.
func TestFollowImporterProviders(t *testing.T) {
	r := Registry(Options{MastodonInstance: "https://mastodon.example"})
	for _, k := range []source.Kind{
		source.Reddit, source.Mastodon,
		source.Instagram, source.Twitter, source.TikTok,
	} {
		p, ok := r.Get(k)
		if !ok {
			t.Fatalf("%s provider not registered", k)
		}
		if _, isImp := p.(source.FollowImporter); !isImp {
			t.Errorf("%s provider does not implement source.FollowImporter", k)
		}
	}

	// A provider without the capability (Hacker News) must NOT be an importer, so
	// the action is offered for exactly the follow-capable sources.
	hn, _ := r.Get(source.HackerNews)
	if _, isImp := hn.(source.FollowImporter); isImp {
		t.Error("hackernews unexpectedly implements source.FollowImporter")
	}
}
