// Package hackernews adapts the standalone github.com/go-hackernews/hackernews
// client to the aggregator's source.Provider contract. Hacker News has no
// channels; Query.Sort selects the story list (top|new|best) and Query.Limit
// caps the count.
package hackernews

import (
	"context"
	"strconv"
	"strings"

	gohn "github.com/go-hackernews/hackernews"

	"github.com/go-news-reader/reader/source"
)

// defaultLimit is used when Query.Limit is unset.
const defaultLimit = 30

// client is the slice of *gohn.Client the adapter uses; an interface for tests.
type client interface {
	Stories(ctx context.Context, kind gohn.StoryKind, limit int) ([]gohn.Item, error)
}

// Provider fetches Hacker News stories as normalized items.
type Provider struct {
	client client
}

// New returns a Hacker News provider using the public Firebase API.
func New() *Provider { return &Provider{client: gohn.New()} }

// NewWithClient wraps a preconfigured client (or a fake in tests).
func NewWithClient(c client) *Provider { return &Provider{client: c} }

// Kind reports source.HackerNews.
func (p *Provider) Kind() source.Kind { return source.HackerNews }

// Feed returns a page of stories. Query.Sort picks the list; Query.Cursor is
// unused (the API exposes no stable cursor).
func (p *Provider) Feed(ctx context.Context, q source.Query) (source.Result, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	stories, err := p.client.Stories(ctx, parseKind(q.Sort), limit)
	if err != nil {
		return source.Result{}, err
	}
	items := make([]source.Item, 0, len(stories))
	for _, s := range stories {
		if s.Deleted || s.Dead {
			continue
		}
		items = append(items, mapItem(s))
	}
	return source.Result{Items: items}, nil
}

func parseKind(s string) gohn.StoryKind {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "new", "newest":
		return gohn.Newest
	case "best":
		return gohn.Best
	default:
		return gohn.Top
	}
}

func mapItem(s gohn.Item) source.Item {
	id := strconv.Itoa(s.ID)
	it := source.Item{
		ID:        id,
		Source:    source.HackerNews,
		Title:     s.Title,
		Author:    s.By,
		Body:      s.Text,
		Link:      s.URL,
		Permalink: "https://news.ycombinator.com/item?id=" + id,
		Score:     s.Score,
		Comments:  s.Descendants,
		Created:   s.Time,
	}
	return it
}
