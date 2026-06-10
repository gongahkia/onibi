package daemon

import (
	"bytes"
	"sync"
	"testing"
)

func TestRingBufferBelowCap(t *testing.T) {
	r := NewRingBuffer(16)
	_, _ = r.Write([]byte("hello"))
	got := r.Snapshot()
	if !bytes.Equal(got, []byte("hello")) {
		t.Fatalf("got %q", got)
	}
	if r.Len() != 5 {
		t.Fatalf("len %d", r.Len())
	}
}

func TestRingBufferWraps(t *testing.T) {
	r := NewRingBuffer(8)
	_, _ = r.Write([]byte("abcdefghij")) // 10 bytes, cap 8 → "cdefghij"
	got := r.Snapshot()
	if !bytes.Equal(got, []byte("cdefghij")) {
		t.Fatalf("got %q", got)
	}
	if r.Len() != 8 {
		t.Fatalf("len %d", r.Len())
	}
}

func TestRingBufferMultiWrap(t *testing.T) {
	r := NewRingBuffer(4)
	_, _ = r.Write([]byte("12"))
	_, _ = r.Write([]byte("3456"))
	_, _ = r.Write([]byte("789")) // logically "3456789" → last 4: "6789"
	got := r.Snapshot()
	if !bytes.Equal(got, []byte("6789")) {
		t.Fatalf("got %q", got)
	}
}

func TestRingBufferReset(t *testing.T) {
	r := NewRingBuffer(8)
	_, _ = r.Write([]byte("hello"))
	r.Reset()
	if r.Len() != 0 {
		t.Fatalf("expected empty, got len %d", r.Len())
	}
	if got := r.Snapshot(); len(got) != 0 {
		t.Fatalf("expected empty snap, got %q", got)
	}
}

func TestRingBufferConcurrent(t *testing.T) {
	r := NewRingBuffer(1024)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_, _ = r.Write([]byte("x"))
			}
		}()
	}
	// concurrent snapshots — must not race
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = r.Snapshot()
		}()
	}
	wg.Wait()
	if r.Len() != 1024 {
		t.Fatalf("expected full, got %d", r.Len())
	}
}
