package ui

import (
	"strings"
	"testing"

	"github.com/go-news-reader/reader/internal/settings"
	"github.com/go-news-reader/reader/source"
)

// redditTextPost is a Reddit self/text post shown in the scrolling reader pane
// (a non-empty Body, no external Link), so its comment thread renders below.
func redditTextPost() source.Item {
	return source.Item{
		ID: "rp", Source: source.Reddit, Channel: "golang", Author: "op", Score: 12,
		Title: "A self post that has some comments",
		Body:  strings.Repeat("post body words ", 10),
	}
}

// commentScene builds a wide scene previewing a Reddit text post, with the given
// comment thread already delivered (as the app would after a fetch).
func commentScene(t *testing.T, thread []source.Comment) *Scene {
	t.Helper()
	s := New(1200, 800, ThemeFor(OSMac, false))
	s.SetSubs(nil)
	it := redditTextPost()
	s.SetItems([]source.Item{it})
	s.SelectPreview(it)
	s.SetComments(it.ID, thread)
	return s
}

// TestCommentsRenderIndentAndWidth: comments appear as a bounded, threaded
// section — the heading counts them, indent strictly increases with depth, and
// no wrapped comment line overflows the pane's right edge, even at max text
// scale.
func TestCommentsRenderIndentAndWidth(t *testing.T) {
	thread := []source.Comment{
		{Author: "alice", Body: strings.Repeat("top-level comment words ", 8), Score: 5, Depth: 0},
		{Author: "bob", Body: strings.Repeat("first reply words ", 8), Score: 3, Depth: 1},
		{Author: "carol", Body: strings.Repeat("deep reply words ", 8), Score: 1, Depth: 2},
	}
	s := commentScene(t, thread)
	buf := make([]byte, s.W*s.H*4)

	// Content height WITHOUT comments (a bare text post) vs WITH, at the same
	// scale: the section must add vertical extent, all of it below the body.
	bare := New(1200, 800, ThemeFor(OSMac, false))
	bare.SetSubs(nil)
	bit := redditTextPost()
	bare.SetItems([]source.Item{bit})
	bare.SelectPreview(bit)
	bare.Draw(make([]byte, bare.W*bare.H*4))
	noComments := bare.previewContent()

	for _, scale := range []float64{1.0, settings.MaxPreviewTextScale} {
		s.SetPreviewTextScale(scale)
		s.Draw(buf)
		d := s.previewContent()
		_, innerW := s.previewInner()

		if d.commentsHeading != "3 comments" {
			t.Fatalf("scale %v heading = %q, want \"3 comments\"", scale, d.commentsHeading)
		}
		if len(d.comments) != 3 {
			t.Fatalf("scale %v: laid out %d comments, want 3", scale, len(d.comments))
		}
		if d.sectionH <= 0 {
			t.Fatalf("scale %v: comment section has no height", scale)
		}
		// Indent strictly increases with depth (0 < 1 < 2).
		if !(d.comments[0].inset < d.comments[1].inset && d.comments[1].inset < d.comments[2].inset) {
			t.Fatalf("scale %v: indent not increasing with depth: %d %d %d",
				scale, d.comments[0].inset, d.comments[1].inset, d.comments[2].inset)
		}
		if d.comments[0].inset != 0 {
			t.Fatalf("scale %v: top-level comment should not be indented, got %d", scale, d.comments[0].inset)
		}
		// No comment line overflows the pane: each wrapped line fits within the
		// column width remaining after its indent.
		for ci, c := range d.comments {
			avail := innerW - c.inset
			if c.metaFace.width(c.meta) > avail {
				t.Fatalf("scale %v comment %d meta overflows: %d > %d", scale, ci, c.metaFace.width(c.meta), avail)
			}
			for _, ln := range c.bodyLines {
				if w := c.bodyFace.width(ln); w > avail {
					t.Fatalf("scale %v comment %d body line overflows: %q %d > %d", scale, ci, ln, w, avail)
				}
			}
		}
	}

	// The comment section makes the content taller than the same post without it.
	s.SetPreviewTextScale(1.0)
	s.Draw(buf)
	withComments := s.previewContent()
	if !(withComments.height > noComments.height) {
		t.Fatalf("comments did not add height: %d !> %d", withComments.height, noComments.height)
	}
	// The scroll content height reflects the section (pane scrolls over comments).
	if s.previewScroll.contentH != withComments.height {
		t.Fatalf("scroll content height %d != laid-out height %d", s.previewScroll.contentH, withComments.height)
	}
}

// TestCommentsAreSelectable: the comment meta + body join the pane's text
// selection — their runs are in the frame's selectable accumulator and land
// inside the (selectable) preview pane.
func TestCommentsAreSelectable(t *testing.T) {
	body := "a uniquely-selectable comment body"
	s := commentScene(t, []source.Comment{{Author: "zed", Body: body, Score: 9, Depth: 0}})
	buf := make([]byte, s.W*s.H*4)
	s.Draw(buf)

	// The comment body's wrapped run is among the frame's selectable runs.
	found := false
	for _, r := range s.selAccum {
		if strings.Contains(r.Text, "uniquely-selectable") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("comment body run not present in the selectable accumulator")
	}
	// A drag over the pane selects text (comments are inside a selectable surface).
	pr := s.previewR
	if !s.SelectableAt(pr.X+pr.W/2, pr.Y+pr.H-8) {
		t.Fatal("the comment region of the preview pane should be selectable")
	}
}

// TestCommentsLoadingLine: while a fetch is in flight (no thread yet) the pane
// shows a loading line and no heading; the section still has height.
func TestCommentsLoadingLine(t *testing.T) {
	s := New(1200, 800, ThemeFor(OSMac, false))
	s.SetSubs(nil)
	it := redditTextPost()
	s.SetItems([]source.Item{it})
	s.SelectPreview(it)
	s.SetCommentsLoading(it.ID, true)

	buf := make([]byte, s.W*s.H*4)
	s.Draw(buf)
	d := s.previewContent()
	if !d.commentsLoading {
		t.Fatal("loading flag not set in laid-out body")
	}
	if d.commentsHeading != "" {
		t.Fatalf("heading should be empty while loading, got %q", d.commentsHeading)
	}
	if d.sectionH <= 0 {
		t.Fatal("loading section should still reserve height")
	}
	// The loading line is drawn (its run is selectable).
	found := false
	for _, r := range s.selAccum {
		if strings.Contains(r.Text, "Loading comments") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("loading line run not present after Draw")
	}
	// Clearing the loading flag directly (v=false) also drops it.
	s.SetCommentsLoading(it.ID, false)
	if s.CommentsLoading(it.ID) {
		t.Fatal("SetCommentsLoading(false) should clear the flag")
	}
	if _, ok := s.PreviewComments(it.ID); ok {
		t.Fatal("no thread delivered yet → PreviewComments not ok")
	}
	// Delivering the thread clears loading and shows the heading.
	s.SetCommentsLoading(it.ID, true)
	s.SetComments(it.ID, []source.Comment{{Author: "a", Body: "hi", Depth: 0}})
	if s.CommentsLoading(it.ID) {
		t.Fatal("delivery should clear the loading flag")
	}
	if cs, ok := s.PreviewComments(it.ID); !ok || len(cs) != 1 || cs[0].Body != "hi" {
		t.Fatalf("PreviewComments after delivery = %+v ok=%v", cs, ok)
	}
	s.Draw(buf)
	if s.previewContent().commentsHeading != "1 comment" {
		t.Fatalf("post-delivery heading = %q, want \"1 comment\"", s.previewContent().commentsHeading)
	}
}

// TestCommentsDeepIndentCaps: a very deep thread at max text scale stops
// indenting past 3/4 of the column (the deep clamp), so text keeps room.
func TestCommentsDeepIndentCaps(t *testing.T) {
	// The UI must stay robust for any depth (independent of the provider's own
	// depth cap), so feed a thread deep enough to march past the 3/4-width clamp.
	var thread []source.Comment
	for depth := 0; depth < 40; depth++ {
		thread = append(thread, source.Comment{Author: "u", Body: "reply body here", Score: 1, Depth: depth})
	}
	s := commentScene(t, thread)
	s.SetPreviewTextScale(settings.MaxPreviewTextScale)
	s.Draw(make([]byte, s.W*s.H*4))
	d := s.previewContent()
	_, innerW := s.previewInner()
	capX := innerW * 3 / 4
	sawCap := false
	for _, c := range d.comments {
		if c.inset > capX {
			t.Fatalf("comment inset %d exceeds 3/4 cap %d", c.inset, capX)
		}
		if c.inset == capX {
			sawCap = true
		}
		// Even capped, a usable text width remains.
		if innerW-c.inset < innerW/4 {
			t.Fatalf("capped inset left too little width: %d", innerW-c.inset)
		}
	}
	if !sawCap {
		t.Fatal("deep thread at max scale should have hit the indent cap")
	}
}

// TestCommentsHeadingText covers the heading pluralisation.
func TestCommentsHeadingText(t *testing.T) {
	cases := map[int]string{0: "No comments", 1: "1 comment", 2: "2 comments", 7: "7 comments"}
	for n, want := range cases {
		if got := commentsHeading(n); got != want {
			t.Errorf("commentsHeading(%d) = %q, want %q", n, got, want)
		}
	}
}

// TestCommentMetaLine covers the muted meta line: a missing author reads
// "[deleted]", and a zero timestamp drops the date.
func TestCommentMetaLine(t *testing.T) {
	dated := commentMetaLine(source.Comment{Author: "amy", Score: 4, Created: 1710000000})
	if !strings.HasPrefix(dated, "amy · 4 pts · ") {
		t.Fatalf("dated meta = %q", dated)
	}
	if got := commentMetaLine(source.Comment{Score: 0}); got != "[deleted] · 0 pts" {
		t.Fatalf("deleted/undated meta = %q", got)
	}
}

// TestCommentsNoneForNonRedditPreview: a text post with no delivered/loading
// thread renders no comment section (the early-return path), so a Usenet or HN
// text preview is unaffected.
func TestCommentsNoneForNonRedditPreview(t *testing.T) {
	s := New(1200, 800, ThemeFor(OSMac, false))
	s.SetSubs(nil)
	it := source.Item{ID: "hn", Source: source.HackerNews, Title: "story", Body: "text body"}
	s.SetItems([]source.Item{it})
	s.SelectPreview(it)
	s.Draw(make([]byte, s.W*s.H*4))
	d := s.previewContent()
	if d.sectionH != 0 || len(d.comments) != 0 || d.commentsHeading != "" || d.commentsLoading {
		t.Fatalf("unexpected comment section for a plain preview: %+v", d)
	}
}
