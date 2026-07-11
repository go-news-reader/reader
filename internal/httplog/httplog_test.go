package httplog

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeRT is a RoundTripper returning a canned response or error.
type fakeRT struct {
	resp *http.Response
	err  error
	req  *http.Request
}

func (f *fakeRT) RoundTrip(req *http.Request) (*http.Response, error) {
	f.req = req
	return f.resp, f.err
}

// errBody is a ReadCloser whose Close reports an error, to exercise Close's
// error-return path.
type errBody struct{ r io.Reader }

func (e errBody) Read(p []byte) (int, error) { return e.r.Read(p) }
func (e errBody) Close() error               { return errors.New("close boom") }

func TestNewRecorderClamp(t *testing.T) {
	if got := NewRecorder(0).cap; got != 1 {
		t.Fatalf("cap for 0 = %d, want 1 (clamped)", got)
	}
	if got := NewRecorder(5).cap; got != 5 {
		t.Fatalf("cap for 5 = %d", got)
	}
}

func TestLogSnapshotAndWraparound(t *testing.T) {
	r := NewRecorder(3)
	if r.Len() != 0 || len(r.Snapshot()) != 0 {
		t.Fatal("fresh recorder should be empty")
	}
	// Fill and overflow: log 5 entries into a cap-3 buffer.
	for i := 1; i <= 5; i++ {
		r.Log(Entry{Method: "GET", Status: i})
	}
	if r.Len() != 3 {
		t.Fatalf("Len = %d, want 3 (clamped to cap)", r.Len())
	}
	snap := r.Snapshot()
	// Newest-first: the last three logged were status 5,4,3.
	want := []int{5, 4, 3}
	if len(snap) != 3 {
		t.Fatalf("snapshot len = %d", len(snap))
	}
	for i, w := range want {
		if snap[i].Status != w {
			t.Fatalf("snap[%d].Status = %d, want %d (newest-first order)", i, snap[i].Status, w)
		}
	}
}

func TestTransportSuccessCountsBytesOnClose(t *testing.T) {
	r := NewRecorder(4)
	// Deterministic duration via the nowFunc seam.
	orig := nowFunc
	calls := 0
	nowFunc = func() time.Time {
		calls++
		return time.Unix(0, 0).Add(time.Duration(calls) * 250 * time.Millisecond)
	}
	defer func() { nowFunc = orig }()

	body := "hello world" // 11 bytes
	rt := r.Transport(&fakeRT{resp: &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(body)),
	}})
	req, _ := http.NewRequest("GET", "https://example.com/path?q=1", nil)
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	// Nothing is recorded until the body is closed.
	if r.Len() != 0 {
		t.Fatalf("entry recorded before Close: Len = %d", r.Len())
	}
	read, _ := io.ReadAll(resp.Body)
	if string(read) != body {
		t.Fatalf("body = %q", read)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// A second Close must not double-record.
	_ = resp.Body.Close()

	if r.Len() != 1 {
		t.Fatalf("Len after close = %d, want exactly 1", r.Len())
	}
	e := r.Snapshot()[0]
	if e.Method != "GET" || e.URL != "https://example.com/path?q=1" || e.Status != 200 {
		t.Fatalf("entry basics = %+v", e)
	}
	if e.Bytes != int64(len(body)) {
		t.Fatalf("Bytes = %d, want %d", e.Bytes, len(body))
	}
	if e.Dur != 250*time.Millisecond {
		t.Fatalf("Dur = %v, want 250ms", e.Dur)
	}
}

func TestTransportCloseError(t *testing.T) {
	r := NewRecorder(2)
	rt := r.Transport(&fakeRT{resp: &http.Response{
		StatusCode: 204,
		Body:       errBody{r: strings.NewReader("x")},
	}})
	req, _ := http.NewRequest("GET", "https://e/x", nil)
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	io.ReadAll(resp.Body)
	if err := resp.Body.Close(); err == nil {
		t.Fatal("Close should propagate the underlying error")
	}
	if r.Len() != 1 {
		t.Fatalf("entry not recorded on erroring Close: Len = %d", r.Len())
	}
}

func TestTransportError(t *testing.T) {
	r := NewRecorder(2)
	rt := r.Transport(&fakeRT{err: errors.New("dial tcp: 403 fingerprint")})
	req, _ := http.NewRequest("POST", "https://blocked/api", nil)
	if _, err := rt.RoundTrip(req); err == nil {
		t.Fatal("want the transport error propagated")
	}
	if r.Len() != 1 {
		t.Fatalf("Len = %d, want 1", r.Len())
	}
	e := r.Snapshot()[0]
	if e.Method != "POST" || e.URL != "https://blocked/api" || e.Err == "" || e.Status != 0 {
		t.Fatalf("error entry = %+v", e)
	}
}

func TestTransportNilBaseUsesDefault(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "ok!")
	}))
	defer srv.Close()

	r := NewRecorder(2)
	client := &http.Client{Transport: r.Transport(nil)} // nil base -> DefaultTransport
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if r.Len() != 1 {
		t.Fatalf("Len = %d, want 1", r.Len())
	}
	if e := r.Snapshot()[0]; e.Status != 200 || e.Bytes != 3 {
		t.Fatalf("entry = %+v", e)
	}
}
