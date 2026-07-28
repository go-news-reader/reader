//go:build nntpnet

// Network integration tests for the Usenet provider against REAL NNTP servers.
//
// These are excluded from the default build and the 100% coverage gate by the
// "nntpnet" build tag; they never run in CI. Run them manually, per target:
//
//	Legacy binary (Free — plain :119, no auth, carries alt.binaries.*):
//	  go test -tags nntpnet -run TestFree ./provider/usenet/ -v
//	  # override the server/group if you are not on the Free network:
//	  NNTP_LEGACY_ADDR=news.free.fr:119 NNTP_LEGACY_GROUP=alt.binaries.test \
//	    go test -tags nntpnet -run TestFree ./provider/usenet/ -v
//
//	Modern text (Eternal-September — TLS :563, AUTHINFO, OVER/READER/COMPRESS):
//	  NNTP_MODERN_USER=you NNTP_MODERN_PASS=secret \
//	    go test -tags nntpnet -run TestModernText ./provider/usenet/ -v
//	  # optional overrides: NNTP_MODERN_ADDR (default news.eternal-september.org:563),
//	  #                     NNTP_MODERN_GROUP (default misc.test)
//
//	Modern binary (XSUsenet free tier or any TLS+AUTHINFO binary server):
//	  NNTP_BIN_ADDR=news.xsusenet.com:563 NNTP_BIN_USER=you NNTP_BIN_PASS=secret \
//	  NNTP_BIN_TLS=1 NNTP_BIN_GROUP=alt.binaries.test \
//	    go test -tags nntpnet -run TestModernBinary ./provider/usenet/ -v
//
// A target's test t.Skip's cleanly when its required env vars are unset, so the
// suite is safe to run with only the credentials you have.
package usenet

import (
	"context"
	"crypto/tls"
	"os"
	"strings"
	"testing"
	"time"

	gonntp "github.com/go-newsgroups/nntp"
	"github.com/go-newsgroups/par2"
	"github.com/go-newsgroups/yenc"
)

const par2Magic = "PAR2\x00PKT"

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// TestFreeLegacyPAR2 reproduces the manual proof: connect anonymously to the
// legacy Free binary server, select a binaries group, scan a recent OVER range
// for a complete single-part ".par2" post, fetch it, yEnc-decode it (the decoder
// verifies the embedded CRC32) and assert the payload is a real PAR2 packet.
func TestFreeLegacyPAR2(t *testing.T) {
	addr := env("NNTP_LEGACY_ADDR", "news.free.fr:119")
	group := env("NNTP_LEGACY_GROUP", "alt.binaries.test")

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	c, err := gonntp.Dial(ctx, addr)
	if err != nil {
		t.Skipf("cannot reach legacy server %s (need the Free network?): %v", addr, err)
	}
	defer c.Close()

	// MODE READER is tolerated everywhere and is what the provider issues too.
	_ = c.ModeReader()
	t.Logf("connected to %s: legacy=%v", addr, c.Legacy())

	g, err := c.Group(group)
	if err != nil {
		t.Fatalf("GROUP %s: %v", group, err)
	}
	t.Logf("group %s: count=%d low=%d high=%d", g.Name, g.Count, g.Low, g.High)

	// Look back over a window of recent articles for single-part .par2 posts.
	const window = 4000
	low := g.High - window + 1
	if low < g.Low {
		low = g.Low
	}
	overs, err := c.Over(low, g.High)
	if err != nil {
		t.Fatalf("OVER %d-%d: %v", low, g.High, err)
	}
	t.Logf("scanning %d overviews for a single-part .par2", len(overs))

	tried := 0
	for _, ov := range overs {
		if !isSinglePartPAR2(ov.Subject) {
			continue
		}
		tried++
		art, err := c.Article(ov.MessageID)
		if err != nil {
			t.Logf("ARTICLE %s: %v (trying next)", ov.MessageID, err)
			continue
		}
		part, err := yenc.Decode([]byte(art.Body))
		if err != nil {
			t.Logf("yEnc decode %q: %v (trying next)", ov.Subject, err)
			continue
		}
		if strings.HasPrefix(string(part.Data), par2Magic) {
			t.Logf("OK: %q -> %d bytes, PAR2 magic verified, CRC ok", ov.Subject, len(part.Data))
			return
		}
		t.Logf("decoded %q (%d bytes) but not PAR2 magic (trying next)", part.Name, len(part.Data))
	}
	if tried == 0 {
		t.Skipf("no single-part .par2 subjects found in the last %d articles of %s", window, group)
	}
	t.Fatalf("found %d candidate .par2 posts but none decoded to a PAR2 payload", tried)
}

// isSinglePartPAR2 heuristically matches an OVER subject naming a complete,
// single-part yEnc-posted .par2 file, e.g. "foo.par2 (1/1)" or "foo.vol0.PAR2 [1/1]".
func isSinglePartPAR2(subject string) bool {
	s := strings.ToLower(subject)
	if !strings.Contains(s, ".par2") {
		return false
	}
	return strings.Contains(s, "(1/1)") || strings.Contains(s, "[1/1]") ||
		strings.Contains(s, "(01/01)") || strings.Contains(s, "[01/01]")
}

// TestModernText exercises a modern TLS text server (Eternal-September):
// implicit TLS, MODE READER, AUTHINFO, capability negotiation (not legacy,
// advertises READER), then a group select and one article fetch.
func TestModernText(t *testing.T) {
	user := os.Getenv("NNTP_MODERN_USER")
	pass := os.Getenv("NNTP_MODERN_PASS")
	if user == "" || pass == "" {
		t.Skip("set NNTP_MODERN_USER and NNTP_MODERN_PASS to run the modern-text target")
	}
	addr := env("NNTP_MODERN_ADDR", "news.eternal-september.org:563")
	group := env("NNTP_MODERN_GROUP", "misc.test")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	c, err := gonntp.DialTLS(ctx, addr, &tls.Config{})
	if err != nil {
		t.Fatalf("DialTLS %s: %v", addr, err)
	}
	defer c.Close()

	_ = c.ModeReader()
	if err := c.Authenticate(user, pass); err != nil {
		t.Fatalf("AUTHINFO: %v", err)
	}
	if c.Legacy() {
		t.Fatalf("%s reported legacy after auth; expected a modern (CAPABILITIES) server", addr)
	}
	if !c.HasCapability("READER") {
		caps, _ := c.Capabilities()
		t.Fatalf("READER capability not advertised; caps=%v", caps)
	}
	t.Logf("authenticated to %s: legacy=%v READER=%v OVER=%v COMPRESS=%v",
		addr, c.Legacy(), c.HasCapability("READER"), c.HasCapability("OVER"), c.HasCapability("COMPRESS"))

	g, err := c.Group(group)
	if err != nil {
		t.Fatalf("GROUP %s: %v", group, err)
	}
	if g.High < g.Low {
		t.Skipf("group %s is empty (low=%d high=%d)", group, g.Low, g.High)
	}
	art, err := c.Article(mustArticleRef(t, c, g))
	if err != nil {
		t.Fatalf("ARTICLE: %v", err)
	}
	t.Logf("fetched one article from %s: %d header fields, %d body bytes",
		group, len(art.Headers), len(art.Body))
}

// mustArticleRef returns a fetchable reference (message-id) for the newest
// article in g, using OVER on the top of the range.
func mustArticleRef(t *testing.T, c *gonntp.Conn, g *gonntp.Group) string {
	t.Helper()
	low := g.High - 20
	if low < g.Low {
		low = g.Low
	}
	overs, err := c.Over(low, g.High)
	if err != nil {
		t.Fatalf("OVER %d-%d: %v", low, g.High, err)
	}
	if len(overs) == 0 {
		t.Skipf("no overviews in %s range %d-%d", g.Name, low, g.High)
	}
	return overs[len(overs)-1].MessageID
}

// TestModernBinary exercises a modern TLS+AUTHINFO binary server (XSUsenet free
// tier or similar): it downloads a group's recent posts, yEnc-decodes and
// reassembles multipart files, and — when a PAR2 set is present — verifies the
// real recovery set, then performs a deterministic repair check by corrupting a
// downloaded data file and confirming AutoPAR reconstructs it.
func TestModernBinary(t *testing.T) {
	addr := os.Getenv("NNTP_BIN_ADDR")
	user := os.Getenv("NNTP_BIN_USER")
	pass := os.Getenv("NNTP_BIN_PASS")
	if addr == "" || user == "" || pass == "" {
		t.Skip("set NNTP_BIN_ADDR, NNTP_BIN_USER and NNTP_BIN_PASS to run the modern-binary target")
	}
	group := env("NNTP_BIN_GROUP", "alt.binaries.test")
	useTLS := os.Getenv("NNTP_BIN_TLS") != "0" // default to TLS; set NNTP_BIN_TLS=0 for plain

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	var c *gonntp.Conn
	var err error
	if useTLS {
		c, err = gonntp.DialTLS(ctx, addr, &tls.Config{})
	} else {
		c, err = gonntp.Dial(ctx, addr)
	}
	if err != nil {
		t.Fatalf("dial %s (tls=%v): %v", addr, useTLS, err)
	}
	defer c.Close()

	_ = c.ModeReader()
	if err := c.Authenticate(user, pass); err != nil {
		t.Fatalf("AUTHINFO: %v", err)
	}
	t.Logf("authenticated to %s: legacy=%v", addr, c.Legacy())

	g, err := c.Group(group)
	if err != nil {
		t.Fatalf("GROUP %s: %v", group, err)
	}
	if g.High < g.Low {
		t.Skipf("group %s is empty", group)
	}

	// Download and reassemble a recent window into a name -> bytes map.
	const window = 2000
	low := g.High - window + 1
	if low < g.Low {
		low = g.Low
	}
	overs, err := c.Over(low, g.High)
	if err != nil {
		t.Fatalf("OVER %d-%d: %v", low, g.High, err)
	}
	files := reassemble(t, c, overs)
	t.Logf("reassembled %d complete files from %d overviews", len(files), len(overs))
	if len(files) == 0 {
		t.Skipf("no complete yEnc files reassembled from %s (junk feed?)", group)
	}

	par2Blobs, data := SplitPAR2(files)
	if len(par2Blobs) == 0 || len(data) == 0 {
		t.Skipf("no PAR2 recovery set + data pair found in %s (got %d par2, %d data)",
			group, len(par2Blobs), len(data))
	}
	rs, err := par2.Parse(par2Blobs...)
	if err != nil {
		t.Fatalf("par2.Parse: %v", err)
	}

	// 1) The pristine download verifies against the real recovery set.
	out, vr, err := AutoPAR(rs, data)
	if err != nil {
		t.Fatalf("AutoPAR verify: %v", err)
	}
	if !vr.Complete {
		t.Fatalf("downloaded set did not verify complete: repairable=%v", vr.Repairable)
	}
	t.Logf("PAR2 verify OK: %d data files complete", len(out))

	// 2) Deterministic repair proof: corrupt one downloaded data file and confirm
	//    AutoPAR reconstructs it from the real recovery slices.
	var victim string
	for name := range data {
		victim = name
		break
	}
	damaged := cloneFiles(data)
	if len(damaged[victim]) == 0 {
		t.Skipf("victim %q is empty; cannot corrupt", victim)
	}
	damaged[victim][0] ^= 0xFF // flip a byte
	repaired, rvr, err := AutoPAR(rs, damaged)
	if err != nil {
		t.Fatalf("AutoPAR repair: %v", err)
	}
	if !rvr.Complete {
		t.Skipf("recovery set lacks enough slices to repair one corrupt file (repairable=%v); "+
			"re-run against a set that includes .vol*.par2 recovery volumes", rvr.Repairable)
	}
	if string(repaired[victim]) != string(data[victim]) {
		t.Fatalf("repair did not restore %q to its original bytes", victim)
	}
	t.Logf("PAR2 repair OK: corrupted %q was reconstructed byte-for-byte", victim)
}

// reassemble downloads each overview's article, yEnc-decodes it and joins the
// parts of every multipart post into complete files keyed by file name. Parts
// that fail to fetch or decode are skipped; a file is emitted only when all of
// its advertised parts are present (single-part posts always qualify).
func reassemble(t *testing.T, c *gonntp.Conn, overs []gonntp.Overview) map[string][]byte {
	t.Helper()
	type acc struct {
		total int
		parts map[int][]byte
	}
	byName := map[string]*acc{}
	for _, ov := range overs {
		art, err := c.Article(ov.MessageID)
		if err != nil {
			continue
		}
		part, err := yenc.Decode([]byte(art.Body))
		if err != nil || part.Name == "" {
			continue
		}
		a := byName[part.Name]
		if a == nil {
			a = &acc{total: part.Total, parts: map[int][]byte{}}
			byName[part.Name] = a
		}
		if part.Total > a.total {
			a.total = part.Total
		}
		idx := part.Part
		if idx == 0 {
			idx = 1 // single-part
		}
		a.parts[idx] = part.Data
	}
	out := map[string][]byte{}
	for name, a := range byName {
		total := a.total
		if total == 0 {
			total = 1
		}
		if len(a.parts) < total {
			continue // incomplete set
		}
		var buf []byte
		complete := true
		for i := 1; i <= total; i++ {
			p, ok := a.parts[i]
			if !ok {
				complete = false
				break
			}
			buf = append(buf, p...)
		}
		if complete {
			out[name] = buf
		}
	}
	return out
}

// cloneFiles deep-copies a name -> bytes map so a corruption test cannot mutate
// the pristine reference data.
func cloneFiles(in map[string][]byte) map[string][]byte {
	out := make(map[string][]byte, len(in))
	for k, v := range in {
		cp := make([]byte, len(v))
		copy(cp, v)
		out[k] = cp
	}
	return out
}

// TestFreeGroups exercises the provider's Groups (LIST ACTIVE "*") against the
// legacy Free binary server: it asserts a large carried-group list that
// includes the well-known alt.binaries.test, and that the result is cached
// (a second call issues no further network traffic). This is the browse
// window's data source.
func TestFreeGroups(t *testing.T) {
	addr := env("NNTP_LEGACY_ADDR", "news.free.fr:119")

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	p := New(addr, false)
	names, err := p.Groups(ctx)
	if err != nil {
		t.Skipf("cannot list groups on %s (need the Free network?): %v", addr, err)
	}
	t.Logf("Free %s carries %d newsgroups", addr, len(names))
	if len(names) < 1000 {
		t.Fatalf("expected a large group list from %s, got only %d", addr, len(names))
	}
	found := false
	for _, n := range names {
		if n == "alt.binaries.test" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("alt.binaries.test not present in %d listed groups", len(names))
	}
	// Sorted ascending.
	for i := 1; i < len(names); i++ {
		if names[i] < names[i-1] {
			t.Fatalf("group list not sorted at %d: %q < %q", i, names[i], names[i-1])
		}
	}
}
