package redgifs

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const sampleSearch = `{
  "gifs": [
    {"id":"fatringedmosquito","createDate":1786432416,"description":"","duration":4.23,
     "hasAudio":false,"height":1920,"width":1080,"tags":["Pussy","Teen"],
     "userName":"vexalunariss",
     "urls":{"hd":"https://media.redgifs.com/FatRingedMosquito.mp4",
             "sd":"https://media.redgifs.com/FatRingedMosquito-mobile.mp4",
             "poster":"https://media.redgifs.com/FatRingedMosquito-poster.jpg",
             "thumbnail":"https://media.redgifs.com/FatRingedMosquito-mobile.jpg",
             "html":"https://www.redgifs.com/ifr/fatringedmosquito",
             "silent":"https://media.redgifs.com/FatRingedMosquito-silent.mp4"}}
  ],
  "page":1, "pages":3334, "total":10000
}`

// newTestClient builds a client aimed at an httptest server, with instant,
// deterministic timing shared between the token clock and the limiter.
func newTestClient(t *testing.T, h http.HandlerFunc) (*Client, *fakeClock) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	fc := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	c := NewClientWithHTTPClient(srv.Client())
	c.baseURL = srv.URL + "/v2"
	c.now = fc.now
	c.limiter.now, c.limiter.sleep = fc.now, fc.sleep
	return c, fc
}

func TestSearchHappyPathAndHeaders(t *testing.T) {
	var authCalls, searchCalls int32
	var sawTokenAuthz, sawTokenReferer, sawTokenUA string
	var sawSearchAuthz, sawSearchReferer, sawSearchUA, sawSearchQuery string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/auth/temporary"):
			atomic.AddInt32(&authCalls, 1)
			sawTokenAuthz = r.Header.Get("Authorization")
			sawTokenReferer = r.Header.Get("Referer")
			sawTokenUA = r.Header.Get("User-Agent")
			_, _ = w.Write([]byte(`{"token":"JWT.abc"}`))
		case strings.HasSuffix(r.URL.Path, "/gifs/search"):
			atomic.AddInt32(&searchCalls, 1)
			sawSearchAuthz = r.Header.Get("Authorization")
			sawSearchReferer = r.Header.Get("Referer")
			sawSearchUA = r.Header.Get("User-Agent")
			sawSearchQuery = r.URL.RawQuery
			_, _ = w.Write([]byte(sampleSearch))
		default:
			http.NotFound(w, r)
		}
	})

	sp, err := c.Search(context.Background(), "teen", "latest", 1, 40)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(sp.Gifs) != 1 || sp.Gifs[0].ID != "fatringedmosquito" {
		t.Fatalf("gifs = %+v", sp.Gifs)
	}
	g := sp.Gifs[0]
	if g.URLs.HD != "https://media.redgifs.com/FatRingedMosquito.mp4" || g.UserName != "vexalunariss" ||
		g.Width != 1080 || g.Height != 1920 || g.CreateDate != 1786432416 || len(g.Tags) != 2 {
		t.Fatalf("decoded gif fields wrong: %+v", g)
	}
	if sp.Page != 1 || sp.Pages != 3334 || sp.Total != 10000 {
		t.Fatalf("paging = %d/%d/%d", sp.Page, sp.Pages, sp.Total)
	}

	// Token request: fixed UA + Referer, NO Authorization.
	if sawTokenUA != userAgent {
		t.Fatalf("token UA = %q, want %q", sawTokenUA, userAgent)
	}
	if sawTokenReferer != referer {
		t.Fatalf("token Referer = %q, want %q", sawTokenReferer, referer)
	}
	if sawTokenAuthz != "" {
		t.Fatalf("token request carried Authorization %q, want none", sawTokenAuthz)
	}
	// Search request: the three mandatory headers.
	if sawSearchAuthz != "Bearer JWT.abc" {
		t.Fatalf("search Authorization = %q, want Bearer JWT.abc", sawSearchAuthz)
	}
	if sawSearchUA != userAgent {
		t.Fatalf("search UA = %q, want %q", sawSearchUA, userAgent)
	}
	if sawSearchReferer != referer {
		t.Fatalf("search Referer = %q, want %q", sawSearchReferer, referer)
	}
	if !strings.Contains(sawSearchQuery, "search_text=teen") || !strings.Contains(sawSearchQuery, "order=latest") ||
		!strings.Contains(sawSearchQuery, "count=40") || !strings.Contains(sawSearchQuery, "page=1") {
		t.Fatalf("search query = %q", sawSearchQuery)
	}
	if authCalls != 1 || searchCalls != 1 {
		t.Fatalf("calls auth=%d search=%d, want 1/1", authCalls, searchCalls)
	}
}

func TestSearchTokenCacheReuse(t *testing.T) {
	var authCalls int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/auth/temporary") {
			atomic.AddInt32(&authCalls, 1)
			_, _ = w.Write([]byte(`{"token":"JWT.abc"}`))
			return
		}
		_, _ = w.Write([]byte(sampleSearch))
	})
	for i := 0; i < 3; i++ {
		if _, err := c.Search(context.Background(), "x", "", 1, 40); err != nil {
			t.Fatalf("Search #%d: %v", i, err)
		}
	}
	if authCalls != 1 {
		t.Fatalf("auth calls = %d, want 1 (cached token reused)", authCalls)
	}
}

func TestSearchRefreshesTokenOn401(t *testing.T) {
	var authCalls, searchCalls int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/auth/temporary") {
			n := atomic.AddInt32(&authCalls, 1)
			// Serve two distinct tokens so we can assert the retry used the fresh one.
			if n == 1 {
				_, _ = w.Write([]byte(`{"token":"stale"}`))
			} else {
				_, _ = w.Write([]byte(`{"token":"fresh"}`))
			}
			return
		}
		n := atomic.AddInt32(&searchCalls, 1)
		if n == 1 {
			if got := r.Header.Get("Authorization"); got != "Bearer stale" {
				t.Errorf("first search Authorization = %q, want Bearer stale", got)
			}
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"code":"NoAuthorizationData"}}`))
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer fresh" {
			t.Errorf("retry search Authorization = %q, want Bearer fresh", got)
		}
		_, _ = w.Write([]byte(sampleSearch))
	})
	sp, err := c.Search(context.Background(), "x", "", 1, 40)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(sp.Gifs) != 1 {
		t.Fatalf("gifs = %d", len(sp.Gifs))
	}
	if authCalls != 2 || searchCalls != 2 {
		t.Fatalf("auth=%d search=%d, want 2/2", authCalls, searchCalls)
	}
}

func TestSearch401RefreshStillFails(t *testing.T) {
	// Every search 401s: after one refresh the second 401 surfaces as an error.
	var searchCalls int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/auth/temporary") {
			_, _ = w.Write([]byte(`{"token":"t"}`))
			return
		}
		atomic.AddInt32(&searchCalls, 1)
		w.WriteHeader(http.StatusUnauthorized)
	})
	_, err := c.Search(context.Background(), "x", "", 1, 40)
	var ae *APIError
	if !errors.As(err, &ae) || ae.StatusCode != http.StatusUnauthorized {
		t.Fatalf("err = %v, want 401 APIError", err)
	}
	if searchCalls != 2 {
		t.Fatalf("search calls = %d, want 2 (initial + one refresh)", searchCalls)
	}
}

func TestSearchRefreshesTokenOnExpiry(t *testing.T) {
	var authCalls int32
	c, fc := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/auth/temporary") {
			atomic.AddInt32(&authCalls, 1)
			_, _ = w.Write([]byte(`{"token":"JWT.abc"}`))
			return
		}
		_, _ = w.Write([]byte(sampleSearch))
	})
	if _, err := c.Search(context.Background(), "x", "", 1, 40); err != nil {
		t.Fatal(err)
	}
	// Jump past the token TTL: the next search must re-fetch.
	fc.t = fc.t.Add(tokenTTL + time.Hour)
	if _, err := c.Search(context.Background(), "x", "", 1, 40); err != nil {
		t.Fatal(err)
	}
	if authCalls != 2 {
		t.Fatalf("auth calls = %d, want 2 (expiry re-fetch)", authCalls)
	}
}

func TestSearchBlankQueryAndClamps(t *testing.T) {
	var sawQuery string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/auth/temporary") {
			_, _ = w.Write([]byte(`{"token":"t"}`))
			return
		}
		sawQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(sampleSearch))
	})
	// Blank query, unknown order (→ trending), page/count out of range (→ clamped).
	if _, err := c.Search(context.Background(), "", "bogus", 0, 9999); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sawQuery, "search_text=") || strings.Contains(sawQuery, "search_text=x") {
		t.Fatalf("blank query not sent as empty search_text: %q", sawQuery)
	}
	if !strings.Contains(sawQuery, "order=trending") {
		t.Fatalf("unknown order not defaulted to trending: %q", sawQuery)
	}
	if !strings.Contains(sawQuery, "page=1") {
		t.Fatalf("page<1 not clamped to 1: %q", sawQuery)
	}
	if !strings.Contains(sawQuery, "count=80") {
		t.Fatalf("count>max not clamped to 80: %q", sawQuery)
	}
	// count < 1 defaults to defaultCount.
	if _, err := c.Search(context.Background(), "x", "latest", 1, 0); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sawQuery, "count=40") {
		t.Fatalf("count<1 not defaulted to %d: %q", defaultCount, sawQuery)
	}
}

func TestSearch401ThenTokenFetchFails(t *testing.T) {
	var authCalls int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/auth/temporary") {
			if atomic.AddInt32(&authCalls, 1) == 1 {
				_, _ = w.Write([]byte(`{"token":"t"}`))
				return
			}
			// The refresh token fetch fails.
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusUnauthorized) // search always 401 → triggers refresh
	})
	_, err := c.Search(context.Background(), "x", "", 1, 40)
	var ae *APIError
	if !errors.As(err, &ae) || ae.StatusCode != http.StatusInternalServerError {
		t.Fatalf("err = %v, want the 500 from the failed token refresh", err)
	}
	if authCalls != 2 {
		t.Fatalf("auth calls = %d, want 2", authCalls)
	}
}

func TestSearchCancelledDuringRetrySleep(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/auth/temporary") {
			_, _ = w.Write([]byte(`{"token":"t"}`))
			return
		}
		w.WriteHeader(http.StatusTooManyRequests) // no Retry-After → backoff sleep
	})
	// Zero baseline spacing so only the 429 backoff sleep is positive, then have
	// that sleep report a cancelled context.
	c.limiter.minInterval = 0
	c.limiter.sleep = func(ctx context.Context, d time.Duration) error {
		if d > 0 {
			return context.Canceled
		}
		return nil
	}
	if _, err := c.Search(context.Background(), "x", "", 1, 40); err != context.Canceled {
		t.Fatalf("err = %v, want context.Canceled from the retry sleep", err)
	}
}

func TestSearchRetriesOn429(t *testing.T) {
	var searchCalls int32
	c, fc := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/auth/temporary") {
			_, _ = w.Write([]byte(`{"token":"t"}`))
			return
		}
		if atomic.AddInt32(&searchCalls, 1) <= 2 {
			w.Header().Set("Retry-After", "3")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(sampleSearch))
	})
	sp, err := c.Search(context.Background(), "x", "", 1, 40)
	if err != nil {
		t.Fatalf("Search after retries: %v", err)
	}
	if len(sp.Gifs) != 1 {
		t.Fatalf("gifs = %d", len(sp.Gifs))
	}
	if searchCalls != 3 {
		t.Fatalf("search calls = %d, want 3", searchCalls)
	}
	var saw3s bool
	for _, d := range fc.slept {
		if d == 3*time.Second {
			saw3s = true
		}
	}
	if !saw3s {
		t.Fatalf("expected a 3s Retry-After sleep among %v", fc.slept)
	}
}

func TestSearchGivesUpAfterMaxRetries(t *testing.T) {
	var searchCalls int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/auth/temporary") {
			_, _ = w.Write([]byte(`{"token":"t"}`))
			return
		}
		atomic.AddInt32(&searchCalls, 1)
		w.WriteHeader(http.StatusTooManyRequests) // no Retry-After → backoff path
	})
	_, err := c.Search(context.Background(), "x", "", 1, 40)
	var ae *APIError
	if !errors.As(err, &ae) || ae.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("err = %v, want 429 APIError", err)
	}
	// A 429 is never a 401, so no token refresh: exactly maxRetries+1 attempts.
	if searchCalls != maxRetries+1 {
		t.Fatalf("search calls = %d, want %d", searchCalls, maxRetries+1)
	}
}

func TestSearchAPIError(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/auth/temporary") {
			_, _ = w.Write([]byte(`{"token":"t"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`HttpNotFoundException`))
	})
	_, err := c.Search(context.Background(), "x", "", 1, 40)
	var ae *APIError
	if !errors.As(err, &ae) || ae.StatusCode != http.StatusNotFound {
		t.Fatalf("err = %v, want 404 APIError", err)
	}
	if ae.Error() == "" || !strings.Contains(ae.Error(), "404") {
		t.Fatalf("APIError.Error() = %q", ae.Error())
	}
}

func TestSearchDecodeError(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/auth/temporary") {
			_, _ = w.Write([]byte(`{"token":"t"}`))
			return
		}
		_, _ = w.Write([]byte(`{"gifs": [ this is not json`))
	})
	_, err := c.Search(context.Background(), "x", "", 1, 40)
	if err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("err = %v, want a decode error", err)
	}
}

func TestTokenEmptyToken(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"token":""}`))
	})
	_, err := c.Search(context.Background(), "x", "", 1, 40)
	if err == nil || !strings.Contains(err.Error(), "empty auth token") {
		t.Fatalf("err = %v, want empty auth token", err)
	}
}

func TestTokenDecodeErrorPropagates(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{bad json`))
	})
	_, err := c.Search(context.Background(), "x", "", 1, 40)
	if err == nil {
		t.Fatal("want token decode error")
	}
}

// errTransport always fails, exercising the c.hc.Do error branch of get().
type errTransport struct{}

func (errTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("boom transport")
}

func TestGetTransportError(t *testing.T) {
	c := NewClientWithHTTPClient(&http.Client{Transport: errTransport{}})
	c.baseURL = "https://example.invalid/v2"
	_, err := c.Search(context.Background(), "x", "", 1, 40)
	if err == nil || !strings.Contains(err.Error(), "boom transport") {
		t.Fatalf("err = %v, want transport error", err)
	}
}

func TestGetNewRequestError(t *testing.T) {
	c := NewClientWithHTTPClient(&http.Client{Transport: errTransport{}})
	// A control character in the URL makes http.NewRequestWithContext fail before
	// any round trip.
	c.baseURL = "http://\x7f\x00bad/v2"
	_, err := c.Search(context.Background(), "x", "", 1, 40)
	if err == nil {
		t.Fatal("want NewRequest error")
	}
}

func TestGetLimiterCancelled(t *testing.T) {
	c, fc := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"token":"t"}`))
	})
	fc.cancel = true
	_, err := c.Search(context.Background(), "x", "", 1, 40)
	if err != context.Canceled {
		t.Fatalf("err = %v, want context.Canceled from limiter wait", err)
	}
}

func TestNormalizeOrder(t *testing.T) {
	for _, o := range []string{"latest", "best", "top28", "oldest", "trending"} {
		if got := normalizeOrder(strings.ToUpper(o)); got != o {
			t.Fatalf("normalizeOrder(%q) = %q, want %q", o, got, o)
		}
	}
	if got := normalizeOrder("nonsense"); got != defaultOrder {
		t.Fatalf("normalizeOrder(nonsense) = %q, want %q", got, defaultOrder)
	}
	if got := normalizeOrder(""); got != defaultOrder {
		t.Fatalf("normalizeOrder(empty) = %q, want %q", got, defaultOrder)
	}
}

func TestConstructors(t *testing.T) {
	if c := NewClient(); c.baseURL != defaultBaseURL || c.hc == nil {
		t.Fatal("NewClient defaults")
	}
	if c := NewClientWithHTTPClient(nil); c.hc == nil {
		t.Fatal("NewClientWithHTTPClient(nil) must supply a fallback client")
	}
}

// TestGifByIDEmptyID rejects a blank id without issuing any request.
func TestGifByIDEmptyID(t *testing.T) {
	c, _ := newTestClient(t, func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("no request expected for a blank id, got %s", r.URL.Path)
	})
	if _, err := c.GifByID(context.Background(), "   "); err == nil {
		t.Fatal("blank id should error")
	}
}

// TestGifByIDHappyPath fetches a gif by id, unwrapping the "gif" envelope and
// carrying the auth headers, and preserves the id's casing in the path.
func TestGifByIDHappyPath(t *testing.T) {
	var sawPath, sawAuthz, sawReferer, sawUA string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/auth/temporary"):
			_, _ = w.Write([]byte(`{"token":"JWT.abc"}`))
		case strings.Contains(r.URL.Path, "/gifs/"):
			sawPath = r.URL.Path
			sawAuthz = r.Header.Get("Authorization")
			sawReferer = r.Header.Get("Referer")
			sawUA = r.Header.Get("User-Agent")
			_, _ = w.Write([]byte(`{"gif":{"id":"abc","width":720,"height":1280,
				"urls":{"hd":"https://media/abc-hd.mp4","poster":"https://media/abc.jpg"}}}`))
		}
	})
	g, err := c.GifByID(context.Background(), "AbC")
	if err != nil {
		t.Fatalf("GifByID: %v", err)
	}
	if g.ID != "abc" || g.URLs.HD != "https://media/abc-hd.mp4" || g.URLs.Poster != "https://media/abc.jpg" {
		t.Fatalf("gif = %+v", g)
	}
	if g.Width != 720 || g.Height != 1280 {
		t.Fatalf("dims = %dx%d", g.Width, g.Height)
	}
	if !strings.HasSuffix(sawPath, "/gifs/AbC") {
		t.Errorf("path = %q, want …/gifs/AbC", sawPath)
	}
	if sawAuthz != "Bearer JWT.abc" {
		t.Errorf("authz = %q", sawAuthz)
	}
	if sawReferer != referer || sawUA != userAgent {
		t.Errorf("headers referer=%q ua=%q", sawReferer, sawUA)
	}
}

// TestGifByID401Refetches retries once with a fresh token when the by-id call
// comes back 401.
func TestGifByID401Refetches(t *testing.T) {
	var tokenCalls, gifCalls int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/auth/temporary"):
			atomic.AddInt32(&tokenCalls, 1)
			_, _ = w.Write([]byte(`{"token":"tok"}`))
		case strings.Contains(r.URL.Path, "/gifs/"):
			if atomic.AddInt32(&gifCalls, 1) == 1 {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"gif":{"id":"x","urls":{"hd":"h"}}}`))
		}
	})
	g, err := c.GifByID(context.Background(), "x")
	if err != nil {
		t.Fatalf("GifByID: %v", err)
	}
	if g.URLs.HD != "h" {
		t.Fatalf("hd = %q", g.URLs.HD)
	}
	if n := atomic.LoadInt32(&tokenCalls); n != 2 {
		t.Errorf("token fetches = %d, want 2 (initial + refetch)", n)
	}
}

// TestGifByID401ThenTokenError surfaces a token-refetch failure that happens
// after the by-id call's 401.
func TestGifByID401ThenTokenError(t *testing.T) {
	var tokenCalls int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/auth/temporary"):
			if atomic.AddInt32(&tokenCalls, 1) == 1 {
				_, _ = w.Write([]byte(`{"token":"tok"}`))
				return
			}
			w.WriteHeader(http.StatusInternalServerError)
		case strings.Contains(r.URL.Path, "/gifs/"):
			w.WriteHeader(http.StatusUnauthorized)
		}
	})
	if _, err := c.GifByID(context.Background(), "x"); err == nil {
		t.Fatal("want the refetch error")
	}
}

// TestGifByIDTokenError surfaces an initial token-fetch failure.
func TestGifByIDTokenError(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	if _, err := c.GifByID(context.Background(), "x"); err == nil {
		t.Fatal("want the token error")
	}
}

// TestGifByIDError surfaces a non-auth API error from the by-id call.
func TestGifByIDError(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/auth/temporary") {
			_, _ = w.Write([]byte(`{"token":"tok"}`))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	})
	var apiErr *APIError
	_, err := c.GifByID(context.Background(), "x")
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusInternalServerError {
		t.Fatalf("err = %v, want APIError 500", err)
	}
}
