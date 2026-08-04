package usenet

import (
	"context"
	"errors"
	"testing"

	gonntp "github.com/go-newsgroups/nntp"
)

func TestSubjectClassification(t *testing.T) {
	bin := []string{
		`[1/50] - "movie.mkv" yEnc (1/999)`, // yEnc marker
		`cool.rar`,                          // archive extension
		`holiday.jpg`,                       // image extension (also binary)
		`SET "recovery.par2" (1/1)`,         // par2
	}
	for _, s := range bin {
		if !isBinarySubject(s) {
			t.Errorf("isBinarySubject(%q) = false, want true", s)
		}
	}
	if isBinarySubject("Re: what do you all think about this?") {
		t.Error("a plain discussion subject must not be binary")
	}
	if !isImageSubject(`vacation.PNG`) {
		t.Error("PNG should be an image subject (case-insensitive)")
	}
	if isImageSubject("archive.rar") {
		t.Error("a non-image binary must not be an image subject")
	}
}

func TestGroupStats(t *testing.T) {
	fc := &fakeConn{
		group: &gonntp.Group{Low: 1, High: 1000},
		over: []gonntp.Overview{
			{Subject: `[1/9] - "a.mkv" yEnc (1/9)`}, // binary
			{Subject: `pic1.jpg`},                   // binary + image
			{Subject: `pic2.png`},                   // binary + image
			{Subject: `Re: discussion here`},        // neither
			{Subject: `notes.pdf`},                  // binary (not image)
		},
	}
	p := NewWithDial(dialing(fc, nil))
	st, err := p.GroupStats(context.Background(), "alt.bin.pics", 3)
	if err != nil {
		t.Fatalf("GroupStats: %v", err)
	}
	if st.Sampled != 5 || st.Binaries != 4 || st.Images != 2 {
		t.Fatalf("stats = %+v, want Sampled=5 Binaries=4 Images=2", st)
	}
	// sample=3 → low = 1000-3+1 = 998 (> Low 1, no clamp).
	if fc.gotLow != 998 || fc.gotHigh != 1000 {
		t.Errorf("Over range = [%d,%d], want [998,1000]", fc.gotLow, fc.gotHigh)
	}
}

func TestGroupStatsDefaultsAndClamp(t *testing.T) {
	fc := &fakeConn{group: &gonntp.Group{Low: 90, High: 100}} // small group
	p := NewWithDial(dialing(fc, nil))
	if _, err := p.GroupStats(context.Background(), "g", 0); err != nil { // sample<=0 → default
		t.Fatalf("GroupStats: %v", err)
	}
	// default 2000 → low = 100-2000+1 < Low 90 → clamps to 90.
	if fc.gotLow != 90 || fc.gotHigh != 100 {
		t.Errorf("clamped range = [%d,%d], want [90,100]", fc.gotLow, fc.gotHigh)
	}
}

func TestGroupStatsErrors(t *testing.T) {
	// dial error
	if _, err := NewWithDial(dialing(nil, errors.New("dial"))).GroupStats(context.Background(), "g", 5); err == nil {
		t.Error("expected a dial error")
	}
	// Group error
	if _, err := NewWithDial(dialing(&fakeConn{groupErr: errors.New("grp")}, nil)).GroupStats(context.Background(), "g", 5); err == nil {
		t.Error("expected a Group error")
	}
	// Over error
	fc := &fakeConn{group: &gonntp.Group{Low: 1, High: 100}, overErr: errors.New("over")}
	if _, err := NewWithDial(dialing(fc, nil)).GroupStats(context.Background(), "g", 5); err == nil {
		t.Error("expected an Over error")
	}
}
