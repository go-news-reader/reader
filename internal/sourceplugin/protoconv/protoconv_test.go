package protoconv

import (
	"reflect"
	"testing"

	pb "github.com/go-news-reader/reader/internal/sourceplugin/grpc"
	"github.com/go-news-reader/reader/source"
)

// fullItem is a source.Item with every field set to a distinct non-zero value,
// so a round trip that drops or mismaps any field is caught.
func fullItem() source.Item {
	return source.Item{
		ID:        "id-1",
		Source:    source.Reddit,
		Channel:   "r/golang",
		Title:     "a title",
		Author:    "an author",
		Body:      "a body",
		Permalink: "https://example.invalid/p/1",
		Link:      "https://example.invalid/out",
		Media: []source.Media{
			{URL: "https://example.invalid/a.png", Kind: source.MediaImage, Width: 12, Height: 34, AltText: "alt"},
			{URL: "https://example.invalid/b.mp4", Kind: source.MediaVideo, Width: 640, Height: 480, AltText: ""},
		},
		Score:      7,
		Comments:   3,
		Created:    1_700_000_000,
		NSFW:       true,
		Pinned:     true,
		Tags:       []string{"go", "news"},
		GroupCount: 99,
		GroupHigh:  1234,
	}
}

func TestItemRoundTrip(t *testing.T) {
	in := fullItem()
	got := ItemFromProto(ItemToProto(in))
	if !reflect.DeepEqual(got, in) {
		t.Fatalf("item round trip mismatch:\n got %+v\nwant %+v", got, in)
	}
}

func TestItemRoundTripNoMedia(t *testing.T) {
	// An item with no media must survive as a nil Media (not an empty slice), so
	// exercise the len==0 branch of ItemFromProto.
	in := source.Item{ID: "id-2", Source: source.HackerNews, Title: "t", Score: -1, Comments: -1}
	got := ItemFromProto(ItemToProto(in))
	if !reflect.DeepEqual(got, in) {
		t.Fatalf("item(no media) round trip mismatch:\n got %+v\nwant %+v", got, in)
	}
	if got.Media != nil {
		t.Fatalf("expected nil Media, got %#v", got.Media)
	}
}

func TestMediaRoundTrip(t *testing.T) {
	in := source.Media{URL: "u", Kind: source.MediaThumbnail, Width: 1, Height: 2, AltText: "a"}
	got := MediaFromProto(MediaToProto(in))
	if !reflect.DeepEqual(got, in) {
		t.Fatalf("media round trip mismatch:\n got %+v\nwant %+v", got, in)
	}
}

func TestQueryRoundTrip(t *testing.T) {
	in := source.Query{Channel: "r/news", Sort: "top", Limit: 25, Cursor: "cur"}
	got := QueryFromProto(QueryToProto(in))
	if !reflect.DeepEqual(got, in) {
		t.Fatalf("query round trip mismatch:\n got %+v\nwant %+v", got, in)
	}
}

func TestResultRoundTrip(t *testing.T) {
	in := source.Result{Items: []source.Item{fullItem(), {ID: "id-3", Source: source.Bluesky}}, Cursor: "next"}
	got := ResultFromProto(ResultToProto(in))
	if !reflect.DeepEqual(got, in) {
		t.Fatalf("result round trip mismatch:\n got %+v\nwant %+v", got, in)
	}
}

func TestResultRoundTripEmpty(t *testing.T) {
	// An empty result must survive as a nil Items slice (the len==0 branch of
	// ResultFromProto) and an empty cursor.
	in := source.Result{}
	got := ResultFromProto(ResultToProto(in))
	if got.Items != nil {
		t.Fatalf("expected nil Items, got %#v", got.Items)
	}
	if got.Cursor != "" {
		t.Fatalf("expected empty cursor, got %q", got.Cursor)
	}
}

// TestResultToProtoErrorFieldUnset guards that ResultToProto does not touch the
// transported error field (the server sets it separately on failure).
func TestResultToProtoErrorFieldUnset(t *testing.T) {
	reply := ResultToProto(source.Result{Items: []source.Item{{ID: "x"}}})
	if reply.GetError() != "" {
		t.Fatalf("ResultToProto set error field: %q", reply.GetError())
	}
	if _, ok := interface{}(reply).(*pb.FeedReply); !ok {
		t.Fatal("ResultToProto did not return *pb.FeedReply")
	}
}
