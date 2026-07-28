package usenet

import (
	"context"
	"crypto/tls"
	"errors"
	"net/textproto"
	"testing"
	"time"

	gonntp "github.com/go-newsgroups/nntp"

	"github.com/go-news-reader/reader/source"
)

type fakeConn struct {
	group    *gonntp.Group
	groupErr error
	over     []gonntp.Overview
	overErr  error
	closed   bool
	gotLow   int
	gotHigh  int

	modeCalled bool
	modeErr    error
	authUser   string
	authPass   string
	authCalled bool
	authErr    error

	list      []gonntp.NewsgroupInfo
	listErr   error
	listCalls int
	listArg   string
}

func (f *fakeConn) Group(string) (*gonntp.Group, error) { return f.group, f.groupErr }
func (f *fakeConn) Over(low, high int) ([]gonntp.Overview, error) {
	f.gotLow, f.gotHigh = low, high
	return f.over, f.overErr
}
func (f *fakeConn) List(wildmat string) ([]gonntp.NewsgroupInfo, error) {
	f.listCalls++
	f.listArg = wildmat
	return f.list, f.listErr
}
func (f *fakeConn) Article(string) (*gonntp.Article, error) { return nil, nil }
func (f *fakeConn) ModeReader() error                       { f.modeCalled = true; return f.modeErr }
func (f *fakeConn) Authenticate(user, pass string) error {
	f.authCalled, f.authUser, f.authPass = true, user, pass
	return f.authErr
}
func (f *fakeConn) Close() error { f.closed = true; return nil }

func dialing(c conn, err error) dialFunc {
	return func(context.Context) (conn, error) { return c, err }
}

func TestKind(t *testing.T) {
	if New("news:119", false).Kind() != source.Usenet {
		t.Fatal("kind")
	}
}

func TestFeedNoChannel(t *testing.T) {
	if _, err := New("x", false).Feed(context.Background(), source.Query{}); !errors.Is(err, ErrNoChannel) {
		t.Fatalf("want ErrNoChannel, got %v", err)
	}
}

func TestFeedDialError(t *testing.T) {
	p := NewWithDial(dialing(nil, errors.New("dial")))
	if _, err := p.Feed(context.Background(), source.Query{Channel: "comp.lang.go"}); err == nil {
		t.Fatal("want dial error")
	}
}

func TestFeedGroupError(t *testing.T) {
	fc := &fakeConn{groupErr: errors.New("411 no such group")}
	p := NewWithDial(dialing(fc, nil))
	if _, err := p.Feed(context.Background(), source.Query{Channel: "x"}); err == nil {
		t.Fatal("want group error")
	}
	if !fc.closed {
		t.Fatal("conn not closed")
	}
}

func TestFeedOverError(t *testing.T) {
	fc := &fakeConn{group: &gonntp.Group{Low: 1, High: 100}, overErr: errors.New("over")}
	p := NewWithDial(dialing(fc, nil))
	if _, err := p.Feed(context.Background(), source.Query{Channel: "x"}); err == nil {
		t.Fatal("want over error")
	}
}

func TestFeedMapAndClamp(t *testing.T) {
	fc := &fakeConn{
		group: &gonntp.Group{Low: 90, High: 100}, // clamp: 100-50+1=51 < 90 -> low=90
		over: []gonntp.Overview{{
			ArticleNum: 100, Subject: "Hello", From: "a@b.com",
			MessageID: "<msg1@host>", Date: time.Unix(1700000000, 0),
		}},
	}
	p := NewWithDial(dialing(fc, nil))
	res, err := p.Feed(context.Background(), source.Query{Channel: "comp.lang.go", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if fc.gotLow != 90 || fc.gotHigh != 100 {
		t.Fatalf("range = %d-%d, want 90-100 (clamped)", fc.gotLow, fc.gotHigh)
	}
	it := res.Items[0]
	if it.Title != "Hello" || it.Author != "a@b.com" || it.ID != "<msg1@host>" ||
		it.Permalink != "news:msg1@host" || it.Score != -1 || it.Comments != -1 || it.Created != 1700000000 {
		t.Fatalf("item %+v", it)
	}
}

func TestFeedDefaultCountNoClamp(t *testing.T) {
	fc := &fakeConn{group: &gonntp.Group{Low: 1, High: 100}} // 100-50+1=51 > 1, no clamp
	p := NewWithDial(dialing(fc, nil))
	if _, err := p.Feed(context.Background(), source.Query{Channel: "x"}); err != nil {
		t.Fatal(err)
	}
	if fc.gotLow != 51 || fc.gotHigh != 100 {
		t.Fatalf("range = %d-%d, want 51-100 (default count 50)", fc.gotLow, fc.gotHigh)
	}
}

func TestNewTransportSelection(t *testing.T) {
	// Drive New's TLS/plaintext branches through overridden primitives.
	origPlain, origTLS := nntpDial, nntpDialTLS
	defer func() { nntpDial, nntpDialTLS = origPlain, origTLS }()

	plainCalled, tlsCalled := false, false
	nntpDial = func(context.Context, string) (conn, error) { plainCalled = true; return &fakeConn{}, nil }
	nntpDialTLS = func(context.Context, string, *tls.Config) (conn, error) { tlsCalled = true; return &fakeConn{}, nil }

	if _, err := New("h:119", false).dial(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := New("h:563", true).dial(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !plainCalled || !tlsCalled {
		t.Fatalf("branches not both taken: plain=%v tls=%v", plainCalled, tlsCalled)
	}
}

func TestDialPrimitiveDefaults(t *testing.T) {
	// A closed port fails the TCP connect immediately, exercising the real
	// nntpDial / nntpDialTLS wrapper bodies without a hanging half-server (a
	// plaintext server would stall a TLS handshake forever). The short timeout
	// is a safety net.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := nntpDial(ctx, "127.0.0.1:1"); err == nil {
		t.Fatal("expected dial error to closed port")
	}
	if _, err := nntpDialTLS(ctx, "127.0.0.1:1", &tls.Config{}); err == nil {
		t.Fatal("expected TLS dial error to closed port")
	}
}

func TestFeedWithAuth(t *testing.T) {
	// With credentials, connect() issues MODE READER then AUTHINFO before Group.
	fc := &fakeConn{group: &gonntp.Group{Low: 1, High: 3}}
	p := NewWithDial(dialing(fc, nil)).WithAuth("alice", "s3cret")
	if _, err := p.Feed(context.Background(), source.Query{Channel: "misc.test"}); err != nil {
		t.Fatal(err)
	}
	if !fc.modeCalled {
		t.Fatal("MODE READER not issued")
	}
	if !fc.authCalled || fc.authUser != "alice" || fc.authPass != "s3cret" {
		t.Fatalf("AUTHINFO not sent with creds: called=%v user=%q", fc.authCalled, fc.authUser)
	}
}

func TestFeedAnonymousNoAuth(t *testing.T) {
	// Without credentials, MODE READER is still issued but AUTHINFO is skipped.
	fc := &fakeConn{group: &gonntp.Group{Low: 1, High: 3}}
	p := NewWithDial(dialing(fc, nil)) // no WithAuth
	if _, err := p.Feed(context.Background(), source.Query{Channel: "alt.binaries.test"}); err != nil {
		t.Fatal(err)
	}
	if !fc.modeCalled {
		t.Fatal("MODE READER not issued")
	}
	if fc.authCalled {
		t.Fatal("AUTHINFO must not be sent without credentials")
	}
}

func TestFeedAuthRejectedNeedsAuth(t *testing.T) {
	// An AUTHINFO rejection during connect maps to a typed Usenet AuthError and
	// the connection is closed.
	fc := &fakeConn{authErr: &textproto.Error{Code: 481, Msg: "authentication failed"}}
	p := NewWithDial(dialing(fc, nil)).WithAuth("bob", "wrong")
	_, err := p.Feed(context.Background(), source.Query{Channel: "misc.test"})
	if ae, ok := source.AsAuthError(err); !ok || ae.Kind != source.Usenet {
		t.Fatalf("AUTHINFO rejection not mapped to Usenet AuthError: %v", err)
	}
	if !fc.closed {
		t.Fatal("connection not closed after auth failure")
	}
	if fc.group != nil || fc.gotHigh != 0 {
		t.Fatal("Group/Over must not run after failed auth")
	}
}

func TestFeedAuthError(t *testing.T) {
	// An NNTP auth-required response code (480) maps to a typed AuthError.
	fc := &fakeConn{groupErr: &textproto.Error{Code: 480, Msg: "authentication required"}}
	_, err := NewWithDial(dialing(fc, nil)).Feed(context.Background(), source.Query{Channel: "comp.lang.go"})
	if ae, ok := source.AsAuthError(err); !ok || ae.Kind != source.Usenet {
		t.Fatalf("480 not mapped to Usenet AuthError: %v", err)
	}
	// An AUTHINFO rejection (formatted as text by the nntp client) maps too.
	fc2 := &fakeConn{groupErr: errors.New("nntp: AUTHINFO PASS rejected code 481: bad login")}
	_, err = NewWithDial(dialing(fc2, nil)).Feed(context.Background(), source.Query{Channel: "x"})
	if _, ok := source.AsAuthError(err); !ok {
		t.Fatalf("AUTHINFO rejection not mapped: %v", err)
	}
	// An HTTP-style 403 surfacing on OVER (indexer-ish) maps too.
	fc3 := &fakeConn{group: &gonntp.Group{Low: 1, High: 3}, overErr: errors.New("unexpected status 403")}
	_, err = NewWithDial(dialing(fc3, nil)).Feed(context.Background(), source.Query{Channel: "x"})
	if _, ok := source.AsAuthError(err); !ok {
		t.Fatalf("403 OVER error not mapped: %v", err)
	}
	// A non-auth NNTP error (411 no such group) passes through untouched.
	fc4 := &fakeConn{groupErr: errors.New("411 no such group")}
	_, err = NewWithDial(dialing(fc4, nil)).Feed(context.Background(), source.Query{Channel: "x"})
	if _, ok := source.AsAuthError(err); ok {
		t.Fatalf("411 misclassified as auth: %v", err)
	}
}

func TestGroupsCachesAndSorts(t *testing.T) {
	fc := &fakeConn{list: []gonntp.NewsgroupInfo{
		{Name: "comp.lang.go"},
		{Name: "alt.binaries.test"},
		{Name: ""}, // blank names are dropped
		{Name: "alt.test"},
	}}
	p := NewWithDial(dialing(fc, nil))

	got, err := p.Groups(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"alt.binaries.test", "alt.test", "comp.lang.go"}
	if len(got) != len(want) {
		t.Fatalf("groups = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("groups[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if fc.listArg != "*" {
		t.Fatalf("List wildmat = %q, want *", fc.listArg)
	}
	if !fc.closed {
		t.Fatal("connection not closed after listing")
	}
	// A second call is served from the cache: List is not issued again.
	if _, err := p.Groups(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fc.listCalls != 1 {
		t.Fatalf("List called %d times, want 1 (cached)", fc.listCalls)
	}
}

func TestRefreshGroupsBypassesCache(t *testing.T) {
	fc := &fakeConn{list: []gonntp.NewsgroupInfo{{Name: "alt.test"}}}
	p := NewWithDial(dialing(fc, nil))
	if _, err := p.Groups(context.Background()); err != nil {
		t.Fatal(err)
	}
	// The server now carries an extra group; RefreshGroups re-fetches it.
	fc.list = []gonntp.NewsgroupInfo{{Name: "alt.test"}, {Name: "alt.new"}}
	got, err := p.RefreshGroups(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "alt.new" || got[1] != "alt.test" {
		t.Fatalf("refreshed groups = %v", got)
	}
	if fc.listCalls != 2 {
		t.Fatalf("List called %d times, want 2 (refresh bypasses cache)", fc.listCalls)
	}
}

func TestGroupsDialError(t *testing.T) {
	p := NewWithDial(dialing(nil, errors.New("dial")))
	if _, err := p.Groups(context.Background()); err == nil {
		t.Fatal("want dial error")
	}
}

func TestGroupsListError(t *testing.T) {
	fc := &fakeConn{listErr: errors.New("503 program error")}
	p := NewWithDial(dialing(fc, nil))
	if _, err := p.Groups(context.Background()); err == nil {
		t.Fatal("want list error")
	}
	if !fc.closed {
		t.Fatal("connection not closed after list error")
	}
}

func TestGroupsAuthError(t *testing.T) {
	// An AUTHINFO rejection while connecting for the list maps to a typed AuthError.
	fc := &fakeConn{listErr: &textproto.Error{Code: 502, Msg: "permission denied"}}
	p := NewWithDial(dialing(fc, nil))
	_, err := p.Groups(context.Background())
	if ae, ok := source.AsAuthError(err); !ok || ae.Kind != source.Usenet {
		t.Fatalf("502 not mapped to Usenet AuthError: %v", err)
	}
}
