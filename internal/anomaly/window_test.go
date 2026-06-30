package anomaly

import (
	"sync"
	"testing"
	"time"
)

func TestWindowCounterTimeWindow(t *testing.T) {
	w := NewWindowCounter(time.Minute, 0)
	now := time.Date(2026, 6, 30, 1, 2, 3, 0, time.UTC)
	if got := w.Add("write:s1", now, 1); got != 1 {
		t.Fatalf("count = %d want 1", got)
	}
	if got := w.Add("write:s1", now.Add(30*time.Second), 2); got != 2 {
		t.Fatalf("count = %d want 2", got)
	}
	if got := w.Add("write:s1", now.Add(61*time.Second), 3); got != 2 {
		t.Fatalf("count = %d want 2 after prune", got)
	}
}

func TestWindowCounterTurnWindow(t *testing.T) {
	w := NewWindowCounter(0, 20)
	if got := w.Add("loop:s1", time.Time{}, 1); got != 1 {
		t.Fatalf("count = %d want 1", got)
	}
	if got := w.Add("loop:s1", time.Time{}, 20); got != 2 {
		t.Fatalf("count = %d want 2", got)
	}
	if got := w.Add("loop:s1", time.Time{}, 22); got != 2 {
		t.Fatalf("count = %d want 2 after turn prune", got)
	}
}

func TestWindowCounterConcurrent(t *testing.T) {
	w := NewWindowCounter(time.Minute, 20)
	now := time.Now()
	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		g := g
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				w.Add("key", now.Add(time.Duration(g*100+i)*time.Millisecond), g*100+i)
			}
		}()
	}
	wg.Wait()
	if w.Len() == 0 {
		t.Fatal("window empty after concurrent adds")
	}
}
