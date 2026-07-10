package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/chatout"
)

func TestProviderSendsApprovalWithTelegramButtons(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/sendMessage") {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		writeTG(t, w, Message{MessageID: 91, Chat: Chat{ID: 42}, Text: "approval"})
	}))
	defer srv.Close()

	c := NewClient(testToken)
	c.BaseURL = srv.URL
	c.RetrySleep = func(context.Context, time.Duration) error { return nil }
	p := NewProvider(c, 42)
	msgID, err := p.SendApproval(t.Context(), chatout.ApprovalRequest{
		ID:        "apr_1",
		SessionID: "s1",
		Agent:     "claude",
		Tool:      "Bash",
		InputJSON: `{"command":"pwd"}`,
		RiskLevel: "low",
	})
	if err != nil {
		t.Fatal(err)
	}
	if msgID != "91" {
		t.Fatalf("message id = %q", msgID)
	}
	if body["chat_id"] != float64(42) || !strings.Contains(body["text"].(string), "Approval apr_1") {
		t.Fatalf("body = %#v", body)
	}
	markup := body["reply_markup"].(map[string]any)
	rows := markup["inline_keyboard"].([]any)
	buttons := rows[0].([]any)
	if buttons[0].(map[string]any)["callback_data"] != "ap:apr_1" || buttons[1].(map[string]any)["callback_data"] != "dn:apr_1" {
		t.Fatalf("buttons = %#v", buttons)
	}
}

func TestProviderRoutesInboundTextAndCallbackDecisions(t *testing.T) {
	var answered []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/answerCallbackQuery") {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		answered = append(answered, body["callback_query_id"].(string))
		writeTG(t, w, true)
	}))
	defer srv.Close()

	c := NewClient(testToken)
	c.BaseURL = srv.URL
	c.RetrySleep = func(context.Context, time.Duration) error { return nil }
	p := NewProvider(c, 42)
	var audit []chatout.AuditInteraction
	p.Audit = func(_ context.Context, item chatout.AuditInteraction) error {
		audit = append(audit, item)
		return nil
	}
	inbound := make(chan string, 1)
	if err := p.OnInboundText(func(text string, sender chatout.Sender) {
		inbound <- text + ":" + sender.ID + ":" + sender.ChannelID
	}); err != nil {
		t.Fatal(err)
	}
	decisions := make(chan chatout.Decision, 1)
	if err := p.OnDecision("*", func(d chatout.Decision) { decisions <- d }); err != nil {
		t.Fatal(err)
	}

	if err := p.routeUpdate(t.Context(), Update{Message: &Message{
		Chat: Chat{ID: 42},
		From: &User{ID: 7, Username: "owner"},
		Text: "pwd",
	}}); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-inbound:
		if got != "pwd:7:42" {
			t.Fatalf("inbound = %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("missing inbound callback")
	}

	if err := p.routeUpdate(t.Context(), Update{CallbackQuery: &CallbackQuery{
		ID:   "cb_1",
		From: User{ID: 7, Username: "owner"},
		Message: &Message{
			MessageID: 99,
			Chat:      Chat{ID: 42},
		},
		Data: "dn:apr_1",
	}}); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-decisions:
		if got.ApprovalID != "apr_1" || got.Verdict != "deny" || got.MessageID != "99" || got.Sender.ID != "7" {
			t.Fatalf("decision = %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("missing decision callback")
	}
	if len(answered) != 1 || answered[0] != "cb_1" {
		t.Fatalf("answered = %#v", answered)
	}
	if len(audit) != 2 || audit[0].Kind != "provider.telegram.text_in" || audit[1].Kind != "provider.telegram.button" {
		t.Fatalf("audit = %#v", audit)
	}
}

func TestProviderConnectPollsUpdates(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		switch {
		case strings.HasSuffix(r.URL.Path, "/deleteWebhook"):
			writeTG(t, w, true)
		case strings.HasSuffix(r.URL.Path, "/getUpdates"):
			writeTG(t, w, []Update{{UpdateID: 5, Message: &Message{Chat: Chat{ID: 42}, Text: "hello"}}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	c := NewClient(testToken)
	c.BaseURL = srv.URL
	c.RetrySleep = func(context.Context, time.Duration) error { return context.Canceled }
	p := NewProvider(c, 42)
	p.Timeout = 1
	seen := make(chan string, 1)
	if err := p.OnInboundText(func(text string, _ chatout.Sender) {
		seen <- text
		cancel()
	}); err != nil {
		t.Fatal(err)
	}
	err := p.Connect(ctx)
	if err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("connect err = %v", err)
	}
	select {
	case got := <-seen:
		if got != "hello" {
			t.Fatalf("seen = %q", got)
		}
	default:
		t.Fatal("missing polled update")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(paths) < 2 || !strings.HasSuffix(paths[0], "/deleteWebhook") || !strings.HasSuffix(paths[1], "/getUpdates") {
		t.Fatalf("paths = %#v", paths)
	}
}

func TestProviderTailStreamAuditsChunks(t *testing.T) {
	var texts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/sendMessage") {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		texts = append(texts, body["text"].(string))
		writeTG(t, w, Message{MessageID: int64(len(texts)), Chat: Chat{ID: 42}, Text: body["text"].(string)})
	}))
	defer srv.Close()

	c := NewClient(testToken)
	c.BaseURL = srv.URL
	c.RetrySleep = func(context.Context, time.Duration) error { return nil }
	var audit []chatout.AuditInteraction
	p := NewProvider(c, 42)
	p.Audit = func(_ context.Context, item chatout.AuditInteraction) error {
		audit = append(audit, item)
		return nil
	}
	ch := make(chan []byte, 1)
	ch <- []byte(strings.Repeat("x", ProviderMessageChunkLimit+2))
	close(ch)
	if err := p.TailStream(t.Context(), "s1", ch); err != nil {
		t.Fatal(err)
	}
	if len(texts) != 2 || len(texts[0]) != ProviderMessageChunkLimit || len(texts[1]) != 2 {
		t.Fatalf("texts = %d", len(texts))
	}
	if len(audit) != 2 || audit[0].Kind != "provider.telegram.tail_chunk" || audit[0].SessionID != "s1" {
		t.Fatalf("audit = %#v", audit)
	}
}
