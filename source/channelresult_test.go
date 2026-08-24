package source

import "testing"

// ChannelResult.Subscription turns a discovery hit into the subscription that
// would add it, verbatim from its Source + Channel.
func TestChannelResultSubscription(t *testing.T) {
	r := ChannelResult{Source: Twitter, Channel: "@golang", Title: "Go"}
	got := r.Subscription()
	if got.Source != Twitter || got.Channel != "@golang" {
		t.Fatalf("Subscription() = %+v, want twitter @golang", got)
	}
}
