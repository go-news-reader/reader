package ui

import (
	"strings"

	"github.com/go-news-reader/reader/source"
)

// This file gives the reader "copy the text you see" support. The system
// clipboard writer is installed by the native window back-end (which owns the
// OS pasteboard); the scene calls it when the user presses the copy chord
// (Cmd/Ctrl+C). Until a selection model lands, the copy target is the article
// currently being read — the preview pane's item, or the full-screen detail
// item — assembled as plain text suitable for pasting into a mail.

// SetClipboardWriter installs the system-clipboard writer. The native back-end
// hands it down through the app so a copy action reaches the real OS pasteboard;
// a nil writer (headless / tests) makes copy a no-op.
func (s *Scene) SetClipboardWriter(w func(string)) { s.clipboard = w }

// copyToClipboard writes text to the system clipboard when a writer is installed
// and text is non-empty, reporting whether it wrote. It is the single choke
// point every copy action funnels through.
func (s *Scene) copyToClipboard(text string) bool {
	if s.clipboard == nil || strings.TrimSpace(text) == "" {
		return false
	}
	s.clipboard(text)
	return true
}

// articlePlainText renders an item as the plain text a reader would want on the
// clipboard: the title, the de-HTML-ified body, and the source link, each
// separated by a blank line and with empty parts dropped.
func articlePlainText(it source.Item) string {
	var parts []string
	if t := strings.TrimSpace(it.Title); t != "" {
		parts = append(parts, t)
	}
	if b := strings.TrimSpace(stripHTML(it.Body)); b != "" {
		parts = append(parts, b)
	}
	if l := strings.TrimSpace(webPreviewURL(it)); l != "" {
		parts = append(parts, l)
	} else if l := strings.TrimSpace(it.Link); l != "" {
		parts = append(parts, l)
	}
	return strings.Join(parts, "\n\n")
}

// currentArticle returns the item currently being read: the detail item in the
// reading view, otherwise the preview pane's selected item. The bool is false
// when neither is present (nothing to copy).
func (s *Scene) currentArticle() (source.Item, bool) {
	if s.mode == ModeDetail {
		return s.detail, true
	}
	if s.previewHas {
		return s.previewItem, true
	}
	return source.Item{}, false
}

// Copy copies the current reading context to the system clipboard, reporting
// whether anything was copied. With no active selection model yet, it copies the
// article being read (detail item, else the preview item) as plain text.
func (s *Scene) Copy() bool {
	it, ok := s.currentArticle()
	if !ok {
		return false
	}
	return s.copyToClipboard(articlePlainText(it))
}
