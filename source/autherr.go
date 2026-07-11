package source

import (
	"errors"
	"fmt"
	"regexp"
)

// AuthError signals that a subscription failed because the provider needs the
// user to sign in or supply configuration — credentials, an instance URL, a
// session token — rather than because of a transient network or server fault.
// The aggregator turns it into a clickable "needs sign-in" prompt that opens the
// Accounts editor for Kind, instead of surfacing a raw error string.
type AuthError struct {
	Kind   Kind   // the provider that needs attention
	Reason string // a short, human-readable explanation ("access token required", …)
}

// Error implements error.
func (e *AuthError) Error() string {
	return fmt.Sprintf("%s: needs authentication: %s", e.Kind, e.Reason)
}

// NeedsAuth builds an *AuthError for kind carrying a short human reason.
func NeedsAuth(kind Kind, reason string) *AuthError {
	return &AuthError{Kind: kind, Reason: reason}
}

// AsAuthError extracts an *AuthError from err, unwrapping through wrappers such
// as *SubscriptionError. It reports false for a nil error or any error whose
// chain contains no *AuthError.
func AsAuthError(err error) (*AuthError, bool) {
	var ae *AuthError
	if errors.As(err, &ae) {
		return ae, true
	}
	return nil, false
}

// HTTPAuthStatus reports whether an HTTP status code means the request needs
// authentication or was refused for lack of permission — 401 Unauthorized or
// 403 Forbidden. These are the statuses the aggregator treats as "needs
// sign-in/config"; a 429 (rate limit) or 5xx stays a transient error.
func HTTPAuthStatus(code int) bool { return code == 401 || code == 403 }

// authStatusRe matches a standalone 401 or 403 token (not part of a longer
// number) anywhere in a string.
var authStatusRe = regexp.MustCompile(`(^|[^0-9])(401|403)([^0-9]|$)`)

// ErrHasAuthStatus is a heuristic for client libraries that fold the HTTP status
// into their error *text* instead of exposing a typed status code: it reports
// whether err's message carries a standalone 401 or 403 token. Provider adapters
// use it to map a permission failure onto a typed AuthError when no richer
// signal is available. It only ever fires on errors the client already produced
// for a non-2xx response, so a stray digit in an unrelated message is not a
// realistic false positive.
func ErrHasAuthStatus(err error) bool {
	return err != nil && authStatusRe.MatchString(err.Error())
}
