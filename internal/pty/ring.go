package pty

import "sync"

const DefaultRingSize = 256 * 1024

// RingBuffer stores the newest bytes written to it. It is safe for one or
// more writers and concurrent snapshot readers.
type RingBuffer struct {
	mu   sync.Mutex
	buf  []byte
	pos  int
	full bool
}

func NewRingBuffer(size int) *RingBuffer {
	if size <= 0 {
		size = DefaultRingSize
	}
	return &RingBuffer{buf: make([]byte, size)}
}

func (r *RingBuffer) Write(p []byte) {
	if len(p) == 0 || len(r.buf) == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(p) >= len(r.buf) {
		copy(r.buf, p[len(p)-len(r.buf):])
		r.pos = 0
		r.full = true
		return
	}
	n := copy(r.buf[r.pos:], p)
	if n < len(p) {
		copy(r.buf, p[n:])
		r.full = true
	}
	r.pos = (r.pos + len(p)) % len(r.buf)
	if r.pos == 0 {
		r.full = true
	}
}

func (r *RingBuffer) Snapshot() []byte {
	if r == nil || len(r.buf) == 0 {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.full {
		return append([]byte(nil), r.buf[:r.pos]...)
	}
	out := make([]byte, len(r.buf))
	n := copy(out, r.buf[r.pos:])
	copy(out[n:], r.buf[:r.pos])
	return out
}
