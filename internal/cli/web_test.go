package cli

import (
	"path/filepath"
	"testing"
)

func TestWebListenerRequiresLoopback(t *testing.T) {
	for _, address := range []string{"127.0.0.1:8787", "[::1]:8787", "localhost:8787"} {
		if err := validateWebListen(address); err != nil {
			t.Errorf("%s: %v", address, err)
		}
	}
	for _, address := range []string{":8787", "0.0.0.0:8787", "192.168.1.2:8787", "[::]:8787", "example.com:8787"} {
		if err := validateWebListen(address); err == nil {
			t.Errorf("accepted non-loopback listener %s", address)
		}
	}
}

func TestWebStateAllowsOnlyOneServer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "web.lock")
	first, err := lockWebState(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if second, err := lockWebState(path); err == nil {
		_ = second.Close()
		t.Fatal("a second web server could recover receipts belonging to the first")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := lockWebState(path)
	if err != nil {
		t.Fatalf("lock was not released: %v", err)
	}
	_ = second.Close()
}
