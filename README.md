<p align="center"><img src="https://raw.githubusercontent.com/go-news-reader/brand/main/social/go-news-reader.png" alt="go-news-reader/reader" width="720"></p>

# go-news-reader / reader

[![CI](https://github.com/go-news-reader/reader/actions/workflows/ci.yml/badge.svg)](https://github.com/go-news-reader/reader/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/go-news-reader/reader.svg)](https://pkg.go.dev/github.com/go-news-reader/reader)
[![License: BSD-3-Clause](https://img.shields.io/badge/License-BSD--3--Clause-blue.svg)](LICENSE)

A pure-Go (**CGO=0**) multi-source news & social **aggregator** with a
[go-widgets](https://github.com/go-widgets) UI. One app, one unified feed,
many sources — each behind the same small [`source.Provider`](source/source.go)
contract.

## Install & run

```sh
go install github.com/go-news-reader/reader/cmd/newsreader@latest

newsreader -sub reddit:golang -sub hackernews: -o feed.png   # render the feed
newsreader -sub reddit:golang -json                          # dump merged feed
newsreader -sub reddit:golang -window                       # native window (macOS/Windows/Linux/Android)
newsreader -sub reddit:golang -serve :8080                   # read-only PNG view over HTTP
```

Subscriptions are `kind:channel` (repeatable): `reddit:golang`,
`hackernews:`, `syndication:https://blog/feed.xml`, `mastodon:#golang`,
`bluesky:@user.bsky.social`, `lemmy:technology`, `usenet:comp.lang.go`,
`usenet:search:ubuntu` (with `-indexer`), … Provider endpoints/credentials are
set with `-mastodon`, `-lemmy`, `-usenet`, `-indexer`.

## Sources

Each platform has a standalone pure-Go client library in its own org; this repo
adds a thin `provider/<name>` adapter that maps the client's objects onto the
normalized [`source.Item`](source/source.go).

| Source | Client library | Adapter | Access |
|--------|----------------|---------|--------|
| Reddit | [`go-reddit/reddit`](https://github.com/go-reddit/reddit) | `provider/reddit` | uTLS browser fingerprint |
| RSS / Atom / JSONFeed | [`go-syndication/feed`](https://github.com/go-syndication/feed) | `provider/syndication` | open |
| Hacker News | [`go-hackernews/hackernews`](https://github.com/go-hackernews/hackernews) | `provider/hackernews` | open |
| Usenet / newsgroups | [`go-newsgroups/nntp`](https://github.com/go-newsgroups/nntp) | `provider/usenet` | legacy NNRP or modern TLS+AUTHINFO + Newznab search |
| Mastodon | [`go-mastodon/mastodon`](https://github.com/go-mastodon/mastodon) | `provider/mastodon` | open |
| Lemmy | [`go-lemmy/lemmy`](https://github.com/go-lemmy/lemmy) | `provider/lemmy` | open |
| Bluesky | [`go-atproto/atproto`](https://github.com/go-atproto/atproto) | `provider/bluesky` | open (AT Protocol) |
| Twitter / X | [`go-birdsite/twitter`](https://github.com/go-birdsite/twitter) | `provider/twitter` | best-effort (session) |
| Instagram | [`go-instagram/instagram`](https://github.com/go-instagram/instagram) | `provider/instagram` | best-effort (session) |
| TikTok | [`go-tiktok/tiktok`](https://github.com/go-tiktok/tiktok) | `provider/tiktok` | best-effort (session) |

## NewsBin-style Usenet

The Usenet provider composes a full binary stack: **Newznab search**
(`go-newsgroups/newznab`, direct indexer or NZBHydra2) → **NZB download** over
NNTP with yEnc reassembly (`go-newsgroups/nzb` + `go-newsgroups/yenc`) →
**AutoPAR** verify/repair (`go-newsgroups/par2` over the `go-erasure/reedsolomon`
GF(2¹⁶) field) → **thumbnails** (`go-images`).

### Legacy vs modern servers, and authentication

Built on `go-newsgroups/nntp` **v0.2.0**, the provider transparently bridges two
kinds of server (the library negotiates `CAPABILITIES` lazily; the reader just
benefits from the OVER→XOVER fallback):

- **Legacy** NNRP servers that predate RFC 3977, reject `CAPABILITIES`, and
  carry binaries anonymously. Example: **Free** (`news.free.fr:119`, plain, no
  auth, `alt.binaries.*`). `Legacy()` reports `true`.
- **Modern** servers (INN and the like) that advertise `OVER`/`READER`/
  `COMPRESS` and gate reading behind `MODE READER` + `AUTHINFO`. Text example:
  **Eternal-September** (`news.eternal-september.org:563`, TLS, free account).
  Binary example: **XSUsenet** free tier (TLS + AUTHINFO, carries binaries).

Configure a Usenet account in the **Accounts** window with `Server host:port`,
the `Implicit TLS` toggle, and `Username` / `Password` (masked). When a username
is set, the provider issues `MODE READER` (safe everywhere) and then
`AUTHINFO USER/PASS` before selecting a group; an `AUTHINFO` rejection surfaces
as an in-feed "sign-in required" prompt. Leaving username empty connects
anonymously (the legacy Free path). The optional `Newznab indexer URL` / `API
key` enable `usenet:search:<query>` feeds.

The `news.free.fr` example is ISP-specific and is **not** a shipped default —
it is documented here and used as the Taskfile default only.

### Network integration tests (manual, tag-gated)

Real-server tests live behind the `nntpnet` build tag in
[`provider/usenet/integration_nntpnet_test.go`](provider/usenet/integration_nntpnet_test.go).
They are excluded from the default build and the 100% coverage gate and never
run in CI; run them on demand:

```sh
task usenet-free     # legacy binary  (news.free.fr:119, no auth) — run from a Free network
task usenet-modern   # modern text    — NNTP_MODERN_USER / NNTP_MODERN_PASS
task usenet-binary   # modern binary  — NNTP_BIN_ADDR / NNTP_BIN_USER / NNTP_BIN_PASS (+NNTP_BIN_TLS)
```

| Target | Env vars | What it proves |
|--------|----------|----------------|
| Legacy binary (Free) | `NNTP_LEGACY_ADDR`, `NNTP_LEGACY_GROUP` | anonymous connect, OVER scan, fetch + yEnc-decode (CRC verified) a single-part `.par2`, assert PAR2 magic |
| Modern text (Eternal-September) | `NNTP_MODERN_USER`, `NNTP_MODERN_PASS`, `NNTP_MODERN_ADDR`, `NNTP_MODERN_GROUP` | TLS + AUTHINFO, `Legacy()==false`, `READER` capability, group select + article fetch |
| Modern binary (XSUsenet / any) | `NNTP_BIN_ADDR`, `NNTP_BIN_USER`, `NNTP_BIN_PASS`, `NNTP_BIN_TLS`, `NNTP_BIN_GROUP` | TLS + AUTHINFO, multipart download + reassembly, real-PAR2 verify, then corrupt-and-repair proof |

Each target `t.Skip`s cleanly when its required env vars are unset.

## Architecture

```
 provider/*  ──▶  source.Registry ──▶ Aggregate() ──▶ app.App ──▶ ui.Scene ──▶ framebuffer
 (10 sources)     (one Provider/Kind)   newest-first    (double-buffered,      (go-widgets,
                                                          damage-gated)          cached sprites)
```

- [`source`](source/) — the `Provider`/`Item`/`Query`/`Result` contract + a
  concurrent `Registry.Aggregate` merging sources newest-first.
- [`ui`](ui/) — the go-widgets scene: unified feed, per-source sidebar,
  search, thumbnails. Cards and chrome render into cached sprites so scrolling
  is a memcpy blit (no per-frame glyph rasterisation).
- [`app`](app/) — wires a registry + subscriptions into the scene;
  double-buffered `Frame()` presents only when the damage sequence advances.

Every package carries **100% test coverage** and builds for the fleet's nine
64-bit targets.

## License

BSD-3-Clause © the go-news-reader/reader authors.
