// Command example-source-plugin is a minimal, self-contained reference feed
// source plugin for the news reader. It implements source.Provider with a
// static two-item feed and serves it over gRPC via sourceplugin.Serve.
//
// It is living documentation — the smallest complete plugin — and is also built
// and launched by the host's end-to-end tests. Build it and drop the binary in
// the reader's plugins directory to have the reader load an "example" source:
//
//	go build -o example-source-plugin ./cmd/example-source-plugin
//
// The provider honors an "error" channel so tests (and curious users) can see
// how a plugin's failure crosses the process boundary: querying that channel
// returns an error the host surfaces as an ordinary Go error.
package main

import (
	"context"
	"errors"

	"github.com/go-news-reader/reader/source"
	"github.com/go-news-reader/reader/sourceplugin"
)

// exampleKind is the source.Kind this plugin serves.
const exampleKind source.Kind = "example"

// provider is a trivial static source.Provider.
type provider struct{}

func (provider) Kind() source.Kind { return exampleKind }

func (provider) Feed(_ context.Context, q source.Query) (source.Result, error) {
	if q.Channel == "error" {
		return source.Result{}, errors.New("example plugin: simulated failure")
	}
	return source.Result{
		Items: []source.Item{
			{
				ID:      "example-1",
				Source:  exampleKind,
				Channel: q.Channel,
				Title:   "Hello from the example plugin",
				Body:    "This item was produced by an out-of-process gRPC plugin.",
				Author:  "example-source-plugin",
				Created: 1_700_000_000,
				Score:   42,
				Tags:    []string{"example", "plugin"},
				Media: []source.Media{
					{URL: "https://example.invalid/pic.png", Kind: source.MediaImage, Width: 320, Height: 240, AltText: "a picture"},
				},
			},
			{
				ID:       "example-2",
				Source:   exampleKind,
				Channel:  q.Channel,
				Title:    "Second example item",
				Body:     "Sources can be added without recompiling the reader.",
				Author:   "example-source-plugin",
				Created:  1_700_000_100,
				Score:    7,
				Comments: 3,
			},
		},
		Cursor: "next-page-token",
	}, nil
}

func main() { sourceplugin.Serve(provider{}) }
