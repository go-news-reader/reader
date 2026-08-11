package instagram

import (
	"context"
	"errors"
	"testing"

	goig "github.com/go-instagram/instagram"

	"github.com/go-news-reader/reader/source"
)

// fakeFollower is a stand-in for the *goig.Client follow capability.
type fakeFollower struct {
	curID     string
	curErr    error
	pages     []*goig.FollowingPage
	pageErr   error
	calls     int
	gotUserID string
}

func (f *fakeFollower) CurrentUserID(context.Context) (string, error) {
	return f.curID, f.curErr
}

func (f *fakeFollower) Following(_ context.Context, userID, _ string) (*goig.FollowingPage, error) {
	f.gotUserID = userID
	if f.pageErr != nil {
		return nil, f.pageErr
	}
	pg := f.pages[f.calls]
	f.calls++
	return pg, nil
}

func TestNewWithFullCookie(t *testing.T) {
	// A full cookie string wires the csrftoken and lifts ds_user_id as the viewer
	// id, so MyFollows can skip the current_user round-trip.
	p := New("sessionid=s1; csrftoken=c1; ds_user_id=42")
	if p.csrf != "c1" {
		t.Errorf("csrf = %q, want c1", p.csrf)
	}
	if p.userID != "42" {
		t.Errorf("userID = %q, want 42", p.userID)
	}
	if p.follow == nil {
		t.Error("follow capability not wired")
	}
}

func TestMyFollowsNoSession(t *testing.T) {
	_, err := (&Provider{}).MyFollows(context.Background())
	ae, ok := source.AsAuthError(err)
	if !ok || ae.Kind != source.Instagram {
		t.Fatalf("err = %v, want Instagram AuthError", err)
	}
}

func TestMyFollowsResolvesCurrentUserAndPaginates(t *testing.T) {
	f := &fakeFollower{
		curID: "77",
		pages: []*goig.FollowingPage{
			{Users: []goig.FollowedUser{{Username: "alice"}, {Username: ""}}, NextMaxID: "C2"},
			{Users: []goig.FollowedUser{{Username: "bob"}}, NextMaxID: ""},
		},
	}
	p := &Provider{sessionID: "s", follow: f} // userID empty → CurrentUserID used

	subs, err := p.MyFollows(context.Background())
	if err != nil {
		t.Fatalf("MyFollows: %v", err)
	}
	if f.gotUserID != "77" {
		t.Errorf("Following userID = %q, want 77 (from CurrentUserID)", f.gotUserID)
	}
	if f.calls != 2 {
		t.Errorf("Following calls = %d, want 2 (paginated)", f.calls)
	}
	want := []source.Subscription{
		{Source: source.Instagram, Channel: "alice"},
		{Source: source.Instagram, Channel: "bob"},
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

func TestMyFollowsUsesDsUserID(t *testing.T) {
	// userID present (from ds_user_id): CurrentUserID must NOT be consulted — its
	// error here would surface if it were.
	f := &fakeFollower{
		curErr: errors.New("current_user must not be called"),
		pages:  []*goig.FollowingPage{{Users: []goig.FollowedUser{{Username: "x"}}}},
	}
	p := &Provider{sessionID: "s", userID: "99", follow: f}

	subs, err := p.MyFollows(context.Background())
	if err != nil {
		t.Fatalf("MyFollows: %v", err)
	}
	if f.gotUserID != "99" {
		t.Errorf("Following userID = %q, want 99 (ds_user_id)", f.gotUserID)
	}
	if len(subs) != 1 || subs[0].Channel != "x" {
		t.Errorf("subs = %+v", subs)
	}
}

func TestMyFollowsCurrentUserErrorPassthrough(t *testing.T) {
	// A non-auth CurrentUserID error passes through unchanged.
	p := &Provider{sessionID: "s", follow: &fakeFollower{curErr: errors.New("dial tcp: timeout")}}
	_, err := p.MyFollows(context.Background())
	if err == nil || err.Error() != "dial tcp: timeout" {
		t.Fatalf("err = %v, want raw timeout", err)
	}
	if _, ok := source.AsAuthError(err); ok {
		t.Fatal("transient error must not be an AuthError")
	}
}

func TestMyFollowsFollowingAuthError(t *testing.T) {
	// A 401/403 folded into the following error maps to a typed auth prompt.
	p := &Provider{sessionID: "s", userID: "1", follow: &fakeFollower{
		pageErr: errors.New("instagram: unexpected status 401 (Unauthorized)"),
	}}
	_, err := p.MyFollows(context.Background())
	ae, ok := source.AsAuthError(err)
	if !ok || ae.Kind != source.Instagram {
		t.Fatalf("err = %v, want Instagram AuthError", err)
	}
}
