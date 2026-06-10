package daemon

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"

	"github.com/gongahkia/onibi/internal/approval"
	"github.com/gongahkia/onibi/internal/auth"
	"github.com/gongahkia/onibi/internal/store"
	"github.com/gongahkia/onibi/internal/telegram"
)

func newApprovalDaemon(t *testing.T) *Daemon {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "d.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	owner := &auth.Owner{}
	if err := auth.SetOwner(context.Background(), db, owner, 100); err != nil {
		t.Fatal(err)
	}
	return New(Options{DB: db, Owner: owner})
}

func TestReplyEditDecidesApproval(t *testing.T) {
	d := newApprovalDaemon(t)
	ctx := context.Background()
	id, ch, err := d.Queue.Request(ctx, "s", "claude", "Bash", `{"command":"rm x"}`)
	if err != nil {
		t.Fatal(err)
	}
	d.pendingEdits[100] = id

	err = d.onReply(ctx, nil, &models.Message{
		From: &models.User{ID: 100},
		Chat: models.Chat{ID: 100},
		Text: `{"command":"echo ok"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	dec := <-ch
	if dec.Verdict != approval.VerdictEdit {
		t.Fatalf("verdict = %s", dec.Verdict)
	}
	if string(dec.UpdatedInput) != `{"command":"echo ok"}` {
		t.Fatalf("updated = %s", dec.UpdatedInput)
	}
	a, _ := d.Queue.Get(ctx, id)
	if a.State != approval.StateEdited {
		t.Fatalf("state = %s", a.State)
	}
}

func TestReplyInvalidJSONKeepsEditPending(t *testing.T) {
	d := newApprovalDaemon(t)
	ctx := context.Background()
	id, _, err := d.Queue.Request(ctx, "s", "claude", "Bash", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	d.pendingEdits[100] = id

	err = d.onReply(ctx, nil, &models.Message{
		From: &models.User{ID: 100},
		Chat: models.Chat{ID: 100},
		Text: `{`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := d.pendingEdits[100]; got != id {
		t.Fatalf("pending edit = %q", got)
	}
	a, _ := d.Queue.Get(ctx, id)
	if a.State != approval.StatePending {
		t.Fatalf("state = %s", a.State)
	}
}

func TestCallbackExpiredMarksExpired(t *testing.T) {
	d := newApprovalDaemon(t)
	ctx := context.Background()
	id, ch, err := d.Queue.Request(ctx, "s", "claude", "Bash", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = d.DB.SQL().ExecContext(ctx,
		`UPDATE approvals SET expires_at = ? WHERE id = ?`,
		time.Now().Add(-time.Minute).Unix(), id)
	if err != nil {
		t.Fatal(err)
	}
	err = d.onCallback(ctx, nil, &models.CallbackQuery{
		ID:   "cb",
		From: models.User{ID: 100},
		Data: "approve:" + id,
	}, "approve", id)
	if err != nil {
		t.Fatal(err)
	}
	dec := <-ch
	if dec.Verdict != approval.VerdictExpire {
		t.Fatalf("verdict = %s", dec.Verdict)
	}
	a, _ := d.Queue.Get(ctx, id)
	if a.State != approval.StateExpired {
		t.Fatalf("state = %s", a.State)
	}
}

func TestRestorePendingApprovalsRerenders(t *testing.T) {
	d := newApprovalDaemon(t)
	ctx := context.Background()
	id, _, err := d.Queue.Request(ctx, "s", "claude", "Bash", `{"command":"ls"}`)
	if err != nil {
		t.Fatal(err)
	}

	mock := telegram.NewMock(nil)
	d.Bot = mock

	if err := d.RestorePendingApprovals(ctx); err != nil {
		t.Fatal(err)
	}
	sent := mock.Sent()
	if len(sent) != 1 {
		t.Fatalf("sent = %d", len(sent))
	}
	gotBody := sent[0].Text
	if !strings.Contains(gotBody, "Re-sent after daemon restart") {
		t.Fatalf("body = %s", gotBody)
	}
	a, err := d.Queue.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if a.ChatID != 100 || a.MsgID != 1 {
		t.Fatalf("message = chat %d msg %d", a.ChatID, a.MsgID)
	}
}
