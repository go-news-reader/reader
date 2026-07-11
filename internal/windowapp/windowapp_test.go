package windowapp

import (
	"testing"

	"github.com/go-news-reader/reader/app"
	"github.com/go-news-reader/reader/source"
	"github.com/go-news-reader/reader/ui"
)

// newApp builds an App with an empty registry at a known size and scale.
func newApp(t *testing.T) *app.App {
	t.Helper()
	a := app.New(app.Config{Registry: source.NewRegistry(), Width: 1000, Height: 700})
	a.Scene().SetScale(1)
	return a
}

func TestFrame(t *testing.T) {
	h := New(newApp(t))
	buf, w, hgt, changed := h.Frame()
	if !changed || w != 1000 || hgt != 700 || len(buf) != w*hgt*4 {
		t.Fatalf("Frame = %d bytes %dx%d changed=%v", len(buf), w, hgt, changed)
	}
	// second call: unchanged (damage-gated)
	if _, _, _, changed := h.Frame(); changed {
		t.Fatal("second Frame reported changed with no state change")
	}
}

func TestResize(t *testing.T) {
	a := newApp(t)
	New(a).Resize(500, 400, 2.0)
	s := a.Scene()
	if s.W != 500 || s.H != 400 || s.Scale != 2.0 {
		t.Fatalf("scene = %dx%d scale=%v", s.W, s.H, s.Scale)
	}
}

func TestScroll(t *testing.T) {
	a := newApp(t)
	// Load enough items to make the feed scrollable.
	items := make([]source.Item, 40)
	for i := range items {
		items[i] = source.Item{ID: string(rune('a' + i)), Source: source.Reddit, Title: "t", Score: -1, Comments: -1}
	}
	a.Scene().SetItems(items)
	New(a).Scroll(120)
	if a.Scene().ScrollY <= 0 {
		t.Fatalf("ScrollY = %d, want > 0", a.Scene().ScrollY)
	}
}

func TestMouseDownItemOpensDetail(t *testing.T) {
	a := newApp(t)
	a.Scene().SetItems([]source.Item{{ID: "1", Source: source.Reddit, Title: "hi", Permalink: "https://ex/1", Score: -1, Comments: -1}})
	New(a).MouseDown(250, 60) // feed row 0 -> opens the in-app reading view
	s := a.Scene()
	if s.Mode() != ui.ModeDetail || s.Detail().ID != "1" {
		t.Fatalf("item click should open detail; mode=%v id=%q", s.Mode(), s.Detail().ID)
	}
}

func TestMouseDownDetailBack(t *testing.T) {
	a := newApp(t)
	s := a.Scene()
	s.OpenDetail(source.Item{ID: "1", Title: "t"})
	New(a).MouseDown(20, 24) // Back button in the detail topbar
	if s.Mode() != ui.ModeFeed {
		t.Fatal("Back should return to the feed")
	}
}

func TestMouseDownDetailOpenExternal(t *testing.T) {
	a := newApp(t)
	s := a.Scene()
	var opened string
	orig := openURL
	openURL = func(u string) error { opened = u; return nil }
	t.Cleanup(func() { openURL = orig })

	// Link is used when present.
	s.OpenDetail(source.Item{ID: "1", Title: "t", Link: "https://ex/link"})
	New(a).MouseDown(s.W-20, 24) // "Open original" button (right of the topbar)
	if opened != "https://ex/link" {
		t.Fatalf("opened = %q, want the Link", opened)
	}
	// Permalink is the fallback when Link is empty.
	s.OpenDetail(source.Item{ID: "2", Title: "t", Permalink: "https://ex/perm"})
	New(a).MouseDown(s.W-20, 24)
	if opened != "https://ex/perm" {
		t.Fatalf("opened = %q, want the Permalink fallback", opened)
	}
}

func TestMouseDownSub(t *testing.T) {
	a := newApp(t)
	a.Scene().SetSubs([]ui.Subscription{{Source: source.Reddit, Channel: "golang"}})
	New(a).MouseDown(10, 60) // "All" sidebar row
	if a.Scene().Active != ui.AllFilter {
		t.Fatalf("Active = %d, want AllFilter", a.Scene().Active)
	}
}

func TestMouseDownSearch(t *testing.T) {
	a := newApp(t)
	New(a).MouseDown(250, 24) // topbar search field
	if !a.Scene().SearchFocused() {
		t.Fatal("click on search should focus it")
	}
}

func TestMouseDownNone(t *testing.T) {
	a := newApp(t)
	a.Scene().FocusSearch(true)
	New(a).MouseDown(10, 400) // empty sidebar area -> HitNone
	if a.Scene().SearchFocused() {
		t.Fatal("click on empty area should blur search")
	}
}

func TestKey(t *testing.T) {
	a := newApp(t)
	h := New(a)
	s := a.Scene()
	s.FocusSearch(true)

	h.Key("", 'a') // printable rune
	h.Key("", 'b')
	if s.Search() != "ab" {
		t.Fatalf("search = %q", s.Search())
	}
	h.Key("Backspace", 0)
	if s.Search() != "a" {
		t.Fatalf("after backspace = %q", s.Search())
	}
	h.Key("", 0) // no-op default (no rune)
	if s.Search() != "a" {
		t.Fatalf("no-op changed search to %q", s.Search())
	}
	h.Key("Enter", 0)
	if s.SearchFocused() {
		t.Fatal("Enter should blur search")
	}
	s.FocusSearch(true)
	h.Key("Escape", 0)
	if s.SearchFocused() {
		t.Fatal("Escape should blur search")
	}
	// In the detail view, Escape returns to the feed.
	s.OpenDetail(source.Item{ID: "x", Title: "t"})
	h.Key("Escape", 0)
	if s.Mode() != ui.ModeFeed {
		t.Fatal("Escape in detail should close it")
	}
}

func TestBrowserCommand(t *testing.T) {
	cases := []struct {
		goos, wantCmd string
		wantArg0      string
	}{
		{"darwin", "open", "https://x"},
		{"windows", "rundll32", "url.dll,FileProtocolHandler"},
		{"linux", "xdg-open", "https://x"},
	}
	for _, c := range cases {
		cmd, args := browserCommand(c.goos, "https://x")
		if cmd != c.wantCmd || len(args) == 0 || args[0] != c.wantArg0 {
			t.Fatalf("%s: cmd=%q args=%v", c.goos, cmd, args)
		}
	}
}

func TestExecStart(t *testing.T) {
	// Exercise the real execStart closure. An empty command name makes
	// exec.Command("").Start() fail immediately, so no process is spawned.
	if err := execStart(""); err == nil {
		t.Fatal("execStart(\"\") should error")
	}
}

func TestDefaultOpenURL(t *testing.T) {
	var gotName string
	var gotArgs []string
	orig := execStart
	execStart = func(name string, args ...string) error { gotName, gotArgs = name, args; return nil }
	t.Cleanup(func() { execStart = orig })
	if err := defaultOpenURL("https://ex"); err != nil {
		t.Fatal(err)
	}
	if gotName == "" || len(gotArgs) == 0 {
		t.Fatalf("execStart not called: %q %v", gotName, gotArgs)
	}
}
