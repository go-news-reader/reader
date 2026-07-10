package usenet

import (
	"context"
	"crypto/tls"
	"errors"
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
}

func (f *fakeConn) Group(string) (*gonntp.Group, error) { return f.group, f.groupErr }
func (f *fakeConn) Over(low, high int) ([]gonntp.Overview, error) {
	f.gotLow, f.gotHigh = low, high
	return f.over, f.overErr
}
func (f *fakeConn) Article(string) (*gonntp.Article, error) { return nil, nil }
func (f *fakeConn) Close() error                            { f.closed = true; return nil }

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
