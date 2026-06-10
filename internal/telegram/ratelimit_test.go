package telegram

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRateLimiterWaitsPerChat(t *testing.T) {
	l := NewRateLimiter(0, 100)
	start := time.Now()
	l.now = func() time.Time { return start }
	var slept time.Duration
	l.sleep = func(_ context.Context, d time.Duration) error {
		slept += d
		start = start.Add(d)
		return nil
	}
	if err := l.Wait(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if err := l.Wait(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if slept != 10*time.Millisecond {
		t.Fatalf("slept %v", slept)
	}
}

func TestRateLimiterContextCancel(t *testing.T) {
	l := NewRateLimiter(0, 1)
	start := time.Now()
	l.now = func() time.Time { return start }
	l.sleep = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
	if err := l.Wait(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := l.Wait(ctx, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}
