// Package usenet adapts the standalone github.com/go-newsgroups/nntp client to
// the aggregator's source.Provider contract. The query channel is a newsgroup
// name (e.g. "comp.lang.go"); the provider dials the configured NNTP server,
// selects the group, and returns recent articles via OVER.
package usenet

import (
	"context"
	"crypto/tls"
	"errors"
	"strings"

	gonntp "github.com/go-newsgroups/nntp"

	"github.com/go-news-reader/reader/source"
)

// ErrNoChannel is returned when Feed is called without a newsgroup name.
var ErrNoChannel = errors.New("usenet: Query.Channel must be a newsgroup name")

// defaultCount caps how many recent articles are fetched when Query.Limit is 0.
const defaultCount = 50

// conn is the slice of *gonntp.Conn the adapter uses; an interface for tests.
type conn interface {
	Group(name string) (*gonntp.Group, error)
	Over(low, high int) ([]gonntp.Overview, error)
	Close() error
}

// dialFunc opens a connection to the NNTP server; a seam for tests.
type dialFunc func(ctx context.Context) (conn, error)

// nntpDial and nntpDialTLS wrap the underlying client's dials as package vars so
// tests can drive New's transport selection without the real network.
var (
	nntpDial = func(ctx context.Context, addr string) (conn, error) {
		return gonntp.Dial(ctx, addr)
	}
	nntpDialTLS = func(ctx context.Context, addr string, cfg *tls.Config) (conn, error) {
		return gonntp.DialTLS(ctx, addr, cfg)
	}
)

// Provider fetches Usenet articles as normalized items.
type Provider struct {
	dial dialFunc
}

// New returns a provider dialing addr ("host:port"). When useTLS is set an
// implicit-TLS connection is made (default port 563), otherwise plaintext (119).
func New(addr string, useTLS bool) *Provider {
	return &Provider{dial: func(ctx context.Context) (conn, error) {
		if useTLS {
			return nntpDialTLS(ctx, addr, &tls.Config{})
		}
		return nntpDial(ctx, addr)
	}}
}

// NewWithDial wraps a custom dial function (used by tests with a fake conn).
func NewWithDial(dial dialFunc) *Provider { return &Provider{dial: dial} }

// Kind reports source.Usenet.
func (p *Provider) Kind() source.Kind { return source.Usenet }

// Feed selects the newsgroup and returns its most recent articles' overviews.
// Not cursor-paginated here, so Result.Cursor is always empty.
func (p *Provider) Feed(ctx context.Context, q source.Query) (source.Result, error) {
	group := strings.TrimSpace(q.Channel)
	if group == "" {
		return source.Result{}, ErrNoChannel
	}
	c, err := p.dial(ctx)
	if err != nil {
		return source.Result{}, err
	}
	defer c.Close()

	g, err := c.Group(group)
	if err != nil {
		return source.Result{}, err
	}

	count := q.Limit
	if count <= 0 {
		count = defaultCount
	}
	low := g.High - count + 1
	if low < g.Low {
		low = g.Low
	}
	overviews, err := c.Over(low, g.High)
	if err != nil {
		return source.Result{}, err
	}

	items := make([]source.Item, 0, len(overviews))
	for _, ov := range overviews {
		items = append(items, mapOverview(group, ov))
	}
	return source.Result{Items: items}, nil
}

func mapOverview(group string, ov gonntp.Overview) source.Item {
	return source.Item{
		ID:        ov.MessageID,
		Source:    source.Usenet,
		Channel:   group,
		Title:     ov.Subject,
		Author:    ov.From,
		Permalink: "news:" + strings.Trim(ov.MessageID, "<>"),
		Score:     -1,
		Comments:  -1,
		Created:   ov.Date.Unix(),
	}
}
