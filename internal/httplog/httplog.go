// Package httplog records the HTTP exchanges the aggregator's providers make so
// they can be inspected in-app (the Network log tab). A Recorder is a
// concurrency-safe fixed-capacity ring buffer of Entry values; its Transport
// wraps an http.RoundTripper to capture each request's method, URL, status,
// duration and the number of body bytes the caller actually read.
//
// The package is deliberately free of any UI dependency: front-ends read
// Snapshot (newest-first) and render it however they like.
package httplog

import (
	"io"
	"net/http"
	"sync"
	"time"
)

// nowFunc is a seam so durations are testable deterministically.
var nowFunc = time.Now

// Entry is one recorded HTTP round trip. For a transport-level failure Status
// and Bytes are zero and Err holds the error text; otherwise Err is empty.
type Entry struct {
	When   time.Time
	Method string
	URL    string
	Status int
	Bytes  int64
	Dur    time.Duration
	Err    string
}

// Recorder is a mutex-guarded ring buffer of the most recent Entry values.
type Recorder struct {
	mu   sync.Mutex
	buf  []Entry
	next int // index the next Log writes to
	n    int // number of valid entries (<= cap)
	cap  int
}

// NewRecorder returns a Recorder retaining the last capacity entries (clamped to
// at least one).
func NewRecorder(capacity int) *Recorder {
	if capacity < 1 {
		capacity = 1
	}
	return &Recorder{buf: make([]Entry, capacity), cap: capacity}
}

// Log appends e, evicting the oldest entry once the buffer is full.
func (r *Recorder) Log(e Entry) {
	r.mu.Lock()
	r.buf[r.next] = e
	r.next = (r.next + 1) % r.cap
	if r.n < r.cap {
		r.n++
	}
	r.mu.Unlock()
}

// Len returns the number of retained entries.
func (r *Recorder) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.n
}

// Snapshot returns a copy of the retained entries, newest first.
func (r *Recorder) Snapshot() []Entry {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Entry, r.n)
	for i := 0; i < r.n; i++ {
		idx := (r.next - 1 - i + r.cap) % r.cap
		out[i] = r.buf[idx]
	}
	return out
}

// Transport returns an http.RoundTripper that records every round trip into r
// before delegating to base (http.DefaultTransport when base is nil).
func (r *Recorder) Transport(base http.RoundTripper) http.RoundTripper {
	return &loggingTransport{rec: r, base: base}
}

// loggingTransport records each round trip through base.
type loggingTransport struct {
	rec  *Recorder
	base http.RoundTripper
}

// RoundTrip times base's round trip. A transport error is recorded immediately;
// a success defers recording until the response body is closed so the byte count
// reflects what the caller actually read.
func (t *loggingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	start := nowFunc()
	resp, err := base.RoundTrip(req)
	if err != nil {
		t.rec.Log(Entry{
			When:   start,
			Method: req.Method,
			URL:    req.URL.String(),
			Dur:    nowFunc().Sub(start),
			Err:    err.Error(),
		})
		return nil, err
	}
	resp.Body = &countingBody{
		rc:  resp.Body,
		rec: t.rec,
		entry: Entry{
			When:   start,
			Method: req.Method,
			URL:    req.URL.String(),
			Status: resp.StatusCode,
			Dur:    nowFunc().Sub(start),
		},
	}
	return resp, nil
}

// countingBody counts the bytes read from a response body and records its entry
// exactly once, when the body is closed.
type countingBody struct {
	rc    io.ReadCloser
	rec   *Recorder
	entry Entry
	done  bool
}

func (c *countingBody) Read(p []byte) (int, error) {
	n, err := c.rc.Read(p)
	c.entry.Bytes += int64(n)
	return n, err
}

func (c *countingBody) Close() error {
	err := c.rc.Close()
	if !c.done {
		c.done = true
		c.rec.Log(c.entry)
	}
	return err
}
