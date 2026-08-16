package ui

import (
	"strconv"

	"github.com/go-widgets/toolkit"

	"github.com/go-news-reader/reader/source"
)

// The reader composes six widgets of its own alongside the stock toolkit ones.
// The toolkit guarantees that every widget it ships answers A11y(), so a local
// widget that stayed silent would be the one hole in an otherwise complete tree
// — and a silent widget is indistinguishable from one with nothing to say.
//
// The images are the point. A post's picture is exactly what someone who cannot
// see it has no other route to, and Twitter/X is the one source whose payload
// carries the author's own description of it. That text now reaches the widget
// that draws the picture instead of stopping at the provider.

// mediaAlt is the accessible name for an item's picture: the author's alt text
// when the source reported one, else a plain statement of what is there, so a
// reader says "image" rather than nothing at all.
func mediaAlt(it source.Item) string {
	for _, m := range it.Media {
		if m.AltText != "" {
			return m.AltText
		}
	}
	for _, m := range it.Media {
		switch m.Kind {
		case source.MediaVideo:
			return "video"
		case source.MediaGIF:
			return "animated image"
		}
	}
	if len(it.Media) > 0 {
		return "image"
	}
	return ""
}

// A11y reports the preview pane's picture as an img described by its alt text.
func (w *previewImage) A11y() toolkit.A11yInfo {
	return toolkit.A11yInfo{Role: toolkit.RoleImg, Name: mediaAlt(w.it)}
}

// A11y reports the reading view's picture as an img described by its alt text.
func (w *detailImage) A11y() toolkit.A11yInfo {
	return toolkit.A11yInfo{Role: toolkit.RoleImg, Name: mediaAlt(w.it)}
}

// A11y reports one network-log row as text: the exchange, read as a line.
func (w *logRow) A11y() toolkit.A11yInfo {
	name := w.e.Method + " " + w.e.URL
	v := strconv.Itoa(w.e.Status)
	if w.e.Err != "" {
		v = w.e.Err
	}
	return toolkit.A11yInfo{Role: toolkit.RoleText, Name: name, Value: v}
}

// A11y reports the sidebar's source dot as presentational: it colour-codes the
// row it sits in and says nothing the row's own label does not.
func (d *sideDot) A11y() toolkit.A11yInfo {
	return toolkit.A11yInfo{Role: toolkit.RolePresentation}
}

// A11y reports the sidebar count chip as a status carrying "unseen/total" — the
// number a reader needs, without the colour that conveys it visually.
func (c *countChip) A11y() toolkit.A11yInfo {
	return toolkit.A11yInfo{
		Role:  toolkit.RoleStatus,
		Value: strconv.Itoa(c.unseen) + "/" + strconv.Itoa(c.total),
	}
}
