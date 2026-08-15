// Package redgifs is a pure-Go (CGO=0) client for the public RedGIFs v2 API and
// the aggregator [source.Provider] built on top of it. RedGIFs is an adult
// short-video host; the provider treats it like any other feed source, marking
// every item NSFW.
//
// # Authentication
//
// RedGIFs issues a short-lived anonymous bearer token from GET /auth/temporary.
// The token is bound to BOTH the caller's IP and the EXACT User-Agent that
// requested it, so the client uses one fixed browser User-Agent for the token
// request and every subsequent call, caches the token, and re-fetches it on
// expiry or on a 401. Beyond the bearer token every API request must also carry
// a "Referer: https://www.redgifs.com/" header: without it the search endpoint
// returns an empty "gifs" array even though the result counts are non-zero.
package redgifs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-browserhttp/browserhttp"
)

const (
	// defaultBaseURL is the RedGIFs v2 API root; overridable in tests.
	defaultBaseURL = "https://api.redgifs.com/v2"

	// referer is the mandatory Referer header. Without it the search endpoint
	// returns an empty gifs array even though total/pages are non-zero.
	referer = "https://www.redgifs.com/"

	// tokenTTL is how long a fetched temporary token is trusted before a
	// pre-emptive refresh. RedGIFs tokens live ~24h; 12h keeps a safe margin.
	tokenTTL = 12 * time.Hour

	// defaultCount is the page size used when the caller asks for none, and
	// maxCount is the API's per-page ceiling.
	defaultCount = 40
	maxCount     = 80

	// defaultOrder is the ordering used when the caller supplies no recognized
	// order; "trending" is what the site's browse view uses.
	defaultOrder = "trending"
)

// userAgent is the single fixed browser User-Agent bound to every temporary
// token. It must be identical for the token request and every subsequent
// request, and match the browser TLS fingerprint the transport presents.
const userAgent = browserhttp.DefaultUserAgent

// GifURLs holds the media variant URLs of a RedGIFs gif.
type GifURLs struct {
	HD        string `json:"hd"`
	SD        string `json:"sd"`
	Poster    string `json:"poster"`
	Thumbnail string `json:"thumbnail"`
	HTML      string `json:"html"`
	Silent    string `json:"silent"`
}

// Gif is one RedGIFs short video as returned by the search endpoint.
type Gif struct {
	ID          string   `json:"id"`
	CreateDate  int64    `json:"createDate"`
	Description string   `json:"description"`
	Duration    float64  `json:"duration"`
	HasAudio    bool     `json:"hasAudio"`
	Width       int      `json:"width"`
	Height      int      `json:"height"`
	Tags        []string `json:"tags"`
	UserName    string   `json:"userName"`
	URLs        GifURLs  `json:"urls"`
}

// SearchPage is a single page of search results plus the paging counters.
type SearchPage struct {
	Gifs  []Gif `json:"gifs"`
	Page  int   `json:"page"`
	Pages int   `json:"pages"`
	Total int   `json:"total"`
}

// tokenResp is the GET /auth/temporary payload.
type tokenResp struct {
	Token string `json:"token"`
}

// gifResp is the GET /gifs/<id> payload: the single gif wrapped under "gif".
type gifResp struct {
	Gif Gif `json:"gif"`
}

// APIError is a non-2xx response from the RedGIFs API.
type APIError struct {
	StatusCode int
	Status     string
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("redgifs: HTTP %s", e.Status)
}

// Client talks to the RedGIFs v2 API. It is safe for concurrent use.
type Client struct {
	baseURL string
	hc      *http.Client
	ua      string
	limiter *limiter

	mu       sync.Mutex
	token    string
	tokenExp time.Time
	// now is a clock seam so token-expiry can be driven deterministically.
	now func() time.Time
}

// NewClient returns a client driving the portable browser-fingerprint HTTP
// client (pure Go, CGO=0, no host web view).
func NewClient() *Client {
	return NewClientWithHTTPClient(browserhttp.NewClient(30 * time.Second))
}

// NewClientWithHTTPClient returns a client driving hc (e.g. the shared,
// request-logging client the aggregator builds so the Network log can show
// RedGIFs' traffic). A nil hc falls back to the portable browser-fingerprint
// client so the constructor is safe on its own.
func NewClientWithHTTPClient(hc *http.Client) *Client {
	if hc == nil {
		hc = browserhttp.NewClient(30 * time.Second)
	}
	return &Client{
		baseURL: defaultBaseURL,
		hc:      hc,
		ua:      userAgent,
		limiter: newLimiter(defaultRequestsPerMinute),
		now:     time.Now,
	}
}

// Search returns one page of gifs matching query, ordered by order (one of
// trending|latest|best|top28|oldest; anything else defaults to trending). A
// blank query browses the ordering itself. page is 1-based; count is clamped to
// [1, maxCount] and defaults to defaultCount. It lazily fetches and caches the
// anonymous bearer token, refreshing it once if the request comes back 401.
func (c *Client) Search(ctx context.Context, query, order string, page, count int) (*SearchPage, error) {
	order = normalizeOrder(order)
	if page < 1 {
		page = 1
	}
	switch {
	case count < 1:
		count = defaultCount
	case count > maxCount:
		count = maxCount
	}
	q := url.Values{}
	q.Set("search_text", query)
	q.Set("order", order)
	q.Set("count", strconv.Itoa(count))
	q.Set("page", strconv.Itoa(page))
	raw := q.Encode()

	tok, err := c.ensureToken(ctx)
	if err != nil {
		return nil, err
	}
	sp, err := c.searchOnce(ctx, raw, tok)
	if isAuthErr(err) {
		// The cached token was rejected: force a fresh one and retry once.
		tok, ferr := c.fetchToken(ctx)
		if ferr != nil {
			return nil, ferr
		}
		sp, err = c.searchOnce(ctx, raw, tok)
	}
	if err != nil {
		return nil, err
	}
	return sp, nil
}

// GifByID fetches a single gif by its id via GET /gifs/<id>, using the same
// temporary-token + Referer auth as [Client.Search]: it lazily fetches and
// caches the anonymous bearer token and refreshes it once if the request comes
// back 401. This is the resolver other providers use to turn a bare RedGIFs
// watch/embed link (a redgifs.com/watch/<id> or /ifr/<id> URL, e.g. the ones
// pasted into Reddit posts) into the actual media variant URLs. A blank id is
// rejected without a request.
func (c *Client) GifByID(ctx context.Context, id string) (*Gif, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, errors.New("redgifs: empty gif id")
	}
	tok, err := c.ensureToken(ctx)
	if err != nil {
		return nil, err
	}
	g, err := c.gifOnce(ctx, id, tok)
	if isAuthErr(err) {
		// The cached token was rejected: force a fresh one and retry once.
		tok, ferr := c.fetchToken(ctx)
		if ferr != nil {
			return nil, ferr
		}
		g, err = c.gifOnce(ctx, id, tok)
	}
	if err != nil {
		return nil, err
	}
	return g, nil
}

// gifOnce performs a single by-id request with the given token, unwrapping the
// "gif" envelope RedGIFs wraps the object in.
func (c *Client) gifOnce(ctx context.Context, id, token string) (*Gif, error) {
	var gr gifResp
	if err := c.get(ctx, "/gifs/"+url.PathEscape(id), "", token, &gr); err != nil {
		return nil, err
	}
	return &gr.Gif, nil
}

// searchOnce performs a single search request with the given token.
func (c *Client) searchOnce(ctx context.Context, rawQuery, token string) (*SearchPage, error) {
	var sp SearchPage
	if err := c.get(ctx, "/gifs/search", rawQuery, token, &sp); err != nil {
		return nil, err
	}
	return &sp, nil
}

// ensureToken returns a cached, unexpired token, fetching a fresh one when the
// cache is empty or stale.
func (c *Client) ensureToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	if c.token != "" && c.now().Before(c.tokenExp) {
		tok := c.token
		c.mu.Unlock()
		return tok, nil
	}
	c.mu.Unlock()
	return c.fetchToken(ctx)
}

// fetchToken requests a fresh anonymous bearer token and caches it. The request
// carries the fixed User-Agent (to which the returned token is bound) and the
// Referer header, but no Authorization.
func (c *Client) fetchToken(ctx context.Context) (string, error) {
	var tr tokenResp
	if err := c.get(ctx, "/auth/temporary", "", "", &tr); err != nil {
		return "", err
	}
	if tr.Token == "" {
		return "", errors.New("redgifs: empty auth token")
	}
	c.mu.Lock()
	c.token = tr.Token
	c.tokenExp = c.now().Add(tokenTTL)
	c.mu.Unlock()
	return tr.Token, nil
}

// get performs a paced GET to path?rawQuery, sending the fixed User-Agent, the
// mandatory Referer, and — when bearer is non-empty — the Authorization header,
// retrying a 429 up to maxRetries times (honouring Retry-After, else an
// exponential backoff) and decoding a 2xx JSON body into out.
func (c *Client) get(ctx context.Context, path, rawQuery, bearer string, out any) error {
	u := c.baseURL + path
	if rawQuery != "" {
		u += "?" + rawQuery
	}
	// A fresh exponential schedule per request drives the 429 fallback when the
	// server sends no usable Retry-After.
	exp := newExpBackoff()
	for attempt := 0; ; attempt++ {
		if err := c.limiter.wait(ctx); err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", c.ua)
		req.Header.Set("Referer", referer)
		req.Header.Set("Accept", "application/json")
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}

		resp, err := c.hc.Do(req)
		if err != nil {
			return err
		}

		if resp.StatusCode == http.StatusTooManyRequests && attempt < maxRetries {
			d := c.limiter.nextDelay(exp, resp.Header)
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			c.limiter.pauseFor(d)
			if err := c.limiter.sleep(ctx, d); err != nil {
				return err
			}
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
			return &APIError{StatusCode: resp.StatusCode, Status: resp.Status, Body: string(body)}
		}
		err = json.NewDecoder(resp.Body).Decode(out)
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("redgifs: decode %s: %w", path, err)
		}
		return nil
	}
}

// isAuthErr reports whether err is an [APIError] carrying HTTP 401, so a search
// can retry once with a freshly fetched token.
func isAuthErr(err error) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae.StatusCode == http.StatusUnauthorized
}

// normalizeOrder maps an ordering hint onto RedGIFs' vocabulary, defaulting to
// [defaultOrder] for empty or unrecognized values.
func normalizeOrder(o string) string {
	switch strings.ToLower(strings.TrimSpace(o)) {
	case "latest", "best", "top28", "oldest", "trending":
		return strings.ToLower(strings.TrimSpace(o))
	default:
		return defaultOrder
	}
}
