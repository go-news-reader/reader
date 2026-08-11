package mediacache

import (
	"bytes"
	"image"
	"image/gif"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// pngBytes returns a real PNG, so http.DetectContentType reports image/png.
func pngBytes(w, h int) []byte {
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		panic(err)
	}
	return b.Bytes()
}

// gifBytes returns a real (single-frame) GIF, so http.DetectContentType reports
// image/gif and the bytes survive a cache round-trip as a browsable .gif.
func gifBytes(w, h int) []byte {
	var b bytes.Buffer
	if err := gif.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h)), nil); err != nil {
		panic(err)
	}
	return b.Bytes()
}

// tmpCache builds a DiskCache pointed at a fresh throwaway dir with the given
// byte budget.
func tmpCache(t *testing.T, budget int64) *DiskCache {
	t.Helper()
	dir := t.TempDir()
	c := NewDiskCache(budget)
	c.Dir = func() (string, error) { return dir, nil }
	return c
}

func TestDiskCacheRoundTrip(t *testing.T) {
	c := tmpCache(t, 1<<30)
	// A stored entry reads back byte-for-byte; an absent key misses.
	if _, ok := c.Get("https://x/never"); ok {
		t.Fatal("absent key should miss")
	}
	c.Put("https://x/a.jpg", []byte("JPEGDATA"))
	got, ok := c.Get("https://x/a.jpg")
	if !ok || string(got) != "JPEGDATA" {
		t.Fatalf("get = %q,%v, want JPEGDATA,true", got, ok)
	}
	// Empty data is never stored.
	c.Put("https://x/empty", nil)
	if _, ok := c.Get("https://x/empty"); ok {
		t.Fatal("empty data must not be stored")
	}
}

func TestGlobPrefixStable(t *testing.T) {
	if GlobPrefix("https://x/a") != GlobPrefix("https://x/a") {
		t.Fatal("same URL must map to the same stem")
	}
	if GlobPrefix("https://x/a") == GlobPrefix("https://x/b") {
		t.Fatal("different URLs must map to different stems")
	}
}

func TestDiskCacheDirError(t *testing.T) {
	c := NewDiskCache(1 << 30)
	c.Dir = func() (string, error) { return "", os.ErrPermission }
	// Both sides degrade gracefully to no-ops when the dir can't be resolved.
	c.Put("https://x/a", []byte("data")) // must not panic
	if _, ok := c.Get("https://x/a"); ok {
		t.Fatal("get should miss when the cache dir is unresolvable")
	}
}

func TestDefaultDir(t *testing.T) {
	// Happy path: resolves under the OS cache dir and ends in the media subdir.
	dir, err := DefaultDir()
	if err != nil {
		t.Fatalf("DefaultDir: %v", err)
	}
	if filepath.Base(dir) != mediaSubdir {
		t.Fatalf("dir = %q, want it to end in /%s", dir, mediaSubdir)
	}
	// Error path: with no HOME (and no XDG cache), os.UserCacheDir fails.
	t.Setenv("HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	if _, err := DefaultDir(); err == nil {
		t.Fatal("want an error when no cache home is resolvable")
	}
}

func TestPutMkdirFailure(t *testing.T) {
	// Point the cache dir under a regular file so MkdirAll fails; Put is a no-op.
	blocker := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := NewDiskCache(1 << 30)
	c.Dir = func() (string, error) { return filepath.Join(blocker, "media"), nil }
	c.Put("https://x/a", []byte("data")) // must not panic
	if _, ok := c.Get("https://x/a"); ok {
		t.Fatal("get should miss after a failed put")
	}
}

func TestPruneEvictsOldest(t *testing.T) {
	dir := t.TempDir()
	// Three 100-byte entries with strictly increasing mtimes.
	names := []string{"old", "mid", "new"}
	base := time.Unix(1_700_000_000, 0)
	for i, n := range names {
		p := filepath.Join(dir, n)
		if err := os.WriteFile(p, make([]byte, 100), 0o644); err != nil {
			t.Fatal(err)
		}
		mt := base.Add(time.Duration(i) * time.Hour)
		if err := os.Chtimes(p, mt, mt); err != nil {
			t.Fatal(err)
		}
	}
	// Budget of 250 bytes: total 300 > 250, so the single oldest (100) is evicted.
	c := &DiskCache{Dir: func() (string, error) { return dir, nil }, Budget: 250}
	c.prune(dir)
	if _, err := os.Stat(filepath.Join(dir, "old")); !os.IsNotExist(err) {
		t.Fatal("oldest entry should have been evicted")
	}
	for _, n := range []string{"mid", "new"} {
		if _, err := os.Stat(filepath.Join(dir, n)); err != nil {
			t.Fatalf("%s should survive prune: %v", n, err)
		}
	}
}

func TestPruneUnderBudgetKeepsAll(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a"), make([]byte, 10), 0o644); err != nil {
		t.Fatal(err)
	}
	// A subdirectory is ignored by the size accounting.
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	c := NewDiskCache(1 << 20)
	c.prune(dir)
	if _, err := os.Stat(filepath.Join(dir, "a")); err != nil {
		t.Fatalf("under-budget prune removed an entry: %v", err)
	}
}

func TestPruneMissingDir(t *testing.T) {
	// A non-existent dir is a silent no-op (ReadDir errors).
	c := NewDiskCache(1)
	c.prune(filepath.Join(t.TempDir(), "does-not-exist"))
}

func TestPutWriteFailure(t *testing.T) {
	// A directory sitting at the exact key path makes WriteFile fail; Put no-ops.
	dir := t.TempDir()
	c := &DiskCache{Dir: func() (string, error) { return dir, nil }, Budget: 1 << 30}
	url := "https://x/collide"
	// "data" sniffs as text (not a known media type), so it is stored under .bin;
	// pre-creating a directory at that exact filename makes WriteFile fail.
	name := GlobPrefix(url) + Ext(url, []byte("data"))
	if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
		t.Fatal(err)
	}
	c.Put(url, []byte("data")) // WriteFile fails (target is a dir); no panic
	if _, ok := c.Get(url); ok {
		t.Fatal("get should miss when the write failed")
	}
}

func TestPruneSkipsVanishedEntry(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a", "b"} {
		if err := os.WriteFile(filepath.Join(dir, n), make([]byte, 200), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	c := NewDiskCache(300)
	c.stat = func(p string) (os.FileInfo, error) {
		if filepath.Base(p) == "a" {
			return nil, os.ErrNotExist // pretend "a" vanished between ReadDir and Stat
		}
		return os.Stat(p)
	}
	// "a" is skipped from accounting; only "b" (200) counts, under the 300 budget,
	// so nothing is evicted and the call completes without touching the vanished one.
	c.prune(dir)
	if _, err := os.Stat(filepath.Join(dir, "b")); err != nil {
		t.Fatalf("b should survive: %v", err)
	}
}

func TestExtForContentType(t *testing.T) {
	cases := map[string]string{
		"image/jpeg": ".jpg",
		"image/png":  ".png",
		"image/gif":  ".gif",
		"image/webp": ".webp",
		"image/bmp":  ".bmp",
		"video/mp4":  ".mp4",
		"video/webm": ".webm",
	}
	for ct, want := range cases {
		if got, ok := extForContentType(ct); !ok || got != want {
			t.Errorf("%s => %q,%v, want %q,true", ct, got, ok, want)
		}
	}
	// An unmapped type reports not-known.
	if got, ok := extForContentType("text/plain"); ok || got != "" {
		t.Errorf("text/plain => %q,%v, want \"\",false", got, ok)
	}
}

func TestKnownURLExt(t *testing.T) {
	cases := map[string]string{
		"https://x/a.jpg":  ".jpg",
		"https://x/a.jpeg": ".jpg", // normalised
		"https://x/a.png":  ".png",
		"https://x/a.gif":  ".gif",
		"https://x/a.webp": ".webp",
		"https://x/a.bmp":  ".bmp",
		"https://x/a.mp4":  ".mp4",
		"https://x/a.webm": ".webm",
		"https://x/a.txt":  "", // not a media extension
		"https://x/a":      "", // no extension
	}
	for url, want := range cases {
		if got := knownURLExt(url); got != want {
			t.Errorf("knownURLExt(%q) = %q, want %q", url, got, want)
		}
	}
}

func TestExt(t *testing.T) {
	// Content sniffing wins: real PNG bytes are stored as .png regardless of URL.
	if got := Ext("https://x/whatever.bin", pngBytes(4, 4)); got != ".png" {
		t.Errorf("png bytes => %q, want .png", got)
	}
	// Inconclusive (octet-stream) falls back to the URL's own media extension...
	octet := []byte{0x99, 0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22, 0x11, 0x00}
	if got := Ext("https://x/clip.mp4", octet); got != ".mp4" {
		t.Errorf("octet+.mp4 url => %q, want .mp4", got)
	}
	// ...or .bin when the URL extension is unknown too.
	if got := Ext("https://x/blob", octet); got != ".bin" {
		t.Errorf("octet+unknown url => %q, want .bin", got)
	}
	// A recognised-but-unmapped type (plain text) is stored as .bin.
	if got := Ext("https://x/note", []byte("hello world")); got != ".bin" {
		t.Errorf("text => %q, want .bin", got)
	}
}

func TestLabelFinderFriendly(t *testing.T) {
	// A normal image URL keeps a readable, sanitised prefix and the content ext.
	name := GlobPrefix("https://i.redd.it/abc123.jpg") + Ext("https://i.redd.it/abc123.jpg", pngBytes(2, 2))
	if !strings.HasPrefix(name, "abc123-") {
		t.Errorf("name %q should start with the readable prefix abc123-", name)
	}
	if !strings.HasSuffix(name, ".png") { // ext follows the DATA, not the URL
		t.Errorf("name %q should end in .png (content-typed)", name)
	}
	// A URL with an empty/space basename falls back to the "media" label.
	if got := label("https://x/  "); got != "media" {
		t.Errorf("empty basename label = %q, want media", got)
	}
	if got := label("http://%zz"); got != "media" { // unparseable URL
		t.Errorf("bad URL label = %q, want media", got)
	}
	// Odd characters are dropped; only [a-z0-9._-] survive.
	if got := label("https://x/A b!c$-1.png"); got != "abc-1" {
		t.Errorf("sanitised label = %q, want abc-1", got)
	}
	// An over-long basename is truncated to 48 characters.
	long := "https://x/" + strings.Repeat("a", 80) + ".png"
	if got := label(long); len(got) != 48 {
		t.Errorf("long label len = %d, want 48", len(got))
	}
}

func TestPreservesRawWithRealExtension(t *testing.T) {
	c := tmpCache(t, 1<<30)
	dir, _ := c.Dir()

	// A GIF is cached under a browsable .gif with its ORIGINAL bytes intact...
	url := "https://x/anim.gif"
	raw := gifBytes(6, 4)
	c.Put(url, raw)
	matches, _ := filepath.Glob(filepath.Join(dir, GlobPrefix(url)+".*"))
	if len(matches) != 1 {
		t.Fatalf("expected one cached file, got %v", matches)
	}
	if !strings.HasSuffix(matches[0], ".gif") {
		t.Fatalf("cached file %q should end in .gif", matches[0])
	}
	// ...and Get (which globs the unknown extension) returns them byte-for-byte.
	got, ok := c.Get(url)
	if !ok || !bytes.Equal(got, raw) {
		t.Fatalf("round trip = %v, bytes-equal=%v; want the RAW gif preserved", ok, bytes.Equal(got, raw))
	}

	// A PNG at a different URL lands under .png (extension follows content).
	purl := "https://x/pic.png"
	c.Put(purl, pngBytes(3, 3))
	pmatches, _ := filepath.Glob(filepath.Join(dir, GlobPrefix(purl)+".*"))
	if len(pmatches) != 1 || !strings.HasSuffix(pmatches[0], ".png") {
		t.Fatalf("png cached as %v, want a single .png", pmatches)
	}
}

func TestGetEmptyFileMisses(t *testing.T) {
	c := tmpCache(t, 1<<30)
	dir, _ := c.Dir()
	// A zero-length file matching the glob is treated as a miss.
	url := "https://x/empty-entry"
	if err := os.WriteFile(filepath.Join(dir, GlobPrefix(url)+".bin"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Get(url); ok {
		t.Fatal("an empty cache entry should miss")
	}
}

func TestZeroValueDiskCacheUsesOsStat(t *testing.T) {
	// A DiskCache built as a struct literal (no NewDiskCache) has a nil stat seam;
	// statFn must fall back to os.Stat so a real Put+prune works.
	dir := t.TempDir()
	c := &DiskCache{Dir: func() (string, error) { return dir, nil }, Budget: 4}
	// Two small PNGs over the tiny 4-byte budget forces a prune through statFn.
	c.Put("https://x/a.png", pngBytes(2, 2))
	c.Put("https://x/b.png", pngBytes(2, 2))
	// The directory is pruned back under budget (both far exceed 4 bytes), so at
	// most one — realistically zero — entries remain; the point is prune ran with
	// the default os.Stat and did not panic.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) > 1 {
		t.Fatalf("prune left %d entries over a 4-byte budget", len(entries))
	}
}
