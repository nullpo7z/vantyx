package httpapi

import (
	"os"
	"sync"
)

// exportTempTracker collects intermediate export artifact paths for cleanup.
type exportTempTracker struct {
	mu    sync.Mutex
	paths []string
}

func (t *exportTempTracker) track(path string) {
	if t == nil || path == "" {
		return
	}
	t.mu.Lock()
	t.paths = append(t.paths, path)
	t.mu.Unlock()
}

func (t *exportTempTracker) cleanup() {
	if t == nil {
		return
	}
	t.mu.Lock()
	paths := t.paths
	t.paths = nil
	t.mu.Unlock()
	for _, p := range paths {
		_ = os.Remove(p)
	}
}

func (t *exportTempTracker) release(path string) {
	if t == nil || path == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for i, p := range t.paths {
		if p == path {
			t.paths = append(t.paths[:i], t.paths[i+1:]...)
			return
		}
	}
}
