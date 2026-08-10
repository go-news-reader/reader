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

import (
	"context"
	"time"
)

// UnixOrZero returns t as Unix seconds, or 0 — the "unknown" sentinel for
// [Item.Created] — when t is the zero Time. A zero time.Time is Unix
// -62135596800 (year 1); left as-is it sorts BELOW genuine unknowns (Created 0)
// and every real item, sinking such items below the fold. Providers map an
// unparsed/missing date through this so a zero time becomes a proper unknown.
func UnixOrZero(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

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
	// AltText is the author's own description of the picture — what a reader
	// announces in place of it, and the only thing anyone who cannot see the
	// image has. Empty when the source reports none, which is most of them;
	// Twitter/X is the one provider whose payload carries it.
	AltText string
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
	// GroupCount is the number of posts in the item's source group/newsgroup (the
	// NNTP GROUP article estimate), for a status-bar count. 0 when unknown.
	GroupCount int
	// GroupHigh is the highest article number in the group (the NNTP GROUP high
	// water mark), a monotonic marker used to count unseen/new posts. 0 unknown.
	GroupHigh int
}

// GroupInfo is one entry of a Usenet server's carried-group list: the full
// newsgroup name and its estimated post count (the NNTP LIST high−low+1 range).
type GroupInfo struct {
	Name  string
	Count int
}

// SubredditResult is one entry of a Reddit subreddit-search result: enough to
// show a discovery row and let the user subscribe by Name (as r/<Name>).
// Reddit does not let subreddits be enumerated, so a query against its search
// endpoint is the only way to discover them; a caller may further narrow the
// returned Name/Description locally (e.g. with a regular expression).
type SubredditResult struct {
	Name        string // display name, e.g. "golang" (subscribe as r/<Name>)
	Title       string // human title
	Description string // short public blurb
	Subscribers int64  // subscriber count
	NSFW        bool   // over-18 flag
}

// GroupStats is a sampled estimate of a newsgroup's content mix: within the last
// Sampled article overviews scanned, how many are binary posts (Binaries) and,
// of those, how many name an image file (Images ⊆ Binaries). A full scan of a
// busy binary group is far too large, so callers sample the tail and extrapolate
// the ratios to the group's full post count. The zero value (Sampled == 0) means
// "not yet scanned".
type GroupStats struct {
	Sampled  int
	Binaries int
	Images   int
}

// Comment is one reply on an [Item] (e.g. a Reddit post's comment tree),
// flattened from the platform's nested thread into display order. Depth is the
// indentation level — 0 for a top-level reply, 1 for a reply to that, and so on
// — so a front-end can render the thread without reconstructing the tree.
// Providers cap the count and depth so a huge thread cannot explode memory.
type Comment struct {
	Author  string // comment author (may be empty for deleted authors)
	Body    string // comment text (plain or lightly-marked-up)
	Score   int    // net upvotes
	Created int64  // creation time, unix seconds UTC (0 if unknown)
	Depth   int    // nesting level, 0 = top-level reply
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
