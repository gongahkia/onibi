package daemon

import (
	"sync"
	"testing"
	"time"
)

func TestRegistryAddGetRemove(t *testing.T) {
	r := NewRegistry()
	s := NewSession("s1", "test", "claude", nil, 1024)
	if err := r.Add(s); err != nil {
		t.Fatal(err)
	}
	got, err := r.Get("s1")
	if err != nil || got != s {
		t.Fatalf("get: %v %v", got, err)
	}
	r.Remove("s1")
	if _, err := r.Get("s1"); err != ErrUnknownSession {
		t.Fatalf("expected ErrUnknownSession after remove, got %v", err)
	}
}

func TestRegistryDuplicateID(t *testing.T) {
	r := NewRegistry()
	_ = r.Add(NewSession("dup", "", "claude", nil, 0))
	if err := r.Add(NewSession("dup", "", "claude", nil, 0)); err == nil {
		t.Fatal("expected dup error")
	}
}

func TestRegistryListSorted(t *testing.T) {
	r := NewRegistry()
	first := NewSession("a", "", "claude", nil, 0)
	time.Sleep(2 * time.Millisecond)
	second := NewSession("b", "", "claude", nil, 0)
	_ = r.Add(second)
	_ = r.Add(first)
	got := r.List()
	if len(got) != 2 {
		t.Fatalf("got %d", len(got))
	}
	if got[0].ID != "a" || got[1].ID != "b" {
		t.Fatalf("unexpected order: %v %v", got[0].ID, got[1].ID)
	}
}

func TestSessionTouchAndIdle(t *testing.T) {
	s := NewSession("x", "", "claude", nil, 0)
	if d := s.SinceActivity(); d > 100*time.Millisecond {
		t.Fatalf("fresh session idle %v", d)
	}
	time.Sleep(30 * time.Millisecond)
	s.Touch()
	if d := s.SinceActivity(); d > 10*time.Millisecond {
		t.Fatalf("expected near-zero idle after touch, got %v", d)
	}
}

func TestRegistryConcurrent(t *testing.T) {
	r := NewRegistry()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := NewID()
			_ = r.Add(NewSession(id, "", "claude", nil, 0))
			_, _ = r.Get(id)
		}(i)
	}
	wg.Wait()
	if got := len(r.List()); got != 50 {
		t.Fatalf("expected 50 sessions, got %d", got)
	}
}

func TestNewIDUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		id := NewID()
		if seen[id] {
			t.Fatalf("collision at %d: %s", i, id)
		}
		seen[id] = true
	}
}
