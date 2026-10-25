package process

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestOutputBoundAndCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	out, err := Run(ctx, []string{"sh", "-c", "while :; do printf abcdefghijklmnopqrstuvwxyz; done"}, t.TempDir(), 512, nil)
	if err == nil || len(out) > 600 || !strings.Contains(out, "truncated") {
		t.Fatal(len(out), err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("cancellation stalled")
	}
}
func TestChildHoldingOutputIsKilled(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := Run(ctx, []string{"sh", "-c", "sleep 30 & wait"}, t.TempDir(), 512, nil)
	if err == nil || time.Since(start) > 3*time.Second {
		t.Fatal(err)
	}
}
