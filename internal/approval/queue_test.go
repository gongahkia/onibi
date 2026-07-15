package approval

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/store"
)

func openDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "a.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestRequestAndDecideApprove(t *testing.T) {
	q := New(openDB(t), DefaultTTL)
	ctx := context.Background()

	id, ch, err := q.Request(ctx, "sess1", "claude", "Bash", `{"command":"ls"}`)
	if err != nil {
		t.Fatal(err)
	}
	if id == "" {
		t.Fatal("empty id")
	}
	if err := q.Decide(ctx, id, VerdictApprove, "", "", 1234); err != nil {
		t.Fatalf("decide: %v", err)
	}
	select {
	case d := <-ch:
		if d.Verdict != VerdictApprove {
			t.Fatalf("verdict = %s", d.Verdict)
		}
		if d.DecidedBy != 1234 {
			t.Fatalf("decided_by = %d", d.DecidedBy)
		}
	case <-time.After(time.Second):
		t.Fatal("decision not delivered")
	}
	a, _ := q.Get(ctx, id)
	if a.State != StateApproved {
		t.Fatalf("state = %s", a.State)
	}
}

func TestDecideEditedCarriesPayload(t *testing.T) {
	db := openDB(t)
	q := New(db, DefaultTTL)
	ctx := context.Background()
	id, ch, _ := q.Request(ctx, "s", "claude", "Bash", `{"command":"rm -rf /tmp/data"}`)
	edited := `{"command":"mv /tmp/data /tmp/data.bak"}`
	if err := q.Decide(ctx, id, VerdictEdit, edited, "", 1); err != nil {
		t.Fatal(err)
	}
	d := <-ch
	if d.Verdict != VerdictEdit {
		t.Fatalf("verdict = %s", d.Verdict)
	}
	if string(d.UpdatedInput) != edited {
		t.Fatalf("updated_input = %q", string(d.UpdatedInput))
	}
	audit, err := db.AuditRecent(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(audit) != 1 || !strings.Contains(audit[0].Detail, "original_sha256=") ||
		!strings.Contains(audit[0].Detail, "edited_sha256=") ||
		!strings.Contains(audit[0].Detail, "diff_sha256=") {
		t.Fatalf("audit = %#v", audit)
	}
}

func TestDecideEditedRejectsNonObjectPayload(t *testing.T) {
	q := New(openDB(t), DefaultTTL)
	for _, edited := range []string{"", "null", "[]", `"text"`, "1", "true", "{"} {
		t.Run(edited, func(t *testing.T) {
			id, _, err := q.Request(t.Context(), "s", "pi", "write", `{}`)
			if err != nil {
				t.Fatal(err)
			}
			if err := q.Decide(t.Context(), id, VerdictEdit, edited, "", 1); err == nil {
				t.Fatalf("accepted %q", edited)
			}
			a, err := q.Get(t.Context(), id)
			if err != nil || a.State != StatePending {
				t.Fatalf("approval=%+v err=%v", a, err)
			}
		})
	}
}

func TestDecideOnlyOnceWins(t *testing.T) {
	q := New(openDB(t), DefaultTTL)
	ctx := context.Background()
	id, ch, _ := q.Request(ctx, "s", "claude", "Bash", "{}")

	var wins, races atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := q.Decide(ctx, id, VerdictApprove, "", "", 1)
			switch {
			case err == nil:
				wins.Add(1)
			case errors.Is(err, ErrAlreadyDecided):
				races.Add(1)
			default:
				t.Errorf("unexpected err: %v", err)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("expected exactly 1 winner, got %d", wins.Load())
	}
	if races.Load() != 19 {
		t.Fatalf("expected 19 race-losers, got %d", races.Load())
	}
	d := <-ch
	if d.Verdict != VerdictApprove {
		t.Fatalf("verdict = %s", d.Verdict)
	}
}

func TestDecideIdempotentlyReplaysOnlySameDecision(t *testing.T) {
	q := New(openDB(t), DefaultTTL)
	ctx := context.Background()
	events, unsubscribe, err := q.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer unsubscribe()
	id, ch, err := q.Request(ctx, "s", "claude", "Bash", "{}")
	if err != nil {
		t.Fatal(err)
	}
	<-events

	results := make(chan DecisionResult, 20)
	errs := make(chan error, 20)
	var wg sync.WaitGroup
	for range cap(results) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := q.DecideIdempotently(ctx, id, VerdictApprove, "", "", 1)
			if err != nil {
				errs <- err
				return
			}
			results <- res
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	var delivered, replayed int
	for res := range results {
		if res.Delivered {
			delivered++
		}
		if res.Replayed {
			replayed++
		}
		if res.Decision.Verdict != VerdictApprove {
			t.Fatalf("decision = %#v", res.Decision)
		}
	}
	if delivered != 1 || replayed != 19 {
		t.Fatalf("delivered=%d replayed=%d", delivered, replayed)
	}
	if got := <-ch; got.Verdict != VerdictApprove {
		t.Fatalf("waiter decision = %#v", got)
	}
	if ev := <-events; ev.Type != EventDecided || ev.Decision.Verdict != VerdictApprove {
		t.Fatalf("event = %#v", ev)
	}
	select {
	case ev := <-events:
		t.Fatalf("duplicate event = %#v", ev)
	case <-time.After(50 * time.Millisecond):
	}
	if _, err := q.DecideIdempotently(ctx, id, VerdictDeny, "", "", 1); !errors.Is(err, ErrAlreadyDecided) {
		t.Fatalf("conflicting replay err = %v", err)
	}
	n, err := q.db.AuditCount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("audit count = %d", n)
	}
}

func TestDecideRejectsUnknown(t *testing.T) {
	q := New(openDB(t), DefaultTTL)
	err := q.Decide(context.Background(), "deadbeef", VerdictApprove, "", "", 1)
	if !errors.Is(err, ErrUnknownApproval) {
		t.Fatalf("expected ErrUnknownApproval, got %v", err)
	}
}

func TestExpireOverdueDelivers(t *testing.T) {
	q := New(openDB(t), DefaultTTL)
	ctx := context.Background()
	id, ch, _ := q.Request(ctx, "s", "claude", "Bash", "{}")

	// force expiry by rewriting expires_at to the past
	_, err := q.db.SQL().ExecContext(ctx,
		`UPDATE approvals SET expires_at = ? WHERE id = ?`,
		time.Now().Add(-time.Minute).Unix(), id)
	if err != nil {
		t.Fatal(err)
	}
	n, err := q.ExpireOverdue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 expired, got %d", n)
	}
	d := <-ch
	if d.Verdict != VerdictExpire {
		t.Fatalf("verdict = %s", d.Verdict)
	}
	a, _ := q.Get(ctx, id)
	if a.State != StateExpired {
		t.Fatalf("state = %s", a.State)
	}
}

func TestLateUserDecisionExpiresInsteadOfApproving(t *testing.T) {
	db := openDB(t)
	q := New(db, DefaultTTL)
	ctx := context.Background()
	id, ch, _ := q.Request(ctx, "s", "claude", "Bash", "{}")

	_, err := q.db.SQL().ExecContext(ctx,
		`UPDATE approvals SET expires_at = ? WHERE id = ?`,
		time.Now().Add(-time.Minute).Unix(), id)
	if err != nil {
		t.Fatal(err)
	}
	_, err = q.DecideWithResult(ctx, id, VerdictApprove, "", "", 1)
	if !errors.Is(err, ErrExpired) {
		t.Fatalf("expected ErrExpired, got %v", err)
	}
	d := <-ch
	if d.Verdict != VerdictExpire {
		t.Fatalf("verdict = %s", d.Verdict)
	}
	a, _ := q.Get(ctx, id)
	if a.State != StateExpired {
		t.Fatalf("state = %s", a.State)
	}
	n, err := db.AuditCount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("audit count = %d", n)
	}
}

func TestPendingReturnsOnlyUnexpiredPending(t *testing.T) {
	q := New(openDB(t), DefaultTTL)
	ctx := context.Background()
	keep, _, _ := q.Request(ctx, "s1", "claude", "Bash", "{}")
	expired, _, _ := q.Request(ctx, "s2", "claude", "Bash", "{}")
	decided, _, _ := q.Request(ctx, "s3", "claude", "Bash", "{}")
	_, err := q.db.SQL().ExecContext(ctx,
		`UPDATE approvals SET expires_at = ? WHERE id = ?`,
		time.Now().Add(-time.Minute).Unix(), expired)
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Decide(ctx, decided, VerdictDeny, "", "no", 1); err != nil {
		t.Fatal(err)
	}
	got, err := q.Pending(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != keep {
		t.Fatalf("pending = %+v", got)
	}
}

func TestDropWaiterStopsDelivery(t *testing.T) {
	q := New(openDB(t), DefaultTTL)
	ctx := context.Background()
	id, ch, _ := q.Request(ctx, "s", "claude", "Bash", "{}")
	q.DropWaiter(id)
	// decide should not panic and channel should not receive
	if err := q.Decide(ctx, id, VerdictApprove, "", "", 1); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ch:
		t.Fatal("channel received after DropWaiter")
	case <-time.After(50 * time.Millisecond):
		// good — orphaned waiter, no delivery
	}
	// row still terminal in DB (audit trail preserved)
	a, _ := q.Get(ctx, id)
	if a.State != StateApproved {
		t.Fatalf("state = %s", a.State)
	}
}

func TestCancelDeliversCancelled(t *testing.T) {
	q := New(openDB(t), DefaultTTL)
	ctx := context.Background()
	id, ch, _ := q.Request(ctx, "s", "claude", "Bash", "{}")
	if err := q.Cancel(ctx, id, "shutting down"); err != nil {
		t.Fatal(err)
	}
	d := <-ch
	if d.Verdict != VerdictCancel {
		t.Fatalf("verdict = %s", d.Verdict)
	}
	if d.Reason != "shutting down" {
		t.Fatalf("reason = %q", d.Reason)
	}
	a, _ := q.Get(ctx, id)
	if a.Reason != "shutting down" {
		t.Fatalf("stored reason = %q", a.Reason)
	}
}

func TestDenyPersistsReasonAndDecider(t *testing.T) {
	q := New(openDB(t), DefaultTTL)
	ctx := context.Background()
	id, ch, _ := q.Request(ctx, "s", "claude", "Bash", "{}")
	if err := q.Decide(ctx, id, VerdictDeny, "", "too risky", 42); err != nil {
		t.Fatal(err)
	}
	d := <-ch
	if d.Reason != "too risky" || d.DecidedBy != 42 {
		t.Fatalf("decision = %#v", d)
	}
	a, _ := q.Get(ctx, id)
	if a.Reason != "too risky" || a.DecidedBy != 42 {
		t.Fatalf("approval = %#v", a)
	}
}

func TestSetMessagePersists(t *testing.T) {
	q := New(openDB(t), DefaultTTL)
	ctx := context.Background()
	id, _, _ := q.Request(ctx, "s", "claude", "Bash", "{}")
	if err := q.SetMessage(ctx, id, 100, 200); err != nil {
		t.Fatal(err)
	}
	a, _ := q.Get(ctx, id)
	if a.ChatID != 100 || a.MsgID != 200 {
		t.Fatalf("got chat=%d msg=%d", a.ChatID, a.MsgID)
	}
}

func TestSubscribeReceivesQueueTransitions(t *testing.T) {
	q := New(openDB(t), DefaultTTL)
	events, unsub, err := q.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer unsub()
	ctx := context.Background()
	id, _, err := q.Request(ctx, "s", "claude", "Bash", `{"command":"ls"}`, "diff")
	if err != nil {
		t.Fatal(err)
	}
	ev := readApprovalEvent(t, events)
	if ev.Type != EventRequested || ev.Approval.ID != id || ev.Approval.State != StatePending || ev.Approval.UnifiedDiff != "diff" {
		t.Fatalf("request event = %#v", ev)
	}
	if err := q.Decide(ctx, id, VerdictDeny, "", "no", 1); err != nil {
		t.Fatal(err)
	}
	ev = readApprovalEvent(t, events)
	if ev.Type != EventDecided || ev.Approval.State != StateDenied || ev.Decision.Verdict != VerdictDeny {
		t.Fatalf("decision event = %#v", ev)
	}
	expID, _, err := q.Request(ctx, "s", "claude", "Bash", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	_ = readApprovalEvent(t, events)
	_, err = q.db.SQL().ExecContext(ctx, `UPDATE approvals SET expires_at = ? WHERE id = ?`, time.Now().Add(-time.Minute).Unix(), expID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.ExpireOverdue(ctx); err != nil {
		t.Fatal(err)
	}
	ev = readApprovalEvent(t, events)
	if ev.Type != EventExpired || ev.Approval.State != StateExpired || ev.Decision.Verdict != VerdictExpire {
		t.Fatalf("expiry event = %#v", ev)
	}
}

func TestSubscribeRejectsWhenFullAndUnsubscribeReleasesSlot(t *testing.T) {
	q := New(openDB(t), DefaultTTL)
	q.MaxSubscribers = 2
	_, unsub1, err := q.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer unsub1()
	_, unsub2, err := q.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer unsub2()
	ch, _, err := q.Subscribe()
	if !errors.Is(err, ErrSubscribersFull) {
		t.Fatalf("err = %v want ErrSubscribersFull", err)
	}
	if _, ok := <-ch; ok {
		t.Fatal("rejected subscriber channel left open")
	}
	unsub1()
	_, unsub3, err := q.Subscribe()
	if err != nil {
		t.Fatalf("subscribe after unsubscribe: %v", err)
	}
	defer unsub3()
}

func TestRequestWithBudgetWarningPublishesWarning(t *testing.T) {
	q := New(openDB(t), DefaultTTL)
	events, unsub, err := q.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer unsub()
	ctx := context.Background()
	warn := &BudgetWarning{
		Scope:           "session",
		CurrentTokens:   8,
		PredictedTokens: 5,
		ProjectedTokens: 13,
		LimitTokens:     10,
		RemainingTokens: 2,
		OnOverrun:       "interrupt",
		Message:         "Predicted session budget overrun",
	}
	id, _, err := q.RequestWithBudgetWarning(ctx, "s", "claude", "Bash", `{"command":"ls"}`, "", warn)
	if err != nil {
		t.Fatal(err)
	}
	warn.Scope = "mutated"
	ev := readApprovalEvent(t, events)
	if ev.Type != EventRequested || ev.Approval.ID != id || ev.Approval.BudgetWarn == nil {
		t.Fatalf("event = %#v", ev)
	}
	if ev.Approval.BudgetWarn.Scope != "session" || ev.Approval.BudgetWarn.ProjectedTokens != 13 {
		t.Fatalf("warning = %#v", ev.Approval.BudgetWarn)
	}
}

func TestRequestSilentSkipsRequestedEvent(t *testing.T) {
	q := New(openDB(t), DefaultTTL)
	events, unsub, err := q.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer unsub()
	ctx := context.Background()
	id, ch, err := q.RequestSilent(ctx, "s", "claude", "Bash", `{"command":"ls"}`)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-events:
		t.Fatalf("unexpected event before decision: %#v", ev)
	default:
	}
	if err := q.Decide(ctx, id, VerdictApprove, "", "", 0); err != nil {
		t.Fatal(err)
	}
	if got := <-ch; got.Verdict != VerdictApprove {
		t.Fatalf("verdict = %s", got.Verdict)
	}
	ev := readApprovalEvent(t, events)
	if ev.Type != EventDecided || ev.Approval.ID != id {
		t.Fatalf("event = %#v", ev)
	}
}

func readApprovalEvent(t *testing.T, events <-chan Event) Event {
	t.Helper()
	select {
	case ev := <-events:
		return ev
	case <-time.After(time.Second):
		t.Fatal("event not delivered")
		return Event{}
	}
}
