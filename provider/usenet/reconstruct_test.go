package usenet

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"hash/crc32"
	"testing"

	gonntp "github.com/go-newsgroups/nntp"
	"github.com/go-newsgroups/par2"
	"github.com/go-newsgroups/yenc"

	"github.com/go-news-reader/reader/source"
)

// yencEscape yEnc-encodes one part's bytes (no line wrapping needed for tests).
func yencEscape(data []byte) string {
	var b bytes.Buffer
	for _, d := range data {
		e := d + 42
		if e == 0x00 || e == 0x0a || e == 0x0d || e == 0x3d {
			b.WriteByte('=')
			b.WriteByte(e + 64)
			continue
		}
		b.WriteByte(e)
	}
	return b.String()
}

// multipartBody builds a genuine multipart yEnc article body for one part of a
// file (part/total, 1-based begin offset), with a valid per-part pcrc32 so
// yenc.Decode's CRC verification passes and placePart's Begin>0 branch runs.
func multipartBody(name string, whole []byte, part, total int, begin, end int) string {
	seg := whole[begin-1 : end]
	crc := crc32.ChecksumIEEE(seg)
	return fmt.Sprintf("=ybegin part=%d total=%d line=128 size=%d name=%s\r\n"+
		"=ypart begin=%d end=%d\r\n"+
		"%s\r\n"+
		"=yend size=%d part=%d pcrc32=%08x\r\n",
		part, total, len(whole), name, begin, end, yencEscape(seg), len(seg), part, crc)
}

// reconFakeConn serves a fixed set of articles by message-id, optionally failing
// the connection prep (auth) or a specific article fetch.
type reconFakeConn struct {
	articles map[string]*gonntp.Article
	authErr  error
	artErr   error
	closed   bool
}

func (c *reconFakeConn) Group(string) (*gonntp.Group, error)      { return nil, nil }
func (c *reconFakeConn) Over(int, int) ([]gonntp.Overview, error) { return nil, nil }
func (c *reconFakeConn) ModeReader() error                        { return nil }
func (c *reconFakeConn) Authenticate(string, string) error        { return c.authErr }
func (c *reconFakeConn) List(string) ([]gonntp.NewsgroupInfo, error) {
	return nil, nil
}
func (c *reconFakeConn) Article(id string) (*gonntp.Article, error) {
	if c.artErr != nil {
		return nil, c.artErr
	}
	a, ok := c.articles[id]
	if !ok {
		return nil, fmt.Errorf("430 no such article %s", id)
	}
	return a, nil
}
func (c *reconFakeConn) Close() error { c.closed = true; return nil }

// buildPost creates a post: a data file split into two yEnc parts plus one
// PAR2 article, and installs a parsePAR2 seam returning a par2.Create recovery
// set (2 recovery slices) over the intact data file — par2 v0.1.0 exposes no
// serializer to round-trip a real blob, so the .par2 article body is a
// placeholder and the seam supplies the parsed set the verify/repair runs on.
// It returns the reconstruct request, the fake conn, the original data bytes,
// and the data filename. The caller must restore parsePAR2 (t.Cleanup does).
func buildPost(t *testing.T) (ReconstructRequest, *reconFakeConn, []byte, string) {
	t.Helper()
	dataName := "release.bin"
	whole := bytes.Repeat([]byte("PARTONE-"), 8) // 64 bytes
	whole = append(whole, bytes.Repeat([]byte("PARTTWO_"), 8)...)
	half := len(whole) / 2

	// 4 recovery slices cover the 4 data slices of the second part, so corrupting
	// that whole part stays repairable.
	rs, err := par2.Create(16, map[string][]byte{dataName: whole}, 4)
	if err != nil {
		t.Fatalf("par2.Create: %v", err)
	}
	orig := parsePAR2
	parsePAR2 = func(...[]byte) (*par2.RecoverySet, error) { return rs, nil }
	t.Cleanup(func() { parsePAR2 = orig })

	arts := map[string]*gonntp.Article{
		"<data1@h>": {Body: multipartBody(dataName, whole, 1, 2, 1, half)},
		"<data2@h>": {Body: multipartBody(dataName, whole, 2, 2, half+1, len(whole))},
		"<par2@h>":  {Body: string(yenc.Encode(dataName+".par2", []byte("par2-placeholder"), 128))},
	}
	fc := &reconFakeConn{articles: arts}
	req := ReconstructRequest{Parts: []ReconstructPart{
		{MessageID: "data1@h", Filename: dataName},
		{MessageID: "data2@h", Filename: dataName},
		{MessageID: "par2@h", Filename: dataName + ".par2"},
	}}
	return req, fc, whole, dataName
}

func TestReconstructByteIdentical(t *testing.T) {
	req, fc, whole, name := buildPost(t)
	var progress [][2]int
	req.OnProgress = func(done, total int) { progress = append(progress, [2]int{done, total}) }
	p := NewWithDial(func(context.Context) (conn, error) { return fc, nil })

	files, vr, err := p.Reconstruct(context.Background(), req)
	if err != nil {
		t.Fatalf("reconstruct: %v", err)
	}
	if !bytes.Equal(files[name], whole) {
		t.Fatalf("reassembled bytes differ:\n got %q\nwant %q", files[name], whole)
	}
	if vr == nil || !vr.Complete {
		t.Fatalf("expected complete PAR2 verification, got %+v", vr)
	}
	if _, ok := files[name+".par2"]; ok {
		t.Fatal("PAR2 blob should be split out of the returned data files")
	}
	if !fc.closed {
		t.Fatal("connection not closed")
	}
	// Progress fired once per fetched article, ending at n/n.
	if len(progress) != 3 || progress[2] != [2]int{3, 3} {
		t.Fatalf("progress = %v", progress)
	}
}

func TestReconstructRepairsCorruptPart(t *testing.T) {
	req, fc, whole, name := buildPost(t)
	// Corrupt the second data part's payload while keeping a valid CRC for it, so
	// the article decodes cleanly but the reassembled file fails PAR2 and must be
	// repaired from the recovery slices.
	bad := make([]byte, len(whole))
	copy(bad, whole)
	for i := len(whole) / 2; i < len(whole); i++ {
		bad[i] = 'X'
	}
	fc.articles["<data2@h>"] = &gonntp.Article{Body: multipartBody(name, bad, 2, 2, len(whole)/2+1, len(whole))}

	p := NewWithDial(func(context.Context) (conn, error) { return fc, nil })
	files, vr, err := p.Reconstruct(context.Background(), req)
	if err != nil {
		t.Fatalf("reconstruct: %v", err)
	}
	if vr == nil || !vr.Complete {
		t.Fatalf("expected complete after repair, got %+v", vr)
	}
	if !bytes.Equal(files[name], whole) {
		t.Fatalf("repair did not restore original bytes:\n got %q\nwant %q", files[name], whole)
	}
}

func TestReconstructSinglePartNoPAR2(t *testing.T) {
	// Single-part files (Begin==0) append in fetch order; no PAR2 -> vr nil.
	payload := []byte("hello single part")
	fc := &reconFakeConn{articles: map[string]*gonntp.Article{
		"<x@h>": {Body: string(yenc.Encode("note.txt", payload, 128))},
	}}
	p := NewWithDial(func(context.Context) (conn, error) { return fc, nil })
	files, vr, err := p.Reconstruct(context.Background(), ReconstructRequest{
		Parts: []ReconstructPart{{MessageID: "x@h"}}, // empty Filename -> use decoded name
	})
	if err != nil {
		t.Fatalf("reconstruct: %v", err)
	}
	if vr != nil {
		t.Fatalf("expected nil verify result without PAR2, got %+v", vr)
	}
	if !bytes.Equal(files["note.txt"], payload) {
		t.Fatalf("payload = %q", files["note.txt"])
	}
}

func TestReconstructDialError(t *testing.T) {
	p := NewWithDial(func(context.Context) (conn, error) { return nil, errors.New("dial") })
	if _, _, err := p.Reconstruct(context.Background(), ReconstructRequest{}); err == nil {
		t.Fatal("want dial error")
	}
}

func TestReconstructAuthError(t *testing.T) {
	fc := &reconFakeConn{authErr: errors.New("481 AUTHINFO rejected")}
	p := NewWithDial(func(context.Context) (conn, error) { return fc, nil })
	p.user = "u" // trigger AUTHINFO during connect
	_, _, err := p.Reconstruct(context.Background(), ReconstructRequest{})
	if _, ok := source.AsAuthError(err); !ok {
		t.Fatalf("want NeedsAuth, got %v", err)
	}
}

func TestReconstructArticleError(t *testing.T) {
	fc := &reconFakeConn{artErr: errors.New("430 gone")}
	p := NewWithDial(func(context.Context) (conn, error) { return fc, nil })
	_, _, err := p.Reconstruct(context.Background(), ReconstructRequest{
		Parts: []ReconstructPart{{MessageID: "x@h"}},
	})
	if err == nil {
		t.Fatal("want article error")
	}
}

func TestReconstructDecodeError(t *testing.T) {
	fc := &reconFakeConn{articles: map[string]*gonntp.Article{"<x@h>": {Body: "not yenc at all"}}}
	p := NewWithDial(func(context.Context) (conn, error) { return fc, nil })
	_, _, err := p.Reconstruct(context.Background(), ReconstructRequest{
		Parts: []ReconstructPart{{MessageID: "x@h"}},
	})
	if err == nil {
		t.Fatal("want decode error")
	}
}

func TestReconstructContextCancelled(t *testing.T) {
	req, fc, _, _ := buildPost(t)
	p := NewWithDial(func(context.Context) (conn, error) { return fc, nil })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := p.Reconstruct(ctx, req); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context canceled, got %v", err)
	}
}

func TestReconstructPAR2ParseError(t *testing.T) {
	// A .par2 article whose decoded body is not a valid PAR2 blob -> parse error.
	fc := &reconFakeConn{articles: map[string]*gonntp.Article{
		"<p@h>": {Body: string(yenc.Encode("x.par2", []byte("garbage"), 128))},
	}}
	p := NewWithDial(func(context.Context) (conn, error) { return fc, nil })
	_, _, err := p.Reconstruct(context.Background(), ReconstructRequest{
		Parts: []ReconstructPart{{MessageID: "p@h", Filename: "x.par2"}},
	})
	if err == nil {
		t.Fatal("want par2 parse error")
	}
}

func TestBracketID(t *testing.T) {
	if got := bracketID("a@h"); got != "<a@h>" {
		t.Fatalf("bracketID bare = %q", got)
	}
	if got := bracketID("<a@h>"); got != "<a@h>" {
		t.Fatalf("bracketID already-bracketed = %q", got)
	}
}
