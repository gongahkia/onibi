package daemon

import "sync"

type RingBuffer struct {
	mu    sync.RWMutex
	data  []byte
	limit int
}

func NewRingBuffer(limit int) *RingBuffer {
	if limit < 4096 {
		limit = 64 * 1024
	}
	return &RingBuffer{limit: limit}
}

func (b *RingBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	b.data = append(b.data, p...)
	if len(b.data) > b.limit {
		b.data = append([]byte(nil), b.data[len(b.data)-b.limit:]...)
	}
	b.mu.Unlock()
	return len(p), nil
}

func (b *RingBuffer) Snapshot() []byte {
	b.mu.RLock()
	out := append([]byte(nil), b.data...)
	b.mu.RUnlock()
	return out
}

func (b *RingBuffer) Reset() { b.mu.Lock(); b.data = nil; b.mu.Unlock() }
