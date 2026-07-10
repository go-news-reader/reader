# go-news-reader / reader

[![CI](https://github.com/go-news-reader/reader/actions/workflows/ci.yml/badge.svg)](https://github.com/go-news-reader/reader/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/go-news-reader/reader.svg)](https://pkg.go.dev/github.com/go-news-reader/reader)
[![License: BSD-3-Clause](https://img.shields.io/badge/License-BSD--3--Clause-blue.svg)](LICENSE)

A pure-Go (**CGO=0**) multi-source news & social **aggregator** with a
[go-widgets](https://github.com/go-widgets) UI. One app, one unified feed,
many sources — each behind the same small [`source.Provider`](source/source.go)
contract.

## Sources

Each platform has a standalone pure-Go client library in its own org; this repo
adds a thin `provider/<name>` adapter that maps the client's objects onto the
normalized [`source.Item`](source/source.go).

| Source | Client library | Adapter | Access |
|--------|----------------|---------|--------|
| Reddit | [`go-reddit/reddit`](https://github.com/go-reddit/reddit) | `provider/reddit` ✅ | uTLS browser fingerprint |
| RSS / Atom | `go-syndication/feed` | `provider/syndication` | open |
| Hacker News | `go-hackernews/hackernews` | `provider/hackernews` | open (Firebase API) |
| Usenet / newsgroups | `go-newsgroups/nntp` | `provider/usenet` | open (NNTP, RFC 3977) |
| Mastodon | `go-mastodon/mastodon` | `provider/mastodon` | open (REST) |
| Lemmy | `go-lemmy/lemmy` | `provider/lemmy` | open (REST) |
| Bluesky | `go-atproto/atproto` | `provider/bluesky` | open (AT Protocol) |
| Twitter / X | `go-birdsite/twitter` | `provider/twitter` | session cookie (fragile) |
| Instagram | `go-instagram/instagram` | `provider/instagram` | session cookie (fragile) |
| TikTok | `go-douyin/tiktok` | `provider/tiktok` | session cookie (fragile) |

## Design

```
                          ┌─────────────────────────┐
   provider/reddit  ─────▶│                         │
   provider/rss     ─────▶│   source.Registry       │──▶ Aggregate() ──▶ unified
   provider/mastodon─────▶│   (one Provider / Kind) │      newest-first    feed
   provider/…       ─────▶│                         │
                          └─────────────────────────┘
```

- [`source`](source/) — the `Provider`/`Item`/`Query`/`Result` contract and a
  concurrent `Registry.Aggregate` that merges many subscriptions newest-first,
  tolerating per-source failures.
- [`browserhttp`](browserhttp/) — a shared uTLS Chrome-fingerprint `http.Client`
  for platforms that 403 non-browser clients, pure Go, no host web view.

All packages carry **100% test coverage** and build for the fleet's nine
64-bit targets (see [CI](.github/workflows/ci.yml)).

## License

BSD-3-Clause © the go-news-reader/reader authors.
