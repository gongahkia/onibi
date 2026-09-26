package approval

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func FuzzDecide(f *testing.F) {
	f.Add(byte(0), byte(1))
	f.Add(byte(2), byte(3))
	f.Fuzz(func(t *testing.T, first, second byte) {
		ctx := context.Background()
		q := New(openDB(t), DefaultTTL)
		id, ch, err := q.Request(ctx, "s", "pi", "bash", `{"command":"true"}`)
		if err != nil {
			t.Fatal(err)
		}
		verdict := fuzzVerdict(first)
		if err := q.Decide(ctx, id, verdict, "first", 1); err != nil {
			t.Fatal(err)
		}
		if err := q.Decide(ctx, id, fuzzVerdict(second), "second", 2); !errors.Is(err, ErrAlreadyDecided) {
			t.Fatalf("second decision=%v", err)
		}
		if got := <-ch; got.Verdict != verdict {
			t.Fatalf("decision=%#v", got)
		}
		fuzzConcurrentDecide(t, first, second)
	})
}

func fuzzConcurrentDecide(t *testing.T, first, second byte) {
	t.Helper()
	ctx := context.Background()
	q := New(openDB(t), DefaultTTL)
	id, ch, err := q.Request(ctx, "s", "pi", "bash", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	verdicts := []Verdict{fuzzVerdict(first), fuzzVerdict(second)}
	errs := make(chan error, len(verdicts))
	var wg sync.WaitGroup
	for i, verdict := range verdicts {
		wg.Add(1)
		go func(i int, verdict Verdict) {
			defer wg.Done()
			errs <- q.Decide(ctx, id, verdict, "race", int64(i+1))
		}(i, verdict)
	}
	wg.Wait()
	close(errs)
	wins := 0
	for err := range errs {
		if err == nil {
			wins++
			continue
		}
		if !errors.Is(err, ErrAlreadyDecided) {
			t.Fatalf("decision=%v", err)
		}
	}
	if wins != 1 {
		t.Fatalf("wins=%d", wins)
	}
	if got := <-ch; got.Verdict == "" {
		t.Fatal("missing decision")
	}
}

func fuzzVerdict(value byte) Verdict {
	switch value % 4 {
	case 0:
		return VerdictApprove
	case 1:
		return VerdictDeny
	case 2:
		return VerdictCancel
	default:
		return VerdictExpire
	}
}
