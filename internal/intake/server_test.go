package intake

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestServeAndReceive(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "onibi.sock")

	var got atomic.Value // Event
	var wg sync.WaitGroup
	wg.Add(1)

	handler := func(_ context.Context, ev Event) error {
		got.Store(ev)
		wg.Done()
		return nil
	}

	srv := New(sock, handler, nil)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	go func() { _ = srv.Serve(ctx) }()

	// wait for bind
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := pingSock(sock); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if err := Send(sock, Event{Type: TypeAgentDone, Session: "s1"}); err != nil {
		t.Fatal(err)
	}
	wg.Wait()

	ev := got.Load().(Event)
	if ev.Type != TypeAgentDone || ev.Session != "s1" {
		t.Fatalf("unexpected event: %+v", ev)
	}
	if ev.TS == 0 {
		t.Fatal("expected server to fill TS")
	}
}

func TestSendFailsOpenIfNoServer(t *testing.T) {
	// nonexistent socket
	err := Send(t.TempDir()+"/nope.sock", Event{Type: TypeAgentDone})
	if err == nil {
		t.Fatal("expected error so caller can exit 0")
	}
}

func TestRejectMalformed(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "onibi.sock")

	var calls atomic.Int32
	handler := func(_ context.Context, _ Event) error {
		calls.Add(1)
		return nil
	}
	srv := New(sock, handler, nil)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = srv.Serve(ctx) }()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := pingSock(sock); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// raw connect + send junk
	if err := rawSend(sock, []byte("this is not json\n")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if calls.Load() != 0 {
		t.Fatalf("expected 0 handler calls on malformed input, got %d", calls.Load())
	}
}

func TestRejectEmptyType(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "onibi.sock")

	var calls atomic.Int32
	handler := func(_ context.Context, _ Event) error {
		calls.Add(1)
		return nil
	}
	srv := New(sock, handler, nil)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = srv.Serve(ctx) }()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := pingSock(sock); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if err := Send(sock, Event{Type: ""}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if calls.Load() != 0 {
		t.Fatalf("expected 0 handler calls on empty type, got %d", calls.Load())
	}
}
