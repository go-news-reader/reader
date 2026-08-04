// Subject classification for the group content-mix estimate (GroupStats): a
// cheap, allocation-light heuristic over NNTP article subjects, kept in the
// provider (which owns the OVER scan) so it needs no UI/app dependency.
package usenet

import "strings"

// imageExts are the image file extensions an image post's subject names.
var imageExts = []string{".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp"}

// binaryExts are common non-image binary/attachment extensions (archives,
// recovery, media, disc images). Combined with a yEnc marker they identify a
// binary post even when no image extension is present.
var binaryExts = []string{".rar", ".zip", ".7z", ".par2", ".nfo", ".mkv", ".mp4", ".avi", ".iso", ".m4v", ".mov", ".mp3", ".flac", ".wav", ".epub", ".pdf"}

// isBinarySubject reports whether an article subject looks like a binary post: a
// yEnc-encoded attachment, or a subject naming a file with a known binary/image
// extension. Text posts (discussion) match neither.
func isBinarySubject(subject string) bool {
	l := strings.ToLower(subject)
	if strings.Contains(l, "yenc") {
		return true
	}
	return containsAny(l, binaryExts) || containsAny(l, imageExts)
}

// isImageSubject reports whether a subject names an image file (a subset of the
// binary posts).
func isImageSubject(subject string) bool {
	return containsAny(strings.ToLower(subject), imageExts)
}

// containsAny reports whether l contains any of subs.
func containsAny(l string, subs []string) bool {
	for _, s := range subs {
		if strings.Contains(l, s) {
			return true
		}
	}
	return false
}
