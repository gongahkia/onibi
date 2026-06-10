package daemon

import "sync"

// RingBuffer is a fixed-capacity byte ring used to retain the most recent
// PTY output for tail rendering. Safe for concurrent Write + Snapshot.
// Older bytes are silently overwritten when capacity is exceeded.
type RingBuffer struct {
	mu       sync.Mutex
	buf      []byte
	cap      int
	wpos     int  // next write position
	full     bool // true once we've wrapped at least once
}

// NewRingBuffer returns a buffer of size bytes. Must be > 0.
func NewRingBuffer(size int) *RingBuffer {
	if size <= 0 {
		size = 1
	}
	return &RingBuffer{buf: make([]byte, size), cap: size}
}

// Write appends p to the ring, overwriting oldest bytes if needed. Never
// returns an error — io.Writer contract satisfied for use with io.Copy.
func (r *RingBuffer) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, b := range p {
		r.buf[r.wpos] = b
		r.wpos++
		if r.wpos == r.cap {
			r.wpos = 0
			r.full = true
		}
	}
	return len(p), nil
}

// Snapshot returns a copy of the buffer's logical contents in chronological
// order (oldest first). Cheap when called occasionally; not designed for
// hot-path reads.
func (r *RingBuffer) Snapshot() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.full {
		out := make([]byte, r.wpos)
		copy(out, r.buf[:r.wpos])
		return out
	}
	out := make([]byte, r.cap)
	n := copy(out, r.buf[r.wpos:])
	copy(out[n:], r.buf[:r.wpos])
	return out
}

// Len returns the number of bytes currently retained.
func (r *RingBuffer) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.full {
		return r.cap
	}
	return r.wpos
}

// Reset discards all stored bytes.
func (r *RingBuffer) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.wpos = 0
	r.full = false
}
