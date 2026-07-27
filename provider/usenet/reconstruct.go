package usenet

import (
	"context"
	"strings"

	"github.com/go-newsgroups/par2"
	"github.com/go-newsgroups/yenc"
)

// ReconstructPart names one article to fetch when reconstructing a post: its
// bare message-id (no "news:" scheme, no angle brackets) and the file it is a
// part of.
type ReconstructPart struct {
	MessageID string
	Filename  string
}

// ReconstructRequest describes a post to rebuild from its member articles. Parts
// lists every article (data and PAR2) discovered for the post; OnProgress, when
// set, is called after each article is downloaded and decoded so a front-end can
// drive a "k/n parts" indicator.
type ReconstructRequest struct {
	Parts      []ReconstructPart
	OnProgress func(done, total int)
}

// Reconstruct downloads every article of a post over one connection,
// yEnc-decodes each (CRC-verified), reassembles the parts per filename by their
// Begin/End offsets, and — when the set includes PAR2 recovery files — verifies
// and repairs the data files. It returns the data files (PAR2 blobs removed)
// and the final verification result (nil when the post carried no PAR2). An
// authentication failure is mapped to a source.NeedsAuth error, as in Feed.
func (p *Provider) Reconstruct(ctx context.Context, req ReconstructRequest) (map[string][]byte, *par2.VerifyResult, error) {
	c, err := p.connect(ctx)
	if err != nil {
		return nil, nil, mapErr(err)
	}
	defer c.Close()

	files := make(map[string][]byte)
	total := len(req.Parts)
	for i, part := range req.Parts {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		art, err := c.Article(bracketID(part.MessageID))
		if err != nil {
			return nil, nil, err
		}
		dec, err := yenc.Decode([]byte(art.Body))
		if err != nil {
			return nil, nil, err
		}
		name := part.Filename
		if name == "" {
			name = dec.Name
		}
		files[name] = placePart(files[name], dec)
		if req.OnProgress != nil {
			req.OnProgress(i+1, total)
		}
	}

	par2Blobs, data := SplitPAR2(files)
	if len(par2Blobs) == 0 {
		return data, nil, nil
	}
	rs, err := parsePAR2(par2Blobs...)
	if err != nil {
		return nil, nil, err
	}
	return autoPAR(rs, data)
}

// parsePAR2 assembles a recovery set from the downloaded ".par2" blobs. A
// package var so tests can inject a set built with par2.Create (which has no
// public serializer to round-trip through the real parser).
var parsePAR2 = par2.Parse

// placePart writes a decoded yEnc part into the accumulating file buffer: a
// multipart part (Begin>0) lands at its 1-based Begin offset (growing the buffer
// to End when needed); a single-part body is appended in fetch order.
func placePart(buf []byte, part *yenc.Part) []byte {
	if part.Begin > 0 {
		if end := int(part.End); end > len(buf) {
			grown := make([]byte, end)
			copy(grown, buf)
			buf = grown
		}
		copy(buf[part.Begin-1:], part.Data)
		return buf
	}
	return append(buf, part.Data...)
}

// bracketID wraps a bare message-id in angle brackets for the NNTP ARTICLE
// command, leaving an already-bracketed id untouched.
func bracketID(id string) string {
	if strings.HasPrefix(id, "<") {
		return id
	}
	return "<" + id + ">"
}
