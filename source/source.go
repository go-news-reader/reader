// Package source defines the common contract every news/social provider
// implements so the aggregator can treat Reddit, RSS, Usenet, Mastodon,
// Bluesky, Twitter, Instagram, TikTok and the rest uniformly. A provider maps
// its platform's native objects onto the normalized [Item]; the aggregator UI
// and storage never see platform-specific types.
//
// Everything here is pure Go with no third-party dependencies, so provider
// client libraries in their own repos can depend on it without pulling in the
// whole application.
package source

import "context"

// Kind identifies a source platform. The zero value is invalid.
type Kind string

// Recognized source kinds. Providers report one of these from [Provider.Kind].
const (
	Reddit      Kind = "reddit"
	Syndication Kind = "syndication" // RSS / Atom / JSONFeed
	HackerNews  Kind = "hackernews"
	Usenet      Kind = "usenet" // NNTP newsgroups
	Mastodon    Kind = "mastodon"
	Lemmy       Kind = "lemmy"
	Bluesky     Kind = "bluesky" // AT Protocol
	Twitter     Kind = "twitter" // X
	Instagram   Kind = "instagram"
	TikTok      Kind = "tiktok"
)

// MediaKind classifies an attachment on an [Item].
type MediaKind string

// Media classifications.
const (
	MediaImage     MediaKind = "image"
	MediaThumbnail MediaKind = "thumbnail"
	MediaGIF       MediaKind = "gif"
	MediaVideo     MediaKind = "video"
	MediaAudio     MediaKind = "audio"
)

// Media is one attachment (image, video, thumbnail, …) on an [Item]. Width and
// Height are 0 when the source does not report dimensions.
type Media struct {
	URL    string
	Kind   MediaKind
	Width  int
	Height int
}

// Item is a single normalized entry — a post, article, toot, tweet, video, or
// newsgroup message — from any source. Providers fill what their platform
// offers and leave the rest zero. Counters that a platform does not expose are
// set to -1 to distinguish "unknown" from a genuine zero.
type Item struct {
	ID        string // stable identifier within Source
	Source    Kind
	Channel   string // subreddit / feed / newsgroup / account / hashtag it came from
	Title     string
	Author    string
	Body      string // text body or summary (plain or lightly-marked-up)
	Permalink string // canonical URL of the item on its platform
	Link      string // external/target URL, if the item links out (else "")
	Media     []Media
	Score     int      // upvotes / likes / points; -1 if not applicable
	Comments  int      // replies / comments; -1 if not applicable
	Created   int64    // creation time, unix seconds UTC (0 if unknown)
	NSFW      bool     // adult / sensitive content
	Pinned    bool     // stickied / pinned in its channel
	Tags      []string // flair, hashtags, categories
}

// Query selects what a provider should fetch.
type Query struct {
	// Channel scopes the fetch: a subreddit, feed URL, newsgroup name, account
	// handle, or hashtag. Empty means the provider's default view (home/front
	// page/public timeline).
	Channel string
	// Sort is a provider-specific ordering hint (hot|new|top|…). Best-effort:
	// providers that cannot honor it ignore it.
	Sort string
	// Limit caps the number of items; 0 means the provider default.
	Limit int
	// Cursor is an opaque pagination token from a prior [Result]. Empty starts
	// at the first page.
	Cursor string
}

// Result is a page of items plus the cursor to fetch the next page.
type Result struct {
	Items  []Item
	Cursor string // opaque; empty when there are no more pages
}

// Provider fetches normalized items from one source platform. Implementations
// must be safe for concurrent use by multiple goroutines.
type Provider interface {
	// Kind reports which platform this provider serves.
	Kind() Kind
	// Feed returns a page of items for the query.
	Feed(ctx context.Context, q Query) (Result, error)
}
