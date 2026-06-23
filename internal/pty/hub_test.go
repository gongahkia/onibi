package pty

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	cpty "github.com/creack/pty"
)

func TestHubBroadcastSubscribers(t *testing.T) {
	h := NewHub(0)
	h.broadcast([]byte("no subscribers"))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, ch, unsub := h.Subscribe(ctx, 0)
	defer unsub()
	h.broadcast([]byte("one"))
	h.broadcast([]byte("two"))
	if got := readFrame(t, ch); string(got) != "one" {
		t.Fatalf("first frame = %q", got)
	}
	if got := readFrame(t, ch); string(got) != "two" {
		t.Fatalf("second frame = %q", got)
	}

	subs := make([]<-chan []byte, 4)
	for i := range subs {
		_, subs[i], _ = h.Subscribe(ctx, 0)
	}
	h.broadcast([]byte("all"))
	for i, sub := range subs {
		if got := readFrame(t, sub); string(got) != "all" {
			t.Fatalf("subscriber %d frame = %q", i, got)
		}
	}
}

func TestHubSlowSubscriberDoesNotBlockFastSubscriber(t *testing.T) {
	h := NewHub(0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, fast, _ := h.Subscribe(ctx, 256*avgFrameSize)
	_, _, _ = h.Subscribe(ctx, 1)

	const frames = 200
	start := time.Now()
	for i := 0; i < frames; i++ {
		h.broadcast([]byte{byte(i)})
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("broadcast blocked behind slow subscriber: %s", elapsed)
	}
	for i := 0; i < frames; i++ {
		got := readFrame(t, fast)
		if got[0] != byte(i) {
			t.Fatalf("fast subscriber frame %d = %d", i, got[0])
		}
	}
}

func TestHubSlowSubscriberReceivesDropMarkerAndSnapshot(t *testing.T) {
	h := NewHub(128)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	id, ch, unsub := h.Subscribe(ctx, DefaultSubscriberBuffer)
	defer unsub()

	for i := 0; i < 9; i++ {
		frame := []byte(fmt.Sprintf("frame-%d;", i))
		h.ring.Write(frame)
		h.broadcast(frame)
	}
	h.mu.Lock()
	sub := h.subs[id]
	dropped := sub.droppedBytes
	needMarker := sub.needMarker
	h.mu.Unlock()
	if !needMarker || dropped == 0 {
		t.Fatalf("drop state needMarker=%v dropped=%d", needMarker, dropped)
	}
	h.ring.Write([]byte("next;"))
	h.broadcast([]byte("next;"))

	marker := readFrame(t, ch)
	if !bytes.HasPrefix(marker, []byte("\x00ONIBI-DROPPED:")) {
		t.Fatalf("first post-overflow frame = %q", marker)
	}
	snapshot := readFrame(t, ch)
	if !bytes.Contains(snapshot, []byte("frame-8;")) || !bytes.Contains(snapshot, []byte("next;")) {
		t.Fatalf("snapshot = %q", snapshot)
	}
	if got := readFrame(t, ch); string(got) != "next;" {
		t.Fatalf("post-snapshot frame = %q", got)
	}
}

func TestHubWriteCoalesces(t *testing.T) {
	h := NewHub(0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, ch, unsub := h.Subscribe(ctx, 0)
	defer unsub()

	_, _ = h.Write([]byte("a"))
	_, _ = h.Write([]byte("b"))
	if got := readFrame(t, ch); string(got) != "ab" {
		t.Fatalf("coalesced frame = %q", got)
	}
}

func TestHostResizeBroadcastsFrame(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	h, err := Spawn(ctx, SpawnOptions{Name: "/bin/cat"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Close() })

	_, ch, unsub := h.Subscribe(context.Background(), 0)
	defer unsub()
	if err := h.Resize(20, 60); err != nil {
		t.Fatalf("resize: %v", err)
	}
	rows, cols, err := cpty.Getsize(h.Master)
	if err != nil {
		t.Fatalf("getsize: %v", err)
	}
	if rows != 20 || cols != 60 {
		t.Fatalf("size = %dx%d", rows, cols)
	}
	if got := readFrame(t, ch); string(got) != string(ResizeFrame(20, 60)) {
		t.Fatalf("resize frame = %q", got)
	}
}

func BenchmarkHubBroadcast4Subs(b *testing.B) {
	for _, subs := range []int{1, 4, 16} {
		b.Run(fmt.Sprintf("%dsubs", subs), func(b *testing.B) {
			h := NewHub(0)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			for i := 0; i < subs; i++ {
				_, ch, _ := h.Subscribe(ctx, 1024*avgFrameSize)
				go func() {
					for range ch {
					}
				}()
			}
			payload := bytes.Repeat([]byte("x"), 1024)
			b.SetBytes(int64(len(payload)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				h.broadcast(payload)
			}
			b.StopTimer()
			h.Close()
		})
	}
}

func readFrame(t *testing.T, ch <-chan []byte) []byte {
	t.Helper()
	select {
	case p, ok := <-ch:
		if !ok {
			t.Fatal("subscription closed")
		}
		return p
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for frame")
	}
	return nil
}
