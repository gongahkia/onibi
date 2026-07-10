package zulip

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/chatout"
)

func TestProviderSendsApprovalToSessionTopic(t *testing.T) {
	var form map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertAuth(t, r)
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/messages" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		form = map[string]string{
			"type":    r.Form.Get("type"),
			"to":      r.Form.Get("to"),
			"topic":   r.Form.Get("topic"),
			"content": r.Form.Get("content"),
		}
		writeZulipJSON(t, w, map[string]any{"result": "success", "msg": "", "id": 42})
	}))
	defer srv.Close()
	p := NewProvider(New(srv.URL, "bot@example.com", "key"), "ops", "home")
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
	if msgID != "42" || form["type"] != "stream" || form["to"] != "ops" || form["topic"] != "onibi-s1" {
		t.Fatalf("msgID=%q form=%#v", msgID, form)
	}
	if !strings.Contains(form["content"], "Approval apr_1") || !strings.Contains(form["content"], "Reply `approve apr_1` or `deny apr_1`") {
		t.Fatalf("content = %q", form["content"])
	}
}

func TestProviderRoutesInboundTextAndDecisions(t *testing.T) {
	p := NewProvider(New("", "bot@example.com", ""), "ops", "home")
	p.OwnerEmail = "owner@example.com"
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
	if err := p.OnDecision("apr_1", func(d chatout.Decision) { decisions <- d }); err != nil {
		t.Fatal(err)
	}
	err := p.routeEvent(t.Context(), Event{Type: "message", Message: &Message{
		ID:             7,
		Type:           "stream",
		SenderEmail:    "owner@example.com",
		SenderFullName: "Owner",
		TopicName:      "onibi-s1",
		Content:        "pwd",
	}})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-inbound:
		if got != "pwd:owner@example.com:onibi-s1" {
			t.Fatalf("inbound = %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("missing inbound")
	}
	err = p.routeEvent(t.Context(), Event{Type: "message", Message: &Message{
		ID:          8,
		Type:        "stream",
		SenderEmail: "owner@example.com",
		TopicName:   "onibi-s1",
		Content:     "deny apr_1",
	}})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-decisions:
		if got.ApprovalID != "apr_1" || got.Verdict != "deny" || got.MessageID != "8" || got.Sender.ID != "owner@example.com" {
			t.Fatalf("decision = %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("missing decision")
	}
	if len(audit) != 2 || audit[0].Kind != "provider.zulip.text_in" || audit[1].Kind != "provider.zulip.button" {
		t.Fatalf("audit = %#v", audit)
	}
}

func TestProviderTailStreamAuditsChunks(t *testing.T) {
	var posts []map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertAuth(t, r)
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/messages" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		posts = append(posts, map[string]string{"topic": r.Form.Get("topic"), "content": r.Form.Get("content")})
		writeZulipJSON(t, w, map[string]any{"result": "success", "msg": "", "id": len(posts)})
	}))
	defer srv.Close()
	var audit []chatout.AuditInteraction
	p := NewProvider(New(srv.URL, "bot@example.com", "key"), "ops", "home")
	p.Audit = func(_ context.Context, item chatout.AuditInteraction) error {
		audit = append(audit, item)
		return nil
	}
	ch := make(chan []byte, 1)
	ch <- []byte(strings.Repeat("x", providerMessageChunkLimit+2))
	close(ch)
	if err := p.TailStream(t.Context(), "s1", ch); err != nil {
		t.Fatal(err)
	}
	if len(posts) != 2 || posts[0]["topic"] != "onibi-s1" || len(posts[0]["content"]) != providerMessageChunkLimit || len(posts[1]["content"]) != 2 {
		t.Fatalf("posts = %#v", posts)
	}
	if len(audit) != 2 || audit[0].Kind != "provider.zulip.tail_chunk" || audit[0].SessionID != "s1" {
		t.Fatalf("audit = %#v", audit)
	}
}

func TestProviderConnectRoutesEventQueue(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var sawQueue bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/register":
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			var eventTypes []string
			if err := json.Unmarshal([]byte(r.Form.Get("event_types")), &eventTypes); err != nil {
				t.Fatal(err)
			}
			var narrow [][]string
			if err := json.Unmarshal([]byte(r.Form.Get("narrow")), &narrow); err != nil {
				t.Fatal(err)
			}
			if strings.Join(eventTypes, ",") != "message" || len(narrow) != 1 || strings.Join(narrow[0], ":") != "channel:ops" {
				t.Fatalf("form = %#v", r.Form)
			}
			writeZulipJSON(t, w, map[string]any{"result": "success", "queue_id": "q1", "last_event_id": 0})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/events":
			sawQueue = r.URL.Query().Get("queue_id") == "q1"
			writeZulipJSON(t, w, map[string]any{"result": "success", "events": []any{map[string]any{
				"id": 1, "type": "message", "message": map[string]any{
					"id": 1, "type": "stream", "sender_email": "owner@example.com", "subject": "onibi-s1", "content": "hello",
				},
			}}})
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/events":
			writeZulipJSON(t, w, map[string]any{"result": "success", "msg": ""})
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	p := NewProvider(New(srv.URL, "bot@example.com", "key"), "ops", "home")
	p.OwnerEmail = "owner@example.com"
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
		if got != "hello" || !sawQueue {
			t.Fatalf("got=%q sawQueue=%v", got, sawQueue)
		}
	default:
		t.Fatal("missing inbound")
	}
}

func TestProviderCapabilitiesAndRateLimit(t *testing.T) {
	p := NewProvider(nil, "ops", "home")
	if p.Name() != "zulip" || len(p.Capabilities()) != 6 {
		t.Fatalf("provider = %s %#v", p.Name(), p.Capabilities())
	}
	rl := p.RateLimit()
	if rl.PerSecond.Window != time.Second || rl.PerMinute.Window != time.Minute {
		t.Fatalf("rate limit = %#v", rl)
	}
}
