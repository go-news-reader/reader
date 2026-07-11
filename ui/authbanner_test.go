package ui

import (
	"testing"

	"github.com/go-news-reader/reader/source"
)

func threePrompts() []AuthPrompt {
	return []AuthPrompt{
		{Kind: source.Reddit, Reason: "sign in with a Reddit app (oauth)"},
		{Kind: source.Mastodon, Reason: "access token required/invalid"},
		{Kind: source.Instagram, Reason: "session/token required"},
	}
}

func TestSetAuthPromptsAccessor(t *testing.T) {
	s := newScene()
	if len(s.AuthPrompts()) != 0 {
		t.Fatal("prompts should start empty")
	}
	s.SetAuthPrompts(threePrompts())
	if got := s.AuthPrompts(); len(got) != 3 || got[0].Kind != source.Reddit {
		t.Fatalf("AuthPrompts = %+v", got)
	}
}

func TestAuthBannerRenderAndHit(t *testing.T) {
	s := newScene()
	s.SetAuthPrompts(threePrompts())
	buf := renderPNG(t, s, "auth-prompt")

	s.layout()
	m := s.m
	feedX := m.sidebarW + m.pad
	feedTop := m.topbarH

	// Pixel fact: the first banner pill is painted in the theme accent.
	bx := feedX + 4
	by := feedTop + m.pad + m.bannerH/2
	if got := px(buf, s.W, bx, by); got.R != s.theme.Accent.R || got.G != s.theme.Accent.G || got.B != s.theme.Accent.B {
		t.Fatalf("banner pixel = %v, want accent %v", got, s.theme.Accent)
	}

	// Clicking banner row 0 returns HitFixAuth for Reddit.
	if h := s.HitTest(bx, by); h.Kind != HitFixAuth || h.Value != string(source.Reddit) {
		t.Fatalf("banner-0 hit = %+v", h)
	}
	// Clicking banner row 1 returns HitFixAuth for Mastodon.
	by1 := feedTop + m.pad + m.bannerH + m.cardGap + m.bannerH/2
	if h := s.HitTest(bx, by1); h.Kind != HitFixAuth || h.Value != string(source.Mastodon) {
		t.Fatalf("banner-1 hit = %+v", h)
	}
}

func TestAuthBannerEmptyLayoutUnchanged(t *testing.T) {
	s := newScene()
	s.layout()
	base := s.rows[0].top
	// No prompts -> the first card sits at the same top offset as before.
	s.SetAuthPrompts(nil)
	s.layout()
	if s.rows[0].top != base {
		t.Fatalf("empty prompts shifted the feed: %d != %d", s.rows[0].top, base)
	}
	if len(s.authRows) != 0 {
		t.Fatalf("authRows = %d, want 0", len(s.authRows))
	}
}

func TestAuthBannerScrollClipping(t *testing.T) {
	// A prompt per source kind in a short window so some banners fall below the
	// viewport (the y >= s.H clip), then scroll far so the early banners pass
	// above the feed top (the y+bannerH < feedTop clip). Both draw-clip branches
	// and the drawAuthBanner path are exercised.
	kinds := []source.Kind{
		source.Reddit, source.HackerNews, source.Syndication, source.Usenet,
		source.Mastodon, source.Lemmy, source.Bluesky, source.Twitter,
		source.Instagram, source.TikTok,
	}
	prompts := make([]AuthPrompt, len(kinds))
	for i, k := range kinds {
		prompts[i] = AuthPrompt{Kind: k, Reason: "x"}
	}
	s := New(500, 260, ThemeFor(OSLinux, false))
	s.SetItems(sampleItems())
	s.SetAuthPrompts(prompts)
	renderPNG(t, s, "auth-prompt-many") // some banners below s.H -> clipped

	s.Scroll(100000) // clamp to bottom; early banners scroll above the feed top
	renderPNG(t, s, "auth-prompt-scrolled")
}

func TestSelectedAccountAccessor(t *testing.T) {
	s := newScene()
	s.SelectAccount(source.Bluesky)
	if s.SelectedAccount() != source.Bluesky {
		t.Fatalf("SelectedAccount = %q, want bluesky", s.SelectedAccount())
	}
}
