package ui

// Per-subscription post counts shown next to each sidebar group: the total
// number of posts (the newsgroup's article count, or the fetched-item count for
// sources without one) and how many are unseen/new since the group was last
// viewed (derived from a persisted high-water marker).

import (
	"strings"

	"github.com/go-news-reader/reader/source"
)

// subKey is the stable per-subscription key for the seen-marker map.
func subKey(src source.Kind, channel string) string {
	return string(src) + "|" + strings.ToLower(channel)
}

// SetSeen records the per-subscription seen markers (from the app's persisted
// store) and re-renders the sidebar so the unseen counts update.
func (s *Scene) SetSeen(seen map[string]int) {
	s.seen = seen
	s.subsRev++
	s.touch()
}

// subMarkerAndTotal scans the loaded items for a subscription and returns its
// current marker (the newsgroup high-water number, else the fetched-item count)
// and total post count.
func (s *Scene) subMarkerAndTotal(sub Subscription) (marker, total int) {
	var maxCount, maxHigh, items int
	for _, it := range s.Items {
		if it.Source != sub.Source || !strings.EqualFold(it.Channel, sub.Channel) {
			continue
		}
		items++
		if it.GroupCount > maxCount {
			maxCount = it.GroupCount
		}
		if it.GroupHigh > maxHigh {
			maxHigh = it.GroupHigh
		}
	}
	total, marker = maxCount, maxHigh
	if total == 0 { // sources without a group article count: use the fetched count
		total, marker = items, items
	}
	if marker == 0 {
		marker = total
	}
	return marker, total
}

// subCounts returns the total and unseen ("new") post counts for a subscription.
func (s *Scene) subCounts(sub Subscription) (total, unseen int) {
	marker, total := s.subMarkerAndTotal(sub)
	unseen = marker - s.seen[subKey(sub.Source, sub.Channel)]
	if unseen < 0 {
		unseen = 0
	}
	if unseen > total {
		unseen = total
	}
	return total, unseen
}

// SubMarker returns the subscription's key and current marker so the app can
// persist it as "seen" when the group is opened. ok is false for a bad index.
func (s *Scene) SubMarker(index int) (key string, marker int, ok bool) {
	if index < 0 || index >= len(s.Subs) {
		return "", 0, false
	}
	sub := s.Subs[index]
	m, _ := s.subMarkerAndTotal(sub)
	return subKey(sub.Source, sub.Channel), m, true
}
