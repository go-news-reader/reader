package feeds

import (
	"net/http"
	"testing"

	"github.com/go-browserhttp/browserhttp"
	"github.com/go-news-reader/reader/internal/httplog"
)

// captureRT is a fake base RoundTripper: it records the request it is handed and
// returns a canned response, so a test can inspect the headers that reach the
// wire without any network.
type captureRT struct{ got *http.Request }

func (c *captureRT) RoundTrip(req *http.Request) (*http.Response, error) {
	c.got = req
	return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: make(http.Header)}, nil
}

// TestMediaClientAlwaysUsable checks MediaClient returns a working client with
// or without a recorder — unlike loggedClient, whose nil means "keep your own
// default". A nil client here would mean no thumbnail ever downloads.
func TestMediaClientAlwaysUsable(t *testing.T) {
	plain := MediaClient(nil)
	if plain == nil || plain.Transport == nil {
		t.Fatalf("MediaClient(nil) = %+v, want a usable client", plain)
	}
	rec := httplog.NewRecorder(8)
	logged := MediaClient(rec)
	if logged == nil || logged.Transport == nil {
		t.Fatalf("MediaClient(rec) = %+v, want a usable client", logged)
	}
	// The recorder-backed client wraps the transport, so the two differ.
	if logged.Transport == plain.Transport {
		t.Fatal("the recorded client should wrap its transport in the recorder")
	}
	if plain.Timeout != logged.Timeout {
		t.Fatalf("timeouts differ: %v vs %v", plain.Timeout, logged.Timeout)
	}
}

// TestMediaClientWrapsWithBrowserUA checks MediaClient's outermost transport is
// the browser-UA wrap, on both the recorder and no-recorder paths — the layer
// that makes reddit's UA-gated media hosts serve the bytes.
func TestMediaClientWrapsWithBrowserUA(t *testing.T) {
	if _, ok := MediaClient(nil).Transport.(browserUATransport); !ok {
		t.Fatalf("MediaClient(nil) transport = %T, want browserUATransport", MediaClient(nil).Transport)
	}
	rec := httplog.NewRecorder(8)
	if _, ok := MediaClient(rec).Transport.(browserUATransport); !ok {
		t.Fatalf("MediaClient(rec) transport = %T, want browserUATransport", MediaClient(rec).Transport)
	}
}

// TestBrowserUATransportSetsUAWhenAbsent checks a request with no User-Agent
// leaves the wrap carrying the desktop-browser UA — while the ORIGINAL request
// stays untouched (the RoundTripper contract) — so a bare media download no
// longer goes out as "Go-http-client/1.1".
func TestBrowserUATransportSetsUAWhenAbsent(t *testing.T) {
	base := &captureRT{}
	req, _ := http.NewRequest(http.MethodGet, "https://preview.redd.it/x.jpg", nil)
	if _, err := (browserUATransport{base: base}).RoundTrip(req); err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	if got := base.got.Header.Get("User-Agent"); got != browserhttp.DefaultUserAgent {
		t.Fatalf("outgoing UA = %q, want the browser UA", got)
	}
	if req.Header.Get("User-Agent") != "" {
		t.Fatal("the original request must not be mutated")
	}
}

// TestBrowserUATransportKeepsExplicitUA checks a request that already sets its
// own User-Agent is left as-is: the wrap fills a gap, it does not override a
// deliberate choice.
func TestBrowserUATransportKeepsExplicitUA(t *testing.T) {
	base := &captureRT{}
	req, _ := http.NewRequest(http.MethodGet, "https://example.com/x.jpg", nil)
	req.Header.Set("User-Agent", "custom/1.0")
	if _, err := (browserUATransport{base: base}).RoundTrip(req); err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	if got := base.got.Header.Get("User-Agent"); got != "custom/1.0" {
		t.Fatalf("outgoing UA = %q, want the caller's own", got)
	}
}

// TestBrowserUATransportNilBaseUsesDefault covers the base==nil fallback: the
// wrap defers to http.DefaultTransport, which rejects an unsupported scheme
// synchronously (no network), proving the default path ran.
func TestBrowserUATransportNilBaseUsesDefault(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "unsupported://x", nil)
	if _, err := (browserUATransport{base: nil}).RoundTrip(req); err == nil {
		t.Fatal("nil base should defer to http.DefaultTransport, which errors on an unsupported scheme")
	}
}
