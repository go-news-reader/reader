package instagram

import (
	"context"
	"errors"
	"testing"

	goig "github.com/go-instagram/instagram"

	"github.com/go-news-reader/reader/source"
)

// The item ID is firstNonEmpty(Shortcode, ID), so a distinct shortcode on the
// UserProfile posts ("P") vs the UserPosts posts ("F") tells the two paths apart.
func feedShortcodes(t *testing.T, p *Provider) []string {
	t.Helper()
	res, err := p.Feed(context.Background(), source.Query{Channel: "acme"})
	if err != nil {
		t.Fatalf("Feed: %v", err)
	}
	out := make([]string, len(res.Items))
	for i, it := range res.Items {
		out[i] = it.ID
	}
	return out
}

func TestFeedCachesIDThenReadsPostsByID(t *testing.T) {
	f := &fakeClient{
		prof:  &goig.Profile{ID: "123", Username: "acme", Posts: []goig.Post{{Shortcode: "P"}}},
		posts: []goig.Post{{Shortcode: "F"}},
	}
	p := NewWithClient(f)

	// First refresh resolves the id through UserProfile and caches it.
	if got := feedShortcodes(t, p); len(got) != 1 || got[0] != "P" {
		t.Fatalf("first refresh items = %v, want [P] (via UserProfile)", got)
	}
	if f.profileCalls != 1 || f.postsCalls != 0 {
		t.Fatalf("first refresh calls: profile=%d posts=%d", f.profileCalls, f.postsCalls)
	}

	// Second refresh reads straight from the feed endpoint by the cached id.
	if got := feedShortcodes(t, p); len(got) != 1 || got[0] != "F" {
		t.Fatalf("second refresh items = %v, want [F] (via UserPosts)", got)
	}
	if f.profileCalls != 1 {
		t.Errorf("web_profile_info hit again: profileCalls = %d, want 1", f.profileCalls)
	}
	if f.postsCalls != 1 || f.gotID != "123" {
		t.Errorf("UserPosts calls=%d id=%q, want 1/123", f.postsCalls, f.gotID)
	}
}

func TestFeedCachedPostsFailureReResolves(t *testing.T) {
	f := &fakeClient{
		prof:     &goig.Profile{ID: "123", Username: "acme", Posts: []goig.Post{{Shortcode: "P"}}},
		postsErr: errors.New("instagram: transient"),
	}
	p := NewWithClient(f)

	feedShortcodes(t, p) // cache 123
	// Second refresh: the by-id read fails, so it drops the id and re-resolves.
	if got := feedShortcodes(t, p); len(got) != 1 || got[0] != "P" {
		t.Fatalf("second refresh items = %v, want [P] (re-resolved)", got)
	}
	if f.postsCalls != 1 || f.profileCalls != 2 {
		t.Errorf("expected a failed UserPosts then a re-resolve: posts=%d profile=%d",
			f.postsCalls, f.profileCalls)
	}
}

func TestFeedNoIDNeverCaches(t *testing.T) {
	// A profile without an id (web_profile_info omitted it) is resolved through
	// UserProfile on every refresh — there is nothing to cache.
	f := &fakeClient{prof: &goig.Profile{Username: "acme", Posts: []goig.Post{{Shortcode: "P"}}}}
	p := NewWithClient(f)

	feedShortcodes(t, p)
	feedShortcodes(t, p)
	if f.profileCalls != 2 || f.postsCalls != 0 {
		t.Errorf("no-id account: profile=%d posts=%d, want 2/0", f.profileCalls, f.postsCalls)
	}
}
