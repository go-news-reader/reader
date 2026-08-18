package ui

import (
	"strings"

	"github.com/go-widgets/toolkit"

	"github.com/go-news-reader/reader/source"
)

// This file gives the reader "copy the text you see" support. Copy writes
// through the toolkit's back-end-neutral clipboard (toolkit.SetClipboardText),
// which the app has pointed at the real OS pasteboard via the native back-end
// (window.ClipboardController); in headless / test builds it is the toolkit's
// in-process clipboard. Until a cross-widget selection model lands, the copy
// target is the article currently being read — the preview pane's item, or the
// full-screen detail item — assembled as plain text suitable for a mail.

// copyToClipboard writes non-empty text to the toolkit-wide clipboard,
// reporting whether it wrote. It is the single choke point every copy action
// funnels through.
func (s *Scene) copyToClipboard(text string) bool {
	if strings.TrimSpace(text) == "" {
		return false
	}
	toolkit.SetClipboardText(text)
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

// Copy copies the current context to the system clipboard, reporting whether
// anything was copied. Precedence, most specific first: the focused web-preview
// address bar (so Cmd/Ctrl+C copies the URL you are editing, with a select-all
// highlight); then a non-empty reading-view text selection (the user picked
// exactly what to copy); then the whole article being read (detail item, else
// the preview item) as plain text.
func (s *Scene) Copy() bool {
	// A focused topbar search field copies its query (with a select-all
	// highlight), so Cmd/Ctrl+C works there like any text field.
	if s.searchFocused && s.searchEntry.Text().Get() != "" {
		s.searchCopied = true
		s.touch()
		return s.copyToClipboard(s.searchEntry.Text().Get())
	}
	if s.webPreviewItem() && s.browser.AddressFocused() {
		if _, ok := s.browser.CopyAddress(); ok {
			s.touch()
			return true
		}
	}
	if sel := s.textSel.SelectedText(); sel != "" {
		return s.copyToClipboard(sel)
	}
	it, ok := s.currentArticle()
	if !ok {
		return false
	}
	return s.copyToClipboard(articlePlainText(it))
}
