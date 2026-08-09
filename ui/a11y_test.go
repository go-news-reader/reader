package ui

import (
	"testing"

	"github.com/go-widgets/toolkit"

	"github.com/go-news-reader/reader/source"
)

// TestMediaAltPrefersTheAuthorsWords checks the ordering that matters: a
// description the author actually wrote always beats the generic fallback.
func TestMediaAltPrefersTheAuthorsWords(t *testing.T) {
	it := source.Item{Media: []source.Media{
		{URL: "a", Kind: source.MediaThumbnail},
		{URL: "b", Kind: source.MediaVideo, AltText: "A rocket clears the tower"},
	}}
	if got := mediaAlt(it); got != "A rocket clears the tower" {
		t.Fatalf("mediaAlt = %q, want the author's description", got)
	}
}

// TestMediaAltFallsBackToWhatIsThere checks the no-alt-text case still says
// something: "image" tells a reader a picture exists, silence does not.
func TestMediaAltFallsBackToWhatIsThere(t *testing.T) {
	cases := []struct {
		name string
		it   source.Item
		want string
	}{
		{"video", source.Item{Media: []source.Media{
			{Kind: source.MediaThumbnail}, {Kind: source.MediaVideo}}}, "video"},
		{"gif", source.Item{Media: []source.Media{{Kind: source.MediaGIF}}}, "animated image"},
		{"photo", source.Item{Media: []source.Media{{Kind: source.MediaImage}}}, "image"},
		{"thumbnail only", source.Item{Media: []source.Media{{Kind: source.MediaThumbnail}}}, "image"},
		{"no media", source.Item{}, ""},
	}
	for _, c := range cases {
		if got := mediaAlt(c.it); got != c.want {
			t.Errorf("%s: mediaAlt = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestImageWidgetsAreDescribed is the point of the whole chain: the alt text a
// provider parsed must reach the widget that draws the picture. Every one of the
// reader's three image widgets must report it.
func TestImageWidgetsAreDescribed(t *testing.T) {
	it := source.Item{Media: []source.Media{{Kind: source.MediaImage, AltText: "Total solar eclipse"}}}
	for name, w := range map[string]toolkit.Accessible{
		"cardThumb":    &cardThumb{it: it},
		"previewImage": &previewImage{it: it},
		"detailImage":  &detailImage{it: it},
	} {
		got := w.A11y()
		if got.Role != toolkit.RoleImg {
			t.Errorf("%s Role = %q, want %q", name, got.Role, toolkit.RoleImg)
		}
		if got.Name != "Total solar eclipse" {
			t.Errorf("%s Name = %q, want the alt text", name, got.Name)
		}
	}
}

// TestEveryLocalWidgetIsAccessible mirrors the toolkit's own guard: the reader
// composes widgets of its own into a tree whose stock widgets all describe
// themselves, so a silent local widget would be the single hole in it.
func TestEveryLocalWidgetIsAccessible(t *testing.T) {
	locals := map[string]any{
		"cardThumb": &cardThumb{}, "previewImage": &previewImage{}, "detailImage": &detailImage{},
		"logRow": &logRow{}, "sideDot": &sideDot{}, "countChip": &countChip{},
	}
	for name, w := range locals {
		if _, ok := w.(toolkit.Accessible); !ok {
			t.Errorf("%s does not implement toolkit.Accessible; CollectA11y would skip it silently", name)
		}
	}
}

func TestLogRowAndChipDescribeThemselves(t *testing.T) {
	row := &logRow{e: LogEntry{Method: "GET", URL: "https://x.com/i/api", Status: 429}}
	got := row.A11y()
	if got.Role != toolkit.RoleText || got.Name != "GET https://x.com/i/api" || got.Value != "429" {
		t.Fatalf("logRow A11y = %+v", got)
	}
	// A failed exchange reports the error instead of a status it never got.
	if got := (&logRow{e: LogEntry{Method: "GET", URL: "u", Err: "dial tcp: refused"}}).A11y(); got.Value != "dial tcp: refused" {
		t.Fatalf("failed logRow Value = %q, want the error", got.Value)
	}
	if got := (&countChip{unseen: 3, total: 20}).A11y(); got.Role != toolkit.RoleStatus || got.Value != "3/20" {
		t.Fatalf("countChip A11y = %+v", got)
	}
	// The colour dot adds nothing the row's label does not already say.
	if got := (&sideDot{}).A11y(); got.Role != toolkit.RolePresentation {
		t.Fatalf("sideDot Role = %q, want presentation", got.Role)
	}
}
