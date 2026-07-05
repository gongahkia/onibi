//go:build !onibi_remote

package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/approval"
	"github.com/gongahkia/onibi/internal/telegram"
	"github.com/gongahkia/onibi/internal/tmux"
)

const testTelegramToken = "123456:ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghi"

func TestSendSessionTextAndCaptureTmux(t *testing.T) {
	r := &tmuxRunner{results: [][]byte{
		nil,
		nil,
		[]byte("$ ls\nREADME.md\nCHANGELOG.md\n"),
		[]byte("$ ls\nREADME.md\nCHANGELOG.md\n"),
		[]byte("$ ls\nREADME.md\nCHANGELOG.md\n"),
	}}
	old := newTmuxController
	newTmuxController = func() *tmux.Controller { return tmux.NewWithRunner(r) }
	t.Cleanup(func() { newTmuxController = old })

	d := New(Options{})
	s := NewSession("s1", "shell", "shell", nil, 0)
	s.Transport = "tmux"
	s.TmuxTarget = "onibi-s1"
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	out, err := d.SendSessionTextAndCapture(context.Background(), "s1", "ls", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "README.md") || !strings.Contains(out, "CHANGELOG.md") {
		t.Fatalf("out = %q", out)
	}
	if !containsCall(r.calls, "send-keys", "-t", "onibi-s1", "-l", "--", "ls") {
		t.Fatalf("missing text send: %#v", r.calls)
	}
	if !containsCall(r.calls, "send-keys", "-t", "onibi-s1", "Enter") {
		t.Fatalf("missing enter send: %#v", r.calls)
	}
}

func TestTelegramBridgePairsOwnerAndPersists(t *testing.T) {
	db := openDaemonTestDB(t)
	d := New(Options{DB: db, TelegramPair: "123456"})
	spy, client := newTelegramAPISpy(t)
	b := &telegramBridge{
		d:         d,
		client:    client,
		seen:      map[string]bool{},
		killArmed: map[int64]time.Time{},
	}

	b.handleUpdate(context.Background(), telegram.Update{Message: &telegram.Message{
		Chat: telegram.Chat{ID: 42},
		Text: "/start 123456",
	}})

	if b.owner() != 42 || d.TelegramOwnerID != 42 {
		t.Fatalf("owner bridge=%d daemon=%d", b.owner(), d.TelegramOwnerID)
	}
	if got, ok, err := db.KVGetString(context.Background(), TelegramKVOwnerChatID); err != nil || !ok || got != "42" {
		t.Fatalf("owner kv got=%q ok=%v err=%v", got, ok, err)
	}
	if _, ok, err := db.KVGetString(context.Background(), TelegramKVPairCode); err != nil || ok {
		t.Fatalf("pair kv ok=%v err=%v", ok, err)
	}
	if got := spy.messageTexts(); len(got) != 2 || !strings.Contains(got[0], "Paired") || !strings.Contains(got[1], "Onibi Telegram") {
		t.Fatalf("messages = %#v", got)
	}

	b.handleUpdate(context.Background(), telegram.Update{Message: &telegram.Message{
		Chat: telegram.Chat{ID: 99},
		Text: "/help",
	}})
	if got := spy.messageTexts(); len(got) != 2 {
		t.Fatalf("unauthorized chat got messages = %#v", got)
	}
}

func TestTelegramBridgePlainTextRoutesToTargetSession(t *testing.T) {
	r := &tmuxRunner{results: [][]byte{
		nil,
		nil,
		[]byte("$ pwd\n/tmp/onibi\n"),
		[]byte("$ pwd\n/tmp/onibi\n"),
		[]byte("$ pwd\n/tmp/onibi\n"),
	}}
	old := newTmuxController
	newTmuxController = func() *tmux.Controller { return tmux.NewWithRunner(r) }
	t.Cleanup(func() { newTmuxController = old })

	db := openDaemonTestDB(t)
	d := New(Options{DB: db, TelegramOwnerID: 42})
	s := NewSession("s1", "shell", "shell", nil, 0)
	s.Transport = "tmux"
	s.TmuxTarget = "onibi-s1"
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	spy, client := newTelegramAPISpy(t)
	b := &telegramBridge{
		d:         d,
		client:    client,
		ownerID:   42,
		seen:      map[string]bool{},
		killArmed: map[int64]time.Time{},
	}
	b.setTarget(context.Background(), 42, "s1")

	b.handleUpdate(context.Background(), telegram.Update{Message: &telegram.Message{
		Chat: telegram.Chat{ID: 42},
		Text: "pwd",
	}})

	if !containsCall(r.calls, "send-keys", "-t", "onibi-s1", "-l", "--", "pwd") {
		t.Fatalf("missing text send: %#v", r.calls)
	}
	if got := strings.Join(spy.messageTexts(), "\n"); !strings.Contains(got, "/tmp/onibi") {
		t.Fatalf("messages = %q", got)
	}
}

func TestTelegramBridgeTargetPersistsAcrossRestart(t *testing.T) {
	db := openDaemonTestDB(t)
	d := New(Options{DB: db, TelegramOwnerID: 42})
	s1 := NewSession("s1", "shell", "shell", nil, 0)
	s2 := NewSession("s2", "claude", "claude", nil, 0)
	if err := d.Registry.Add(s1); err != nil {
		t.Fatal(err)
	}
	if err := d.Registry.Add(s2); err != nil {
		t.Fatal(err)
	}
	spy, client := newTelegramAPISpy(t)
	b := &telegramBridge{
		d:         d,
		client:    client,
		ownerID:   42,
		seen:      map[string]bool{},
		killArmed: map[int64]time.Time{},
	}
	b.handleUpdate(context.Background(), telegram.Update{Message: &telegram.Message{
		Chat: telegram.Chat{ID: 42},
		Text: "/target claude",
	}})
	if got := b.target(context.Background(), 42); got != "s2" {
		t.Fatalf("target = %q", got)
	}
	if got := strings.Join(spy.messageTexts(), "\n"); !strings.Contains(got, "Target: claude (s2)") {
		t.Fatalf("messages = %q", got)
	}
	restarted := &telegramBridge{d: New(Options{DB: db, TelegramOwnerID: 42}), ownerID: 42}
	if got := restarted.target(context.Background(), 42); got != "s2" {
		t.Fatalf("restarted target = %q", got)
	}
}

func TestTelegramBridgeLongOutputChunks(t *testing.T) {
	db := openDaemonTestDB(t)
	d := New(Options{DB: db, TelegramOwnerID: 42})
	spy, client := newTelegramAPISpy(t)
	b := &telegramBridge{d: d, client: client, ownerID: 42}
	b.sendChunks(context.Background(), 42, strings.Repeat("x", 9000))
	msgs := spy.messageTexts()
	if len(msgs) < 3 {
		t.Fatalf("messages = %d", len(msgs))
	}
	for _, msg := range msgs {
		if len(msg) > 3800 {
			t.Fatalf("chunk too long: %d", len(msg))
		}
	}
}

func TestTelegramBridgeKillRequiresConfirmation(t *testing.T) {
	r := &tmuxRunner{}
	old := newTmuxController
	newTmuxController = func() *tmux.Controller { return tmux.NewWithRunner(r) }
	t.Cleanup(func() { newTmuxController = old })

	db := openDaemonTestDB(t)
	d := New(Options{DB: db, TelegramOwnerID: 42})
	s := NewSession("s1", "shell", "shell", nil, 0)
	s.Transport = "tmux"
	s.TmuxTarget = "onibi-s1"
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	spy, client := newTelegramAPISpy(t)
	b := &telegramBridge{
		d:         d,
		client:    client,
		ownerID:   42,
		seen:      map[string]bool{},
		killArmed: map[int64]time.Time{},
	}
	b.setTarget(context.Background(), 42, "s1")

	msg := telegram.Update{Message: &telegram.Message{Chat: telegram.Chat{ID: 42}, Text: "/kill"}}
	b.handleUpdate(context.Background(), msg)
	if containsCall(r.calls, "kill-session", "-t", "onibi-s1") {
		t.Fatalf("first kill should only arm: %#v", r.calls)
	}
	b.handleUpdate(context.Background(), msg)
	if !containsCall(r.calls, "kill-session", "-t", "onibi-s1") || !s.Ended() {
		t.Fatalf("second kill failed: calls=%#v ended=%v", r.calls, s.Ended())
	}
	if got := strings.Join(spy.messageTexts(), "\n"); !strings.Contains(got, "within 2s") || !strings.Contains(got, "Killed") {
		t.Fatalf("messages = %q", got)
	}
}

func TestTelegramBridgeApprovalDedupAndDenyCallback(t *testing.T) {
	db := openDaemonTestDB(t)
	d := New(Options{DB: db, TelegramOwnerID: 42})
	spy, client := newTelegramAPISpy(t)
	b := &telegramBridge{
		d:         d,
		client:    client,
		ownerID:   42,
		seen:      map[string]bool{},
		killArmed: map[int64]time.Time{},
	}
	id, _, err := d.Queue.Request(context.Background(), "s1", "claude", "Bash", `{"command":"ls"}`)
	if err != nil {
		t.Fatal(err)
	}
	a, err := d.Queue.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}

	b.sendApproval(context.Background(), a)
	b.sendApproval(context.Background(), a)
	if got := spy.messagesCopy(); len(got) != 1 || got[0].ReplyMarkup == nil || !strings.Contains(got[0].Text, "Approval "+id) {
		t.Fatalf("messages = %#v", got)
	}

	b.handleCallback(context.Background(), &telegram.CallbackQuery{
		ID:      "cb1",
		Message: &telegram.Message{Chat: telegram.Chat{ID: 42}},
		Data:    "dn:" + id,
	})
	decided, err := d.Queue.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if decided.State != approval.StateDenied || decided.DecidedBy != 42 {
		t.Fatalf("approval = %#v", decided)
	}
	if got := spy.callbackTexts(); len(got) != 1 || got[0] != "ok" {
		t.Fatalf("callbacks = %#v", got)
	}
}

func TestTelegramBridgeApprovalCallbackAfterRestart(t *testing.T) {
	db := openDaemonTestDB(t)
	d1 := New(Options{DB: db, TelegramOwnerID: 42})
	id, _, err := d1.Queue.Request(context.Background(), "s1", "claude", "Bash", `{"command":"ls"}`)
	if err != nil {
		t.Fatal(err)
	}
	d2 := New(Options{DB: db, TelegramOwnerID: 42})
	spy, client := newTelegramAPISpy(t)
	b := &telegramBridge{
		d:         d2,
		client:    client,
		ownerID:   42,
		seen:      map[string]bool{},
		killArmed: map[int64]time.Time{},
	}
	b.handleCallback(context.Background(), &telegram.CallbackQuery{
		ID:      "cb-restart",
		Message: &telegram.Message{Chat: telegram.Chat{ID: 42}},
		Data:    "dn:" + id,
	})
	a, err := d2.Queue.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if a.State != approval.StateDenied || a.DecidedBy != 42 {
		t.Fatalf("approval = %#v", a)
	}
	if got := spy.callbackTexts(); len(got) != 1 || got[0] != "ok" {
		t.Fatalf("callbacks = %#v", got)
	}
}

func TestTelegramBridgeHighRiskApprovalRequiresConfirm(t *testing.T) {
	db := openDaemonTestDB(t)
	d := New(Options{DB: db, TelegramOwnerID: 42})
	spy, client := newTelegramAPISpy(t)
	b := &telegramBridge{
		d:         d,
		client:    client,
		ownerID:   42,
		seen:      map[string]bool{},
		killArmed: map[int64]time.Time{},
	}
	id, _, err := d.Queue.Request(context.Background(), "s1", "claude", "Bash", `{"command":"rm -rf /tmp/onibi"}`)
	if err != nil {
		t.Fatal(err)
	}

	b.handleCallback(context.Background(), &telegram.CallbackQuery{
		ID:      "cb1",
		Message: &telegram.Message{Chat: telegram.Chat{ID: 42}},
		Data:    "ap:" + id,
	})
	a, err := d.Queue.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if a.State != approval.StatePending {
		t.Fatalf("approval state after first approve = %s", a.State)
	}
	if got := strings.Join(spy.messageTexts(), "\n"); !strings.Contains(got, "High-risk approval") {
		t.Fatalf("messages = %q", got)
	}

	b.handleCallback(context.Background(), &telegram.CallbackQuery{
		ID:      "cb2",
		Message: &telegram.Message{Chat: telegram.Chat{ID: 42}},
		Data:    "cf:" + id,
	})
	a, err = d.Queue.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if a.State != approval.StateApproved || a.DecidedBy != 42 {
		t.Fatalf("approval = %#v", a)
	}
	if got := spy.callbackTexts(); len(got) != 2 || got[0] != "confirm required" || got[1] != "ok" {
		t.Fatalf("callbacks = %#v", got)
	}
}

func TestNewTelegramPairCodeSixDigits(t *testing.T) {
	code, err := NewTelegramPairCode()
	if err != nil {
		t.Fatal(err)
	}
	if len(code) != 6 {
		t.Fatalf("code = %q", code)
	}
	for _, r := range code {
		if r < '0' || r > '9' {
			t.Fatalf("code = %q", code)
		}
	}
}

type telegramAPISpy struct {
	t         *testing.T
	mu        sync.Mutex
	messages  []sentTelegramMessage
	callbacks []sentTelegramCallback
}

type sentTelegramMessage struct {
	ChatID      int64                          `json:"chat_id"`
	Text        string                         `json:"text"`
	ReplyMarkup *telegram.InlineKeyboardMarkup `json:"reply_markup"`
}

type sentTelegramCallback struct {
	ID   string `json:"callback_query_id"`
	Text string `json:"text"`
}

func newTelegramAPISpy(t *testing.T) (*telegramAPISpy, *telegram.Client) {
	t.Helper()
	spy := &telegramAPISpy{t: t}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/sendMessage"):
			var msg sentTelegramMessage
			if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
				t.Fatal(err)
			}
			spy.mu.Lock()
			spy.messages = append(spy.messages, msg)
			msgID := int64(len(spy.messages))
			spy.mu.Unlock()
			writeTelegramAPIResult(t, w, telegram.Message{MessageID: msgID, Chat: telegram.Chat{ID: msg.ChatID}, Text: msg.Text})
		case strings.HasSuffix(r.URL.Path, "/answerCallbackQuery"):
			var cb sentTelegramCallback
			if err := json.NewDecoder(r.Body).Decode(&cb); err != nil {
				t.Fatal(err)
			}
			spy.mu.Lock()
			spy.callbacks = append(spy.callbacks, cb)
			spy.mu.Unlock()
			writeTelegramAPIResult(t, w, true)
		default:
			t.Fatalf("unexpected telegram path %s", r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)
	client := telegram.NewClient(testTelegramToken)
	client.BaseURL = srv.URL
	return spy, client
}

func (s *telegramAPISpy) messagesCopy() []sentTelegramMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]sentTelegramMessage(nil), s.messages...)
}

func (s *telegramAPISpy) messageTexts() []string {
	msgs := s.messagesCopy()
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, m.Text)
	}
	return out
}

func (s *telegramAPISpy) callbackTexts() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.callbacks))
	for _, c := range s.callbacks {
		out = append(out, c.Text)
	}
	return out
}

func writeTelegramAPIResult(t *testing.T, w http.ResponseWriter, result any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": result}); err != nil {
		t.Fatal(err)
	}
}
