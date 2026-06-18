package daemon

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/gongahkia/onibi/internal/approval"
	"github.com/gongahkia/onibi/internal/envelope"
	"github.com/gongahkia/onibi/internal/telegram"
)

func enableEncryptedTestDaemon(t *testing.T, d *Daemon) string {
	t.Helper()
	seed, err := envelope.GenerateSeed()
	if err != nil {
		t.Fatal(err)
	}
	d.EncryptedMode = "on"
	d.EnvelopeSeed = seed
	d.MiniAppURL = "https://example.com/onibi/"
	return seed
}

func decryptSentMessage(t *testing.T, seed string, msg tgbot.SendMessageParams) envelope.Plain {
	t.Helper()
	kb, ok := msg.ReplyMarkup.(*models.ReplyKeyboardMarkup)
	if !ok {
		t.Fatalf("reply markup = %T", msg.ReplyMarkup)
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
	return plain
}

func assertSentMessagesHide(t *testing.T, mock *telegram.Mock, secret string) {
	t.Helper()
	for _, msg := range mock.Sent() {
		if strings.Contains(msg.Text, secret) {
			t.Fatalf("telegram text leaked %q in %q", secret, msg.Text)
		}
	}
}

func TestEncryptedTextOutputHidesPayload(t *testing.T) {
	d := newApprovalDaemon(t)
	seed := enableEncryptedTestDaemon(t, d)
	mock := telegram.NewMock(nil)
	if _, err := d.sendTextOutput(context.Background(), mock, 100, "session done", "secret output", "out.txt"); err != nil {
		t.Fatal(err)
	}
	sent := mock.Sent()
	if len(sent) != 1 {
		t.Fatalf("sent = %d", len(sent))
	}
	assertSentMessagesHide(t, mock, "secret output")
	plain := decryptSentMessage(t, seed, sent[0])
	if plain.Body != "secret output" || plain.Title != "session done" {
		t.Fatalf("plain = %#v", plain)
	}
}

func TestAskApprovalDoesNotSendPlaintextCopy(t *testing.T) {
	d := newApprovalDaemon(t)
	seed := enableEncryptedTestDaemon(t, d)
	d.EncryptedMode = "ask"
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
	assertSentMessagesHide(t, mock, "echo secret")
	plain := decryptSentMessage(t, seed, sent[0])
	if !strings.Contains(plain.Body, "echo secret") {
		t.Fatalf("plain body = %q", plain.Body)
	}
}

func TestEncryptedFreeTextRequiresSecureControls(t *testing.T) {
	d := newApprovalDaemon(t)
	enableEncryptedTestDaemon(t, d)
	_, s := pipeSession(t, "abc123", "claude")
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	mock := telegram.NewMock(nil)
	if err := d.onText(context.Background(), mock, &models.Message{
		From: &models.User{ID: 100},
		Chat: models.Chat{ID: 100},
		Text: "secret prompt",
	}); err != nil {
		t.Fatal(err)
	}
	assertSentMessagesHide(t, mock, "secret prompt")
	rows, err := d.DB.PromptList(context.Background(), s.ID, true, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("prompt rows = %#v", rows)
	}
}

func TestEncryptedWebAppPromptWritesWithoutPlainAck(t *testing.T) {
	d := newApprovalDaemon(t)
	seed := enableEncryptedTestDaemon(t, d)
	r, s := pipeSession(t, "abc123", "claude")
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	action := webAppDecision{Version: 1, Action: "prompt", Session: s.ID, Text: "secret prompt"}
	body, err := json.Marshal(action)
	if err != nil {
		t.Fatal(err)
	}
	token, err := envelope.Encrypt(seed, envelope.Plain{Kind: "action", Body: string(body)}, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(webAppEnvelopePayload{Version: 1, Envelope: token})
	if err != nil {
		t.Fatal(err)
	}
	mock := telegram.NewMock(nil)
	if err := d.onWebAppData(context.Background(), mock, &models.Message{
		From:       &models.User{ID: 100},
		Chat:       models.Chat{ID: 100},
		WebAppData: &models.WebAppData{Data: string(payload), ButtonText: "Open secure controls"},
	}); err != nil {
		t.Fatal(err)
	}
	if got := readPipe(t, r); got != "secret prompt\n" {
		t.Fatalf("injected = %q", got)
	}
	assertSentMessagesHide(t, mock, "secret prompt")
}

func TestEncryptedWebAppDenyCarriesReason(t *testing.T) {
	d := newApprovalDaemon(t)
	seed := enableEncryptedTestDaemon(t, d)
	ctx := context.Background()
	id, ch, err := d.Queue.Request(ctx, "s", "claude", "Bash", `{"command":"rm x"}`)
	if err != nil {
		t.Fatal(err)
	}
	action := webAppDecision{Version: 1, Action: "deny", ID: id, Reason: "not now"}
	body, err := json.Marshal(action)
	if err != nil {
		t.Fatal(err)
	}
	token, err := envelope.Encrypt(seed, envelope.Plain{Kind: "action", Body: string(body)}, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(webAppEnvelopePayload{Version: 1, Envelope: token})
	if err != nil {
		t.Fatal(err)
	}
	mock := telegram.NewMock(nil)
	if err := d.onWebAppData(ctx, mock, &models.Message{
		From:       &models.User{ID: 100},
		Chat:       models.Chat{ID: 100},
		WebAppData: &models.WebAppData{Data: string(payload), ButtonText: "Open secure controls"},
	}); err != nil {
		t.Fatal(err)
	}
	dec := <-ch
	if dec.Verdict != approval.VerdictDeny || dec.Reason != "not now" {
		t.Fatalf("decision = %#v", dec)
	}
}
