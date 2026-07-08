package approval

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
)

func FuzzDecide(f *testing.F) {
	for _, seed := range []struct {
		op     byte
		edit   string
		second byte
	}{
		{0, `{"command":"ls"}`, 1},
		{1, `{"reason":"deny"}`, 2},
		{2, `{"command":"echo ok"}`, 3},
		{2, `{`, 4},
		{3, `{"reason":"cancel"}`, 0},
		{4, `{"reason":"expire"}`, 2},
	} {
		f.Add(seed.op, seed.edit, seed.second)
	}
	f.Fuzz(func(t *testing.T, op byte, edit string, second byte) {
		if len(edit) > 8192 {
			t.Skip("edit payload too large")
		}
		ctx := context.Background()
		q := New(openDB(t), DefaultTTL)
		id, ch, err := q.Request(ctx, "s", "claude", "Bash", `{"command":"original"}`)
		if err != nil {
			t.Fatal(err)
		}

		verdict := fuzzVerdict(op)
		edited := fuzzEdited(verdict, edit)
		if verdict == VerdictEdit && !json.Valid([]byte(edited)) {
			if err := q.Decide(ctx, id, VerdictEdit, edited, "", 1); err == nil {
				t.Fatal("invalid edit JSON was accepted")
			}
			assertApprovalState(t, q, id, StatePending)
			verdict = fuzzVerdict(second)
			edited = fuzzEdited(verdict, `{"command":"fallback"}`)
		}

		if err := q.Decide(ctx, id, verdict, edited, "reason", 1); err != nil {
			t.Fatalf("first decide: %v", err)
		}
		a := assertTerminalState(t, q, id)
		wantState := StateForVerdict(verdict)
		if a.State != wantState {
			t.Fatalf("state = %s want %s", a.State, wantState)
		}
		if err := q.Decide(ctx, id, fuzzVerdict(second), fuzzEdited(fuzzVerdict(second), `{"command":"second"}`), "again", 2); !errors.Is(err, ErrAlreadyDecided) {
			t.Fatalf("second decide err = %v", err)
		}
		assertApprovalState(t, q, id, wantState)
		select {
		case d := <-ch:
			if d.Verdict != verdict {
				t.Fatalf("delivered verdict = %s want %s", d.Verdict, verdict)
			}
		default:
			t.Fatal("decision not delivered")
		}
		fuzzConcurrentDecide(t, op, second)
	})
}

func fuzzConcurrentDecide(t *testing.T, a, b byte) {
	t.Helper()
	ctx := context.Background()
	q := New(openDB(t), DefaultTTL)
	id, ch, err := q.Request(ctx, "s", "claude", "Bash", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	verdicts := []Verdict{fuzzVerdict(a), fuzzVerdict(b)}
	errs := make(chan error, len(verdicts))
	var wg sync.WaitGroup
	for i, verdict := range verdicts {
		wg.Add(1)
		go func(i int, verdict Verdict) {
			defer wg.Done()
			errs <- q.Decide(ctx, id, verdict, fuzzEdited(verdict, `{"command":"concurrent"}`), "race", int64(i+1))
		}(i, verdict)
	}
	wg.Wait()
	close(errs)

	wins := 0
	already := 0
	for err := range errs {
		switch {
		case err == nil:
			wins++
		case errors.Is(err, ErrAlreadyDecided):
			already++
		default:
			t.Fatalf("concurrent decide err = %v", err)
		}
	}
	if wins != 1 || already != len(verdicts)-1 {
		t.Fatalf("concurrent decisions wins=%d already=%d", wins, already)
	}
	assertTerminalState(t, q, id)
	select {
	case <-ch:
	default:
		t.Fatal("concurrent decision not delivered")
	}
}

func fuzzVerdict(op byte) Verdict {
	switch op % 5 {
	case 0:
		return VerdictApprove
	case 1:
		return VerdictDeny
	case 2:
		return VerdictEdit
	case 3:
		return VerdictCancel
	default:
		return VerdictExpire
	}
}

func fuzzEdited(verdict Verdict, edit string) string {
	if verdict == VerdictEdit {
		return edit
	}
	return ""
}

func assertTerminalState(t *testing.T, q *Queue, id string) *Approval {
	t.Helper()
	a, err := q.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if !Terminal(a.State) {
		t.Fatalf("state is not terminal: %s", a.State)
	}
	return a
}

func assertApprovalState(t *testing.T, q *Queue, id, want string) {
	t.Helper()
	a, err := q.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if a.State != want {
		t.Fatalf("state = %s want %s", a.State, want)
	}
}
