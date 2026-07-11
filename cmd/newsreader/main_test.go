package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/go-news-reader/reader/app"
	"github.com/go-news-reader/reader/source"
)

type fakeProv struct {
	items []source.Item
	err   error
}

func (f fakeProv) Kind() source.Kind { return source.Reddit }
func (f fakeProv) Feed(context.Context, source.Query) (source.Result, error) {
	return source.Result{Items: f.items}, f.err
}

// stubApp swaps buildApp for an app backed by a fake provider (no network).
func stubApp(t *testing.T, prov source.Provider) {
	t.Helper()
	orig := buildApp
	buildApp = func(c config) *app.App {
		reg := source.NewRegistry()
		reg.Register(prov)
		return app.New(app.Config{Registry: reg, Subscriptions: c.subs, Width: c.w, Height: c.h, OS: c.osName, Dark: c.dark})
	}
	t.Cleanup(func() { buildApp = orig })
}

func TestRunPNGStdout(t *testing.T) {
	stubApp(t, fakeProv{items: []source.Item{{ID: "a", Source: source.Reddit, Title: "hi", Score: -1, Comments: -1}}})
	var out, errb bytes.Buffer
	if code := run([]string{"-sub", "reddit:golang", "-w", "360", "-h", "240"}, &out, &errb); code != 0 {
		t.Fatalf("code=%d err=%s", code, errb.String())
	}
	if !bytes.HasPrefix(out.Bytes(), []byte("\x89PNG")) {
		t.Fatal("stdout is not a PNG")
	}
}

func TestRunPNGFile(t *testing.T) {
	stubApp(t, fakeProv{})
	var got struct {
		name string
		n    int
	}
	orig := writeFile
	writeFile = func(name string, data []byte, perm os.FileMode) error {
		got.name, got.n = name, len(data)
		return nil
	}
	t.Cleanup(func() { writeFile = orig })

	var out, errb bytes.Buffer
	if code := run([]string{"-o", "/tmp/x.png"}, &out, &errb); code != 0 {
		t.Fatalf("code=%d", code)
	}
	if got.name != "/tmp/x.png" || got.n == 0 {
		t.Fatalf("writeFile got %+v", got)
	}
}

func TestRunPNGFileError(t *testing.T) {
	stubApp(t, fakeProv{})
	orig := writeFile
	writeFile = func(string, []byte, os.FileMode) error { return errors.New("disk full") }
	t.Cleanup(func() { writeFile = orig })
	var out, errb bytes.Buffer
	if code := run([]string{"-o", "/tmp/x.png"}, &out, &errb); code != 1 {
		t.Fatalf("code=%d", code)
	}
}

func TestRunPNGRenderError(t *testing.T) {
	stubApp(t, fakeProv{})
	orig := renderPNG
	renderPNG = func(*app.App) ([]byte, error) { return nil, errors.New("render") }
	t.Cleanup(func() { renderPNG = orig })
	var out, errb bytes.Buffer
	if code := run(nil, &out, &errb); code != 1 {
		t.Fatalf("code=%d", code)
	}
}

func TestRunPNGStdoutWriteError(t *testing.T) {
	stubApp(t, fakeProv{})
	var errb bytes.Buffer
	if code := run(nil, failWriter{}, &errb); code != 1 {
		t.Fatalf("code=%d", code)
	}
}

func TestRunJSON(t *testing.T) {
	stubApp(t, fakeProv{items: []source.Item{{ID: "a", Source: source.Reddit, Title: "hi"}}})
	var out, errb bytes.Buffer
	if code := run([]string{"-sub", "reddit:golang", "-json"}, &out, &errb); code != 0 {
		t.Fatalf("code=%d", code)
	}
	if !strings.Contains(out.String(), `"ID": "a"`) {
		t.Fatalf("json = %s", out.String())
	}
}

func TestRunJSONError(t *testing.T) {
	stubApp(t, fakeProv{items: []source.Item{{ID: "a"}}})
	var errb bytes.Buffer
	if code := run([]string{"-json"}, failWriter{}, &errb); code != 1 {
		t.Fatalf("code=%d", code)
	}
}

func TestRunServe(t *testing.T) {
	stubApp(t, fakeProv{})
	orig := serveFunc
	serveFunc = func(addr string, h http.Handler) error { return nil }
	t.Cleanup(func() { serveFunc = orig })
	var out, errb bytes.Buffer
	if code := run([]string{"-serve", ":0"}, &out, &errb); code != 0 {
		t.Fatalf("code=%d", code)
	}
	// error path
	serveFunc = func(string, http.Handler) error { return errors.New("bind") }
	if code := run([]string{"-serve", ":0"}, &out, &errb); code != 1 {
		t.Fatalf("serve error code=%d", code)
	}
}

func TestFeedHandler(t *testing.T) {
	reg := source.NewRegistry()
	reg.Register(fakeProv{})
	a := app.New(app.Config{Registry: reg, Width: 360, Height: 240})
	h := feedHandler(a)

	// index
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "frame.png") {
		t.Fatalf("index = %d %s", rec.Code, rec.Body.String())
	}
	// unknown path
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/nope", nil))
	if rec.Code != 404 {
		t.Fatalf("notfound = %d", rec.Code)
	}
	// frame.png
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/frame.png", nil))
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("frame = %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	// frame.png render error
	orig := renderPNG
	renderPNG = func(*app.App) ([]byte, error) { return nil, errors.New("x") }
	defer func() { renderPNG = orig }()
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/frame.png", nil))
	if rec.Code != 500 {
		t.Fatalf("frame error = %d", rec.Code)
	}
}

func TestRunFlagError(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"-nope"}, &out, &errb); code != 2 {
		t.Fatalf("code=%d", code)
	}
}

func TestRunBadSub(t *testing.T) {
	stubApp(t, fakeProv{})
	var out, errb bytes.Buffer
	if code := run([]string{"-sub", "bogus:x"}, &out, &errb); code != 2 {
		t.Fatalf("code=%d", code)
	}
}

func TestRunRefreshWarning(t *testing.T) {
	stubApp(t, fakeProv{err: errors.New("upstream 500")})
	var out, errb bytes.Buffer
	if code := run([]string{"-sub", "reddit:golang"}, &out, &errb); code != 0 {
		t.Fatalf("code=%d", code)
	}
	if !strings.Contains(errb.String(), "warning:") {
		t.Fatalf("no warning: %s", errb.String())
	}
}

func TestParseKind(t *testing.T) {
	for _, s := range []string{"reddit", "hn", "hackernews", "rss", "syndication", "atom", "feed",
		"usenet", "nntp", "mastodon", "lemmy", "bluesky", "atproto", "x", "twitter", "ig", "instagram", "tiktok"} {
		if _, ok := parseKind(s); !ok {
			t.Fatalf("kind %q not recognised", s)
		}
	}
	if _, ok := parseKind("nope"); ok {
		t.Fatal("bogus kind accepted")
	}
}

func TestParseSubs(t *testing.T) {
	subs, err := parseSubs([]string{"reddit:golang", "syndication:https://x/feed.xml", "hn:"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 3 || subs[1].Channel != "https://x/feed.xml" || subs[0].Limit != 10 {
		t.Fatalf("subs = %+v", subs)
	}
	if _, err := parseSubs([]string{"bad:x"}, 5); err == nil {
		t.Fatal("want error")
	}
}

func TestMainAndDefaultBuild(t *testing.T) {
	// defaultBuildApp builds a real registry (no network until Refresh).
	if defaultBuildApp(config{w: 400, h: 300}) == nil {
		t.Fatal("defaultBuildApp nil")
	}
	// main() drives osExit; stub it and buildApp so no real exit/network.
	stubApp(t, fakeProv{})
	origExit := osExit
	var code int
	osExit = func(c int) { code = c }
	t.Cleanup(func() { osExit = origExit })
	main() // parses the test binary's own args -> flag error -> exit 2
	_ = code
}

func TestMultiFlag(t *testing.T) {
	var m multiFlag
	m.Set("a")
	m.Set("b")
	if m.String() != "a,b" {
		t.Fatalf("String = %q", m.String())
	}
}

// failWriter fails every write.
type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("write fail") }
