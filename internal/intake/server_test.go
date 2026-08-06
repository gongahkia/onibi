package intake

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestServeRefusesActiveSocket(t *testing.T) {
	sock := testSocket(t)
	cancel := startServer(t, sock)
	defer cancel()
	if err := New(sock, nil).Serve(t.Context()); err == nil || !strings.Contains(err.Error(), "already in use") {
		t.Fatalf("err=%v", err)
	}
}

func TestRejectsMalformedAndEmptyType(t *testing.T) {
	sock := testSocket(t)
	cancel := startServer(t, sock)
	defer cancel()
	if err := rawSend(sock, []byte("not json\n")); err != nil {
		t.Fatal(err)
	}
	if err := rawSend(sock, []byte(`{}`+"\n")); err != nil {
		t.Fatal(err)
	}
}

func testSocket(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "onibi-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "sock")
}

func startServer(t *testing.T, socket string) context.CancelFunc {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	errs := make(chan error, 1)
	go func() { errs <- New(socket, nil).Serve(ctx) }()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-errs:
			cancel()
			t.Fatalf("server exited before binding: %v", err)
		default:
		}
		if SocketActive(socket, 20*time.Millisecond) {
			return cancel
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	t.Fatal("socket did not bind")
	return nil
}

func waitSocket(t *testing.T, socket string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if SocketActive(socket, 20*time.Millisecond) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("socket did not bind")
}
