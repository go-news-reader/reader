package instagram

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-browserhttp/browserhttp"

	"github.com/go-news-reader/reader/source"
)

// homeChannels are the reserved subscription channels that select the
// authenticated home (following) timeline instead of a public account.
var homeChannels = map[string]bool{"home": true, "following": true}

// isHomeChannel reports whether ch names the home timeline.
func isHomeChannel(ch string) bool {
	return homeChannels[strings.ToLower(strings.TrimSpace(ch))]
}

// timelinePath is Instagram's private web feed endpoint. It returns the
// logged-in user's following timeline as JSON when the request carries a valid
// sessionid + csrftoken; without them it answers 302→login.
const timelinePath = "/api/v1/feed/timeline/"

// timelineResponse mirrors the subset of the feed/timeline JSON we consume.
type timelineResponse struct {
	FeedItems []struct {
		MediaOrAd *mediaOrAd `json:"media_or_ad"`
	} `json:"feed_items"`
	NextMaxID string `json:"next_max_id"`
}

// mediaOrAd is one organic post in the timeline (ad / suggested entries carry a
// different shape and are skipped).
type mediaOrAd struct {
	PK      string `json:"pk"`
	Code    string `json:"code"`
	Caption struct {
		Text string `json:"text"`
	} `json:"caption"`
	User struct {
		Username string `json:"username"`
	} `json:"user"`
	LikeCount     int   `json:"like_count"`
	CommentCount  int   `json:"comment_count"`
	TakenAt       int64 `json:"taken_at"`
	ImageVersions struct {
		Candidates []mediaCandidate `json:"candidates"`
	} `json:"image_versions2"`
	VideoVersions []mediaCandidate `json:"video_versions"`
}

// mediaCandidate is one encoded rendition of a post's image or video.
type mediaCandidate struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// homeFeed fetches the authenticated home timeline. It requires a sessionid; the
// csrftoken is sent when known (a full cookie string was imported). A non-2xx
// status, or a body that is not the expected JSON (the 302→login page a stale or
// csrf-less session yields), is reported as a sign-in prompt rather than a crash.
func (p *Provider) homeFeed(ctx context.Context, q source.Query) (source.Result, error) {
	if p.sessionID == "" {
		return source.Result{}, source.NeedsAuth(source.Instagram,
			"sign in to Instagram and import your session cookie for the home feed")
	}

	endpoint := strings.TrimRight(p.homeBase, "/") + timelinePath
	form := url.Values{"reason": {"cold_start_fetch"}}
	if q.Cursor != "" {
		form.Set("max_id", q.Cursor)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint,
		strings.NewReader(form.Encode()))
	if err != nil {
		return source.Result{}, err
	}
	req.Header.Set("User-Agent", browserhttp.DefaultUserAgent)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-IG-App-ID", p.appID)
	cookie := "sessionid=" + p.sessionID
	if p.csrf != "" {
		req.Header.Set("X-CSRFToken", p.csrf)
		cookie += "; csrftoken=" + p.csrf
	}
	req.Header.Set("Cookie", cookie)

	resp, err := p.hc.Do(req)
	if err != nil {
		return source.Result{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return source.Result{}, source.NeedsAuth(source.Instagram,
			"Instagram refused the home feed (status "+strconv.Itoa(resp.StatusCode)+
				"); re-import a fresh sessionid + csrftoken")
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return source.Result{}, err
	}

	var parsed timelineResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		// A csrf-less or stale session is redirected to a login HTML page that is
		// not JSON: report it as needing a fresh sign-in, not as a decode bug.
		return source.Result{}, source.NeedsAuth(source.Instagram,
			"Instagram did not return the home feed; import a full cookie string "+
				"(sessionid=…; csrftoken=…) from a signed-in browser")
	}

	limit := q.Limit
	items := make([]source.Item, 0, len(parsed.FeedItems))
	for _, fi := range parsed.FeedItems {
		if fi.MediaOrAd == nil {
			continue // an ad / suggested entry, not an organic post
		}
		if limit > 0 && len(items) >= limit {
			break
		}
		items = append(items, mapTimelineItem(*fi.MediaOrAd))
	}
	return source.Result{Items: items, Cursor: parsed.NextMaxID}, nil
}

// mapTimelineItem projects one organic timeline post onto a normalized Item.
func mapTimelineItem(m mediaOrAd) source.Item {
	it := source.Item{
		ID:        firstNonEmpty(m.Code, m.PK),
		Source:    source.Instagram,
		Channel:   "home",
		Author:    m.User.Username,
		Body:      m.Caption.Text,
		Permalink: goigPermalink(m.Code),
		Score:     m.LikeCount,
		Comments:  m.CommentCount,
		Created:   source.UnixOrZero(time.Unix(m.TakenAt, 0).UTC()),
	}
	if img, ok := bestCandidate(m.ImageVersions.Candidates); ok {
		it.Media = append(it.Media, source.Media{
			URL: img.URL, Kind: source.MediaImage, Width: img.Width, Height: img.Height,
		})
	}
	if vid, ok := bestCandidate(m.VideoVersions); ok {
		it.Media = append(it.Media, source.Media{
			URL: vid.URL, Kind: source.MediaVideo, Width: vid.Width, Height: vid.Height,
		})
	}
	return it
}

// bestCandidate returns the first (highest-resolution) rendition with a URL.
func bestCandidate(cs []mediaCandidate) (mediaCandidate, bool) {
	for _, c := range cs {
		if c.URL != "" {
			return c, true
		}
	}
	return mediaCandidate{}, false
}

// goigPermalink builds the canonical post URL for a shortcode, or "" when the
// post carries none.
func goigPermalink(code string) string {
	if code == "" {
		return ""
	}
	return "https://www.instagram.com/p/" + code + "/"
}
