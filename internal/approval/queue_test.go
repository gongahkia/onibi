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
