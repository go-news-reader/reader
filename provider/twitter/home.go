package twitter

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-browserhttp/browserhttp"

	"github.com/go-news-reader/reader/source"
)

// defaultHomeBase is the origin X serves its private GraphQL API from.
const defaultHomeBase = "https://x.com"

// webBearer is the public web app bearer token X's own site sends on GraphQL
// calls. It is not a user secret — it is the same constant every logged-out and
// logged-in web session uses; the per-user authentication is the auth_token +
// ct0 cookies. X may rotate it, in which case the read fails and is surfaced as a
// sign-in prompt.
const webBearer = "AAAAAAAAAAAAAAAAAAAAANRILgAAAAAAnNwIzUejRCOuH5E6I8xnZz4puTs=" +
	"1Zv7ttfk8LF81IUq16cHjhLTvJu4FA33AGWWjCpTnA"

// homeTimelineQueryID is the GraphQL query id for the HomeTimeline operation. X
// rotates these ids; when it does the endpoint 404s and the read is reported as a
// sign-in/unavailable prompt rather than crashing.
const homeTimelineQueryID = "HCosKfLNW1AcOo3la3mMgg"

// xCreatedAtLayout is X's created_at timestamp format.
const xCreatedAtLayout = "Mon Jan 02 15:04:05 -0700 2006"

// homeChannels are the reserved subscription channels that select the
// authenticated home timeline instead of a public account.
var homeChannels = map[string]bool{"home": true, "following": true}

// isHomeChannel reports whether ch names the home timeline.
func isHomeChannel(ch string) bool {
	return homeChannels[strings.ToLower(strings.TrimSpace(ch))]
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

// homeResponse mirrors the subset of X's HomeTimeline GraphQL response we consume.
type homeResponse struct {
	Data struct {
		Home struct {
			Timeline struct {
				Instructions []struct {
					Type    string      `json:"type"`
					Entries []homeEntry `json:"entries"`
				} `json:"instructions"`
			} `json:"home_timeline_urt"`
		} `json:"home"`
	} `json:"data"`
}

// homeEntry is one timeline entry — a tweet item or a pagination cursor.
type homeEntry struct {
	EntryID string `json:"entryId"`
	Content struct {
		EntryType   string `json:"entryType"`
		CursorType  string `json:"cursorType"`
		Value       string `json:"value"`
		ItemContent struct {
			TweetResults struct {
				Result *tweetResult `json:"result"`
			} `json:"tweet_results"`
		} `json:"itemContent"`
	} `json:"content"`
}

// tweetResult is a tweet node, possibly wrapped by a visibility-results envelope
// (__typename "TweetWithVisibilityResults") that nests the real tweet under
// "tweet".
type tweetResult struct {
	RestID string       `json:"rest_id"`
	Tweet  *tweetResult `json:"tweet"`
	Core   struct {
		UserResults struct {
			Result struct {
				Legacy struct {
					ScreenName string `json:"screen_name"`
					Name       string `json:"name"`
				} `json:"legacy"`
			} `json:"result"`
		} `json:"user_results"`
	} `json:"core"`
	Legacy tweetLegacy `json:"legacy"`
}

// tweetLegacy carries the tweet's own text, counts, timestamp and media.
type tweetLegacy struct {
	FullText      string `json:"full_text"`
	FavoriteCount int    `json:"favorite_count"`
	ReplyCount    int    `json:"reply_count"`
	CreatedAt     string `json:"created_at"`
	PossiblySens  bool   `json:"possibly_sensitive"`
	ExtEntities   struct {
		Media []graphMedia `json:"media"`
	} `json:"extended_entities"`
}

// graphMedia is one attachment on a tweet.
type graphMedia struct {
	Type      string                      `json:"type"` // photo | video | animated_gif
	MediaURL  string                      `json:"media_url_https"`
	AltText   string                      `json:"ext_alt_text"`
	OrigInfo  struct{ Width, Height int } `json:"original_info"`
	VideoInfo struct {
		Variants []struct {
			Bitrate     int    `json:"bitrate"`
			ContentType string `json:"content_type"`
			URL         string `json:"url"`
		} `json:"variants"`
	} `json:"video_info"`
}

// resolve unwraps a visibility-results envelope to the underlying tweet.
func (t *tweetResult) resolve() *tweetResult {
	if t != nil && t.RestID == "" && t.Tweet != nil {
		return t.Tweet
	}
	return t
}

// homeFeed fetches the authenticated home timeline via X's GraphQL API. It
// requires the auth_token + ct0 cookies; without them, or when X refuses the read
// (rotated query id, expired session), it returns a typed sign-in/unavailable
// error instead of crashing or fabricating a feed.
func (p *Provider) homeFeed(ctx context.Context, q source.Query) (source.Result, error) {
	if p.authToken == "" || p.csrf == "" {
		return source.Result{}, source.NeedsAuth(source.Twitter,
			"sign in to X and import your session cookie (auth_token + ct0) for the home feed")
	}

	// The request body is a fixed-shape map of JSON-safe scalars/slices, so
	// json.Marshal cannot fail here; the error is deliberately discarded rather
	// than left as an untestable branch.
	body, _ := json.Marshal(homeRequestBody(q))
	endpoint := strings.TrimRight(p.homeBase, "/") + "/i/api/graphql/" + homeTimelineQueryID + "/HomeTimeline"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return source.Result{}, err
	}
	req.Header.Set("User-Agent", browserhttp.DefaultUserAgent)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+webBearer)
	req.Header.Set("x-csrf-token", p.csrf)
	req.Header.Set("x-twitter-auth-type", "OAuth2Session")
	req.Header.Set("x-twitter-active-user", "yes")
	req.Header.Set("Cookie", "auth_token="+p.authToken+"; ct0="+p.csrf)

	resp, err := p.hc.Do(req)
	if err != nil {
		return source.Result{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return source.Result{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return source.Result{}, source.NeedsAuth(source.Twitter,
			"X refused the home feed (status "+strconv.Itoa(resp.StatusCode)+
				"); re-import a fresh auth_token + ct0 (or the query id rotated)")
	}

	var parsed homeResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return source.Result{}, source.NeedsAuth(source.Twitter,
			"X did not return the home feed; re-import your X session cookie")
	}

	items, cursor := collectHome(parsed, q.Limit)
	return source.Result{Items: items, Cursor: cursor}, nil
}

// homeRequestBody builds the GraphQL variables/features/queryId payload X expects.
func homeRequestBody(q source.Query) map[string]any {
	count := q.Limit
	if count <= 0 {
		count = 20
	}
	variables := map[string]any{
		"count":                  count,
		"includePromotedContent": false,
		"latestControlAvailable": true,
		"withCommunity":          true,
		"seenTweetIds":           []string{},
	}
	if q.Cursor != "" {
		variables["cursor"] = q.Cursor
	}
	return map[string]any{
		"variables": variables,
		"features":  homeFeatures,
		"queryId":   homeTimelineQueryID,
	}
}

// homeFeatures is the feature-flag block X requires on the HomeTimeline call.
// The exact set changes over time; a stale set makes X reject the call, which is
// surfaced as a sign-in/unavailable error.
var homeFeatures = map[string]any{
	"responsive_web_graphql_timeline_navigation_enabled":       true,
	"creator_subscriptions_tweet_preview_api_enabled":          true,
	"tweetypie_unmention_optimization_enabled":                 true,
	"responsive_web_edit_tweet_api_enabled":                    true,
	"view_counts_everywhere_api_enabled":                       true,
	"longform_notetweets_consumption_enabled":                  true,
	"responsive_web_twitter_article_tweet_consumption_enabled": true,
	"freedom_of_speech_not_reach_fetch_enabled":                true,
	"standardized_nudges_misinfo":                              true,
	"rweb_video_timestamps_enabled":                            true,
	"longform_notetweets_rich_text_read_enabled":               true,
	"longform_notetweets_inline_media_enabled":                 true,
}

// collectHome walks the timeline instructions, mapping tweet entries into items
// (up to limit, 0 = no cap) and returning the Bottom pagination cursor.
func collectHome(r homeResponse, limit int) ([]source.Item, string) {
	var items []source.Item
	var cursor string
	for _, ins := range r.Data.Home.Timeline.Instructions {
		for _, e := range ins.Entries {
			if e.Content.EntryType == "TimelineTimelineCursor" {
				if e.Content.CursorType == "Bottom" {
					cursor = e.Content.Value
				}
				continue
			}
			tw := e.Content.ItemContent.TweetResults.Result.resolve()
			if tw == nil || tw.RestID == "" {
				continue // a non-tweet entry (who-to-follow, promoted, empty)
			}
			if limit > 0 && len(items) >= limit {
				return items, cursor
			}
			items = append(items, mapHomeTweet(tw))
		}
	}
	return items, cursor
}

// mapHomeTweet projects one GraphQL tweet node onto a normalized Item.
func mapHomeTweet(t *tweetResult) source.Item {
	u := t.Core.UserResults.Result.Legacy
	author := u.Name
	if strings.TrimSpace(author) == "" {
		author = u.ScreenName
	}
	it := source.Item{
		ID:        t.RestID,
		Source:    source.Twitter,
		Channel:   "home",
		Author:    author,
		Body:      t.Legacy.FullText,
		Permalink: homePermalink(u.ScreenName, t.RestID),
		Score:     t.Legacy.FavoriteCount,
		Comments:  t.Legacy.ReplyCount,
		Created:   source.UnixOrZero(parseCreatedAt(t.Legacy.CreatedAt)),
		NSFW:      t.Legacy.PossiblySens,
	}
	for _, m := range t.Legacy.ExtEntities.Media {
		it.Media = append(it.Media, mapGraphMedia(m)...)
	}
	return it
}

// homePermalink builds the canonical tweet URL, or "" when the screen name is
// missing.
func homePermalink(screenName, id string) string {
	if screenName == "" {
		return ""
	}
	return "https://x.com/" + screenName + "/status/" + id
}

// parseCreatedAt parses X's created_at, returning the zero time on any failure so
// the item sorts as an unknown date rather than erroring the whole feed.
func parseCreatedAt(s string) time.Time {
	t, err := time.Parse(xCreatedAtLayout, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// mapGraphMedia turns one attachment into the feed's media entries: a photo is one
// image; a video / animated gif is a still thumbnail plus the best progressive MP4.
func mapGraphMedia(m graphMedia) []source.Media {
	still := source.Media{
		URL: m.MediaURL, Kind: source.MediaImage,
		Width: m.OrigInfo.Width, Height: m.OrigInfo.Height, AltText: m.AltText,
	}
	if m.Type == "photo" {
		return []source.Media{still}
	}
	kind := source.MediaVideo
	if m.Type == "animated_gif" {
		kind = source.MediaGIF
	}
	url, ok := bestVariant(m)
	if !ok {
		still.Kind = kind // only an adaptive stream: keep the frame, call it a video
		return []source.Media{still}
	}
	still.Kind = source.MediaThumbnail
	playable := source.Media{
		URL: url, Kind: kind,
		Width: m.OrigInfo.Width, Height: m.OrigInfo.Height, AltText: m.AltText,
	}
	return []source.Media{still, playable}
}

// bestVariant returns the highest-bitrate progressive MP4 URL for a video.
func bestVariant(m graphMedia) (string, bool) {
	best, bestBitrate, found := "", -1, false
	for _, v := range m.VideoInfo.Variants {
		if v.ContentType == "video/mp4" && v.URL != "" && v.Bitrate >= bestBitrate {
			best, bestBitrate, found = v.URL, v.Bitrate, true
		}
	}
	return best, found
}
