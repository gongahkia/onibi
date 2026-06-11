package mcpserver

import "testing"

func TestNewDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("New panic: %v", r)
		}
	}()
	if New(Options{}) == nil {
		t.Fatal("nil server")
	}
}
