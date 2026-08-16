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

	// The banners are pinned above the feed list, one per prompt, stacked from the
	// topbar down. Pixel fact: the first banner pill is painted in the theme accent.
	bx := feedX + 4
	by := feedTop + m.bannerH/2
	if got := px(buf, s.W, bx, by); got.R != s.theme.Accent.R || got.G != s.theme.Accent.G || got.B != s.theme.Accent.B {
		t.Fatalf("banner pixel = %v, want accent %v", got, s.theme.Accent)
	}

	// Clicking banner row 0 returns HitFixAuth for Reddit.
	if h := s.HitTest(bx, by); h.Kind != HitFixAuth || h.Value != string(source.Reddit) {
		t.Fatalf("banner-0 hit = %+v", h)
	}
	// Clicking banner row 1 returns HitFixAuth for Mastodon.
	by1 := feedTop + (m.bannerH + m.cardGap) + m.bannerH/2
	if h := s.HitTest(bx, by1); h.Kind != HitFixAuth || h.Value != string(source.Mastodon) {
		t.Fatalf("banner-1 hit = %+v", h)
	}
}

func TestAuthBannerShiftsFeedList(t *testing.T) {
	s := newScene()
	s.layout()
	// No prompts: the feed list starts immediately below the topbar.
	base := s.feed.list.Bounds().Y
	if base != s.m.topbarH {
		t.Fatalf("no-prompt list top = %d, want topbarH %d", base, s.m.topbarH)
	}
	// Prompts push the feed list down by the pinned banner stack's height.
	s.SetAuthPrompts(threePrompts())
	s.layout()
	if s.feedBannerH() == 0 || s.feed.list.Bounds().Y != base+s.feedBannerH() {
		t.Fatalf("banners did not push the feed list down: top=%d base=%d bannerH=%d",
			s.feed.list.Bounds().Y, base, s.feedBannerH())
	}
	// Clearing the prompts restores the original list top.
	s.SetAuthPrompts(nil)
	s.layout()
	if s.feed.list.Bounds().Y != base {
		t.Fatalf("cleared prompts left the feed list at %d, want %d", s.feed.list.Bounds().Y, base)
	}
}

func TestAuthBannerManyRender(t *testing.T) {
	// One prompt per source kind in a short window: the pinned banner stack is
	// taller than the viewport, so drawAuthBanner paints (and the painter clamps)
	// every prompt without panicking; a feed scroll leaves the pinned banners put.
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
	renderPNG(t, s, "auth-prompt-many")

	s.Scroll(100000) // scrolls the feed cards; the pinned banners stay put
	renderPNG(t, s, "auth-prompt-scrolled")
}

func TestSelectedAccountAccessor(t *testing.T) {
	s := newScene()
	s.SelectAccount(source.Bluesky)
	if s.SelectedAccount() != source.Bluesky {
		t.Fatalf("SelectedAccount = %q, want bluesky", s.SelectedAccount())
	}
}
