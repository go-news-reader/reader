// Command newsreader is the go-news-reader aggregator CLI. It aggregates a set
// of source subscriptions and either renders the unified feed to a PNG, prints
// it as JSON, or serves a live-rendered view over HTTP.
//
//	newsreader -sub reddit:golang -sub hackernews: -o feed.png
//	newsreader -sub reddit:golang -json
//	newsreader -sub reddit:golang -serve :8080
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"

	"github.com/go-news-reader/reader/app"
	"github.com/go-news-reader/reader/feeds"
	"github.com/go-news-reader/reader/internal/httplog"
	"github.com/go-news-reader/reader/internal/settings"
	"github.com/go-news-reader/reader/internal/window"
	"github.com/go-news-reader/reader/internal/windowapp"
	"github.com/go-news-reader/reader/source"
	"github.com/go-news-reader/reader/ui"
)

// osExit is a seam so main() is testable.
var osExit = os.Exit

func main() { osExit(run(os.Args[1:], os.Stdout, os.Stderr)) }

// config is the parsed command line plus the resolved persistence state (only
// the -window path populates set/store).
type config struct {
	subs   []source.Subscription
	opts   feeds.Options
	w, h   int
	osName string
	dark   bool
	limit  int
	out    string
	asJSON bool
	serve  string
	window bool

	set   *settings.Settings // window mode: the loaded/seeded settings
	store *settings.Store    // window mode: the persistence backend
}

// Seams so tests avoid the network, real servers, and the real home directory.
var (
	buildApp      = defaultBuildApp
	writeFile     = os.WriteFile
	serveFunc     = http.ListenAndServe
	renderPNG     = (*app.App).RenderPNG
	openWindow    = window.Run
	settingsStore = defaultSettingsStore
)

// defaultSettingsStore returns a Store at the per-user settings path.
func defaultSettingsStore() (*settings.Store, error) {
	p, err := settings.DefaultPath()
	if err != nil {
		return nil, err
	}
	return settings.NewStore(p), nil
}

// defaultOSToken maps the actual runtime OS to a ui look-and-feel token so the
// app wears its native palette (WhiteSur / Fluent / Adwaita) on each system.
func defaultOSToken() string { return osToken(runtime.GOOS) }

func osToken(goos string) string {
	switch goos {
	case "windows":
		return ui.OSWindows
	case "darwin":
		return ui.OSMac
	default:
		return ui.OSLinux
	}
}

func defaultBuildApp(c config) *app.App {
	// One recorder captures every provider's HTTP traffic; the same instance
	// feeds the scene's Network-log view.
	rec := httplog.NewRecorder(200)
	// The initial registry already honours any persisted per-provider credentials
	// (so stored Reddit OAuth is live from launch); the accounts editor rebuilds
	// it live from the same base options afterwards.
	initOpts := c.opts
	if c.set != nil {
		initOpts = app.AccountsToOptions(c.opts, c.set.Accounts)
	}
	initOpts.Recorder = rec
	return app.New(app.Config{
		Registry:      feeds.Registry(initOpts),
		Recorder:      rec,
		Options:       c.opts,  // flag-derived base for live account rebuilds
		Settings:      c.set,   // nil except in -window mode
		Store:         c.store, // nil except in -window mode
		Subscriptions: c.subs,  // used when Settings is nil
		Width:         c.w,
		Height:        c.h,
		OS:            c.osName,
		Dark:          c.dark,
	})
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("newsreader", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var subSpecs multiFlag
	fs.Var(&subSpecs, "sub", "subscription as kind:channel (repeatable), e.g. reddit:golang")
	w := fs.Int("w", 1000, "window width")
	h := fs.Int("h", 700, "window height")
	osName := fs.String("os", defaultOSToken(), "look & feel: mac|linux|windows (default: this system)")
	dark := fs.Bool("dark", false, "dark theme")
	limit := fs.Int("limit", 25, "items per subscription")
	out := fs.String("o", "", "write PNG to this file (\"\" or \"-\" = stdout)")
	asJSON := fs.Bool("json", false, "print the merged feed as JSON instead of an image")
	serve := fs.String("serve", "", "serve a live read-only PNG view at this address, e.g. :8080")
	windowMode := fs.Bool("window", false, "open a native window that blits the UI directly (macOS/Windows/Linux)")
	mastodon := fs.String("mastodon", "", "Mastodon instance URL (enables mastodon)")
	lemmy := fs.String("lemmy", "", "Lemmy instance URL (enables lemmy)")
	usenet := fs.String("usenet", "", "NNTP server host:port (enables usenet)")
	indexer := fs.String("indexer", "", "Newznab indexer URL (enables usenet search)")
	indexerKey := fs.String("indexer-key", "", "Newznab API key")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cfg := config{
		w: *w, h: *h, osName: *osName, dark: *dark, limit: *limit, out: *out, asJSON: *asJSON, serve: *serve, window: *windowMode,
		opts: feeds.Options{
			MastodonInstance:    *mastodon,
			LemmyInstance:       *lemmy,
			UsenetAddr:          *usenet,
			UsenetIndexerURL:    *indexer,
			UsenetIndexerAPIKey: *indexerKey,
		},
	}
	subs, err := parseSubs(subSpecs, *limit)
	if err != nil {
		fmt.Fprintln(stderr, "newsreader:", err)
		return 2
	}
	cfg.subs = subs

	if cfg.window {
		return runWindow(cfg, stdout, stderr)
	}

	a := buildApp(cfg)

	for _, e := range a.Refresh(context.Background()) {
		fmt.Fprintln(stderr, "warning:", e)
	}

	switch {
	case cfg.asJSON:
		return emitJSON(a, stdout, stderr)
	case cfg.serve != "":
		return emitServe(cfg.serve, feedHandler(a), stdout, stderr)
	default:
		return emitPNG(a, cfg.out, stdout, stderr)
	}
}

func emitJSON(a *app.App, stdout, stderr io.Writer) int {
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(a.Items()); err != nil {
		fmt.Fprintln(stderr, "newsreader:", err)
		return 1
	}
	return 0
}

func emitPNG(a *app.App, out string, stdout, stderr io.Writer) int {
	data, err := renderPNG(a)
	if err != nil {
		fmt.Fprintln(stderr, "newsreader:", err)
		return 1
	}
	if out == "" || out == "-" {
		if _, err := stdout.Write(data); err != nil {
			fmt.Fprintln(stderr, "newsreader:", err)
			return 1
		}
		return 0
	}
	if err := writeFile(out, data, 0o644); err != nil {
		fmt.Fprintln(stderr, "newsreader:", err)
		return 1
	}
	fmt.Fprintf(stderr, "wrote %s (%d bytes)\n", out, len(data))
	return 0
}

// runWindow loads the persisted settings (seeding the default profile from any
// -sub flags on first run), builds the app around them, and opens the native
// window. Profile switches and settings-view edits persist back through the store.
func runWindow(cfg config, stdout, stderr io.Writer) int {
	st, err := settingsStore()
	if err != nil {
		fmt.Fprintln(stderr, "newsreader:", err)
		return 1
	}
	_, statErr := os.Stat(st.Path)
	firstRun := os.IsNotExist(statErr)
	set, err := st.Load()
	if err != nil {
		fmt.Fprintln(stderr, "newsreader:", err)
		return 1
	}
	// On a fresh install, -sub flags seed/replace the default profile's subs.
	if firstRun && len(cfg.subs) > 0 {
		set.Profiles[set.Active].Subs = cfg.subs
	}
	cfg.set, cfg.store = set, st
	return emitWindow(buildApp(cfg), cfg, stdout, stderr)
}

// emitWindow opens a native window that blits the UI directly, refreshing the
// feed concurrently so the window appears immediately and fills in once loaded.
// Off macOS (or if the window can't open) it falls back to a printed notice.
func emitWindow(a *app.App, cfg config, stdout, stderr io.Writer) int {
	go refreshFeed(a, stderr)
	runtime.LockOSThread()
	err := openWindow(window.Config{
		Title:  "News Reader",
		Width:  float64(cfg.w),
		Height: float64(cfg.h),
	}, windowapp.New(a))
	if err != nil {
		fmt.Fprintln(stderr, "newsreader:", err)
		fmt.Fprintln(stdout, "native window unavailable; use -serve or -o to view the feed")
		return 1
	}
	return 0
}

// refreshFeed aggregates the subscriptions into a and reports per-source
// failures. Run in a goroutine by emitWindow so the window appears immediately.
func refreshFeed(a *app.App, stderr io.Writer) {
	for _, e := range a.Refresh(context.Background()) {
		fmt.Fprintln(stderr, "warning:", e)
	}
}

func emitServe(addr string, h http.Handler, stdout, stderr io.Writer) int {
	fmt.Fprintf(stdout, "serving %s — open http://localhost%s/\n", addr, addr)
	if err := serveFunc(addr, h); err != nil {
		fmt.Fprintln(stderr, "newsreader:", err)
		return 1
	}
	return 0
}

// feedHandler serves an HTML page that shows the rendered feed and reloads it.
func feedHandler(a *app.App) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/frame.png", func(w http.ResponseWriter, r *http.Request) {
		data, err := renderPNG(a)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write(data)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, indexHTML)
	})
	return mux
}

const indexHTML = `<!doctype html><meta charset="utf-8"><title>News</title>
<style>body{margin:0;background:#111}img{display:block;width:100%;height:auto}</style>
<img id="f" src="/frame.png">
<script>setInterval(function(){document.getElementById('f').src='/frame.png?'+Date.now()},3000)</script>`

// parseSubs turns "kind:channel" specs into subscriptions with the given limit.
func parseSubs(specs []string, limit int) ([]source.Subscription, error) {
	out := make([]source.Subscription, 0, len(specs))
	for _, spec := range specs {
		kindStr, channel, _ := strings.Cut(spec, ":")
		kind, ok := parseKind(kindStr)
		if !ok {
			return nil, fmt.Errorf("unknown source kind %q in %q", kindStr, spec)
		}
		out = append(out, source.Subscription{Source: kind, Channel: channel, Limit: limit})
	}
	return out, nil
}

func parseKind(s string) (source.Kind, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "reddit":
		return source.Reddit, true
	case "hackernews", "hn":
		return source.HackerNews, true
	case "syndication", "rss", "atom", "feed":
		return source.Syndication, true
	case "usenet", "nntp":
		return source.Usenet, true
	case "mastodon":
		return source.Mastodon, true
	case "lemmy":
		return source.Lemmy, true
	case "bluesky", "atproto":
		return source.Bluesky, true
	case "twitter", "x":
		return source.Twitter, true
	case "instagram", "ig":
		return source.Instagram, true
	case "tiktok":
		return source.TikTok, true
	default:
		return "", false
	}
}

// multiFlag collects repeated string flags.
type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }
