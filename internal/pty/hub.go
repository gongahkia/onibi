package pty

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	DefaultSubscriberBuffer = 32 * 1024
	avgFrameSize            = 4 * 1024
	minSubscriberFrames     = 4
	coalesceDelay           = 16 * time.Millisecond
	coalesceFlushBytes      = 64 * 1024
)

type Hub struct {
	mu            sync.Mutex
	subs          map[uint64]*subscriber
	nextID        uint64
	ring          *RingBuffer
	seq           uint64
	coalesceBuf   []byte
	coalesceTimer *time.Timer
	transcoder    kittyGraphicsTranscoder
	closed        bool
}

type Replay struct {
	Data     []byte
	Seq      uint64
	Snapshot bool
}

type subscriber struct {
	ch           chan []byte
	done         chan struct{}
	bufBytes     int
	bufCap       int
	droppedBytes uint64
	closed       atomic.Bool
	needMarker   bool
}

func NewHub(ringSize int) *Hub {
	return &Hub{
		subs: map[uint64]*subscriber{},
		ring: NewRingBuffer(ringSize),
	}
}

func (h *Hub) Subscribe(ctx context.Context, bufCap int) (uint64, <-chan []byte, func()) {
	return h.subscribe(ctx, bufCap, true)
}

func (h *Hub) SubscribeLive(ctx context.Context, bufCap int) (uint64, <-chan []byte, func()) {
	return h.subscribe(ctx, bufCap, false)
}

func (h *Hub) ReplaySince(seq uint64) Replay {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.replaySinceLocked(seq)
}

func (h *Hub) SubscribeFrom(ctx context.Context, bufCap int, seq uint64) (Replay, <-chan []byte, func()) {
	if ctx == nil {
		ctx = context.Background()
	}
	if bufCap <= 0 {
		bufCap = DefaultSubscriberBuffer
	}
	frameCap := bufCap / avgFrameSize
	if frameCap < minSubscriberFrames {
		frameCap = minSubscriberFrames
	}
	sub := &subscriber{
		ch:     make(chan []byte, frameCap),
		done:   make(chan struct{}),
		bufCap: bufCap,
	}

	h.mu.Lock()
	replay := h.replaySinceLocked(seq)
	if h.closed {
		close(sub.ch)
		h.mu.Unlock()
		return replay, sub.ch, func() {}
	}
	h.nextID++
	id := h.nextID
	h.subs[id] = sub
	h.mu.Unlock()

	unsub := func() {
		if sub.closed.Swap(true) {
			return
		}
		close(sub.done)
		h.mu.Lock()
		delete(h.subs, id)
		close(sub.ch)
		h.mu.Unlock()
	}
	go func() {
		select {
		case <-ctx.Done():
			unsub()
		case <-sub.done:
		}
	}()
	return replay, sub.ch, unsub
}

func (h *Hub) replaySinceLocked(seq uint64) Replay {
	snapshot := h.ring.Snapshot()
	current := h.seq
	var earliest uint64
	if uint64(len(snapshot)) < current {
		earliest = current - uint64(len(snapshot))
	}
	if seq < earliest || seq > current {
		return Replay{Data: snapshot, Seq: current, Snapshot: true}
	}
	offset := int(seq - earliest)
	return Replay{Data: append([]byte(nil), snapshot[offset:]...), Seq: current}
}

func (h *Hub) subscribe(ctx context.Context, bufCap int, replay bool) (uint64, <-chan []byte, func()) {
	if ctx == nil {
		ctx = context.Background()
	}
	if bufCap <= 0 {
		bufCap = DefaultSubscriberBuffer
	}
	frameCap := bufCap / avgFrameSize
	if frameCap < minSubscriberFrames {
		frameCap = minSubscriberFrames
	}
	sub := &subscriber{
		ch:     make(chan []byte, frameCap),
		done:   make(chan struct{}),
		bufCap: bufCap,
	}

	h.mu.Lock()
	var snapshot []byte
	if replay {
		snapshot = h.ring.Snapshot()
	}
	if h.closed {
		if len(snapshot) > 0 {
			sub.sendLocked(snapshot, false, false)
		}
		close(sub.ch)
		h.mu.Unlock()
		return 0, sub.ch, func() {}
	}
	h.nextID++
	id := h.nextID
	h.subs[id] = sub
	if len(snapshot) > 0 {
		sub.sendLocked(snapshot, false, false)
	}
	h.mu.Unlock()

	unsub := func() {
		if sub.closed.Swap(true) {
			return
		}
		close(sub.done)
		h.mu.Lock()
		delete(h.subs, id)
		close(sub.ch)
		h.mu.Unlock()
	}
	go func() {
		select {
		case <-ctx.Done():
			unsub()
		case <-sub.done:
		}
	}()
	return id, sub.ch, unsub
}

func (h *Hub) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return len(p), nil
	}
	out := h.transcoder.Write(p)
	if len(out) == 0 {
		h.mu.Unlock()
		return len(p), nil
	}
	h.ring.Write(out)
	h.seq += uint64(len(out))
	h.coalesceBuf = append(h.coalesceBuf, out...)
	if len(h.coalesceBuf) >= coalesceFlushBytes {
		h.flushLocked()
	} else if h.coalesceTimer == nil {
		h.coalesceTimer = time.AfterFunc(coalesceDelay, h.flush)
	}
	h.mu.Unlock()
	return len(p), nil
}

func (h *Hub) BroadcastResize(rows, cols uint16) {
	h.broadcast(ResizeFrame(rows, cols))
}

func ResizeFrame(rows, cols uint16) []byte {
	return []byte(fmt.Sprintf("\x00ONIBI-RESIZE:%dx%d\x00", rows, cols))
}

func ParseResizeFrame(p []byte) (uint16, uint16, bool) {
	s := string(p)
	if !strings.HasPrefix(s, "\x00ONIBI-RESIZE:") || !strings.HasSuffix(s, "\x00") {
		return 0, 0, false
	}
	body := strings.TrimSuffix(strings.TrimPrefix(s, "\x00ONIBI-RESIZE:"), "\x00")
	rowsText, colsText, ok := strings.Cut(body, "x")
	if !ok {
		return 0, 0, false
	}
	rows, err := strconv.ParseUint(rowsText, 10, 16)
	if err != nil || rows == 0 {
		return 0, 0, false
	}
	cols, err := strconv.ParseUint(colsText, 10, 16)
	if err != nil || cols == 0 {
		return 0, 0, false
	}
	return uint16(rows), uint16(cols), true
}

func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.flushLocked()
	h.closed = true
	if h.coalesceTimer != nil {
		h.coalesceTimer.Stop()
		h.coalesceTimer = nil
	}
	for id, sub := range h.subs {
		delete(h.subs, id)
		if !sub.closed.Swap(true) {
			close(sub.done)
			close(sub.ch)
		}
	}
}

func (h *Hub) broadcast(p []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.broadcastLocked(p)
}

func (h *Hub) flush() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.flushLocked()
}

func (h *Hub) flushLocked() {
	if len(h.coalesceBuf) == 0 {
		if h.coalesceTimer != nil {
			h.coalesceTimer.Stop()
			h.coalesceTimer = nil
		}
		return
	}
	p := append([]byte(nil), h.coalesceBuf...)
	h.coalesceBuf = h.coalesceBuf[:0]
	if h.coalesceTimer != nil {
		h.coalesceTimer.Stop()
		h.coalesceTimer = nil
	}
	h.broadcastLocked(p)
}

func (h *Hub) broadcastLocked(p []byte) {
	if h.closed || len(p) == 0 {
		return
	}
	var snapshot []byte
	for _, sub := range h.subs {
		if sub.needMarker && snapshot == nil {
			snapshot = h.ring.Snapshot()
		}
		sub.enqueueLocked(p, snapshot)
	}
}

func (s *subscriber) enqueueLocked(p, snapshot []byte) {
	if s.closed.Load() {
		return
	}
	if s.needMarker {
		dropped := s.droppedBytes + uint64(s.clearQueuedLocked())
		s.droppedBytes = 0
		s.needMarker = false
		s.sendLocked([]byte(fmt.Sprintf("\x00ONIBI-DROPPED:%d\x00", dropped)), false, false)
		if len(snapshot) > 0 {
			s.sendLocked(snapshot, false, false)
		}
		s.sendLocked(p, false, false)
		return
	}
	s.sendLocked(p, true, true)
}

func (s *subscriber) sendLocked(p []byte, trackDrop, enforceCap bool) {
	if len(p) == 0 {
		return
	}
	frame := append([]byte(nil), p...)
	if len(s.ch) == 0 {
		s.bufBytes = 0
	}
	if enforceCap && s.bufCap > 0 {
		if len(frame) > s.bufCap {
			for len(s.ch) > 0 {
				s.dropOldestLocked(trackDrop)
			}
		} else {
			for s.bufBytes+len(frame) > s.bufCap && len(s.ch) > 0 {
				s.dropOldestLocked(trackDrop)
			}
		}
	}
	for {
		select {
		case s.ch <- frame:
			s.bufBytes += len(frame)
			return
		default:
			if !s.dropOldestLocked(trackDrop) {
				return
			}
		}
	}
}

func (s *subscriber) dropOldestLocked(trackDrop bool) bool {
	select {
	case old := <-s.ch:
		s.bufBytes -= len(old)
		if s.bufBytes < 0 {
			s.bufBytes = 0
		}
		if trackDrop {
			s.droppedBytes += uint64(len(old))
			s.needMarker = true
		}
		return true
	default:
		return false
	}
}

func (s *subscriber) clearQueuedLocked() int {
	total := 0
	for {
		select {
		case old := <-s.ch:
			total += len(old)
		default:
			s.bufBytes = 0
			return total
		}
	}
}
