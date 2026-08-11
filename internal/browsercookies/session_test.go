package browsercookies

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestInstagramSession(t *testing.T) {
	f := setupProfile(t, [][3]string{
		{".other.com", "sessionid", "wrong"},
		{".instagram.com", "sessionid", "SID"},
		{".instagram.com", "csrftoken", "CSRF"},
	})
	got, err := f.InstagramSession()
	if err != nil {
		t.Fatal(err)
	}
	if got != "sessionid=SID; csrftoken=CSRF" {
		t.Fatalf("instagram session = %q", got)
	}
}

func TestInstagramSessionOnlyPrimary(t *testing.T) {
	// csrftoken absent: it is simply omitted, sessionid alone is still a valid import.
	f := setupProfile(t, [][3]string{{"www.instagram.com", "sessionid", "SID"}})
	got, err := f.InstagramSession()
	if err != nil {
		t.Fatal(err)
	}
	if got != "sessionid=SID" {
		t.Fatalf("instagram session = %q", got)
	}
}

func TestInstagramSessionMissingPrimary(t *testing.T) {
	// Only a csrftoken, no sessionid => not logged in.
	f := setupProfile(t, [][3]string{{".instagram.com", "csrftoken", "CSRF"}})
	if _, err := f.InstagramSession(); !errors.Is(err, ErrNoCookie) {
		t.Fatalf("want ErrNoCookie, got %v", err)
	}
}

func TestTikTokSession(t *testing.T) {
	f := setupProfile(t, [][3]string{
		{".tiktok.com", "sessionid", "TS"},
		{".tiktok.com", "msToken", "MS"},
	})
	got, err := f.TikTokSession()
	if err != nil {
		t.Fatal(err)
	}
	if got != "sessionid=TS; msToken=MS" {
		t.Fatalf("tiktok session = %q", got)
	}
}

func TestTwitterSessionAnchoredHost(t *testing.T) {
	// A cookie on netflix.com (which textually ends in "x.com") must NOT match the
	// anchored x.com patterns; only the real x.com auth_token/ct0 are imported.
	f := setupProfile(t, [][3]string{
		{".netflix.com", "auth_token", "SHOULD-NOT-MATCH"},
		{".x.com", "auth_token", "AT"},
		{"x.com", "ct0", "C0"},
	})
	got, err := f.TwitterSession()
	if err != nil {
		t.Fatal(err)
	}
	if got != "auth_token=AT; ct0=C0" {
		t.Fatalf("twitter session = %q", got)
	}
}

func TestTwitterSessionMissing(t *testing.T) {
	f := setupProfile(t, [][3]string{{".x.com", "ct0", "C0"}}) // no auth_token
	if _, err := f.TwitterSession(); !errors.Is(err, ErrNoCookie) {
		t.Fatalf("want ErrNoCookie, got %v", err)
	}
}

func TestImportSessionProfileError(t *testing.T) {
	// No Firefox: the cookieDBPath error propagates out of the importer.
	if _, err := finderFor("linux", t.TempDir(), "").InstagramSession(); !errors.Is(err, ErrNoFirefox) {
		t.Fatalf("want ErrNoFirefox, got %v", err)
	}
}

func TestReadSessionQueryError(t *testing.T) {
	// A DB with no moz_cookies table makes lookupCookie fail; readSession propagates.
	base := t.TempDir()
	dbPath := filepath.Join(base, "cookies.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE unrelated (x)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	spec := sessionSpec{hostPatterns: []string{"%instagram.com"}, primary: "sessionid", names: []string{"sessionid"}}
	if _, err := readSession(dbPath, spec); err == nil || errors.Is(err, ErrNoCookie) {
		t.Fatalf("want a raw query error, got %v", err)
	}
}

func TestReadSessionOpenError(t *testing.T) {
	// openCookieDB failure (a directory as the db path) propagates.
	spec := sessionSpec{hostPatterns: []string{"%x.com"}, primary: "auth_token", names: []string{"auth_token"}}
	if _, err := readSession(t.TempDir(), spec); err == nil {
		t.Fatal("want open error")
	}
}
