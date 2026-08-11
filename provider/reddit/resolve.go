package reddit

import (
	"context"
	"net/url"
	"strings"

	goreddit "github.com/go-reddit/reddit"

	"github.com/go-news-reader/reader/source"
)

// imageExts are the URL suffixes that denote a still image by extension.
var imageExts = []string{".jpg", ".jpeg", ".png", ".gif", ".webp"}

// hasImageExt reports whether the lower-cased path/segment ends in an image
// extension.
func hasImageExt(lower string) bool {
	for _, ext := range imageExts {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// resolveImgur turns an imgur link on the post into a direct-media URL and, when
// it can, sets the item's Link and appends the matching Media (image, or video
// for an mp4), reporting whether it handled the post. The transforms, verified
// against live imgur URLs, are:
//
//   - a "*.gifv" (imgur's HTML5 wrapper page) → the "*.mp4" it wraps;
//   - a bare "imgur.com/<id>" page (no extension, and not an "/a/<id>" album or
//     "/gallery/<id>") → "https://i.imgur.com/<id>.jpg" — imgur serves the asset
//     by id regardless of the extension in the path, so ".jpg" always resolves;
//   - an already-direct "i.imgur.com/<id>.{jpg,png,gif,webp}" → itself.
//
// Albums and galleries are many images, so they are left to the generic path.
func resolveImgur(it *source.Item, rawURL string) bool {
	direct, kind, ok := imgurDirect(rawURL)
	if !ok {
		return false
	}
	it.Link = direct
	it.Media = append(it.Media, source.Media{URL: direct, Kind: kind})
	return true
}

// imgurDirect resolves an imgur URL to its direct-media form and kind, reporting
// ok=false when raw is not an imgur URL it maps (a non-imgur host, an
// album/gallery, or an unmappable path).
func imgurDirect(raw string) (string, source.MediaKind, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", "", false
	}
	if !isImgurHost(strings.ToLower(u.Host)) {
		return "", "", false
	}
	// An imgur ".gifv" is a video wrapper on any imgur host; its mp4 lives on the
	// i.imgur.com CDN.
	if lp := strings.ToLower(u.Path); strings.HasSuffix(lp, ".gifv") {
		return "https://i.imgur.com" + u.Path[:len(u.Path)-len(".gifv")] + ".mp4", source.MediaVideo, true
	}
	if strings.EqualFold(u.Host, "i.imgur.com") {
		lp := strings.ToLower(u.Path)
		switch {
		case hasImageExt(lp):
			return "https://i.imgur.com" + u.Path, source.MediaImage, true
		case strings.HasSuffix(lp, ".mp4"):
			return "https://i.imgur.com" + u.Path, source.MediaVideo, true
		}
		return "", "", false
	}
	// A page host (imgur.com / www.imgur.com / m.imgur.com): only a bare single
	// segment is a single image. "/a/<id>", "/gallery/<id>", "/t/<tag>/<id>" and
	// an empty path are all multi-segment (or empty) and left to the generic path.
	seg := strings.Trim(u.Path, "/")
	if seg == "" || strings.ContainsRune(seg, '/') {
		return "", "", false
	}
	switch ls := strings.ToLower(seg); {
	case hasImageExt(ls):
		return "https://i.imgur.com/" + seg, source.MediaImage, true
	case strings.HasSuffix(ls, ".mp4"):
		return "https://i.imgur.com/" + seg, source.MediaVideo, true
	case strings.ContainsRune(seg, '.'):
		// Some other extension we do not map (e.g. ".pdf"): leave it alone.
		return "", "", false
	default:
		return "https://i.imgur.com/" + seg + ".jpg", source.MediaImage, true
	}
}

// isImgurHost reports whether host (already lower-cased) is an imgur host the
// resolver understands.
func isImgurHost(host string) bool {
	switch host {
	case "imgur.com", "www.imgur.com", "m.imgur.com", "i.imgur.com":
		return true
	}
	return false
}

// redditAudioURL derives the URL of a reddit-hosted video's separate DASH audio
// track from its video-only fallback URL. Reddit serves the audio as its own
// file at the same v.redd.it media base; the file name depends on the video's
// era, which the video file name reveals: modern videos name their streams with
// a ".mp4" suffix (e.g. "DASH_720.mp4") and carry audio as "DASH_AUDIO_128.mp4",
// while legacy videos use bare stream names (e.g. "DASH_4_8_M") and carry audio
// as "audio" (verified live against the v.redd.it CDN — legacy "…/audio"
// returns 200). Returns "" for a URL that is not a v.redd.it stream. A silent
// video simply has no track: the derived URL then 403s, which a player reads as
// "no audio".
func redditAudioURL(video string) string {
	u, err := url.Parse(video)
	if err != nil || !strings.EqualFold(u.Host, "v.redd.it") {
		return ""
	}
	i := strings.LastIndex(u.Path, "/")
	if i <= 0 {
		return "" // no "<id>/<stream>" shape to hang an audio sibling off of
	}
	dir, file := u.Path[:i], u.Path[i+1:]
	name := "audio"
	if strings.HasSuffix(strings.ToLower(file), ".mp4") {
		name = "DASH_AUDIO_128.mp4"
	}
	return u.Scheme + "://" + u.Host + dir + "/" + name
}

// resolveRedgifs turns a redgifs.com link on the post into the actual playable
// media by asking the RedGIFs API for the gif by id and mapping its HD (or SD)
// mp4 plus poster onto the item, reporting whether it handled the post. It
// no-ops (returning false, so the generic path keeps Reddit's static poster)
// when the post is not a redgifs link, no resolver is wired, the lookup fails, or
// the gif carries no video URL.
func (pr *Provider) resolveRedgifs(ctx context.Context, it *source.Item, p goreddit.Post) bool {
	id, ok := redgifsID(p.URL)
	if !ok || pr.redgifs == nil {
		return false
	}
	g, err := pr.redgifs.GifByID(ctx, id)
	if err != nil || g == nil {
		return false
	}
	video := g.URLs.HD
	if video == "" {
		video = g.URLs.SD
	}
	if video == "" {
		return false
	}
	it.Link = video
	it.Media = append(it.Media, source.Media{
		URL:    video,
		Kind:   source.MediaVideo,
		Width:  g.Width,
		Height: g.Height,
	})
	poster := g.URLs.Poster
	if poster == "" {
		poster = g.URLs.Thumbnail
	}
	if poster != "" {
		it.Media = append(it.Media, source.Media{URL: poster, Kind: source.MediaImage})
	}
	return true
}

// redgifsID extracts the gif id from a redgifs.com watch or embed link
// ("redgifs.com/watch/<id>" or "redgifs.com/ifr/<id>", any sub-domain), reporting
// ok=false for any other URL. RedGIFs ids are lower-cased for the API, which
// accepts either casing but returns the id in lower case.
func redgifsID(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	host := strings.ToLower(u.Host)
	if host != "redgifs.com" && !strings.HasSuffix(host, ".redgifs.com") {
		return "", false
	}
	segs := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(segs) < 2 || segs[1] == "" {
		return "", false
	}
	switch strings.ToLower(segs[0]) {
	case "watch", "ifr":
		return strings.ToLower(segs[1]), true
	}
	return "", false
}
