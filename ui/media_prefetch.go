package ui

import (
	"strings"

	"github.com/go-news-reader/reader/source"
)

// MediaRequest names one feed item's remote thumbnail: the item ID the decoded
// image is keyed by, the URL to fetch it from, and the item's Created time (Unix
// seconds, UTC; 0 when unknown) so the on-disk media cache can stamp the stored
// file with the POST's date rather than the download moment.
type MediaRequest struct {
	ID      string
	URL     string
	Created int64
}

// MediaPrefetch lists the remote thumbnails the currently shown feed is still
// missing: one per item that declares media, names it over http(s), and has no
// decoded thumbnail yet.
//
// Usenet posts are excluded — their images arrive over NNTP through
// [Scene.ImagePrefetch], which was the ONLY path that ever populated Thumbs. So
// every card from every other source drew the "image"/"video" placeholder
// forever, however well its provider had reported the media. This is the list
// the app fetches to close that gap.
func (s *Scene) MediaPrefetch() []MediaRequest {
	var out []MediaRequest
	seen := map[string]bool{}
	for _, e := range groupItems(s.filtered()) {
		it := e.item
		if e.group != nil || it.Source == source.Usenet || it.ID == "" || seen[it.ID] {
			continue
		}
		if s.hasThumb(it.ID) {
			continue
		}
		u := firstMediaURL(it)
		if u == "" {
			continue
		}
		seen[it.ID] = true
		out = append(out, MediaRequest{ID: it.ID, URL: u, Created: it.Created})
	}
	return out
}

// firstMediaURL picks the URL to draw as an item's thumbnail: its first still
// image, else its first http(s) attachment of any kind. The fallback matters
// because a video attachment's URL is often already its preview frame (a tweet's
// media_url_https is the still, never the MP4); when it really is a video file
// the fetch simply fails to decode and the card keeps its placeholder.
func firstMediaURL(it source.Item) string {
	fallback := ""
	for _, m := range it.Media {
		if !isHTTPURL(m.URL) {
			continue
		}
		if m.Kind == source.MediaImage {
			return m.URL
		}
		if fallback == "" {
			fallback = m.URL
		}
	}
	return fallback
}

// isHTTPURL reports whether raw is an absolute http(s) URL — the only kind a
// thumbnail fetch can follow (Usenet permalinks are "news:<…>").
func isHTTPURL(raw string) bool {
	return strings.HasPrefix(raw, "https://") || strings.HasPrefix(raw, "http://")
}
