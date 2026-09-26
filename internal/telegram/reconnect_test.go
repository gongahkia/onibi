package telegram

import (
	"testing"
	"time"
)

func TestReconnectBackoffCapsAtSixtySeconds(t *testing.T) {
	want := []time.Duration{
		time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		16 * time.Second,
		32 * time.Second,
		60 * time.Second,
		60 * time.Second,
	}
	for i, expected := range want {
		if got := ReconnectBackoff(i + 1); got != expected {
			t.Fatalf("failure %d backoff = %s, want %s", i+1, got, expected)
		}
	}
	if got := ReconnectBackoff(0); got != 0 {
		t.Fatalf("zero failures backoff = %s", got)
	}
}
