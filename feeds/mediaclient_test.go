package feeds

import (
	"testing"

	"github.com/go-news-reader/reader/internal/httplog"
)

// TestMediaClientAlwaysUsable checks MediaClient returns a working client with
// or without a recorder — unlike loggedClient, whose nil means "keep your own
// default". A nil client here would mean no thumbnail ever downloads.
func TestMediaClientAlwaysUsable(t *testing.T) {
	plain := MediaClient(nil)
	if plain == nil || plain.Transport == nil {
		t.Fatalf("MediaClient(nil) = %+v, want a usable client", plain)
	}
	rec := httplog.NewRecorder(8)
	logged := MediaClient(rec)
	if logged == nil || logged.Transport == nil {
		t.Fatalf("MediaClient(rec) = %+v, want a usable client", logged)
	}
	// The recorder-backed client wraps the transport, so the two differ.
	if logged.Transport == plain.Transport {
		t.Fatal("the recorded client should wrap its transport in the recorder")
	}
	if plain.Timeout != logged.Timeout {
		t.Fatalf("timeouts differ: %v vs %v", plain.Timeout, logged.Timeout)
	}
}
