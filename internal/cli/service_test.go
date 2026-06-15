package cli

import (
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"
)

func TestWaitForSocketReady(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "onibi.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err == nil {
			_ = c.Close()
		}
	}()
	if !waitForSocket(context.Background(), sock, time.Second) {
		t.Fatal("socket not detected")
	}
}

func TestWaitForSocketTimeout(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "missing.sock")
	if waitForSocket(context.Background(), sock, 10*time.Millisecond) {
		t.Fatal("missing socket detected")
	}
}
