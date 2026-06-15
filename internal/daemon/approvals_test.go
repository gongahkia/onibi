package daemon

import (
	"context"
	"errors"
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

func TestRestorePendingApprovalsEditsInPlace(t *testing.T) {
	d := newApprovalDaemon(t)
	ctx := context.Background()
	id, _, err := d.Queue.Request(ctx, "s", "claude", "Bash", `{"command":"ls"}`)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Queue.SetMessage(ctx, id, 100, 77); err != nil {
		t.Fatal(err)
	}
	mock := telegram.NewMock(nil)
	restarted := New(Options{DB: d.DB, Secrets: d.Secrets, Owner: d.Owner, Bot: mock})
	if err := restarted.RestorePendingApprovals(ctx); err != nil {
		t.Fatal(err)
	}
	if sent := mock.Sent(); len(sent) != 0 {
		t.Fatalf("sent = %d", len(sent))
	}
	edits := mock.EditedText()
	if len(edits) != 1 {
		t.Fatalf("text edits = %d", len(edits))
	}
	if edits[0].MessageID != 77 || edits[0].ChatID != int64(100) {
		t.Fatalf("edit target = chat %#v msg %d", edits[0].ChatID, edits[0].MessageID)
	}
	if strings.Contains(edits[0].Text, "Re-sent after daemon restart") {
		t.Fatalf("edit body = %s", edits[0].Text)
	}
	if edits[0].ParseMode != models.ParseModeHTML || edits[0].ReplyMarkup == nil {
		t.Fatalf("edit params = %#v", edits[0])
	}
}

func TestRestorePendingApprovalsFallsBackOnEditError(t *testing.T) {
	d := newApprovalDaemon(t)
	ctx := context.Background()
	id, _, err := d.Queue.Request(ctx, "s", "claude", "Bash", `{"command":"ls"}`)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Queue.SetMessage(ctx, id, 100, 77); err != nil {
		t.Fatal(err)
	}
	mock := telegram.NewMock(nil)
	mock.SetEditMessageTextError(errors.New("Bad Request: message to edit not found"))
	restarted := New(Options{DB: d.DB, Secrets: d.Secrets, Owner: d.Owner, Bot: mock})
	if err := restarted.RestorePendingApprovals(ctx); err != nil {
		t.Fatal(err)
	}
	if edits := mock.EditedText(); len(edits) != 1 {
		t.Fatalf("text edits = %d", len(edits))
	}
	sent := mock.Sent()
	if len(sent) != 1 {
		t.Fatalf("sent = %d", len(sent))
	}
	if !strings.Contains(sent[0].Text, "Re-sent after daemon restart") {
		t.Fatalf("fallback body = %s", sent[0].Text)
	}
	a, err := restarted.Queue.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if a.MsgID != 1 || a.ChatID != 100 {
		t.Fatalf("message = chat %d msg %d", a.ChatID, a.MsgID)
	}
}

func TestRestorePendingApprovalsEncryptedSkipsEditInPlace(t *testing.T) {
	d := newApprovalDaemon(t)
	ctx := context.Background()
	id, _, err := d.Queue.Request(ctx, "s", "claude", "Bash", `{"command":"echo secret"}`)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Queue.SetMessage(ctx, id, 100, 77); err != nil {
		t.Fatal(err)
	}
	seed, err := envelope.GenerateSeed()
	if err != nil {
		t.Fatal(err)
	}
	mock := telegram.NewMock(nil)
	restarted := New(Options{
		DB:            d.DB,
		Secrets:       d.Secrets,
		Owner:         d.Owner,
		Bot:           mock,
		EncryptedMode: "on",
		EnvelopeSeed:  seed,
		MiniAppURL:    "https://example.com/onibi/",
	})
	if err := restarted.RestorePendingApprovals(ctx); err != nil {
		t.Fatal(err)
	}
	if edits := mock.EditedText(); len(edits) != 0 {
		t.Fatalf("text edits = %d", len(edits))
	}
	if sent := mock.Sent(); len(sent) != 1 {
		t.Fatalf("sent = %d", len(sent))
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
	if err := d.Registry.Add(NewSession("s", "codex", "codex", nil, 1024)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

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
