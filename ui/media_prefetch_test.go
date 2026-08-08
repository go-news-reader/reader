package ui

import (
	"image"
	"testing"

	"github.com/go-news-reader/reader/source"
)

// webItem is a non-Usenet post carrying one remote attachment.
func webItem(id, url string, kind source.MediaKind) source.Item {
	return source.Item{ID: id, Source: source.Twitter, Channel: "NASA", Score: -1, Comments: -1,
		Body: "post " + id, Media: []source.Media{{URL: url, Kind: kind}}}
}

func TestIsHTTPURL(t *testing.T) {
	for _, u := range []string{"https://a/b.jpg", "http://a/b.jpg"} {
		if !isHTTPURL(u) {
			t.Fatalf("%q should be fetchable", u)
		}
	}
	for _, u := range []string{"", "news:<abc@x>", "ftp://a/b", "//a/b.jpg", "/local.jpg"} {
		if isHTTPURL(u) {
			t.Fatalf("%q should not be fetchable", u)
		}
	}
}

func TestFirstMediaURL(t *testing.T) {
	// A still image wins even when a video is listed first.
	it := source.Item{Media: []source.Media{
		{URL: "https://v/clip.mp4", Kind: source.MediaVideo},
		{URL: "https://i/photo.jpg", Kind: source.MediaImage},
	}}
	if got := firstMediaURL(it); got != "https://i/photo.jpg" {
		t.Fatalf("got %q, want the still image", got)
	}
	// With no still, the first fetchable attachment is the fallback — a tweet's
	// video entry already points at its preview frame.
	vid := source.Item{Media: []source.Media{
		{URL: "news:<x@y>", Kind: source.MediaImage}, // not fetchable over HTTP
		{URL: "https://pbs/thumb.jpg", Kind: source.MediaVideo},
		{URL: "https://pbs/other.jpg", Kind: source.MediaVideo},
	}}
	if got := firstMediaURL(vid); got != "https://pbs/thumb.jpg" {
		t.Fatalf("got %q, want the first fetchable attachment", got)
	}
	if got := firstMediaURL(source.Item{}); got != "" {
		t.Fatalf("got %q, want empty for an item with no media", got)
	}
	if got := firstMediaURL(source.Item{Media: []source.Media{{URL: "news:<x@y>"}}}); got != "" {
		t.Fatalf("got %q, want empty when nothing is fetchable", got)
	}
}

// TestMediaPrefetchListsMissingThumbnails is the regression this exists for:
// before it, nothing ever asked for a non-Usenet item's image, so every such
// card drew the placeholder forever.
func TestMediaPrefetchListsMissingThumbnails(t *testing.T) {
	s := wrapScene()
	s.SetItems([]source.Item{
		webItem("a", "https://i/a.jpg", source.MediaImage),
		webItem("b", "https://i/b.jpg", source.MediaVideo),
	})
	got := s.MediaPrefetch()
	if len(got) != 2 {
		t.Fatalf("requests = %+v, want both items", got)
	}
	if got[0] != (MediaRequest{ID: "a", URL: "https://i/a.jpg"}) {
		t.Fatalf("request[0] = %+v", got[0])
	}
	if got[1].ID != "b" {
		t.Fatalf("request[1] = %+v", got[1])
	}
}

func TestMediaPrefetchSkipsWhatItShould(t *testing.T) {
	s := wrapScene()
	usenetItem := source.Item{ID: "u", Source: source.Usenet, Title: "pic.jpg", Score: -1, Comments: -1,
		Media: []source.Media{{URL: "https://i/u.jpg", Kind: source.MediaImage}}}
	noMedia := source.Item{ID: "n", Source: source.Twitter, Body: "text only", Score: -1, Comments: -1}
	noID := webItem("", "https://i/x.jpg", source.MediaImage)
	notFetchable := source.Item{ID: "f", Source: source.Bluesky, Score: -1, Comments: -1,
		Media: []source.Media{{URL: "news:<x@y>", Kind: source.MediaImage}}}
	already := webItem("t", "https://i/t.jpg", source.MediaImage)

	s.SetItems([]source.Item{usenetItem, noMedia, noID, notFetchable, already})
	// "already" has a decoded thumbnail, so it must not be requested again.
	s.SetThumb("t", image.NewRGBA(image.Rect(0, 0, 2, 2)))

	if got := s.MediaPrefetch(); len(got) != 0 {
		t.Fatalf("requests = %+v, want none", got)
	}
}

// TestMediaPrefetchDeduplicatesByID checks one request per item even when the
// same post appears twice in the merged feed.
func TestMediaPrefetchDeduplicatesByID(t *testing.T) {
	s := wrapScene()
	dup := webItem("d", "https://i/d.jpg", source.MediaImage)
	s.SetItems([]source.Item{dup, dup})
	got := s.MediaPrefetch()
	if len(got) != 1 {
		t.Fatalf("requests = %+v, want exactly one", got)
	}
}

// TestMediaPrefetchSkipsUsenetGroups checks a collapsed multipart Usenet post
// (a group card, not a single item) is left to the NNTP prefetch path.
func TestMediaPrefetchSkipsUsenetGroups(t *testing.T) {
	s := wrapScene()
	part := func(n string) source.Item {
		return source.Item{ID: "g" + n, Source: source.Usenet, Score: -1, Comments: -1,
			Title:     "[" + n + "/2] holiday.jpg yEnc (1/9)",
			Permalink: "news:<" + n + "@srv>",
			Media:     []source.Media{{URL: "https://i/g.jpg", Kind: source.MediaImage}}}
	}
	s.SetItems([]source.Item{part("1"), part("2")})
	if got := s.MediaPrefetch(); len(got) != 0 {
		t.Fatalf("requests = %+v, want none for a Usenet group", got)
	}
}
