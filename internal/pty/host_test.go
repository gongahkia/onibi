package pty

import (
	"bytes"
	"context"
	"io"
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

	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(&buf, h.Master)
		close(done)
	}()
	if err := h.Wait(); err != nil {
		t.Fatalf("wait: %v", err)
	}
	_ = h.Close()
	<-done

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

	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(&buf, h.Master)
		close(done)
	}()
	if err := h.Wait(); err != nil {
		t.Fatalf("wait: %v", err)
	}
	_ = h.Close()
	<-done
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

	// cat echoes stdin to stdout; PTY master is bidirectional
	go func() {
		_, _ = h.Write([]byte("ping\n"))
		time.Sleep(100 * time.Millisecond)
		_ = h.Close() // forces cat to exit
	}()

	got := make([]byte, 1024)
	deadline := time.Now().Add(3 * time.Second)
	var total int
	for time.Now().Before(deadline) {
		n, err := h.Master.Read(got[total:])
		if n > 0 {
			total += n
			if bytes.Contains(got[:total], []byte("ping")) {
				return
			}
		}
		if err != nil {
			break
		}
	}
	t.Fatalf("expected to see 'ping' in PTY echo, got %q", got[:total])
}
