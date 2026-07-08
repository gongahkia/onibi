package chatout

import (
	"context"
	"net/http"
	"reflect"
	"testing"
	"time"
)

func TestChunksRuneSafe(t *testing.T) {
	got := Chunks("ab🙂cd", 3)
	if len(got) != 2 || got[0] != "ab🙂" || got[1] != "cd" {
		t.Fatalf("chunks = %#v", got)
	}
	if got := Chunks(" ", 10); len(got) != 1 || got[0] != "(empty)" {
		t.Fatalf("empty chunks = %#v", got)
	}
}

func TestRetryAfterParsesSecondsAndDate(t *testing.T) {
	h := http.Header{"Retry-After": []string{"1.5"}}
	if got := RetryAfter(h, time.Second); got != 1500*time.Millisecond {
		t.Fatalf("seconds = %s", got)
	}
	when := time.Now().Add(2 * time.Second).UTC().Format(http.TimeFormat)
	h = http.Header{"Retry-After": []string{when}}
	if got := RetryAfter(h, time.Second); got <= 0 || got > 3*time.Second {
		t.Fatalf("date = %s", got)
	}
}

func TestSendChunksPacesBetweenChunks(t *testing.T) {
	var ops []string
	var sleeps []time.Duration
	err := SendChunks(context.Background(), "abcdef", 2, 250*time.Millisecond,
		func(_ context.Context, d time.Duration) error {
			ops = append(ops, "sleep")
			sleeps = append(sleeps, d)
			return nil
		},
		func(_ context.Context, chunk string) error {
			ops = append(ops, "send:"+chunk)
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ops, []string{"send:ab", "sleep", "send:cd", "sleep", "send:ef"}) {
		t.Fatalf("ops = %#v", ops)
	}
	if !reflect.DeepEqual(sleeps, []time.Duration{250 * time.Millisecond, 250 * time.Millisecond}) {
		t.Fatalf("sleeps = %#v", sleeps)
	}
}
