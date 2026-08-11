package tiktok

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	gott "github.com/go-tiktok/tiktok"

	"github.com/go-news-reader/reader/source"
)

// homeChannels are the reserved subscription channels that select the
// authenticated following feed instead of a public account.
var homeChannels = map[string]bool{"home": true, "following": true}

// isHomeChannel reports whether ch names the following feed.
func isHomeChannel(ch string) bool {
	return homeChannels[strings.ToLower(strings.TrimSpace(ch))]
}

// followingPath is TikTok's web following-feed endpoint.
const followingPath = "/api/following/item_list/"

// webParams are the constant query parameters TikTok's website sends on
// item_list requests, mirrored so the request looks like the real web client.
var webParams = map[string]string{
	"aid": "1988", "app_language": "en", "app_name": "tiktok_web",
	"channel": "tiktok_web", "device_platform": "web_pc", "cookie_enabled": "true",
	"history_len": "1", "focus_state": "true", "is_fullscreen": "false",
	"is_page_visible": "true", "language": "en", "os": "mac",
	"region": "US", "screen_height": "1080", "screen_width": "1920",
	"tz_name": "America/New_York", "webcast_language": "en",
}

// followingResponse mirrors the subset of the item_list JSON we consume.
type followingResponse struct {
	ItemList   []followingItem `json:"itemList"`
	Cursor     string          `json:"cursor"`
	HasMore    boolNumber      `json:"hasMore"`
	StatusCode int             `json:"statusCode"`
	StatusMsg  string          `json:"statusMsg"`
}

// followingItem is one video in the following feed.
type followingItem struct {
	ID     string `json:"id"`
	Desc   string `json:"desc"`
	Author struct {
		UniqueID string `json:"uniqueId"`
	} `json:"author"`
	Video struct {
		Cover    string `json:"cover"`
		PlayAddr string `json:"playAddr"`
	} `json:"video"`
	Stats struct {
		DiggCount    int `json:"diggCount"`
		CommentCount int `json:"commentCount"`
	} `json:"stats"`
	CreateTime int64 `json:"createTime"`
}

// boolNumber decodes both a JSON boolean and TikTok's numeric-string flags.
type boolNumber bool

// UnmarshalJSON accepts true/false, "true"/"false", and 0/1 (bare or quoted).
func (n *boolNumber) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	*n = boolNumber(s == "true" || s == "1")
	return nil
}

// followingFeed attempts the authenticated following feed. It requires a
// credential (msToken or sessionid); it builds the web following/item_list
// request with the msToken cookie/param and the standard web parameters, but
// TikTok additionally requires a signed param and the viewer's own secUid that a
// pure-Go client cannot forge, so a blocked, empty, or non-zero-status response
// is reported as a typed anti-bot/sign-in error.
func (p *Provider) followingFeed(ctx context.Context, q source.Query) (source.Result, error) {
	if !p.hasCred {
		return source.Result{}, source.NeedsAuth(source.TikTok,
			"sign in to TikTok and import your ms_token + session cookie for the following feed")
	}

	count := q.Limit
	if count <= 0 {
		count = defaultCount
	}
	vals := url.Values{}
	for k, v := range webParams {
		vals.Set(k, v)
	}
	vals.Set("count", strconv.Itoa(count))
	vals.Set("maxCursor", firstNonEmpty(q.Cursor, "0"))
	vals.Set("minCursor", "0")
	if p.msToken != "" {
		vals.Set("msToken", p.msToken)
	}

	endpoint := strings.TrimRight(p.homeBase, "/") + followingPath + "?" + vals.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return source.Result{}, err
	}
	req.Header.Set("User-Agent", gott.DefaultUserAgent)
	req.Header.Set("Referer", gott.DefaultBaseURL+"/")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	if p.session != "" {
		req.AddCookie(&http.Cookie{Name: "sessionid", Value: p.session})
	}
	if p.msToken != "" {
		req.AddCookie(&http.Cookie{Name: "msToken", Value: p.msToken})
	}

	resp, err := p.hc.Do(req)
	if err != nil {
		return source.Result{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return source.Result{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return source.Result{}, antiBotErr(resp.StatusCode, body)
	}
	if len(body) == 0 {
		return source.Result{}, source.NeedsAuth(source.TikTok,
			"TikTok returned an empty following feed (anti-bot block); a signed request is required")
	}

	var parsed followingResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return source.Result{}, fmt.Errorf("tiktok: decode following feed: %w", err)
	}
	if parsed.StatusCode != 0 {
		return source.Result{}, source.NeedsAuth(source.TikTok,
			fmt.Sprintf("TikTok refused the following feed (status %d %s); a signed request is required",
				parsed.StatusCode, parsed.StatusMsg))
	}

	items := make([]source.Item, 0, len(parsed.ItemList))
	for _, it := range parsed.ItemList {
		items = append(items, mapFollowingItem(it))
	}
	cursor := ""
	if bool(parsed.HasMore) {
		cursor = parsed.Cursor
	}
	return source.Result{Items: items, Cursor: cursor}, nil
}

// antiBotErr classifies a non-2xx following-feed response. A 401/403 is a plain
// sign-in prompt; the 400 "missing required fields" TikTok answers to an unsigned
// request is reported as the signing wall it is.
func antiBotErr(status int, body []byte) error {
	return source.NeedsAuth(source.TikTok,
		fmt.Sprintf("TikTok refused the following feed (HTTP %d: %s); a signed request is required",
			status, snippet(body)))
}

// snippet returns a short printable prefix of a response body for error messages.
func snippet(b []byte) string {
	const max = 120
	if len(b) > max {
		return string(b[:max]) + "…"
	}
	return string(b)
}

// mapFollowingItem projects one following-feed video onto a normalized Item.
func mapFollowingItem(it followingItem) source.Item {
	item := source.Item{
		ID:       it.ID,
		Source:   source.TikTok,
		Channel:  "home",
		Author:   it.Author.UniqueID,
		Body:     it.Desc,
		Score:    it.Stats.DiggCount,
		Comments: it.Stats.CommentCount,
		Created:  source.UnixOrZero(time.Unix(it.CreateTime, 0).UTC()),
	}
	if it.Author.UniqueID != "" && it.ID != "" {
		item.Permalink = fmt.Sprintf("%s/@%s/video/%s", gott.DefaultBaseURL, it.Author.UniqueID, it.ID)
	}
	if it.Video.Cover != "" {
		item.Media = append(item.Media, source.Media{URL: it.Video.Cover, Kind: source.MediaThumbnail})
	}
	if it.Video.PlayAddr != "" {
		item.Media = append(item.Media, source.Media{URL: it.Video.PlayAddr, Kind: source.MediaVideo})
	}
	return item
}

// firstNonEmpty returns the first non-empty string among its arguments.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
