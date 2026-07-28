package app

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// seenFilePath is where the per-subscription last-seen markers are persisted (so
// the unseen/new counts survive restarts). A package var for tests.
var seenFilePath = func() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, groupCacheAppDir, "seen.json"), nil
}

// loadSeen reads the persisted seen markers (empty map on any error).
func loadSeen() map[string]int {
	m := map[string]int{}
	path, err := seenFilePath()
	if err != nil {
		return m
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return m
	}
	_ = json.Unmarshal(data, &m) // a corrupt file just resets the markers
	return m
}

// saveSeen persists the seen markers (best-effort).
func saveSeen(m map[string]int) {
	path, err := seenFilePath()
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	data, _ := json.Marshal(m) // a map[string]int always marshals
	_ = os.WriteFile(path, data, 0o644)
}

// ViewSub selects the sidebar subscription (a group filter) and marks it seen:
// its current high-water marker is recorded + persisted so its unseen count
// drops to zero. AllFilter (or any marker-less sub) just switches the filter.
func (a *App) ViewSub(index int) {
	a.scene.SetActive(index)
	key, marker, ok := a.scene.SubMarker(index)
	if !ok || marker <= 0 {
		return
	}
	if a.seen == nil {
		a.seen = map[string]int{}
	}
	a.seen[key] = marker
	saveSeen(a.seen)
	a.scene.SetSeen(a.seen)
}
