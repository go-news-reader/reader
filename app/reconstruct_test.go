package app

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/go-newsgroups/par2"

	"github.com/go-news-reader/reader/provider/usenet"
	"github.com/go-news-reader/reader/source"
)

// fakeUsenet is a provider registered under source.Usenet that also implements
// the reconstructor capability, recording the request it received.
type fakeUsenet struct {
	files  map[string][]byte
	vr     *par2.VerifyResult
	err    error
	gotReq usenet.ReconstructRequest
}

func (f *fakeUsenet) Kind() source.Kind { return source.Usenet }
func (f *fakeUsenet) Feed(context.Context, source.Query) (source.Result, error) {
	return source.Result{}, nil
}
func (f *fakeUsenet) Reconstruct(_ context.Context, req usenet.ReconstructRequest) (map[string][]byte, *par2.VerifyResult, error) {
	f.gotReq = req
	if req.OnProgress != nil {
		req.OnProgress(len(req.Parts), len(req.Parts))
	}
	return f.files, f.vr, f.err
}

// usenetPost is a two-part groupable Usenet post (base "release").
func usenetPost() []source.Item {
	return []source.Item{
		{ID: "d1", Source: source.Usenet, Channel: "a.b.test", Permalink: "news:d1",
			Title: `[1/2] - "release.tar.zst" yEnc (1/1) 1000`},
		{ID: "d2", Source: source.Usenet, Channel: "a.b.test", Permalink: "news:d2",
			Title: `[2/2] - "release.tar.zst.par2" yEnc (1/1) 200`},
	}
}

// syncReconstruct wires a synchronous reconstruction hook for determinism.
func syncReconstruct(a *App) {
	a.SetReconstructHook(func(base string) { a.doReconstruct(context.Background(), base) })
}

func TestReconstructGroupSavesVerified(t *testing.T) {
	saved := map[string][]byte{}
	orig := writeFile
	writeFile = func(name string, data []byte, _ os.FileMode) error { saved[name] = data; return nil }
	defer func() { writeFile = orig }()

	fu := &fakeUsenet{files: map[string][]byte{"release.bin": []byte("payload")}, vr: &par2.VerifyResult{Complete: true}}
	a := New(Config{Registry: newReg(fu), Width: 400, Height: 300})
	a.Scene().SetCachePath("/downloads")
	a.Scene().SetItems(usenetPost())
	syncReconstruct(a)

	a.ReconstructGroup("release")

	if got, ok := saved["/downloads/release.bin"]; !ok || string(got) != "payload" {
		t.Fatalf("saved = %v", saved)
	}
	// The request carried both parts with their message-ids and filenames.
	if len(fu.gotReq.Parts) != 2 || fu.gotReq.Parts[0].MessageID != "d1" ||
		fu.gotReq.Parts[0].Filename != "release.tar.zst" {
		t.Fatalf("request parts = %+v", fu.gotReq.Parts)
	}
	if st := a.Scene().Status; !strings.Contains(st, "Saved release") || !strings.Contains(st, "verified") {
		t.Fatalf("status = %q", st)
	}
	if a.Scene().Loading() {
		t.Fatal("loading should be off after reconstruct")
	}
}

func TestReconstructGroupVerifyWordVariants(t *testing.T) {
	cases := []struct {
		vr   *par2.VerifyResult
		want string
	}{
		{nil, "no PAR2"},
		{&par2.VerifyResult{Complete: true}, "verified"},
		{&par2.VerifyResult{Complete: false}, "incomplete"},
	}
	for _, c := range cases {
		writeFileWas := writeFile
		writeFile = func(string, []byte, os.FileMode) error { return nil }
		fu := &fakeUsenet{files: map[string][]byte{"r.bin": {1}}, vr: c.vr}
		a := New(Config{Registry: newReg(fu), Width: 400, Height: 300})
		a.Scene().SetItems(usenetPost())
		syncReconstruct(a)
		a.ReconstructGroup("release")
		if !strings.Contains(a.Scene().Status, c.want) {
			t.Fatalf("vr=%+v status=%q, want %q", c.vr, a.Scene().Status, c.want)
		}
		writeFile = writeFileWas
	}
}

func TestReconstructGroupProviderError(t *testing.T) {
	fu := &fakeUsenet{err: errors.New("dial failed")}
	a := New(Config{Registry: newReg(fu), Width: 400, Height: 300})
	a.Scene().SetItems(usenetPost())
	syncReconstruct(a)
	a.ReconstructGroup("release")
	if st := a.Scene().Status; !strings.Contains(st, "failed") {
		t.Fatalf("status = %q", st)
	}
	if a.Scene().Loading() {
		t.Fatal("loading should be off after failure")
	}
}

func TestReconstructGroupSaveError(t *testing.T) {
	orig := writeFile
	writeFile = func(string, []byte, os.FileMode) error { return errors.New("disk full") }
	defer func() { writeFile = orig }()

	fu := &fakeUsenet{files: map[string][]byte{"r.bin": {1}}, vr: &par2.VerifyResult{Complete: true}}
	a := New(Config{Registry: newReg(fu), Width: 400, Height: 300})
	a.Scene().SetItems(usenetPost())
	syncReconstruct(a)
	a.ReconstructGroup("release")
	if st := a.Scene().Status; !strings.Contains(st, "save failed") {
		t.Fatalf("status = %q", st)
	}
}

func TestReconstructGroupNoGroup(t *testing.T) {
	a := New(Config{Registry: newReg(&fakeUsenet{}), Width: 400, Height: 300})
	// No matching items -> nothing to reconstruct.
	syncReconstruct(a)
	a.ReconstructGroup("missing")
	if !strings.Contains(a.Scene().Status, "Nothing to reconstruct") {
		t.Fatalf("status = %q", a.Scene().Status)
	}
}

func TestReconstructGroupNoUsenetProvider(t *testing.T) {
	a := New(Config{Registry: newReg(fakeProv{kind: source.Reddit}), Width: 400, Height: 300})
	a.Scene().SetItems(usenetPost())
	syncReconstruct(a)
	a.ReconstructGroup("release")
	if !strings.Contains(a.Scene().Status, "not configured") {
		t.Fatalf("status = %q", a.Scene().Status)
	}
}

func TestReconstructGroupProviderCannotReconstruct(t *testing.T) {
	// A Usenet-kind provider without a Reconstruct method fails the assertion.
	a := New(Config{Registry: newReg(fakeProv{kind: source.Usenet}), Width: 400, Height: 300})
	a.Scene().SetItems(usenetPost())
	syncReconstruct(a)
	a.ReconstructGroup("release")
	if !strings.Contains(a.Scene().Status, "cannot reconstruct") {
		t.Fatalf("status = %q", a.Scene().Status)
	}
}

func TestDefaultReconstructHook(t *testing.T) {
	// Exercise the default (goroutine) reconstruct hook; no assertion on the async
	// result, just that the trigger path runs without panicking.
	fu := &fakeUsenet{files: map[string][]byte{}, vr: nil}
	a := New(Config{Registry: newReg(fu), Width: 400, Height: 300})
	a.Scene().SetItems(usenetPost())
	a.ReconstructGroup("release") // spawns go a.doReconstruct(...)
	_ = a.Items()
}
