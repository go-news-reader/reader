package source

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestAuthErrorAndConstructor(t *testing.T) {
	ae := NeedsAuth(Reddit, "sign in")
	if ae.Kind != Reddit || ae.Reason != "sign in" {
		t.Fatalf("NeedsAuth fields = %+v", ae)
	}
	if got := ae.Error(); got != "reddit: needs authentication: sign in" {
		t.Fatalf("Error() = %q", got)
	}
}

func TestAsAuthError(t *testing.T) {
	// Direct.
	if ae, ok := AsAuthError(NeedsAuth(Mastodon, "x")); !ok || ae.Kind != Mastodon {
		t.Fatalf("direct: %v %v", ae, ok)
	}
	// Wrapped through *SubscriptionError.Unwrap.
	wrapped := &SubscriptionError{Sub: Subscription{Source: Lemmy}, Err: NeedsAuth(Lemmy, "y")}
	if ae, ok := AsAuthError(wrapped); !ok || ae.Kind != Lemmy || ae.Reason != "y" {
		t.Fatalf("wrapped: %v %v", ae, ok)
	}
	// Wrapped through a plain %w chain too.
	if ae, ok := AsAuthError(fmt.Errorf("boom: %w", NeedsAuth(TikTok, "z"))); !ok || ae.Kind != TikTok {
		t.Fatalf("fmt-wrapped: %v %v", ae, ok)
	}
	// Non-auth error and nil.
	if _, ok := AsAuthError(errors.New("plain")); ok {
		t.Fatal("plain error matched AsAuthError")
	}
	if _, ok := AsAuthError(nil); ok {
		t.Fatal("nil matched AsAuthError")
	}
}

func TestHTTPAuthStatus(t *testing.T) {
	for _, code := range []int{401, 403} {
		if !HTTPAuthStatus(code) {
			t.Fatalf("HTTPAuthStatus(%d) = false", code)
		}
	}
	for _, code := range []int{200, 404, 429, 500, 0} {
		if HTTPAuthStatus(code) {
			t.Fatalf("HTTPAuthStatus(%d) = true", code)
		}
	}
}

func TestErrHasAuthStatus(t *testing.T) {
	yes := []error{
		errors.New("mastodon: GET /x: unexpected status 401: nope"),
		errors.New("lemmy: unexpected status 403: forbidden"),
		errors.New("403"),
		fmt.Errorf("wrap: %w", errors.New("atproto: xrpc error 401: AuthRequired")),
	}
	for _, e := range yes {
		if !ErrHasAuthStatus(e) {
			t.Fatalf("ErrHasAuthStatus(%v) = false, want true", e)
		}
	}
	no := []error{
		nil,
		errors.New("connection refused"),
		errors.New("unexpected status 429: rate limited"),
		errors.New("status 4013 is not auth"), // 401 is a substring but not standalone
		errors.New("status 14030"),            // 403 embedded in a longer number
		errors.New("500 internal"),
	}
	for _, e := range no {
		if ErrHasAuthStatus(e) {
			t.Fatalf("ErrHasAuthStatus(%v) = true, want false", e)
		}
	}
}

func TestRegistryFeedUnregisteredIsAuthError(t *testing.T) {
	r := NewRegistry()
	_, err := r.Feed(context.Background(), Mastodon, Query{})
	ae, ok := AsAuthError(err)
	if !ok {
		t.Fatalf("unregistered kind error = %v; want *AuthError", err)
	}
	if ae.Kind != Mastodon || ae.Reason != "not configured" {
		t.Fatalf("AuthError = %+v", ae)
	}
}
