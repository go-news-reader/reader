package tiktok

import (
	"context"
	"errors"
	"strings"
	"testing"

	gott "github.com/go-tiktok/tiktok"

	"github.com/go-news-reader/reader/source"
)

type fakeFollower struct {
	lists  []*gott.FollowingList
	err    error
	calls  int
	gotSec string
}

func (f *fakeFollower) Following(_ context.Context, secUid string, _ int, _ string) (*gott.FollowingList, error) {
	f.gotSec = secUid
	if f.err != nil {
		return nil, f.err
	}
	l := f.lists[f.calls]
	f.calls++
	return l, nil
}

func TestMyFollowsNoCred(t *testing.T) {
	_, err := (&Provider{}).MyFollows(context.Background())
	if ae, ok := source.AsAuthError(err); !ok || ae.Kind != source.TikTok {
		t.Fatalf("err = %v, want TikTok AuthError", err)
	}
}

func TestMyFollowsWallNoSecUID(t *testing.T) {
	// Credential present but no viewer secUid: this is the production path — the
	// signing wall, reported as a typed sign-in/anti-bot error.
	_, err := (&Provider{hasCred: true}).MyFollows(context.Background())
	ae, ok := source.AsAuthError(err)
	if !ok || ae.Kind != source.TikTok {
		t.Fatalf("err = %v, want TikTok AuthError", err)
	}
	if !strings.Contains(ae.Reason, "signed request") {
		t.Errorf("reason = %q, want the signing wall", ae.Reason)
	}
}

func TestMyFollowsWallOnRefusal(t *testing.T) {
	// Even with a secUid, TikTok refuses the unsigned request; the refusal maps to
	// the typed anti-bot wall, never a fabricated list.
	f := &fakeFollower{err: errors.New("tiktok: user list request failed: status 403: blocked")}
	p := &Provider{hasCred: true, followSecUID: "SEC", follow: f}
	_, err := p.MyFollows(context.Background())
	ae, ok := source.AsAuthError(err)
	if !ok || ae.Kind != source.TikTok {
		t.Fatalf("err = %v, want TikTok AuthError", err)
	}
	if !strings.Contains(ae.Reason, "signing wall") || !strings.Contains(ae.Reason, "403") {
		t.Errorf("reason = %q, want the anti-bot wall carrying the underlying status", ae.Reason)
	}
}

func TestMyFollowsMapsAndPaginates(t *testing.T) {
	f := &fakeFollower{lists: []*gott.FollowingList{
		{Users: []gott.FollowedUser{{SecUID: "SEC_A"}, {SecUID: ""}}, HasMore: true, MaxCursor: "C2"},
		{Users: []gott.FollowedUser{{SecUID: "SEC_B"}}, HasMore: false, MaxCursor: "C3"},
	}}
	p := &Provider{hasCred: true, followSecUID: "VIEWER", follow: f}

	subs, err := p.MyFollows(context.Background())
	if err != nil {
		t.Fatalf("MyFollows: %v", err)
	}
	if f.gotSec != "VIEWER" {
		t.Errorf("Following secUid = %q, want VIEWER", f.gotSec)
	}
	if f.calls != 2 {
		t.Errorf("calls = %d, want 2", f.calls)
	}
	want := []source.Subscription{
		{Source: source.TikTok, Channel: "SEC_A"},
		{Source: source.TikTok, Channel: "SEC_B"},
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
	f := &fakeFollower{lists: []*gott.FollowingList{
		{Users: []gott.FollowedUser{{SecUID: "SOLO"}}, HasMore: true, MaxCursor: ""},
	}}
	p := &Provider{hasCred: true, followSecUID: "V", follow: f}
	subs, err := p.MyFollows(context.Background())
	if err != nil {
		t.Fatalf("MyFollows: %v", err)
	}
	if f.calls != 1 || len(subs) != 1 || subs[0].Channel != "SOLO" {
		t.Fatalf("calls=%d subs=%+v", f.calls, subs)
	}
}
