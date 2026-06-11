package daemon

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"

	"github.com/gongahkia/onibi/internal/auth"
	"github.com/gongahkia/onibi/internal/pty"
	"github.com/gongahkia/onibi/internal/render"
	"github.com/gongahkia/onibi/internal/secrets"
	"github.com/gongahkia/onibi/internal/telegram"
)

func TestRenderOverrideExplicitTarget(t *testing.T) {
	d := newApprovalDaemon(t)
	s := NewSession("abc123", "claude", "claude", nil, 1024)
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	mock := telegram.NewMock(nil)
	msg := &models.Message{From: &models.User{ID: 100}, Chat: models.Chat{ID: 100}, Text: "/screenshot abc"}
	if !d.handleTextCommand(context.Background(), mock, msg) {
		t.Fatal("command not handled")
	}
	if got := d.renderOverride("abc123"); got != render.ModePNG {
		t.Fatalf("override = %s", got)
	}
	if sent := mock.Sent(); len(sent) != 1 || !strings.Contains(sent[0].Text, "png") {
		t.Fatalf("sent = %#v", sent)
	}
}

func TestRenderOverrideAmbiguousWithoutTarget(t *testing.T) {
	d := newApprovalDaemon(t)
	_ = d.Registry.Add(NewSession("a", "one", "claude", nil, 1024))
	_ = d.Registry.Add(NewSession("b", "two", "claude", nil, 1024))
	mock := telegram.NewMock(nil)
	d.handleTextCommand(context.Background(), mock, &models.Message{
		From: &models.User{ID: 100},
		Chat: models.Chat{ID: 100},
		Text: "/text",
	})
	if sent := mock.Sent(); len(sent) != 1 || !strings.Contains(sent[0].Text, "Multiple active sessions") {
		t.Fatalf("sent = %#v", sent)
	}
}

func TestNotifyTurnCompleteUsesScreenshotOverride(t *testing.T) {
	d := newApprovalDaemon(t)
	mock := telegram.NewMock(nil)
	d.Bot = mock
	s := NewSession("abc123", "claude", "claude", nil, 1024)
	_, _ = s.Buf.Write([]byte("hello"))
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	d.setRenderOverride("abc123", render.ModePNG)
	if err := d.notifyTurnComplete(context.Background(), "abc123", "agent_done", ""); err != nil {
		t.Fatal(err)
	}
	if len(mock.Photos()) != 1 {
		t.Fatalf("photos = %d messages = %d", len(mock.Photos()), len(mock.Sent()))
	}
}

func TestNotifyTurnCompleteDefaultsToText(t *testing.T) {
	d := newApprovalDaemon(t)
	mock := telegram.NewMock(nil)
	d.Bot = mock
	s := NewSession("abc123", "claude", "claude", nil, 1024)
	_, _ = s.Buf.Write([]byte("hello"))
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	if err := d.notifyTurnComplete(context.Background(), "abc123", "agent_done", ""); err != nil {
		t.Fatal(err)
	}
	if len(mock.Sent()) != 1 || len(mock.Photos()) != 0 {
		t.Fatalf("messages = %d photos = %d", len(mock.Sent()), len(mock.Photos()))
	}
	if !strings.Contains(mock.Sent()[0].Text, "<pre>hello</pre>") {
		t.Fatalf("text = %s", mock.Sent()[0].Text)
	}
}

func TestLongOutputUsesDocument(t *testing.T) {
	d := newApprovalDaemon(t)
	mock := telegram.NewMock(nil)
	body := strings.Repeat("x", telegram.SafeTextLimit*maxTextChunks+100)
	if _, err := d.sendTextOutput(context.Background(), mock, 100, "long", body, "long.txt"); err != nil {
		t.Fatal(err)
	}
	if len(mock.Documents()) != 1 {
		t.Fatalf("docs = %d messages = %d", len(mock.Documents()), len(mock.Sent()))
	}
}

func TestFreeTextInjectsOnlyLiveSession(t *testing.T) {
	d := newApprovalDaemon(t)
	r, s := pipeSession(t, "abc123", "claude")
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	mock := telegram.NewMock(nil)
	err := d.onText(context.Background(), mock, &models.Message{
		From: &models.User{ID: 100},
		Chat: models.Chat{ID: 100},
		Text: "continue",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := readPipe(t, r); got != "continue\n" {
		t.Fatalf("injected = %q", got)
	}
	rows, err := d.DB.PromptList(context.Background(), s.ID, true, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].State != "sent" {
		t.Fatalf("prompt rows = %#v", rows)
	}
}

func TestReplyRoutesByThreadedMessage(t *testing.T) {
	d := newApprovalDaemon(t)
	r, s := pipeSession(t, "abc123", "claude")
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	d.bindMessage(&models.Message{ID: 42, Chat: models.Chat{ID: 100}}, s.ID)
	mock := telegram.NewMock(nil)
	err := d.onReply(context.Background(), mock, &models.Message{
		From:           &models.User{ID: 100},
		Chat:           models.Chat{ID: 100},
		ReplyToMessage: &models.Message{ID: 42},
		Text:           "yes",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := readPipe(t, r); got != "yes\n" {
		t.Fatalf("injected = %q", got)
	}
}

func TestAmbiguousTextUsesTargetCallbackForPendingInject(t *testing.T) {
	d := newApprovalDaemon(t)
	_, s1 := pipeSession(t, "aaa111", "one")
	r2, s2 := pipeSession(t, "bbb222", "two")
	_ = d.Registry.Add(s1)
	_ = d.Registry.Add(s2)
	mock := telegram.NewMock(nil)
	if err := d.onText(context.Background(), mock, &models.Message{
		From: &models.User{ID: 100},
		Chat: models.Chat{ID: 100},
		Text: "run tests",
	}); err != nil {
		t.Fatal(err)
	}
	if len(mock.Sent()) != 1 || !strings.Contains(mock.Sent()[0].Text, "Pick target") {
		t.Fatalf("sent = %#v", mock.Sent())
	}
	if err := d.onCallback(context.Background(), mock, &models.CallbackQuery{
		ID:   "cb",
		From: models.User{ID: 100},
		Data: "target:" + s2.ID,
	}, "target", s2.ID); err != nil {
		t.Fatal(err)
	}
	if got := readPipe(t, r2); got != "run tests\n" {
		t.Fatalf("injected = %q", got)
	}
}

func TestSnoozeSuppressesTurnComplete(t *testing.T) {
	d := newApprovalDaemon(t)
	mock := telegram.NewMock(nil)
	d.handleSnoozeCommand(context.Background(), mock, 100, "claude 1h")
	d.Bot = telegram.NewMock(nil)
	s := NewSession("abc123", "claude", "claude", nil, 1024)
	_, _ = s.Buf.Write([]byte("hello"))
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	if err := d.notifyTurnComplete(context.Background(), s.ID, "agent_done", ""); err != nil {
		t.Fatal(err)
	}
	if len(d.Bot.(*telegram.Mock).Sent()) != 0 {
		t.Fatalf("sent while snoozed = %#v", d.Bot.(*telegram.Mock).Sent())
	}
}

func TestQueuedPromptWaitsUntilTurnComplete(t *testing.T) {
	d := newApprovalDaemon(t)
	mock := telegram.NewMock(nil)
	d.Bot = mock
	r, s := pipeSession(t, "abc123", "claude")
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	d.threadMu.Lock()
	d.busySessions[s.ID] = true
	d.threadMu.Unlock()
	if err := d.onText(context.Background(), mock, &models.Message{
		From: &models.User{ID: 100},
		Chat: models.Chat{ID: 100},
		Text: "second prompt",
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := d.DB.PromptList(context.Background(), s.ID, false, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].State != "queued" {
		t.Fatalf("queued rows = %#v", rows)
	}
	_, _ = s.Buf.Write([]byte("done"))
	if err := d.notifyTurnComplete(context.Background(), s.ID, "agent_done", ""); err != nil {
		t.Fatal(err)
	}
	if got := readPipe(t, r); got != "second prompt\n" {
		t.Fatalf("injected after ready = %q", got)
	}
}

func TestEditPromptCommand(t *testing.T) {
	d := newApprovalDaemon(t)
	_, s := pipeSession(t, "abc123", "claude")
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	p, err := d.DB.PromptEnqueue(context.Background(), s.ID, 100, "old")
	if err != nil {
		t.Fatal(err)
	}
	mock := telegram.NewMock(nil)
	d.handleTextCommand(context.Background(), mock, &models.Message{
		From: &models.User{ID: 100},
		Chat: models.Chat{ID: 100},
		Text: "/editprompt " + p.ID + " new text",
	})
	got, err := d.DB.PromptGet(context.Background(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "new text" {
		t.Fatalf("text = %q", got.Text)
	}
}

func TestInterruptAndKillSession(t *testing.T) {
	d := newApprovalDaemon(t)
	r, s := pipeSession(t, "abc123", "claude")
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	mock := telegram.NewMock(nil)
	d.handleInterruptCommand(context.Background(), mock, 100, s.ID)
	if got := readPipe(t, r); got != string([]byte{3}) {
		t.Fatalf("interrupt = %q", got)
	}
	d.handleKillCommand(context.Background(), mock, 100, s.ID)
	if !s.Ended() {
		t.Fatal("session not ended")
	}
}

func TestKillRequiresTOTPWhenEnabled(t *testing.T) {
	d := newApprovalDaemon(t)
	_, s := pipeSession(t, "abc123", "claude")
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	secret, err := auth.NewSecret()
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Secrets.Set(secrets.KeyTOTPSecret, auth.EncodeHex(secret)); err != nil {
		t.Fatal(err)
	}
	mock := telegram.NewMock(nil)
	d.handleKillCommand(context.Background(), mock, 100, s.ID)
	if s.Ended() {
		t.Fatal("session ended without TOTP")
	}
	if sent := mock.Sent(); len(sent) != 1 || !strings.Contains(sent[0].Text, "TOTP required") {
		t.Fatalf("sent = %#v", sent)
	}
	code := fmt.Sprintf("%06d", auth.Code(secret, time.Now().Unix()))
	d.handleKillCommand(context.Background(), mock, 100, s.ID+" "+code)
	if !s.Ended() {
		t.Fatal("session not ended with valid TOTP")
	}
}

func TestParanoidWithoutTOTPSecretFailsClosed(t *testing.T) {
	d := newApprovalDaemon(t)
	_, s := pipeSession(t, "abc123", "claude")
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	if err := d.DB.KVSetString(context.Background(), "paranoid", "1"); err != nil {
		t.Fatal(err)
	}
	mock := telegram.NewMock(nil)
	d.handleKillCommand(context.Background(), mock, 100, s.ID)
	if s.Ended() {
		t.Fatal("session ended with paranoid mode missing TOTP")
	}
	if sent := mock.Sent(); len(sent) != 1 || !strings.Contains(sent[0].Text, "TOTP unavailable") {
		t.Fatalf("sent = %#v", sent)
	}
}

func TestSessionActionCallbackRequiresTOTPWhenEnabled(t *testing.T) {
	d := newApprovalDaemon(t)
	_, s := pipeSession(t, "abc123", "claude")
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	secret, err := auth.NewSecret()
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Secrets.Set(secrets.KeyTOTPSecret, auth.EncodeHex(secret)); err != nil {
		t.Fatal(err)
	}
	mock := telegram.NewMock(nil)
	if err := d.handleSessionActionCallback(context.Background(), mock, &models.CallbackQuery{
		ID:   "cb",
		From: models.User{ID: 100},
		Data: "kill:" + s.ID,
	}, "kill", s.ID); err != nil {
		t.Fatal(err)
	}
	if s.Ended() {
		t.Fatal("session ended from callback without TOTP")
	}
	if sent := mock.Sent(); len(sent) != 1 || !strings.Contains(sent[0].Text, "TOTP required") {
		t.Fatalf("sent = %#v", sent)
	}
	if answers := mock.Answered(); len(answers) != 1 || answers[0].Text != "TOTP required" {
		t.Fatalf("answers = %#v", answers)
	}
}

func pipeSession(t *testing.T, id, name string) (*os.File, *Session) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = r.Close()
		_ = w.Close()
	})
	return r, NewSession(id, name, "claude", &pty.Host{Master: w}, 1024)
}

func readPipe(t *testing.T, r *os.File) string {
	t.Helper()
	ch := make(chan string, 1)
	go func() {
		buf := make([]byte, 256)
		n, _ := r.Read(buf)
		ch <- string(buf[:n])
	}()
	select {
	case s := <-ch:
		return s
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for PTY write")
	}
	return ""
}
