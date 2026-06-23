package pty

import (
	"bytes"
	"context"
	"testing"
	"time"
)

func TestSpawnEchoCommand(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	h, err := Spawn(ctx, SpawnOptions{
		Name: "/bin/echo",
		Args: []string{"hello pty"},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Close() })

	_, ch, unsub := h.Subscribe(context.Background(), 0)
	defer unsub()
	if err := h.Wait(); err != nil {
		t.Fatalf("wait: %v", err)
	}
	_ = h.Close()
	buf := drainSubscription(t, ch)

	if !bytes.Contains(buf.Bytes(), []byte("hello pty")) {
		t.Fatalf("expected echoed text, got %q", buf.String())
	}
}

func TestSpawnNonexistentBinary(t *testing.T) {
	_, err := Spawn(context.Background(), SpawnOptions{Name: "/definitely/not/a/binary/onibi-test"})
	if err == nil {
		t.Fatal("expected error on missing binary")
	}
}

func TestSpawnArgv0Override(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	h, err := Spawn(ctx, SpawnOptions{
		Name:  "/bin/sh",
		Argv0: "-sh",
		Args:  []string{"-c", "printf %s \"$0\""},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Close() })

	_, ch, unsub := h.Subscribe(context.Background(), 0)
	defer unsub()
	if err := h.Wait(); err != nil {
		t.Fatalf("wait: %v", err)
	}
	_ = h.Close()
	buf := drainSubscription(t, ch)
	if !bytes.Contains(buf.Bytes(), []byte("-sh")) {
		t.Fatalf("expected argv0 override, got %q", buf.String())
	}
}

func TestSpawnEmptyName(t *testing.T) {
	_, err := Spawn(context.Background(), SpawnOptions{Name: ""})
	if err == nil {
		t.Fatal("expected error on empty name")
	}
}

func TestWriteAndRead(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	h, err := Spawn(ctx, SpawnOptions{
		Name: "/bin/cat",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Close() })

	ctxSub, cancelSub := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelSub()
	_, ch, unsub := h.Subscribe(ctxSub, 0)
	defer unsub()

	// cat echoes stdin to stdout; PTY master is bidirectional
	go func() {
		_, _ = h.Write([]byte("ping\n"))
		time.Sleep(100 * time.Millisecond)
		_ = h.Close() // forces cat to exit
	}()

	var buf bytes.Buffer
	for p := range ch {
		buf.Write(p)
		if bytes.Contains(buf.Bytes(), []byte("ping")) {
			return
		}
	}
	t.Fatalf("expected to see 'ping' in PTY echo, got %q", buf.Bytes())
}

func drainSubscription(t *testing.T, ch <-chan []byte) bytes.Buffer {
	t.Helper()
	done := make(chan bytes.Buffer, 1)
	go func() {
		var buf bytes.Buffer
		for p := range ch {
			buf.Write(p)
		}
		done <- buf
	}()
	select {
	case buf := <-done:
		return buf
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for PTY subscription to close")
	}
	return bytes.Buffer{}
}
