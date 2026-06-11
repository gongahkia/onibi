package daemon

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"

	"github.com/gongahkia/onibi/internal/approval"
	"github.com/gongahkia/onibi/internal/auth"
	"github.com/gongahkia/onibi/internal/envelope"
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

func TestRenderApprovalMessageShowsRisk(t *testing.T) {
	got := renderApprovalMessage("Bash", `{"command":"rm -rf /tmp/x"}`, "s")
	if !strings.Contains(got, "Risk: high - recursive delete") {
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

func TestReplyInvalidToolSchemaKeepsEditPending(t *testing.T) {
	d := newApprovalDaemon(t)
	ctx := context.Background()
	id, _, err := d.Queue.Request(ctx, "s", "claude", "Bash", `{"command":"rm x"}`)
	if err != nil {
		t.Fatal(err)
	}
	d.pendingEdits[100] = id

	err = d.onReply(ctx, nil, &models.Message{
		From: &models.User{ID: 100},
		Chat: models.Chat{ID: 100},
		Text: `{"command":"echo ok","env":{"X":"1"}}`,
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
	d.pendingEdits[100] = id
	mock := telegram.NewMock(nil)
	if err := d.onReply(ctx, mock, &models.Message{
		From: &models.User{ID: 100},
		Chat: models.Chat{ID: 100},
		Text: `{"command":"echo ok"}`,
	}); err != nil {
		t.Fatal(err)
	}
	if got := d.pendingEdits[100]; got != id {
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

func TestApprovalMessageArmsRaceWarning(t *testing.T) {
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
	if !mock.AwaitingOwnerInteraction() {
		t.Fatal("awaiting interaction not armed")
	}
	mock.RecordEmptyPolls(ctx, 10)
	mock.RecordEmptyPolls(ctx, 10)
	sent := mock.Sent()
	if len(sent) != 2 {
		t.Fatalf("sent = %d", len(sent))
	}
	if !strings.Contains(sent[1].Text, "Possible token race") {
		t.Fatalf("warning = %q", sent[1].Text)
	}
}

func TestEncryptedApprovalMessageHidesPayload(t *testing.T) {
	d := newApprovalDaemon(t)
	seed, err := envelope.GenerateSeed()
	if err != nil {
		t.Fatal(err)
	}
	d.EncryptedMode = "on"
	d.EnvelopeSeed = seed
	d.MiniAppURL = "https://example.com/onibi/"
	ctx := context.Background()
	id, _, err := d.Queue.Request(ctx, "s", "claude", "Bash", `{"command":"echo secret"}`)
	if err != nil {
		t.Fatal(err)
	}
	mock := telegram.NewMock(nil)
	d.Bot = mock
	if _, err := d.sendApprovalMessage(ctx, id, "Bash", `{"command":"echo secret"}`, "s", false, time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	sent := mock.Sent()
	if len(sent) != 1 {
		t.Fatalf("sent = %d", len(sent))
	}
	if strings.Contains(sent[0].Text, "echo secret") {
		t.Fatalf("telegram text leaked payload: %q", sent[0].Text)
	}
	kb, ok := sent[0].ReplyMarkup.(*models.ReplyKeyboardMarkup)
	if !ok {
		t.Fatalf("reply markup = %T", sent[0].ReplyMarkup)
	}
	u, err := url.Parse(kb.Keyboard[0][0].WebApp.URL)
	if err != nil {
		t.Fatal(err)
	}
	q, err := url.ParseQuery(u.Fragment)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := envelope.Decrypt(seed, q.Get("onibi"), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plain.Body, "echo secret") {
		t.Fatalf("plain body = %q", plain.Body)
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

func TestApprovalRequestApprovesViaMockCallback(t *testing.T) {
	d := newApprovalDaemon(t)
	mock := telegram.NewMock(nil)
	d.Bot = mock
	mock.SetHandler(d.Router.Dispatch)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	respCh := make(chan intake.Response, 1)
	errCh := make(chan error, 1)
	go func() {
		resp, err := d.handleApprovalRequest(ctx, intake.Event{
			Type:      intake.TypeApprovalRequest,
			Session:   "s",
			Tool:      "Bash",
			InputJSON: `{"command":"echo ok"}`,
		})
		respCh <- resp
		errCh <- err
	}()

	var id string
	for ctx.Err() == nil {
		row := d.DB.SQL().QueryRowContext(ctx, `SELECT id FROM approvals WHERE state = ?`, approval.StatePending)
		if err := row.Scan(&id); err == nil && id != "" && len(mock.Sent()) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if id == "" {
		t.Fatal("approval was not rendered")
	}
	mock.Dispatch(ctx, &models.Update{CallbackQuery: &models.CallbackQuery{
		ID:   "cb1",
		From: models.User{ID: 100},
		Data: "approve:" + id,
	}})

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
	if len(mock.Answered()) != 1 {
		t.Fatalf("answers = %d", len(mock.Answered()))
	}
	if len(mock.Edited()) != 1 {
		t.Fatalf("edits = %d", len(mock.Edited()))
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
