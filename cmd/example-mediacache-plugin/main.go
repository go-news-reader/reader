// Command example-mediacache-plugin is a minimal, self-contained reference
// media-cache plugin for the news reader: a SHARED / NETWORK cache backed by a
// plain HTTP store. It implements mediacache.Cache by mapping each media URL to a
// content-addressed key and talking to a base HTTP endpoint —
//
//	GET  <base>/<key>  -> 200 with the cached bytes, or 404 on a miss
//	PUT  <base>/<key>  <- the original bytes to store
//
// so several reader instances pointed at the same <base> share one media cache
// (a team NAS, an S3-compatible bucket behind a tiny proxy, a Redis HTTP gateway,
// …). The base endpoint comes from the CACHE_BASE_URL environment variable, and
// the key is a hash of the media URL so it is filesystem/URL-safe and collision-
// free. It serves the cache over gRPC via mediacacheplugin.Serve.
//
// It is living documentation — the smallest complete cache plugin — and is also
// built and launched by the host's end-to-end tests. Build it and point the
// reader's CacheBackend setting at the binary (with CACHE_BASE_URL set) to have
// every reader share one media cache:
//
//	CACHE_BASE_URL=https://cache.example.internal/media \
//	  go build -o example-mediacache-plugin ./cmd/example-mediacache-plugin
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-news-reader/reader/mediacache"
	"github.com/go-news-reader/reader/mediacacheplugin"
)

// httpCache is a mediacache.Cache backed by an HTTP key/value store rooted at
// base. It is the reference shared/network cache: Get/Put are single HTTP
// requests, so any number of readers pointed at the same base share the cache.
type httpCache struct {
	base   string
	client *http.Client
}

// key maps a media URL to a URL-safe, collision-free store key: the hex SHA-256
// of the URL.
func key(url string) string {
	sum := sha256.Sum256([]byte(url))
	return hex.EncodeToString(sum[:])
}

// entryURL is the store URL for a media URL: "<base>/<key>".
func (c httpCache) entryURL(mediaURL string) string {
	return strings.TrimRight(c.base, "/") + "/" + key(mediaURL)
}

// Get fetches the cached bytes for url from the HTTP store. A non-200 response,
// an empty body or any transport error is a miss (best-effort, per the Cache
// contract).
func (c httpCache) Get(url string) ([]byte, bool) {
	resp, err := c.client.Get(c.entryURL(url))
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil || len(data) == 0 {
		return nil, false
	}
	return data, true
}

// Put stores the original bytes for url in the HTTP store with a PUT. It is
// best-effort: an empty body is skipped and any error is ignored, since the
// caller already holds the bytes.
func (c httpCache) Put(url string, data []byte) {
	if len(data) == 0 {
		return
	}
	req, err := http.NewRequest(http.MethodPut, c.entryURL(url), bytes.NewReader(data))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := c.client.Do(req)
	if err != nil {
		return
	}
	_ = resp.Body.Close()
}

func main() {
	cache := httpCache{
		base:   os.Getenv("CACHE_BASE_URL"),
		client: &http.Client{Timeout: 30 * time.Second},
	}
	mediacacheplugin.Serve(cache)
}

// Ensure httpCache satisfies the Cache interface at compile time.
var _ mediacache.Cache = httpCache{}
