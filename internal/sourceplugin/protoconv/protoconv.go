// Package protoconv converts between the reader's in-process source types
// (source.Item, source.Media, source.Query, source.Result) and their gRPC wire
// forms in package sourcepb. Both sides of a source plugin — the host that
// dials the plugin and the plugin that serves items — go through these
// functions, so the mapping is defined once and its every field is exercised by
// a single round-trip test.
package protoconv

import (
	pb "github.com/go-news-reader/reader/internal/sourceplugin/grpc"
	"github.com/go-news-reader/reader/source"
)

// MediaToProto maps a source.Media to its wire form.
func MediaToProto(m source.Media) *pb.MediaMsg {
	return &pb.MediaMsg{
		Url:     m.URL,
		Kind:    string(m.Kind),
		Width:   int32(m.Width),
		Height:  int32(m.Height),
		AltText: m.AltText,
	}
}

// MediaFromProto maps a wire MediaMsg back to a source.Media.
func MediaFromProto(m *pb.MediaMsg) source.Media {
	return source.Media{
		URL:     m.GetUrl(),
		Kind:    source.MediaKind(m.GetKind()),
		Width:   int(m.GetWidth()),
		Height:  int(m.GetHeight()),
		AltText: m.GetAltText(),
	}
}

// ItemToProto maps a source.Item to its wire form, mirroring every field.
func ItemToProto(it source.Item) *pb.ItemMsg {
	media := make([]*pb.MediaMsg, len(it.Media))
	for i, m := range it.Media {
		media[i] = MediaToProto(m)
	}
	return &pb.ItemMsg{
		Id:         it.ID,
		Source:     string(it.Source),
		Channel:    it.Channel,
		Title:      it.Title,
		Author:     it.Author,
		Body:       it.Body,
		Permalink:  it.Permalink,
		Link:       it.Link,
		Media:      media,
		Score:      int32(it.Score),
		Comments:   int32(it.Comments),
		Created:    it.Created,
		Nsfw:       it.NSFW,
		Pinned:     it.Pinned,
		Tags:       it.Tags,
		GroupCount: int32(it.GroupCount),
		GroupHigh:  int32(it.GroupHigh),
	}
}

// ItemFromProto maps a wire ItemMsg back to a source.Item, mirroring every field.
func ItemFromProto(m *pb.ItemMsg) source.Item {
	var media []source.Media
	if len(m.GetMedia()) > 0 {
		media = make([]source.Media, len(m.GetMedia()))
		for i, mm := range m.GetMedia() {
			media[i] = MediaFromProto(mm)
		}
	}
	return source.Item{
		ID:         m.GetId(),
		Source:     source.Kind(m.GetSource()),
		Channel:    m.GetChannel(),
		Title:      m.GetTitle(),
		Author:     m.GetAuthor(),
		Body:       m.GetBody(),
		Permalink:  m.GetPermalink(),
		Link:       m.GetLink(),
		Media:      media,
		Score:      int(m.GetScore()),
		Comments:   int(m.GetComments()),
		Created:    m.GetCreated(),
		NSFW:       m.GetNsfw(),
		Pinned:     m.GetPinned(),
		Tags:       m.GetTags(),
		GroupCount: int(m.GetGroupCount()),
		GroupHigh:  int(m.GetGroupHigh()),
	}
}

// QueryToProto maps a source.Query to a FeedRequest.
func QueryToProto(q source.Query) *pb.FeedRequest {
	return &pb.FeedRequest{
		Channel: q.Channel,
		Sort:    q.Sort,
		Cursor:  q.Cursor,
		Limit:   int32(q.Limit),
	}
}

// QueryFromProto maps a FeedRequest back to a source.Query.
func QueryFromProto(r *pb.FeedRequest) source.Query {
	return source.Query{
		Channel: r.GetChannel(),
		Sort:    r.GetSort(),
		Limit:   int(r.GetLimit()),
		Cursor:  r.GetCursor(),
	}
}

// ResultToProto maps a source.Result to a FeedReply (without the error field,
// which the server sets separately when Feed fails).
func ResultToProto(res source.Result) *pb.FeedReply {
	items := make([]*pb.ItemMsg, len(res.Items))
	for i, it := range res.Items {
		items[i] = ItemToProto(it)
	}
	return &pb.FeedReply{
		Items:  items,
		Cursor: res.Cursor,
	}
}

// ResultFromProto maps a FeedReply's items and cursor back to a source.Result.
// The reply's error field is handled by the caller, not here.
func ResultFromProto(r *pb.FeedReply) source.Result {
	var items []source.Item
	if len(r.GetItems()) > 0 {
		items = make([]source.Item, len(r.GetItems()))
		for i, m := range r.GetItems() {
			items[i] = ItemFromProto(m)
		}
	}
	return source.Result{
		Items:  items,
		Cursor: r.GetCursor(),
	}
}
