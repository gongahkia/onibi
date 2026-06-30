package anomaly

import (
	"sync"
	"time"
)

type WindowCounter struct {
	mu        sync.Mutex
	timeLimit time.Duration
	turnLimit int
	entries   []WindowEntry
}

type WindowEntry struct {
	Key  string
	At   time.Time
	Turn int
}

func NewWindowCounter(timeLimit time.Duration, turnLimit int) *WindowCounter {
	return &WindowCounter{timeLimit: timeLimit, turnLimit: turnLimit}
}

func (w *WindowCounter) Add(key string, at time.Time, turn int) int {
	if w == nil {
		return 0
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.entries = append(w.entries, WindowEntry{Key: key, At: at, Turn: turn})
	w.pruneLocked(at, turn)
	return w.countLocked(key)
}

func (w *WindowCounter) Count(key string, at time.Time, turn int) int {
	if w == nil {
		return 0
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pruneLocked(at, turn)
	return w.countLocked(key)
}

func (w *WindowCounter) Len() int {
	if w == nil {
		return 0
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.entries)
}

func (w *WindowCounter) pruneLocked(now time.Time, turn int) {
	if w.timeLimit <= 0 && w.turnLimit <= 0 {
		return
	}
	cutoff := time.Time{}
	if w.timeLimit > 0 && !now.IsZero() {
		cutoff = now.Add(-w.timeLimit)
	}
	out := w.entries[:0]
	for _, entry := range w.entries {
		if !cutoff.IsZero() && !entry.At.IsZero() && entry.At.Before(cutoff) {
			continue
		}
		if w.turnLimit > 0 && turn > 0 && entry.Turn > 0 && turn-entry.Turn > w.turnLimit {
			continue
		}
		out = append(out, entry)
	}
	w.entries = out
}

func (w *WindowCounter) countLocked(key string) int {
	n := 0
	for _, entry := range w.entries {
		if entry.Key == key {
			n++
		}
	}
	return n
}
