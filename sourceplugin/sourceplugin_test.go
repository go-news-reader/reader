package sourceplugin

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	goplugin "github.com/hashicorp/go-plugin"

	"github.com/go-news-reader/reader/internal/sourceplugin/transport"
	"github.com/go-news-reader/reader/source"
)

// staticProvider is a trivial in-test source.Provider used by the launcher-seam
// tests (which never spawn a real process).
type staticProvider struct {
	kind source.Kind
	res  source.Result
	err  error
}

func (p staticProvider) Kind() source.Kind { return p.kind }
func (p staticProvider) Feed(context.Context, source.Query) (source.Result, error) {
	return p.res, p.err
}

// TestServe covers the SDK entry point by injecting the go-plugin server seam so
// the call does not block on a live server, and asserts the served config wraps
// the given provider.
func TestServe(t *testing.T) {
	orig := pluginServe
	defer func() { pluginServe = orig }()

	var got *goplugin.ServeConfig
	pluginServe = func(c *goplugin.ServeConfig) { got = c }

	prov := staticProvider{kind: "example"}
	Serve(prov)

	if got == nil {
		t.Fatal("pluginServe was not called")
	}
	sp, ok := got.Plugins["source"].(*transport.SourcePlugin)
	if !ok {
		t.Fatalf("served plugin is not a *transport.SourcePlugin: %T", got.Plugins["source"])
	}
	if sp.Impl.Kind() != "example" {
		t.Fatalf("served provider kind = %q, want example", sp.Impl.Kind())
	}
}

// TestLoadMissingDir: a missing plugins directory is a no-op, not an error, and
// returns a Close that is safe to call.
func TestLoadMissingDir(t *testing.T) {
	provs, closeFn, err := load(filepath.Join(t.TempDir(), "does-not-exist"), failLaunch(t))
	if err != nil {
		t.Fatalf("missing dir returned error: %v", err)
	}
	if provs != nil {
		t.Fatalf("missing dir returned providers: %v", provs)
	}
	if e := closeFn(); e != nil {
		t.Fatalf("close of no-op returned error: %v", e)
	}
}

// TestLoadNotADirectory: a path that exists but is not a directory is a hard error.
func TestLoadNotADirectory(t *testing.T) {
	f := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := load(f, failLaunch(t)); err == nil {
		t.Fatal("expected error reading a file as a directory")
	}
}

// TestLoadSkipsNonRegularAndFailedLaunch: subdirectories are skipped, a file
// whose launch fails is skipped, and a file that launches is kept. The kept
// provider's killer error is aggregated by Close.
func TestLoadSkipsNonRegularAndFailedLaunch(t *testing.T) {
	dir := t.TempDir()
	// A subdirectory (skipped: not a regular file).
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A file whose launch fails (skipped).
	mustWrite(t, filepath.Join(dir, "bad"))
	// A file that launches (kept).
	mustWrite(t, filepath.Join(dir, "good"))

	killErr := errors.New("kill failed")
	launch := func(path string) (source.Provider, func() error, error) {
		if filepath.Base(path) == "bad" {
			return nil, nil, errors.New("not a plugin")
		}
		return staticProvider{kind: "good"}, func() error { return killErr }, nil
	}

	provs, closeFn, err := load(dir, launch)
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if len(provs) != 1 || provs[0].Kind() != "good" {
		t.Fatalf("want one 'good' provider, got %v", provs)
	}
	// Close aggregates the killer's error.
	if e := closeFn(); !errors.Is(e, killErr) {
		t.Fatalf("close error = %v, want %v", e, killErr)
	}
}

// TestLoadCloseNoError: a kept provider whose killer returns nil yields a nil
// Close error (the errors.Join of zero errors).
func TestLoadCloseNoError(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "good"))
	launch := func(string) (source.Provider, func() error, error) {
		return staticProvider{kind: "good"}, func() error { return nil }, nil
	}
	provs, closeFn, err := load(dir, launch)
	if err != nil {
		t.Fatal(err)
	}
	if len(provs) != 1 {
		t.Fatalf("want 1 provider, got %d", len(provs))
	}
	if e := closeFn(); e != nil {
		t.Fatalf("close error = %v, want nil", e)
	}
}

// TestLoadEndToEnd builds the reference plugin, loads it through the real
// transport (exercising Load, transport.Launch, Serve and the gRPC round trip),
// and asserts Kind, a successful Feed, a failing Feed, that a non-plugin file is
// skipped, and that Close stops the subprocess.
func TestLoadEndToEnd(t *testing.T) {
	dir := t.TempDir()

	// Build the reference plugin into the plugins directory.
	binName := "example-source-plugin"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	binPath := filepath.Join(dir, binName)
	build := exec.Command("go", "build", "-o", binPath, "github.com/go-news-reader/reader/cmd/example-source-plugin")
	build.Env = append(os.Environ(), "GOWORK=off")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building reference plugin failed: %v\n%s", err, out)
	}

	// A non-plugin file in the same directory must be skipped gracefully.
	mustWrite(t, filepath.Join(dir, "not-a-plugin.txt"))

	provs, closeFn, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	t.Cleanup(func() {
		if e := closeFn(); e != nil {
			t.Errorf("Close: %v", e)
		}
	})
	if len(provs) != 1 {
		t.Fatalf("want exactly one loaded plugin, got %d", len(provs))
	}
	p := provs[0]
	if p.Kind() != "example" {
		t.Fatalf("plugin kind = %q, want example", p.Kind())
	}

	// A successful Feed crosses the boundary with all fields intact.
	res, err := p.Feed(context.Background(), source.Query{Channel: "r/test", Limit: 5})
	if err != nil {
		t.Fatalf("Feed: %v", err)
	}
	if len(res.Items) != 2 {
		t.Fatalf("want 2 items, got %d", len(res.Items))
	}
	if res.Cursor != "next-page-token" {
		t.Fatalf("cursor = %q, want next-page-token", res.Cursor)
	}
	it := res.Items[0]
	if it.ID != "example-1" || it.Source != "example" || it.Channel != "r/test" || it.Score != 42 {
		t.Fatalf("item[0] mismatch: %+v", it)
	}
	if len(it.Media) != 1 || it.Media[0].Kind != source.MediaImage || it.Media[0].Width != 320 {
		t.Fatalf("item[0] media mismatch: %+v", it.Media)
	}
	if len(it.Tags) != 2 || it.Tags[0] != "example" {
		t.Fatalf("item[0] tags mismatch: %+v", it.Tags)
	}

	// A provider error is reconstituted as an ordinary Go error on the host side.
	if _, err := p.Feed(context.Background(), source.Query{Channel: "error"}); err == nil {
		t.Fatal("expected an error from the 'error' channel")
	}
}

// mustWrite creates a non-executable regular file at path.
func mustWrite(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("not a plugin\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// failLaunch returns a launcher that fails the test if it is ever called (used
// by tests whose directory has no launchable candidates).
func failLaunch(t *testing.T) launcher {
	t.Helper()
	return func(path string) (source.Provider, func() error, error) {
		t.Fatalf("launch unexpectedly called for %s", path)
		return nil, nil, nil
	}
}
