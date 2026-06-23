package daemon

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"

	"github.com/gongahkia/onibi/internal/approval"
	"github.com/gongahkia/onibi/internal/auth"
	"github.com/gongahkia/onibi/internal/intake"
	"github.com/gongahkia/onibi/internal/secrets"
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
	sec, err := secrets.Open(secrets.Options{
		EnvFallbackPath: filepath.Join(t.TempDir(), ".env"),
		PreferDotenv:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return New(Options{DB: db, Secrets: sec, Owner: owner})
}

func TestReplyEditDecidesApproval(t *testing.T) {
	d := newApprovalDaemon(t)
	ctx := context.Background()
	id, ch, err := d.Queue.Request(ctx, "s", "claude", "Bash", `{"command":"rm x"}`)
	if err != nil {
		t.Fatal(err)
	}
	d.setPending(ctx, pendingKindApprovalEdit, 100, id)

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

func TestApprovalEditSurvivesRestart(t *testing.T) {
	d := newApprovalDaemon(t)
	ctx := context.Background()
	id, _, err := d.Queue.Request(ctx, "s", "claude", "Bash", `{"command":"rm x"}`)
	if err != nil {
		t.Fatal(err)
	}
	d.setPending(ctx, pendingKindApprovalEdit, 100, id)
	restarted := New(Options{DB: d.DB, Secrets: d.Secrets, Owner: d.Owner})
	if err := restarted.onReply(ctx, nil, &models.Message{
		From: &models.User{ID: 100},
		Chat: models.Chat{ID: 100},
		Text: `{"command":"echo ok"}`,
	}); err != nil {
		t.Fatal(err)
	}
	a, err := restarted.Queue.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if a.State != approval.StateEdited {
		t.Fatalf("state = %s", a.State)
	}
	if string(a.EditedJSON) != `{"command":"echo ok"}` {
		t.Fatalf("edited = %s", a.EditedJSON)
	}
}

func TestRenderApprovalMessageShowsRisk(t *testing.T) {
	got := renderApprovalMessage("Bash", `{"command":"rm -rf /tmp/x"}`, "s")
	if !strings.Contains(got, "Risk: high - recursive delete") {
		t.Fatalf("message = %s", got)
	}
}

func TestRenderApprovalMessageShowsContextTargetAndTTL(t *testing.T) {
	expires := time.Now().Add(5 * time.Minute)
	got := renderApproval("Bash", `{"command":"git reset --hard HEAD~1"}`, approvalRenderContext{
		Agent:        "codex",
		SessionLabel: "codex (abc123)",
		CWD:          "/tmp/repo",
		ExpiresAt:    expires,
	}).Plain
	for _, want := range []string{
		"Agent: codex",
		"Session: codex (abc123)",
		"Project: /tmp/repo",
		"Tool: Bash",
		"Risk: high - git rewrite",
		"Target: git reset --hard HEAD~1",
		"expires:",
		"local",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("message missing %q:\n%s", want, got)
		}
	}
}

func TestRenderApprovalMessageDoesNotJSONEscapeHTML(t *testing.T) {
	got := renderApprovalMessage("apply_patch", `{"command":"<article>ok</article>"}`, "s")
	if strings.Contains(got, `\u003c`) || strings.Contains(got, `\u003e`) {
		t.Fatalf("message escaped json html = %s", got)
	}
	if !strings.Contains(got, "&lt;article&gt;ok&lt;/article&gt;") {
		t.Fatalf("message = %s", got)
	}
}

func TestReplyInvalidJSONKeepsEditPending(t *testing.T) {
	d := newApprovalDaemon(t)
	ctx := context.Background()
	id, _, err := d.Queue.Request(ctx, "s", "claude", "Bash", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	d.setPending(ctx, pendingKindApprovalEdit, 100, id)

	err = d.onReply(ctx, nil, &models.Message{
		From: &models.User{ID: 100},
		Chat: models.Chat{ID: 100},
		Text: `{`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := d.peekPending(ctx, pendingKindApprovalEdit, 100); !ok || got != id {
		t.Fatalf("pending edit = %q", got)
	}
	a, _ := d.Queue.Get(ctx, id)
	if a.State != approval.StatePending {
		t.Fatalf("state = %s", a.State)
	}
}

func TestReplyInvalidToolSchemaKeepsEditPending(t *testing.T) {
	d := newApprovalDaemon(t)
	ctx := context.Background()
	id, _, err := d.Queue.Request(ctx, "s", "claude", "Bash", `{"command":"rm x"}`)
	if err != nil {
		t.Fatal(err)
	}
	d.setPending(ctx, pendingKindApprovalEdit, 100, id)

	err = d.onReply(ctx, nil, &models.Message{
		From: &models.User{ID: 100},
		Chat: models.Chat{ID: 100},
		Text: `{"command":"echo ok","env":{"X":"1"}}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := d.peekPending(ctx, pendingKindApprovalEdit, 100); !ok || got != id {
		t.Fatalf("pending edit = %q", got)
	}
	a, _ := d.Queue.Get(ctx, id)
	if a.State != approval.StatePending {
		t.Fatalf("state = %s", a.State)
	}
}

func TestDenyReasonReplyReturnsReasonToHook(t *testing.T) {
	d := newApprovalDaemon(t)
	ctx := context.Background()
	id, ch, err := d.Queue.Request(ctx, "s", "claude", "Bash", `{"command":"rm x"}`)
	if err != nil {
		t.Fatal(err)
	}
	mock := telegram.NewMock(nil)
	if err := d.onCallback(ctx, mock, &models.CallbackQuery{
		ID:   "cb",
		From: models.User{ID: 100},
		Data: "reason:" + id,
	}, "deny_reason", id); err != nil {
		t.Fatal(err)
	}
	if got, ok := d.peekPending(ctx, pendingKindDenyReason, 100); !ok || got != id {
		t.Fatalf("pending deny reason = %q", got)
	}
	if err := d.onReply(ctx, mock, &models.Message{
		From: &models.User{ID: 100},
		Chat: models.Chat{ID: 100},
		Text: "too risky",
	}); err != nil {
		t.Fatal(err)
	}
	dec := <-ch
	if dec.Verdict != approval.VerdictDeny || dec.Reason != "too risky" {
		t.Fatalf("decision = %#v", dec)
	}
	a, _ := d.Queue.Get(ctx, id)
	if a.Reason != "too risky" {
		t.Fatalf("stored reason = %q", a.Reason)
	}
}

func TestParanoidReplyEditRequiresTOTP(t *testing.T) {
	d := newApprovalDaemon(t)
	ctx := context.Background()
	secret, err := auth.NewSecret()
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Secrets.Set(secrets.KeyTOTPSecret, auth.EncodeHex(secret)); err != nil {
		t.Fatal(err)
	}
	if err := d.DB.KVSetString(ctx, "paranoid", "1"); err != nil {
		t.Fatal(err)
	}
	id, ch, err := d.Queue.Request(ctx, "s", "claude", "Bash", `{"command":"rm x"}`)
	if err != nil {
		t.Fatal(err)
	}
	d.setPending(ctx, pendingKindApprovalEdit, 100, id)
	mock := telegram.NewMock(nil)
	if err := d.onReply(ctx, mock, &models.Message{
		From: &models.User{ID: 100},
		Chat: models.Chat{ID: 100},
		Text: `{"command":"echo ok"}`,
	}); err != nil {
		t.Fatal(err)
	}
	if got, ok := d.peekPending(ctx, pendingKindApprovalEdit, 100); !ok || got != id {
		t.Fatalf("pending edit = %q", got)
	}
	if sent := mock.Sent(); len(sent) != 1 || !strings.Contains(sent[0].Text, "Paranoid mode requires") {
		t.Fatalf("sent = %#v", sent)
	}
	code := fmt.Sprintf("%06d", auth.Code(secret, time.Now().Unix()))
	if err := d.onReply(ctx, mock, &models.Message{
		From: &models.User{ID: 100},
		Chat: models.Chat{ID: 100},
		Text: "{\"command\":\"echo ok\"}\n" + code,
	}); err != nil {
		t.Fatal(err)
	}
	dec := <-ch
	if dec.Verdict != approval.VerdictEdit {
		t.Fatalf("verdict = %s", dec.Verdict)
	}
	id2, ch2, err := d.Queue.Request(ctx, "s", "claude", "Bash", `{"command":"rm y"}`)
	if err != nil {
		t.Fatal(err)
	}
	d.setPending(ctx, pendingKindApprovalEdit, 100, id2)
	if err := d.onReply(ctx, mock, &models.Message{
		From: &models.User{ID: 100},
		Chat: models.Chat{ID: 100},
		Text: `{"command":"echo grace"}`,
	}); err != nil {
		t.Fatal(err)
	}
	dec = <-ch2
	if dec.Verdict != approval.VerdictEdit {
		t.Fatalf("grace verdict = %s", dec.Verdict)
	}
	if sent := mock.Sent(); !strings.Contains(sent[len(sent)-1].Text, "(within TOTP grace)") {
		t.Fatalf("sent = %#v", sent)
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

func TestRestorePendingApprovalsNoopForWebEvents(t *testing.T) {
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
	if sent := mock.Sent(); len(sent) != 0 {
		t.Fatalf("sent = %d", len(sent))
	}
	if edits := mock.EditedText(); len(edits) != 0 {
		t.Fatalf("edits = %d", len(edits))
	}
	a, err := d.Queue.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if a.ChatID != 0 || a.MsgID != 0 || a.State != approval.StatePending {
		t.Fatalf("message = chat %d msg %d", a.ChatID, a.MsgID)
	}
}

func TestSendApprovalMessageNoopForWebEmitter(t *testing.T) {
	d := newApprovalDaemon(t)
	ctx := context.Background()
	id, _, err := d.Queue.Request(ctx, "s", "claude", "Bash", `{"command":"ls"}`)
	if err != nil {
		t.Fatal(err)
	}
	mock := telegram.NewMock(nil)
	d.Bot = mock
	if _, err := d.sendApprovalMessage(ctx, id, "Bash", `{"command":"ls"}`, "s", false, time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if sent := mock.Sent(); len(sent) != 0 {
		t.Fatalf("sent = %d", len(sent))
	}
	if mock.AwaitingOwnerInteraction() {
		t.Fatal("awaiting interaction armed")
	}
}

func TestHighRiskApproveRequiresConfirm(t *testing.T) {
	d := newApprovalDaemon(t)
	mock := telegram.NewMock(nil)
	d.Bot = mock
	ctx := context.Background()
	id, ch, err := d.Queue.Request(ctx, "s", "claude", "Bash", `{"command":"rm -rf /tmp/x"}`)
	if err != nil {
		t.Fatal(err)
	}
	q := &models.CallbackQuery{
		ID:      "cb1",
		From:    models.User{ID: 100},
		Message: models.MaybeInaccessibleMessage{Message: &models.Message{ID: 1, Chat: models.Chat{ID: 100}}},
	}
	if err := d.onCallback(ctx, mock, q, "approve", id); err != nil {
		t.Fatal(err)
	}
	select {
	case dec := <-ch:
		t.Fatalf("unexpected decision: %#v", dec)
	default:
	}
	if len(mock.Edited()) != 1 {
		t.Fatalf("edits = %d", len(mock.Edited()))
	}
	if err := d.onCallback(ctx, mock, q, "confirm_approve", id); err != nil {
		t.Fatal(err)
	}
	select {
	case dec := <-ch:
		if dec.Verdict != approval.VerdictApprove {
			t.Fatalf("verdict = %s", dec.Verdict)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for decision")
	}
}

func TestApprovalRequestReturnsQueueDecisionWithoutTelegramRender(t *testing.T) {
	d := newApprovalDaemon(t)
	mock := telegram.NewMock(nil)
	d.Bot = mock
	mock.SetHandler(d.Router.Dispatch)
	if err := d.Registry.Add(NewSession("s", "codex", "codex", nil, 1024)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	events, unsub := d.Queue.Subscribe()
	defer unsub()

	respCh := make(chan intake.Response, 1)
	errCh := make(chan error, 1)
	go func() {
		resp, err := d.handleApprovalRequest(ctx, intake.Event{
			Type:      intake.TypeApprovalRequest,
			Session:   "s",
			Managed:   true,
			Tool:      "Bash",
			InputJSON: `{"command":"echo ok"}`,
		})
		respCh <- resp
		errCh <- err
	}()

	var id string
	for ctx.Err() == nil {
		row := d.DB.SQL().QueryRowContext(ctx, `SELECT id FROM approvals WHERE state = ?`, approval.StatePending)
		if err := row.Scan(&id); err == nil && id != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if id == "" {
		t.Fatal("approval was not queued")
	}
	ev := readApprovalEvent(t, events)
	if ev.Type != approval.EventRequested || ev.Approval.ID != id || ev.Approval.SessionID != "s" {
		t.Fatalf("event = %#v", ev)
	}
	if sent := mock.Sent(); len(sent) != 0 {
		t.Fatalf("sent = %d", len(sent))
	}
	if err := d.Queue.Decide(ctx, id, approval.VerdictApprove, "", "", 0); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case resp := <-respCh:
		if resp.Decision != "approve" {
			t.Fatalf("decision = %s", resp.Decision)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if len(mock.Edited()) != 0 {
		t.Fatalf("edits = %d", len(mock.Edited()))
	}
}

func TestApprovalRequestIgnoresUnmanagedExternalHook(t *testing.T) {
	d := newApprovalDaemon(t)
	mock := telegram.NewMock(nil)
	d.Bot = mock
	resp, err := d.handleApprovalRequest(context.Background(), intake.Event{
		Type:              intake.TypeApprovalRequest,
		Session:           "019eca70-2277-7ed1-9ecb-6444045e6462",
		Managed:           false,
		Agent:             "codex",
		ProviderSessionID: "019eca70-2277-7ed1-9ecb-6444045e6462",
		CWD:               "/tmp/hot-cross-buns-2",
		Tool:              "Bash",
		InputJSON:         `{"command":"sed -n '1,20p' README.md"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Decision != "cancelled" {
		t.Fatalf("decision = %q", resp.Decision)
	}
	if len(mock.Sent()) != 0 {
		t.Fatalf("sent = %#v", mock.Sent())
	}
	if rows, err := d.Queue.Pending(context.Background()); err != nil || len(rows) != 0 {
		t.Fatalf("pending = %d err=%v", len(rows), err)
	}
	entries, err := d.DB.AuditRecent(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Action != "approval.ignored" || !strings.Contains(entries[0].Detail, "unmanaged provider hook") {
		t.Fatalf("audit = %#v", entries)
	}
}

func readApprovalEvent(t *testing.T, events <-chan approval.Event) approval.Event {
	t.Helper()
	select {
	case ev := <-events:
		return ev
	case <-time.After(time.Second):
		t.Fatal("event not delivered")
		return approval.Event{}
	}
}

func TestAgentDoneIgnoresUnmanagedExternalHook(t *testing.T) {
	d := newApprovalDaemon(t)
	d.Bot = telegram.NewMock(nil)
	err := d.handleEvent(context.Background(), intake.Event{
		Type:              intake.TypeAgentDone,
		Session:           "019eca70-2277-7ed1-9ecb-6444045e6462",
		Managed:           false,
		Agent:             "codex",
		ProviderSessionID: "019eca70-2277-7ed1-9ecb-6444045e6462",
		CWD:               "/tmp/hot-cross-buns-2",
		Text:              "Stop",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := d.Registry.List(); len(got) != 0 {
		t.Fatalf("sessions = %#v", got)
	}
	if sent := d.Bot.(*telegram.Mock).Sent(); len(sent) != 0 {
		t.Fatalf("sent = %#v", sent)
	}
	entries, err := d.DB.AuditRecent(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Action != "hook.ignored" || !strings.Contains(entries[0].Detail, "unmanaged provider hook") {
		t.Fatalf("audit = %#v", entries)
	}
}

func TestParanoidCapsExplicitApprovalTTL(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "d.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.KVSetString(context.Background(), "paranoid", "1"); err != nil {
		t.Fatal(err)
	}
	d := New(Options{DB: db, ApprovalTTL: 5 * time.Minute})
	id, _, err := d.Queue.Request(context.Background(), "s", "claude", "Bash", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	a, err := d.Queue.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	got := a.ExpiresAt.Sub(a.CreatedAt)
	if got != approval.ParanoidTTL {
		t.Fatalf("ttl = %s", got)
	}
}
