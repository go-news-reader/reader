// Package usenet adapts the standalone github.com/go-newsgroups/nntp client to
// the aggregator's source.Provider contract. The query channel is a newsgroup
// name (e.g. "comp.lang.go"); the provider dials the configured NNTP server,
// selects the group, and returns recent articles via OVER.
package usenet

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"net/textproto"
	"sort"
	"strings"
	"sync"

	"github.com/go-newsgroups/newznab"
	gonntp "github.com/go-newsgroups/nntp"

	"github.com/go-news-reader/reader/source"
)

// ErrNoChannel is returned when Feed is called without a newsgroup name.
var ErrNoChannel = errors.New("usenet: Query.Channel must be a newsgroup name")

// ErrNoSearch is returned when a "search:" query is issued but no indexer was
// configured (use NewWithSearch).
var ErrNoSearch = errors.New("usenet: no Newznab indexer configured for search")

// searchPrefix marks a Query.Channel as a Newznab search rather than a group.
const searchPrefix = "search:"

// defaultCount caps how many recent articles are fetched when Query.Limit is 0.
const defaultCount = 50

// conn is the slice of *gonntp.Conn the adapter uses; an interface for tests.
// Authenticate and ModeReader support modern servers that gate reader commands
// behind MODE READER and require AUTHINFO credentials; both are no-ops on legacy
// binary servers (Free), where the connection is used anonymously.
type conn interface {
	Group(name string) (*gonntp.Group, error)
	Over(low, high int) ([]gonntp.Overview, error)
	Article(msgIDorNum string) (*gonntp.Article, error)
	List(wildmat string) ([]gonntp.NewsgroupInfo, error)
	Authenticate(user, pass string) error
	ModeReader() error
	Close() error
}

// searcher is the slice of *newznab.Client the adapter uses; an interface for tests.
type searcher interface {
	Search(ctx context.Context, opts newznab.SearchOptions) (*newznab.SearchResult, error)
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

// Provider fetches Usenet articles as normalized items. With an indexer
// configured (NewWithSearch) it also answers "search:" queries via Newznab.
type Provider struct {
	dial   dialFunc
	search searcher // nil when no indexer configured
	user   string   // AUTHINFO USER; "" means connect anonymously (legacy Free)
	pass   string   // AUTHINFO PASS

	// groups caches the server's full active group list (tens of thousands of
	// names). Groups fetches it once and memoises it here; RefreshGroups clears it
	// and re-fetches. Guarded by gmu because the browse window loads it on a
	// background goroutine.
	gmu    sync.Mutex
	groups []string
}

// WithAuth attaches AUTHINFO credentials used when connecting to modern servers
// (TLS text/binary providers such as Eternal-September or XSUsenet). It returns
// the provider so it can be chained onto any constructor; an empty user leaves
// the connection anonymous (the legacy Free path). Password without a user is
// ignored — AUTHINFO USER is what triggers authentication.
func (p *Provider) WithAuth(user, pass string) *Provider {
	p.user, p.pass = user, pass
	return p
}

func dialer(addr string, useTLS bool) dialFunc {
	return func(ctx context.Context) (conn, error) {
		if useTLS {
			return nntpDialTLS(ctx, addr, &tls.Config{})
		}
		return nntpDial(ctx, addr)
	}
}

// New returns a provider dialing addr ("host:port"). When useTLS is set an
// implicit-TLS connection is made (default port 563), otherwise plaintext (119).
func New(addr string, useTLS bool) *Provider {
	return &Provider{dial: dialer(addr, useTLS)}
}

// NewWithSearch returns a provider that also answers "search:<query>" feeds via
// a Newznab indexer (works with a direct indexer or an NZBHydra2 endpoint).
func NewWithSearch(addr string, useTLS bool, indexerURL, apiKey string) *Provider {
	return &Provider{dial: dialer(addr, useTLS), search: newznab.New(indexerURL, apiKey)}
}

// NewWithSearchClient is like NewWithSearch but routes the Newznab indexer's
// HTTP calls through hc (e.g. the shared, request-logging client so the Network
// log captures the indexer traffic). The NNTP transport is not HTTP and is not
// logged.
func NewWithSearchClient(hc *http.Client, addr string, useTLS bool, indexerURL, apiKey string) *Provider {
	return &Provider{dial: dialer(addr, useTLS), search: newznab.New(indexerURL, apiKey, newznab.WithHTTPClient(hc))}
}

// NewWithDial wraps a custom dial function (used by tests with a fake conn).
func NewWithDial(dial dialFunc) *Provider { return &Provider{dial: dial} }

// Kind reports source.Usenet.
func (p *Provider) Kind() source.Kind { return source.Usenet }

// connect dials the server and prepares the connection: it always issues MODE
// READER (safe everywhere — a no-op on servers that greet in reader mode, and
// what a modern server needs before reader commands) and then, when credentials
// are configured, performs AUTHINFO USER/PASS before any group selection. A
// preparation failure closes the connection and is returned raw for the caller
// to classify (mapErr turns an AUTHINFO rejection into a NeedsAuth prompt).
func (p *Provider) connect(ctx context.Context) (conn, error) {
	c, err := p.dial(ctx)
	if err != nil {
		return nil, err
	}
	if err := prepare(c, p.user, p.pass); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

// prepare issues MODE READER (tolerated everywhere) and, when user is non-empty,
// authenticates with AUTHINFO. The MODE READER result is intentionally ignored:
// the library already treats a 500/501 "unknown command" as success, and any
// genuine transport failure surfaces on the following command.
func prepare(c conn, user, pass string) error {
	_ = c.ModeReader()
	if user != "" {
		return c.Authenticate(user, pass)
	}
	return nil
}

// Feed returns a page of items. A Query.Channel of "search:<query>" runs a
// Newznab search (NZB results); any other non-empty channel is a newsgroup name
// whose most recent article overviews are returned. Not cursor-paginated, so
// Result.Cursor is always empty.
func (p *Provider) Feed(ctx context.Context, q source.Query) (source.Result, error) {
	if term, ok := strings.CutPrefix(strings.TrimSpace(q.Channel), searchPrefix); ok {
		return p.searchFeed(ctx, term, q)
	}
	group := strings.TrimSpace(q.Channel)
	if group == "" {
		return source.Result{}, ErrNoChannel
	}
	c, err := p.connect(ctx)
	if err != nil {
		return source.Result{}, mapErr(err)
	}
	defer c.Close()

	g, err := c.Group(group)
	if err != nil {
		return source.Result{}, mapErr(err)
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
		return source.Result{}, mapErr(err)
	}

	items := make([]source.Item, 0, len(overviews))
	for _, ov := range overviews {
		items = append(items, mapOverview(group, ov))
	}
	return source.Result{Items: items}, nil
}

// Groups returns the newsgroups the configured server carries (LIST ACTIVE),
// sorted by name. The first call dials the server, fetches the full active list
// and memoises it; later calls return the cached slice without touching the
// network — the list runs to tens of thousands of names, so the browse window
// fetches it once. Use RefreshGroups to bypass the cache. An authentication or
// permission failure is mapped to a typed source.AuthError so the UI can prompt
// for credentials.
func (p *Provider) Groups(ctx context.Context) ([]string, error) {
	p.gmu.Lock()
	cached := p.groups
	p.gmu.Unlock()
	if cached != nil {
		return cached, nil
	}
	return p.fetchGroups(ctx)
}

// RefreshGroups discards any cached list and re-fetches the server's full active
// group list, so the browse window's Refresh control shows newly-carried groups.
func (p *Provider) RefreshGroups(ctx context.Context) ([]string, error) {
	p.gmu.Lock()
	p.groups = nil
	p.gmu.Unlock()
	return p.fetchGroups(ctx)
}

// fetchGroups connects, issues LIST ACTIVE "*" (every carried group), closes,
// sorts the names and stores them in the cache.
func (p *Provider) fetchGroups(ctx context.Context) ([]string, error) {
	c, err := p.connect(ctx)
	if err != nil {
		return nil, mapErr(err)
	}
	defer c.Close()

	infos, err := c.List("*")
	if err != nil {
		return nil, mapErr(err)
	}
	names := make([]string, 0, len(infos))
	for _, in := range infos {
		if in.Name != "" {
			names = append(names, in.Name)
		}
	}
	sort.Strings(names)

	p.gmu.Lock()
	p.groups = names
	p.gmu.Unlock()
	return names, nil
}

// nntpAuthCodes are the NNTP response codes that mean the server needs (or
// refused) credentials: 480 authentication required, 481 authentication failed,
// 482 authentication rejected, 502 permission/no-permission.
var nntpAuthCodes = map[int]bool{480: true, 481: true, 482: true, 502: true}

// mapErr translates an authentication/permission failure into a typed
// source.AuthError so the UI can prompt the user for server or indexer
// credentials. It fires on an NNTP auth response code (a typed
// *textproto.Error), on an AUTHINFO rejection (the nntp client formats those as
// text), and on a Newznab indexer HTTP 401/403. Everything else — a dial
// timeout, a malformed response — stays a transient error.
func mapErr(err error) error {
	var te *textproto.Error
	if errors.As(err, &te) && nntpAuthCodes[te.Code] {
		return source.NeedsAuth(source.Usenet, "server/indexer credentials required")
	}
	if strings.Contains(err.Error(), "AUTHINFO") || source.ErrHasAuthStatus(err) {
		return source.NeedsAuth(source.Usenet, "server/indexer credentials required")
	}
	return err
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

// searchFeed runs a Newznab search and maps the NZB results to items whose Link
// is the .nzb download URL (feed FetchNZB to download the binary).
func (p *Provider) searchFeed(ctx context.Context, term string, q source.Query) (source.Result, error) {
	if p.search == nil {
		return source.Result{}, ErrNoSearch
	}
	res, err := p.search.Search(ctx, newznab.SearchOptions{Query: term, Limit: q.Limit})
	if err != nil {
		return source.Result{}, mapErr(err)
	}
	items := make([]source.Item, 0, len(res.Items))
	for _, it := range res.Items {
		items = append(items, mapSearchItem(it))
	}
	return source.Result{Items: items}, nil
}

func mapSearchItem(it newznab.Item) source.Item {
	item := source.Item{
		ID:        it.GUID,
		Source:    source.Usenet,
		Channel:   it.Group,
		Title:     it.Title,
		Author:    it.Poster,
		Permalink: it.NZBURL,
		Link:      it.NZBURL,
		Score:     it.Grabs,
		Comments:  -1,
		Created:   it.PublishDate.Unix(),
	}
	if it.Category != "" {
		item.Tags = []string{it.Category}
	}
	return item
}
