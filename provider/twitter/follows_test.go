package twitter

import (
	"context"
	"errors"
	"testing"

	gotw "github.com/go-birdsite/twitter"

	"github.com/go-news-reader/reader/source"
)

type fakeFollower struct {
	pages     []*gotw.FollowingPage
	err       error
	calls     int
	gotUserID string
}

func (f *fakeFollower) Following(_ context.Context, userID, _ string) (*gotw.FollowingPage, error) {
	f.gotUserID = userID
	if f.err != nil {
		return nil, f.err
	}
	pg := f.pages[f.calls]
	f.calls++
	return pg, nil
}

func TestViewerID(t *testing.T) {
	cases := map[string]string{
		"":                                  "",
		"auth_token=a; ct0=b":               "", // no twid
		"twid=u%3D1234567890":               "1234567890",
		`twid="u=42"; auth_token=a`:         "42",
		"twid=u%3D999; ct0=c; auth_token=d": "999",
	}
	for session, want := range cases {
		if got := viewerID(session); got != want {
			t.Errorf("viewerID(%q) = %q, want %q", session, got, want)
		}
	}
}

func TestNewWithSessionWiresFollow(t *testing.T) {
	p := NewWithSession(nil, "auth_token=AT; ct0=CT; twid=u%3D555")
	if p.authToken != "AT" || p.csrf != "CT" {
		t.Errorf("session cookies = %q/%q", p.authToken, p.csrf)
	}
	if p.followUserID != "555" {
		t.Errorf("followUserID = %q, want 555", p.followUserID)
	}
	if p.follow == nil {
		t.Error("follow capability not wired")
	}
}

func TestMyFollowsNoSession(t *testing.T) {
	_, err := (&Provider{}).MyFollows(context.Background())
	if ae, ok := source.AsAuthError(err); !ok || ae.Kind != source.Twitter {
		t.Fatalf("err = %v, want Twitter AuthError", err)
	}
}

func TestMyFollowsNoUserID(t *testing.T) {
	// Cookies present but no twid → cannot determine the viewer id.
	_, err := (&Provider{authToken: "a", csrf: "b"}).MyFollows(context.Background())
	if ae, ok := source.AsAuthError(err); !ok || ae.Kind != source.Twitter {
		t.Fatalf("err = %v, want Twitter AuthError", err)
	}
}

func TestMyFollowsSuccessPaginates(t *testing.T) {
	f := &fakeFollower{pages: []*gotw.FollowingPage{
		{Users: []gotw.FollowedUser{{ScreenName: "alice"}, {ScreenName: ""}}, Cursor: "C2"},
		{Users: []gotw.FollowedUser{{ScreenName: "bob"}}, Cursor: "C3"},
		{Users: nil, Cursor: "C4"}, // empty page → stop (never loops on a live cursor)
	}}
	p := &Provider{authToken: "a", csrf: "b", followUserID: "42", follow: f}

	subs, err := p.MyFollows(context.Background())
	if err != nil {
		t.Fatalf("MyFollows: %v", err)
	}
	if f.gotUserID != "42" {
		t.Errorf("Following userID = %q, want 42", f.gotUserID)
	}
	if f.calls != 3 {
		t.Errorf("calls = %d, want 3", f.calls)
	}
	want := []source.Subscription{
		{Source: source.Twitter, Channel: "alice"},
		{Source: source.Twitter, Channel: "bob"},
	}
	if len(subs) != len(want) {
		t.Fatalf("subs = %+v, want %+v", subs, want)
	}
	for i := range want {
		if subs[i] != want[i] {
			t.Errorf("sub[%d] = %+v, want %+v", i, subs[i], want[i])
		}
	}
}

func TestMyFollowsStopsOnEmptyCursor(t *testing.T) {
	f := &fakeFollower{pages: []*gotw.FollowingPage{
		{Users: []gotw.FollowedUser{{ScreenName: "solo"}}, Cursor: ""},
	}}
	p := &Provider{authToken: "a", csrf: "b", followUserID: "42", follow: f}
	subs, err := p.MyFollows(context.Background())
	if err != nil {
		t.Fatalf("MyFollows: %v", err)
	}
	if f.calls != 1 || len(subs) != 1 || subs[0].Channel != "solo" {
		t.Fatalf("calls=%d subs=%+v", f.calls, subs)
	}
}

func TestMyFollowsErrorMapping(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		wantAE  bool
		wantMsg string
	}{
		{"needs-auth", gotw.ErrNeedsAuth, true, ""},
		{"query-id-rotated", gotw.ErrQueryIDRotated, true, ""},
		{"transient", errors.New("boom"), false, "boom"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &Provider{authToken: "a", csrf: "b", followUserID: "42", follow: &fakeFollower{err: tc.err}}
			_, err := p.MyFollows(context.Background())
			ae, ok := source.AsAuthError(err)
			if tc.wantAE {
				if !ok || ae.Kind != source.Twitter {
					t.Fatalf("err = %v, want Twitter AuthError", err)
				}
				return
			}
			if ok {
				t.Fatalf("err = %v, want raw error not AuthError", err)
			}
			if err == nil || err.Error() != tc.wantMsg {
				t.Fatalf("err = %v, want %q", err, tc.wantMsg)
			}
		})
	}
}
