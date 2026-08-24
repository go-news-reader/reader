<!--
Copyright (c) the go-news-reader authors. All rights reserved.
SPDX-License-Identifier: BSD-3-Clause
-->

# Retained-mode rendering — migration plan

Status: **proposed**, phased. Phases 2–4 are not started.

## Why

reader renders in **immediate mode**: `ui.Scene.Draw` repaints the *entire*
window into a fresh framebuffer every frame. It is hosted through a
`toolkit.Surface` whose `Frame()` returns that whole buffer.

That is why, on a frame where only the loading spinner ticks, the profile still
shows the window **background re-filled** and **static chrome** (scrollbars,
dividers) **re-rasterised** — none of which changed. The toolkit already has the
fix (`toolkit/scene` — a retained widget tree that repaints only invalidated
widgets, damage levels 1–2), but reader does not use it: it hands the backend a
`Surface`, not a `scene.HostRoot`.

Migrating to the retained path is the one *clean, unified* way to stop redrawing
static chrome every frame, and it is what makes reader a faithful exemplar of
toolkit best practice for other apps to copy.

## What is already done (immediate-mode, layer-cached)

These landed first and are the reason the frame is already cheap; the migration
builds on them, it does not replace the toolkit primitives:

- **Damage-aware present** — `toolkit.Surface.Damage` + `DiffRects`
  (toolkit v0.238), reader `App.DamageRects` gated to animation-only frames
  (#231–#234). Level-3 (present) damage.
- **Per-card raster cache** — `virtual.CardList.CacheKey` (toolkit v0.242) +
  `OnVisibleRow` (v0.243); reader's feed caches card tiles and memoises text
  runs (#235, #236). Level-2 (paint) caching for the feed.

`BenchmarkFramePopulatedSpinner`: **1.92 ms → 0.21 ms/frame (~9×)**, allocations
2662 → 552.

## The measured ceiling (decide with this, not on faith)

A post-cache `pprof` of the populated-feed + spinner frame (0.21 ms) breaks down
as:

- cache-tile **blit** (`DrawImage`/`memmove`) — the win working as intended,
  **inherent**, retained-mode cannot remove it;
- **whole-window background fill** (`Backdrop.Draw`) ≈ 25.6 %;
- **scrollbar track re-raster** (`drawVScrollbar`) ≈ 13.8 %;
- everything else spread thin.

So the **entire CPU headroom retained-mode can recover is the bg + static-chrome
portion ≈ 0.08 ms/frame** — a ~40 % *relative* frame cut, but in absolute terms
~0.08 ms on an already-negligible frame (~0.3 % CPU at 15 fps → ~0.2 %).

**Conclusion: this migration is an architecture-quality / exemplarity
investment, NOT a CPU one.** The reported CPU problem is already solved. Do this
to make reader exemplary and to retire the immediate-mode tax on principle —
not expecting a user-visible speed-up.

## Interaction with existing damage

Retained widget-invalidation damage and the current buffer-**diff** damage
(#231–#234) are **mutually exclusive by construction**: the diff needs a full
frame to compare, which is exactly what retained-mode stops producing. Phase 3
therefore *replaces* the diff path with widget damage — it is not additive.

## Phases (each mergeable, each pixel-equivalence-gated)

0. **Equivalence harness.** A test that renders the whole window the old way and
   the new way and asserts byte-for-byte identity. It only has meaning once a
   second path exists, so it lands with Phase 1.
1. **Feasibility prototype, behind a flag.** Host the scene under a
   `scene.HostRoot` and prove the retained path renders pixel-identically to
   `Scene.Draw`. This requires making the scene (or a coarse chrome/feed split)
   presentable as `toolkit.Widget`s (`Draw(painter, theme)`), since `Scene.Draw`
   currently targets a raw `[]byte`. **Decision gate:** confirm the pattern is
   clean and measure a coarse chrome-vs-feed invalidation; only then commit to
   2–4.
2. **Region widgets.** Decompose the scene into `topbar / sidebar / feed /
   preview / statusbar` child widgets under one container that owns their
   layout (untangling e.g. feed-geometry-depends-on-preview-width). The riskiest
   phase: reader's regions are bespoke and interdependent.
3. **Invalidation wiring.** Replace global `touch()`/`Rev` bumps with per-region
   `Invalidate`; retire the buffer-diff damage in favour of widget damage. The
   bg fill and static chrome stop redrawing.
4. **Sub-widget granularity.** Make the spinner and the selection highlight their
   own widgets, so a spinner tick invalidates only the spinner.

## Risks

- reader's rendering is deeply bespoke (hit-testing, variable-height cards,
  cross-surface text selection, HiDPI metric scale, appearance switches). Each
  must survive the decomposition; the equivalence harness is the guard.
- Phase 3 removes a working, tested damage path (#231–#234). Do not start it
  until Phases 1–2 are proven.
- Large surface area → land it in small, independently-revertible PRs, never a
  big-bang.

## Recommendation

Execute **Phase 1 as a measured prototype in a fresh session with full budget**,
then re-decide from its data. If the prototype shows a clean pattern, proceed to
2–4 for exemplarity; if it does not, keep the current (already-excellent)
architecture — the CPU goal is met either way.
