package pty

import (
	"bytes"
	"sync"
	"testing"
)

func TestRingBufferSnapshotKeepsNewestBytes(t *testing.T) {
	r := NewRingBuffer(5)
	r.Write([]byte("abc"))
	if got := r.Snapshot(); string(got) != "abc" {
		t.Fatalf("initial snapshot = %q", got)
	}
	r.Write([]byte("defg"))
	if got := r.Snapshot(); string(got) != "cdefg" {
		t.Fatalf("wrapped snapshot = %q", got)
	}
	r.Write([]byte("hijklmnop"))
	if got := r.Snapshot(); string(got) != "lmnop" {
		t.Fatalf("oversized snapshot = %q", got)
	}
}

func TestRingBufferConcurrentWriteSnapshot(t *testing.T) {
	r := NewRingBuffer(1024)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				r.Write(bytes.Repeat([]byte("x"), 32))
			}
		}()
	}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				if got := r.Snapshot(); len(got) > 1024 {
					t.Errorf("snapshot too large: %d", len(got))
				}
			}
		}()
	}
	wg.Wait()
}
