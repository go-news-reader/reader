package twitter

import (
	"context"
	"errors"
	"testing"

	gotw "github.com/go-birdsite/twitter"

	"github.com/go-news-reader/reader/source"
)

// authFake implements both the public (UserTweets) and authenticated
// (UserIDByScreenName + UserTweetsByID) client slices, counting calls so a test
// can assert which path ran and that the id is resolved only once.
type authFake struct {
	fakeClient // public UserTweets (records screenName in .got, returns tl/err)

	id        string
	idErr     error
	byID      *gotw.Timeline
	byIDErr   error
	idCalls   int
	byIDCalls int
}

func (a *authFake) UserIDByScreenName(_ context.Context, _ string) (string, error) {
	a.idCalls++
	return a.id, a.idErr
}

func (a *authFake) UserTweetsByID(_ context.Context, _ string) (*gotw.Timeline, error) {
	a.byIDCalls++
	return a.byID, a.byIDErr
}

// authedProvider wraps c as a provider that holds a logged-in session, so the
// authenticated timeline path is taken.
func authedProvider(c client) *Provider {
	return &Provider{client: c, authToken: "a", csrf: "b", idCache: map[string]string{}}
}

func tweetWithText(id, text string) gotw.Tweet { return gotw.Tweet{ID: id, Text: text} }

func TestUserTweetsAuthenticatedPath(t *testing.T) {
	f := &authFake{
		id:   "42",
		byID: &gotw.Timeline{Tweets: []gotw.Tweet{tweetWithText("1", "authed")}},
		fakeClient: fakeClient{ // syndication would return a different marker
			tl: &gotw.Timeline{Tweets: []gotw.Tweet{tweetWithText("9", "syndication")}},
		},
	}
	p := authedProvider(f)
	res, err := p.Feed(context.Background(), source.Query{Channel: "@nasa"})
	if err != nil {
		t.Fatalf("Feed: %v", err)
	}
	if len(res.Items) != 1 || res.Items[0].Body != "authed" {
		t.Fatalf("expected the authenticated timeline, got %+v", res.Items)
	}
	if f.idCalls != 1 || f.byIDCalls != 1 {
		t.Errorf("auth calls: id=%d byID=%d, want 1/1", f.idCalls, f.byIDCalls)
	}
	if f.got != "" {
		t.Errorf("syndication UserTweets was called (%q); it must not be", f.got)
	}
}

func TestUserTweetsIDCachedAcrossRefreshes(t *testing.T) {
	f := &authFake{id: "42", byID: &gotw.Timeline{}}
	p := authedProvider(f)
	for i := 0; i < 3; i++ {
		if _, err := p.Feed(context.Background(), source.Query{Channel: "@nasa"}); err != nil {
			t.Fatalf("Feed %d: %v", i, err)
		}
	}
	if f.idCalls != 1 {
		t.Errorf("UserByScreenName called %d times, want 1 (cached)", f.idCalls)
	}
	if f.byIDCalls != 3 {
		t.Errorf("UserTweetsByID called %d times, want 3", f.byIDCalls)
	}
}

func TestUserTweetsFallbackToSyndication(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"rotated query id", gotw.ErrQueryIDRotated},
		{"session rejected", gotw.ErrNeedsAuth},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &authFake{
				id:      "42",
				byIDErr: tc.err,
				fakeClient: fakeClient{
					tl: &gotw.Timeline{Tweets: []gotw.Tweet{tweetWithText("9", "public")}},
				},
			}
			p := authedProvider(f)
			res, err := p.Feed(context.Background(), source.Query{Channel: "@nasa"})
			if err != nil {
				t.Fatalf("Feed: %v", err)
			}
			if len(res.Items) != 1 || res.Items[0].Body != "public" {
				t.Fatalf("expected the public fallback, got %+v", res.Items)
			}
			if f.got != "nasa" {
				t.Errorf("syndication not used on fallback (got %q)", f.got)
			}
		})
	}
}

func TestUserTweetsResolveFallback(t *testing.T) {
	// A rotated/expired failure while resolving the id also falls back.
	f := &authFake{
		idErr: gotw.ErrNeedsAuth,
		fakeClient: fakeClient{
			tl: &gotw.Timeline{Tweets: []gotw.Tweet{tweetWithText("9", "public")}},
		},
	}
	p := authedProvider(f)
	res, err := p.Feed(context.Background(), source.Query{Channel: "@nasa"})
	if err != nil {
		t.Fatalf("Feed: %v", err)
	}
	if len(res.Items) != 1 || res.Items[0].Body != "public" {
		t.Fatalf("expected the public fallback, got %+v", res.Items)
	}
	if f.byIDCalls != 0 {
		t.Errorf("UserTweetsByID should not run when the id resolve fails")
	}
}

func TestUserTweetsNoFallbackOnRealError(t *testing.T) {
	// A genuine failure (not a session/rotation problem) is reported, not retried
	// on the weaker public endpoint.
	sentinel := errors.New("twitter: rate limited")
	f := &authFake{id: "42", byIDErr: sentinel}
	p := authedProvider(f)
	_, err := p.Feed(context.Background(), source.Query{Channel: "@nasa"})
	if !errors.Is(err, sentinel) {
		t.Fatalf("want the real error, got %v", err)
	}
	if f.got != "" {
		t.Errorf("syndication must not be tried on a real error (got %q)", f.got)
	}
}

func TestUserTweetsResolveRealErrorNoFallback(t *testing.T) {
	sentinel := errors.New("twitter: boom")
	f := &authFake{idErr: sentinel}
	p := authedProvider(f)
	_, err := p.Feed(context.Background(), source.Query{Channel: "@nasa"})
	if !errors.Is(err, sentinel) {
		t.Fatalf("want the real error, got %v", err)
	}
	if f.got != "" {
		t.Errorf("syndication must not be tried (got %q)", f.got)
	}
}

func TestUserTweetsNoSessionUsesSyndication(t *testing.T) {
	// Same authenticated-capable client, but no session: the public path is used.
	f := &authFake{
		id:   "42",
		byID: &gotw.Timeline{Tweets: []gotw.Tweet{tweetWithText("1", "authed")}},
		fakeClient: fakeClient{
			tl: &gotw.Timeline{Tweets: []gotw.Tweet{tweetWithText("9", "public")}},
		},
	}
	p := NewWithClient(f) // no authToken/csrf
	res, err := p.Feed(context.Background(), source.Query{Channel: "@nasa"})
	if err != nil {
		t.Fatalf("Feed: %v", err)
	}
	if len(res.Items) != 1 || res.Items[0].Body != "public" {
		t.Fatalf("expected the public path, got %+v", res.Items)
	}
	if f.idCalls != 0 || f.byIDCalls != 0 {
		t.Errorf("authenticated path used without a session: id=%d byID=%d", f.idCalls, f.byIDCalls)
	}
}

func TestUserTweetsNonAuthClientUsesSyndication(t *testing.T) {
	// A plain client (no authenticated methods) with a session still uses the
	// public path — there is nothing else it can do.
	f := &fakeClient{tl: &gotw.Timeline{Tweets: []gotw.Tweet{tweetWithText("9", "public")}}}
	p := authedProvider(f)
	res, err := p.Feed(context.Background(), source.Query{Channel: "@nasa"})
	if err != nil {
		t.Fatalf("Feed: %v", err)
	}
	if len(res.Items) != 1 || res.Items[0].Body != "public" {
		t.Fatalf("expected the public path, got %+v", res.Items)
	}
	if f.got != "nasa" {
		t.Errorf("syndication not used (got %q)", f.got)
	}
}
