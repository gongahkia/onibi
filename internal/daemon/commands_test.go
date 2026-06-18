package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"

	"github.com/gongahkia/onibi/internal/auth"
	"github.com/gongahkia/onibi/internal/pty"
	"github.com/gongahkia/onibi/internal/render"
	"github.com/gongahkia/onibi/internal/secrets"
	"github.com/gongahkia/onibi/internal/store"
	"github.com/gongahkia/onibi/internal/telegram"
)

func TestRenderOverrideExplicitTarget(t *testing.T) {
	d := newApprovalDaemon(t)
	s := NewSession("abc123", "claude", "claude", nil, 1024)
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	mock := telegram.NewMock(nil)
	msg := &models.Message{From: &models.User{ID: 100}, Chat: models.Chat{ID: 100}, Text: "/render abc"}
	if !d.handleTextCommand(context.Background(), mock, msg) {
		t.Fatal("command not handled")
	}
	if got := d.renderOverride("abc123"); got != render.ModePNG {
		t.Fatalf("override = %s", got)
	}
	if len(mock.Photos()) != 1 {
		t.Fatalf("photos = %d messages = %d", len(mock.Photos()), len(mock.Sent()))
	}
}

func TestScreenshotAliasExplainsRender(t *testing.T) {
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
	if sent := mock.Sent(); len(sent) != 1 || !strings.Contains(sent[0].Text, "Using /render") {
		t.Fatalf("sent = %#v", sent)
	}
	if len(mock.Photos()) != 1 {
		t.Fatalf("photos = %d", len(mock.Photos()))
	}
}

func TestStartCommandShowsHelp(t *testing.T) {
	d := newApprovalDaemon(t)
	mock := telegram.NewMock(nil)
	msg := &models.Message{From: &models.User{ID: 100}, Chat: models.Chat{ID: 100}, Text: "/start"}
	if !d.handleTextCommand(context.Background(), mock, msg) {
		t.Fatal("command not handled")
	}
	sent := mock.Sent()
	if len(sent) != 1 || !strings.Contains(sent[0].Text, "Onibi is paired and listening") || !strings.Contains(sent[0].Text, "/menu") || sent[0].ReplyMarkup == nil {
		t.Fatalf("sent = %#v", sent)
	}
}

func TestStartCommandIgnoresPairPayload(t *testing.T) {
	d := newApprovalDaemon(t)
	mock := telegram.NewMock(nil)
	msg := &models.Message{From: &models.User{ID: 100}, Chat: models.Chat{ID: 100}, Text: "/start pair_abc123"}
	if !d.handleTextCommand(context.Background(), mock, msg) {
		t.Fatal("command not handled")
	}
	sent := mock.Sent()
	if len(sent) != 1 || !strings.Contains(sent[0].Text, "Onibi is paired and listening") || !strings.Contains(sent[0].Text, "/menu") || sent[0].ReplyMarkup == nil {
		t.Fatalf("sent = %#v", sent)
	}
}

func TestHelpDetailKnown(t *testing.T) {
	got := telegram.HelpDetail("/prompt")
	if !strings.Contains(got, "/prompt <text>") || !strings.Contains(got, "default target session") {
		t.Fatalf("detail = %q", got)
	}
}

func TestHelpDetailKnownWithoutSlash(t *testing.T) {
	got := telegram.HelpDetail("prompt")
	if !strings.Contains(got, "/prompt <text>") || !strings.Contains(got, "default target session") {
		t.Fatalf("detail = %q", got)
	}
}

func TestHelpDetailUnknown(t *testing.T) {
	if got := telegram.HelpDetail("/foo"); !strings.Contains(got, "No such command") {
		t.Fatalf("detail = %q", got)
	}
}

func TestHelpCommandWithArg(t *testing.T) {
	d := newApprovalDaemon(t)
	mock := telegram.NewMock(nil)
	if !d.handleTextCommand(context.Background(), mock, &models.Message{
		From: &models.User{ID: 100},
		Chat: models.Chat{ID: 100},
		Text: "/help prompt",
	}) {
		t.Fatal("command not handled")
	}
	sent := mock.Sent()
	if len(sent) != 1 || !strings.Contains(sent[0].Text, "/prompt &lt;text&gt;") || !strings.Contains(sent[0].Text, "default target session") {
		t.Fatalf("sent = %#v", sent)
	}
}

func TestStatusIncludesContext(t *testing.T) {
	d := newApprovalDaemon(t)
	d.EncryptedMode = "ask"
	s := NewSession("abc123456", "claude", "claude", nil, 1024)
	s.Cmd = "claude"
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	d.setDefaultTarget(ctx, 100, s.ID)
	d.threadMu.Lock()
	d.busySessions[s.ID] = true
	d.threadMu.Unlock()
	if err := d.DB.KVSetString(ctx, snoozeKey("global"), "indefinite"); err != nil {
		t.Fatal(err)
	}
	got := d.statusText(ctx, 100)
	for _, want := range []string{"encrypted_mode=ask", "default_target=claude (abc123)", "snooze=global=indefinite", "* claude (abc123)", "state=busy"} {
		if !strings.Contains(got, want) {
			t.Fatalf("status missing %q:\n%s", want, got)
		}
	}
}

func TestStatusReportsTelegramPollerConflict(t *testing.T) {
	d := newApprovalDaemon(t)
	ctx := context.Background()
	if err := d.DB.SetTelegramPollerConflict(ctx, "conflict: another getUpdates poller is active"); err != nil {
		t.Fatal(err)
	}
	got := d.statusText(ctx, 100)
	if !strings.Contains(got, "telegram_poller=conflict: another getUpdates poller is active") {
		t.Fatalf("status = %s", got)
	}
}

func TestPingCommandReportsConnectivity(t *testing.T) {
	d := newApprovalDaemon(t)
	mock := telegram.NewMock(nil)
	msg := &models.Message{
		From: &models.User{ID: 100},
		Chat: models.Chat{ID: 100},
		Text: "/ping",
		Date: int(time.Now().Add(-2 * time.Second).Unix()),
	}
	if !d.handleTextCommand(context.Background(), mock, msg) {
		t.Fatal("command not handled")
	}
	sent := mock.Sent()
	if len(sent) != 1 {
		t.Fatalf("sent = %#v", sent)
	}
	for _, want := range []string{"pong", "uptime=", "sessions=0", "telegram_poller=ok", "telegram_ingress_lag="} {
		if !strings.Contains(sent[0].Text, want) {
			t.Fatalf("ping missing %q:\n%s", want, sent[0].Text)
		}
	}
}

func TestStatusClearsStaleDefaultTarget(t *testing.T) {
	d := newApprovalDaemon(t)
	ctx := context.Background()
	d.setDefaultTarget(ctx, 100, "deadbeef")
	got := d.statusText(ctx, 100)
	if strings.Contains(got, "stale") || !strings.Contains(got, "default_target=none") {
		t.Fatalf("status = %s", got)
	}
	if id := d.defaultTarget(ctx, 100); id != "" {
		t.Fatalf("default target not cleared: %q", id)
	}
}

func TestMenuDashboardShowsState(t *testing.T) {
	d := newApprovalDaemon(t)
	ctx := context.Background()
	d.EncryptedMode = "ask"
	d.EnvelopeSeed = "seed"
	d.MiniAppURL = "https://example.com/onibi/"
	s1 := NewSession("aaa111", "one", "claude", nil, 1024)
	s2 := NewSession("bbb222", "two", "codex", nil, 1024)
	if err := d.Registry.Add(s1); err != nil {
		t.Fatal(err)
	}
	if err := d.Registry.Add(s2); err != nil {
		t.Fatal(err)
	}
	d.setDefaultTarget(ctx, 100, s2.ID)
	d.threadMu.Lock()
	d.busySessions[s1.ID] = true
	d.threadMu.Unlock()
	if _, _, err := d.Queue.Request(ctx, s2.ID, "codex", "Bash", `{"command":"true"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.DB.PromptEnqueue(ctx, s2.ID, 100, "queued prompt"); err != nil {
		t.Fatal(err)
	}
	if err := d.DB.KVSetString(ctx, snoozeKey("global"), "indefinite"); err != nil {
		t.Fatal(err)
	}
	got := d.menuText(ctx, 100)
	for _, want := range []string{
		"Onibi",
		"daemon: up",
		"target: two (bbb222)",
		"sessions: 2 total, 1 busy, 2 headless, 0 visible",
		"approvals: 1 pending",
		"queue: 1 queued",
		"snooze: global",
		"secure: ask, seed ok, mini app ok",
		"hooks: warn",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("menu missing %q:\n%s", want, got)
		}
	}
}

func TestSecureStatusCommandShowsReadiness(t *testing.T) {
	d := newApprovalDaemon(t)
	enableEncryptedTestDaemon(t, d)
	mock := telegram.NewMock(nil)
	d.handleTextCommand(context.Background(), mock, &models.Message{
		From: &models.User{ID: 100},
		Chat: models.Chat{ID: 100},
		Text: "/secure status",
	})
	sent := mock.Sent()
	if len(sent) != 1 {
		t.Fatalf("sent = %#v", sent)
	}
	for _, want := range []string{"Secure mode readiness", "mode=on", "seed_present=yes", "mini_app_url_allowed=yes", "webapp_action_last_seen=never", "plaintext_commands_blocked=yes", "secure_button_available=yes"} {
		if !strings.Contains(sent[0].Text, want) {
			t.Fatalf("status missing %q:\n%s", want, sent[0].Text)
		}
	}
}

func TestEncryptedModeBlocksPlaintextPromptCommands(t *testing.T) {
	d := newApprovalDaemon(t)
	enableEncryptedTestDaemon(t, d)
	r, s := pipeSession(t, "abc123", "claude")
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"/prompt secret", "/send secret", "/editprompt abc new", "/rename abc123 new"} {
		mock := telegram.NewMock(nil)
		d.handleTextCommand(context.Background(), mock, &models.Message{
			From: &models.User{ID: 100},
			Chat: models.Chat{ID: 100},
			Text: text,
		})
		sent := mock.Sent()
		if len(sent) != 1 || !strings.Contains(sent[0].Text, "Plaintext command blocked") || sent[0].ReplyMarkup == nil {
			t.Fatalf("%s sent = %#v", text, sent)
		}
	}
	if got := readPipeOptional(r, 50*time.Millisecond); got != "" {
		t.Fatalf("plaintext wrote to PTY: %q", got)
	}
	rows, err := d.DB.PromptList(context.Background(), s.ID, false, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("prompt rows = %#v", rows)
	}
	if s.Name != "claude" {
		t.Fatalf("session renamed to %q", s.Name)
	}
}

func TestMenuNoSessionsShowsNextStep(t *testing.T) {
	d := newApprovalDaemon(t)
	mock := telegram.NewMock(nil)
	d.handleMenuCommand(context.Background(), mock, 100)
	sent := mock.Sent()
	if len(sent) != 1 || !strings.Contains(sent[0].Text, "sessions: 0 total") || sent[0].ReplyMarkup == nil {
		t.Fatalf("sent = %#v", sent)
	}
	got := fmt.Sprint(sent[0].ReplyMarkup)
	for _, want := range []string{"New Visible", "New Headless", "Projects", "Test Approval", "Doctor", "Hooks"} {
		if !strings.Contains(got, want) {
			t.Fatalf("menu missing %q: %s", want, got)
		}
	}
}

func TestMenuShowsGlobalAndSessionButtons(t *testing.T) {
	d := newApprovalDaemon(t)
	if err := d.Registry.Add(NewSession("abc123456", "claude", "claude", nil, 1024)); err != nil {
		t.Fatal(err)
	}
	mock := telegram.NewMock(nil)
	d.handleMenuCommand(context.Background(), mock, 100)
	sent := mock.Sent()
	if len(sent) != 1 {
		t.Fatalf("sent = %#v", sent)
	}
	markup, ok := sent[0].ReplyMarkup.(*models.InlineKeyboardMarkup)
	if !ok {
		t.Fatalf("reply markup = %#v", sent[0].ReplyMarkup)
	}
	for _, want := range []string{"daemon:", "target: claude (abc123)", "sessions:", "approvals:", "queue:", "snooze:", "secure:", "hooks:"} {
		if !strings.Contains(sent[0].Text, want) {
			t.Fatalf("menu text missing %q:\n%s", want, sent[0].Text)
		}
	}
	got := fmt.Sprint(markup.InlineKeyboard)
	for _, want := range []string{"Status", "Sessions", "Queue", "Secure", "New Visible", "New Headless", "Projects", "Test Approval", "Peek", "Send", "Interrupt", "Show", "Doctor", "Hooks"} {
		if !strings.Contains(got, want) {
			t.Fatalf("menu missing %q: %s", want, got)
		}
	}
}

func TestOnboardVisibleUsesProjectAlias(t *testing.T) {
	d := newApprovalDaemon(t)
	ctx := context.Background()
	dir := t.TempDir()
	if err := d.DB.KVSetString(ctx, projectAliasKey("repo"), dir); err != nil {
		t.Fatal(err)
	}
	mock := telegram.NewMock(nil)
	if err := d.onCallback(ctx, mock, &models.CallbackQuery{ID: "cb", From: models.User{ID: 100}}, "onboard_visible", ""); err != nil {
		t.Fatal(err)
	}
	if sent := mock.Sent(); len(sent) != 1 || !strings.Contains(sent[0].Text, "/new --visible --project repo shell") {
		t.Fatalf("sent = %#v", sent)
	}
}

func TestProjectListShowsHealthAndAliasButtons(t *testing.T) {
	d := newApprovalDaemon(t)
	ctx := context.Background()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := d.DB.KVSetString(ctx, projectAliasKey("repo"), dir); err != nil {
		t.Fatal(err)
	}
	mock := telegram.NewMock(nil)
	d.handleProjectCommand(ctx, mock, 100, "list")
	sent := mock.Sent()
	if len(sent) != 1 || !strings.Contains(sent[0].Text, "repo  ok writable git") || sent[0].ReplyMarkup == nil {
		t.Fatalf("sent = %#v", sent)
	}
	if strings.Contains(sent[0].Text, dir) {
		t.Fatalf("raw path leaked: %q", sent[0].Text)
	}
}

func TestDemoApprovalCallbackSendsApproval(t *testing.T) {
	d := newApprovalDaemon(t)
	mock := telegram.NewMock(nil)
	d.Bot = mock
	if err := d.onCallback(context.Background(), mock, &models.CallbackQuery{ID: "cb", From: models.User{ID: 100}}, "demo_approval", ""); err != nil {
		t.Fatal(err)
	}
	sent := mock.Sent()
	if len(sent) != 1 || !strings.Contains(sent[0].Text, "Approval request") || !strings.Contains(sent[0].Text, "Agent: demo") {
		t.Fatalf("sent = %#v", sent)
	}
	pending, err := d.Queue.Pending(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].Agent != "demo" {
		t.Fatalf("pending = %#v", pending)
	}
}

func TestMenuWithMultipleSessionsShowsDefault(t *testing.T) {
	d := newApprovalDaemon(t)
	ctx := context.Background()
	s1 := NewSession("aaa111", "one", "claude", nil, 1024)
	s2 := NewSession("bbb222", "two", "codex", nil, 1024)
	_ = d.Registry.Add(s1)
	_ = d.Registry.Add(s2)
	d.setDefaultTarget(ctx, 100, s2.ID)
	mock := telegram.NewMock(nil)
	d.handleMenuCommand(ctx, mock, 100)
	sent := mock.Sent()
	if len(sent) != 1 || !strings.Contains(sent[0].Text, "target: two (bbb222)") {
		t.Fatalf("sent = %#v", sent)
	}
	if !strings.Contains(fmt.Sprint(sent[0].ReplyMarkup), "* two codex bbb222") {
		t.Fatalf("default not marked: %#v", sent[0].ReplyMarkup)
	}
}

func TestMenuSendCallbackSendsNextText(t *testing.T) {
	d := newApprovalDaemon(t)
	r, s := pipeSession(t, "abc123", "claude")
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	mock := telegram.NewMock(nil)
	if err := d.onCallback(context.Background(), mock, &models.CallbackQuery{
		ID:   "cb",
		From: models.User{ID: 100},
		Data: "msend:" + s.ID,
	}, "menu_send", s.ID); err != nil {
		t.Fatal(err)
	}
	if sent := mock.Sent(); len(sent) != 1 || !strings.Contains(sent[0].Text, "Reply with text to send") {
		t.Fatalf("sent = %#v", sent)
	}
	if err := d.onText(context.Background(), mock, &models.Message{
		From: &models.User{ID: 100},
		Chat: models.Chat{ID: 100},
		Text: "/help",
	}); err != nil {
		t.Fatal(err)
	}
	if got := readPipe(t, r); got != "/help\n" {
		t.Fatalf("sent text = %q", got)
	}
}

func TestMenuSendCallbackUsesSecureWhenEncrypted(t *testing.T) {
	d := newApprovalDaemon(t)
	enableEncryptedTestDaemon(t, d)
	s := NewSession("abc123", "claude", "claude", nil, 1024)
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	mock := telegram.NewMock(nil)
	if err := d.onCallback(context.Background(), mock, &models.CallbackQuery{
		ID:   "cb",
		From: models.User{ID: 100},
		Data: "msend:" + s.ID,
	}, "menu_send", s.ID); err != nil {
		t.Fatal(err)
	}
	if sent := mock.Sent(); len(sent) != 1 || !strings.Contains(sent[0].Text, "Encrypted Onibi controls") {
		t.Fatalf("sent = %#v", sent)
	}
	if _, ok := d.peekPending(context.Background(), pendingKindMenuSend, 100); ok {
		t.Fatal("plaintext send pending in encrypted mode")
	}
}

func TestMenuStatusCallbackAnswersAndSendsStatus(t *testing.T) {
	d := newApprovalDaemon(t)
	mock := telegram.NewMock(nil)
	if err := d.onCallback(context.Background(), mock, &models.CallbackQuery{ID: "cb", From: models.User{ID: 100}}, "menu_status", ""); err != nil {
		t.Fatal(err)
	}
	if answers := mock.Answered(); len(answers) != 1 || answers[0].Text != "Sending status" {
		t.Fatalf("answers = %#v", answers)
	}
	if sent := mock.Sent(); len(sent) != 1 || !strings.Contains(sent[0].Text, "Onibi status") {
		t.Fatalf("sent = %#v", sent)
	}
}

func TestMenuSnoozeCallbacksToggleGlobal(t *testing.T) {
	d := newApprovalDaemon(t)
	ctx := context.Background()
	mock := telegram.NewMock(nil)
	if err := d.onCallback(ctx, mock, &models.CallbackQuery{ID: "cb1", From: models.User{ID: 100}}, "menu_snooze", ""); err != nil {
		t.Fatal(err)
	}
	if got := d.menuSnoozeLabel(ctx); got != "global" {
		t.Fatalf("snooze = %q", got)
	}
	if err := d.onCallback(ctx, mock, &models.CallbackQuery{ID: "cb2", From: models.User{ID: 100}}, "menu_unsnooze", ""); err != nil {
		t.Fatal(err)
	}
	if got := d.menuSnoozeLabel(ctx); got != "off" {
		t.Fatalf("snooze = %q", got)
	}
}

func TestNoopCallbackAnswersAlreadyDecided(t *testing.T) {
	d := newApprovalDaemon(t)
	mock := telegram.NewMock(nil)
	if err := d.onCallback(context.Background(), mock, &models.CallbackQuery{ID: "cb", From: models.User{ID: 100}}, "noop", ""); err != nil {
		t.Fatal(err)
	}
	if answers := mock.Answered(); len(answers) != 1 || answers[0].Text != "Already decided" {
		t.Fatalf("answers = %#v", answers)
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

func TestNotifyTurnCompleteUsesRenderOverride(t *testing.T) {
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

func TestDoubleSlashInjectsSlashCommand(t *testing.T) {
	d := newApprovalDaemon(t)
	r, s := pipeSession(t, "abc123", "claude")
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	mock := telegram.NewMock(nil)
	if err := d.onText(context.Background(), mock, &models.Message{
		From: &models.User{ID: 100},
		Chat: models.Chat{ID: 100},
		Text: "//help",
	}); err != nil {
		t.Fatal(err)
	}
	if got := readPipe(t, r); got != "/help\n" {
		t.Fatalf("injected = %q", got)
	}
}

func TestSendCommandInjectsSlashText(t *testing.T) {
	d := newApprovalDaemon(t)
	r, s := pipeSession(t, "abc123", "claude")
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	mock := telegram.NewMock(nil)
	if err := d.onText(context.Background(), mock, &models.Message{
		From: &models.User{ID: 100},
		Chat: models.Chat{ID: 100},
		Text: "/send /model opus",
	}); err != nil {
		t.Fatal(err)
	}
	if got := readPipe(t, r); got != "/model opus\n" {
		t.Fatalf("injected = %q", got)
	}
}

func TestEnterCommandInjectsNewline(t *testing.T) {
	d := newApprovalDaemon(t)
	r, s := pipeSession(t, "abc123", "codex")
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	mock := telegram.NewMock(nil)
	if err := d.onText(context.Background(), mock, &models.Message{
		From: &models.User{ID: 100},
		Chat: models.Chat{ID: 100},
		Text: "/enter abc",
	}); err != nil {
		t.Fatal(err)
	}
	if got := readPipe(t, r); got != "\n" {
		t.Fatalf("injected = %q", got)
	}
}

func TestEscCommandInjectsEscape(t *testing.T) {
	d := newApprovalDaemon(t)
	r, s := pipeSession(t, "abc123", "codex")
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	mock := telegram.NewMock(nil)
	if err := d.onText(context.Background(), mock, &models.Message{
		From: &models.User{ID: 100},
		Chat: models.Chat{ID: 100},
		Text: "/esc abc",
	}); err != nil {
		t.Fatal(err)
	}
	if got := readPipe(t, r); got != "\x1b" {
		t.Fatalf("injected = %q", got)
	}
}

func TestAgentCommandShell(t *testing.T) {
	bin, agent, args, ok := agentCommand("shell", []string{"zsh", "-c", "echo hi"})
	if !ok || bin != "zsh" || agent != "shell" || strings.Join(args, " ") != "-i -c echo hi" {
		t.Fatalf("bin=%q agent=%q args=%#v ok=%v", bin, agent, args, ok)
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

func TestQueueCommandShowsCardsAndControls(t *testing.T) {
	d := newApprovalDaemon(t)
	ctx := context.Background()
	_, s := pipeSession(t, "abc123", "claude")
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	d.threadMu.Lock()
	d.busySessions[s.ID] = true
	d.threadMu.Unlock()
	if _, err := d.DB.PromptEnqueue(ctx, s.ID, 100, "first line\nsecond line"); err != nil {
		t.Fatal(err)
	}
	mock := telegram.NewMock(nil)
	d.handleQueueCommand(ctx, mock, 100, "")
	sent := mock.Sent()
	if len(sent) != 1 || sent[0].ReplyMarkup == nil {
		t.Fatalf("sent = %#v", sent)
	}
	for _, want := range []string{"Prompt queue:", "target=claude (abc123)", "state=queued", "reason=queued behind active turn", "first line"} {
		if !strings.Contains(sent[0].Text, want) {
			t.Fatalf("queue missing %q:\n%s", want, sent[0].Text)
		}
	}
}

func TestPromptSendRequiresConfirmWhenBusy(t *testing.T) {
	d := newApprovalDaemon(t)
	ctx := context.Background()
	r, s := pipeSession(t, "abc123", "claude")
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	p, err := d.DB.PromptEnqueue(ctx, s.ID, 100, "urgent")
	if err != nil {
		t.Fatal(err)
	}
	d.threadMu.Lock()
	d.busySessions[s.ID] = true
	d.threadMu.Unlock()
	mock := telegram.NewMock(nil)
	if err := d.onCallback(ctx, mock, &models.CallbackQuery{ID: "cb", From: models.User{ID: 100}}, "prompt_send", p.ID); err != nil {
		t.Fatal(err)
	}
	if got := readPipeOptional(r, 50*time.Millisecond); got != "" {
		t.Fatalf("sent before confirm = %q", got)
	}
	if sent := mock.Sent(); len(sent) != 1 || !strings.Contains(sent[0].Text, "Confirm sending") {
		t.Fatalf("sent = %#v", sent)
	}
	if err := d.onCallback(ctx, mock, &models.CallbackQuery{ID: "cb2", From: models.User{ID: 100}}, "prompt_confirm_send", p.ID); err != nil {
		t.Fatal(err)
	}
	if got := readPipe(t, r); got != "urgent\n" {
		t.Fatalf("sent after confirm = %q", got)
	}
	got, err := d.DB.PromptGet(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != store.PromptSent {
		t.Fatalf("state = %s", got.State)
	}
}

func TestPromptTopAndFlushCallbacks(t *testing.T) {
	d := newApprovalDaemon(t)
	ctx := context.Background()
	_, s := pipeSession(t, "abc123", "claude")
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	p1, _ := d.DB.PromptEnqueue(ctx, s.ID, 100, "one")
	p2, _ := d.DB.PromptEnqueue(ctx, s.ID, 100, "two")
	mock := telegram.NewMock(nil)
	if err := d.onCallback(ctx, mock, &models.CallbackQuery{ID: "top", From: models.User{ID: 100}}, "prompt_top", p2.ID); err != nil {
		t.Fatal(err)
	}
	got, err := d.DB.PromptGet(ctx, p2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Position != 1 {
		t.Fatalf("position = %d", got.Position)
	}
	if err := d.onCallback(ctx, mock, &models.CallbackQuery{ID: "flush", From: models.User{ID: 100}}, "prompt_flush", p1.ID); err != nil {
		t.Fatal(err)
	}
	rows, err := d.DB.PromptList(ctx, s.ID, false, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("rows = %#v", rows)
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
	if sent := mock.Sent(); len(sent) != 2 || !strings.Contains(sent[1].Text, "(60s grace)") {
		t.Fatalf("sent = %#v", sent)
	}
}

func TestKillUsesTOTPGraceAfterSuccess(t *testing.T) {
	d := newApprovalDaemon(t)
	_, s1 := pipeSession(t, "abc123", "one")
	_, s2 := pipeSession(t, "def456", "two")
	if err := d.Registry.Add(s1); err != nil {
		t.Fatal(err)
	}
	if err := d.Registry.Add(s2); err != nil {
		t.Fatal(err)
	}
	secret, err := auth.NewSecret()
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Secrets.Set(secrets.KeyTOTPSecret, auth.EncodeHex(secret)); err != nil {
		t.Fatal(err)
	}
	base := time.Now().Truncate(time.Second)
	withTOTPNow(t, base)
	mock := telegram.NewMock(nil)
	code := fmt.Sprintf("%06d", auth.Code(secret, base.Unix()))
	d.handleKillCommand(context.Background(), mock, 100, s1.ID+" "+code)
	if !s1.Ended() {
		t.Fatal("first session not ended")
	}
	withTOTPNow(t, base.Add(30*time.Second))
	d.handleKillCommand(context.Background(), mock, 100, s2.ID)
	if !s2.Ended() {
		t.Fatal("second session not ended within grace")
	}
	if sent := mock.Sent(); len(sent) != 2 || !strings.Contains(sent[1].Text, "(within TOTP grace)") {
		t.Fatalf("sent = %#v", sent)
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

func TestSessionActionCallbackSetsRenderMode(t *testing.T) {
	d := newApprovalDaemon(t)
	_, s := pipeSession(t, "abc123", "claude")
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	mock := telegram.NewMock(nil)
	if err := d.handleSessionActionCallback(context.Background(), mock, &models.CallbackQuery{
		ID:   "cb",
		From: models.User{ID: 100},
		Data: "text:" + s.ID,
	}, "text", s.ID); err != nil {
		t.Fatal(err)
	}
	if got := d.renderOverride(s.ID); got != render.ModeText {
		t.Fatalf("render mode = %s", got)
	}
	if answers := mock.Answered(); len(answers) != 1 || answers[0].Text != "Text output" {
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

func readPipeOptional(r *os.File, timeout time.Duration) string {
	_ = r.SetReadDeadline(time.Now().Add(timeout))
	defer r.SetReadDeadline(time.Time{})
	buf := make([]byte, 256)
	n, err := r.Read(buf)
	if err != nil {
		return ""
	}
	return string(buf[:n])
}
