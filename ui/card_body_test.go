package ui

import (
	"strings"
	"testing"

	"github.com/go-news-reader/reader/source"
)

// tweet builds an untitled social post: the whole content lives in Body, which
// is exactly the shape Twitter/X, Mastodon and Bluesky items have.
func tweet(id, body string) source.Item {
	return source.Item{ID: id, Source: source.Twitter, Channel: "NASA", Author: "NASA",
		Body: body, Score: -1, Comments: -1}
}

// TestCardTextPrefersTitle checks the headline stays the title whenever there is
// one, so every titled source renders exactly as before.
func TestCardTextPrefersTitle(t *testing.T) {
	it := source.Item{Title: "The title", Body: "the body"}
	if got := cardText(it); got != "The title" {
		t.Fatalf("cardText = %q, want the title", got)
	}
	// A surrounding-whitespace title is still a title, and is trimmed.
	if got := cardText(source.Item{Title: "  padded  ", Body: "b"}); got != "padded" {
		t.Fatalf("cardText = %q, want %q", got, "padded")
	}
}

// TestCardTextFallsBackToBody checks the fallback that makes untitled social
// posts legible: the body is used, HTML-stripped, and flattened to one paragraph.
func TestCardTextFallsBackToBody(t *testing.T) {
	it := tweet("1", "Look at this\n\nrocket   launch <b>now</b> &amp; later")
	const want = "Look at this rocket launch now & later"
	if got := cardText(it); got != want {
		t.Fatalf("cardText = %q, want %q", got, want)
	}
	// A whitespace-only title does not count as a title.
	if got := cardText(source.Item{Title: "   ", Body: "real text"}); got != "real text" {
		t.Fatalf("cardText = %q, want the body", got)
	}
	// Neither title nor body is empty, not a crash.
	if got := cardText(source.Item{}); got != "" {
		t.Fatalf("cardText = %q, want empty", got)
	}
}

func TestCollapseSpace(t *testing.T) {
	cases := map[string]string{
		"a  b":           "a b",
		"a\n\nb\tc":      "a b c",
		"  lead\n":       "lead",
		"":               "",
		" \n\t ":         "",
		"one":            "one",
		"a\u00a0\u00a0b": "a b", // a no-break space is whitespace to strings.Fields
	}
	for in, want := range cases {
		if got := collapseSpace(in); got != want {
			t.Fatalf("collapseSpace(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestCardMaxLines checks a body headline is allowed more lines than a title, so
// a short post reads in full instead of stopping at a three-line ellipsis.
func TestCardMaxLines(t *testing.T) {
	if got := cardMaxLines(source.Item{Title: "t"}); got != cardTitleMaxLines {
		t.Fatalf("titled cap = %d, want %d", got, cardTitleMaxLines)
	}
	if got := cardMaxLines(tweet("1", "b")); got != cardBodyMaxLines {
		t.Fatalf("body cap = %d, want %d", got, cardBodyMaxLines)
	}
	if cardBodyMaxLines <= cardTitleMaxLines {
		t.Fatal("a body headline must be allowed more lines than a title")
	}
}

// TestUntitledCardRendersItsBody is the regression this whole change exists for:
// before it, an item with no Title produced a blank card. The headline lines now
// come from the body, and the card grows to hold them.
func TestUntitledCardRendersItsBody(t *testing.T) {
	s := wrapScene()
	s.layout()
	it := tweet("tw", "It's a great time to tune in and stream space with us! "+
		"Don't miss out — keep an eye on nasa.gov/live for the latest updates.")

	lines := s.cardTitleLines(it, 220)
	if len(lines) < 2 {
		t.Fatalf("body lines = %d (%q), want the body to wrap", len(lines), lines)
	}
	joined := strings.Join(lines, " ")
	if !strings.Contains(joined, "great time to tune in") {
		t.Fatalf("rendered lines %q do not carry the body text", lines)
	}
	// Blank: exactly what the bug looked like.
	if strings.TrimSpace(joined) == "" {
		t.Fatal("untitled card rendered blank")
	}
	if h := s.cardHeight(it, 220); h <= s.m.rowH {
		t.Fatalf("multi-line body card height = %d, should exceed rowH %d", h, s.m.rowH)
	}
}

// TestUntitledCardCapsAtBodyMaxLines checks the higher cap is still a cap: a
// pathologically long body is clamped and visibly ellipsised.
func TestUntitledCardCapsAtBodyMaxLines(t *testing.T) {
	s := wrapScene()
	s.layout()
	it := tweet("long", strings.Repeat("wrap ", 80))
	lines := s.cardTitleLines(it, 130)
	if len(lines) != cardBodyMaxLines {
		t.Fatalf("lines = %d, want the body cap %d", len(lines), cardBodyMaxLines)
	}
	if !strings.HasSuffix(lines[cardBodyMaxLines-1], "…") {
		t.Fatalf("last line %q should be ellipsised", lines[cardBodyMaxLines-1])
	}
	wantH := s.m.rowH + (cardBodyMaxLines-1)*s.titleLineH()
	if h := s.cardHeight(it, 130); h != wantH {
		t.Fatalf("capped card height = %d, want %d", h, wantH)
	}
}

// TestSameItemDistinguishesUntitledPosts checks post identity survives the
// fallback: two untitled posts sharing an (empty) ID are told apart by their
// bodies, and an item is still equal to itself.
func TestSameItemDistinguishesUntitledPosts(t *testing.T) {
	a := tweet("", "first post")
	b := tweet("", "second post")
	if sameItem(a, b) {
		t.Fatal("different bodies must not be the same item")
	}
	if !sameItem(a, a) {
		t.Fatal("an item must equal itself")
	}
	// Different sources never match, even with identical text.
	c := a
	c.Source = source.Mastodon
	if sameItem(a, c) {
		t.Fatal("different sources must not be the same item")
	}
}

// TestUntitledCardSpritesDoNotCollide checks the sprite cache is keyed on the
// rendered headline, so two untitled posts do not blit each other's bitmap.
func TestUntitledCardSpritesDoNotCollide(t *testing.T) {
	s := wrapScene()
	s.layout()
	a := tweet("a", "alpha post text")
	b := tweet("b", "a considerably longer beta post text that wraps onto a second line for sure")
	th := s.theme
	onAccent, muteS := themeOnAccent(th), mute(th.OnSurface, th.Surface)
	spA := s.cardSprite(a, 260, onAccent, muteS)
	spB := s.cardSprite(b, 260, onAccent, muteS)
	if spA == spB {
		t.Fatal("distinct posts must not share one sprite")
	}
	if spA.Bounds().Dy() == spB.Bounds().Dy() {
		t.Fatalf("the wrapping post should be taller: %d vs %d", spA.Bounds().Dy(), spB.Bounds().Dy())
	}
	// The same item hits the cache rather than re-rasterising.
	if again := s.cardSprite(a, 260, onAccent, muteS); again != spA {
		t.Fatal("second call should return the cached sprite")
	}
}
