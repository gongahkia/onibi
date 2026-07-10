package signal

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

func TestProviderSendsApprovalAndStoresTimestamp(t *testing.T) {
	var body rpcTestRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/rpc" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		writeRPCResult(t, w, body.ID, SendResult{Timestamp: 123})
	}))
	defer srv.Close()
	p := NewProvider(New(srv.URL, "+111"), []string{"+222"}, "")
	msgID, err := p.SendApproval(t.Context(), chatout.ApprovalRequest{ID: "apr_1", SessionID: "s1", Agent: "claude", Tool: "Bash", InputJSON: `{"command":"pwd"}`})
	if err != nil {
		t.Fatal(err)
	}
	if msgID != "123" || body.Method != "send" || body.Params["account"] != "+111" {
		t.Fatalf("msgID=%q body=%#v", msgID, body)
	}
	if body.Params["message"] == nil || !strings.Contains(body.Params["message"].(string), "Approval apr_1") {
		t.Fatalf("message = %#v", body.Params["message"])
	}
	if p.approvals[123] != "apr_1" {
		t.Fatalf("approvals = %#v", p.approvals)
	}
}

func TestProviderRoutesTextAndReactionDecision(t *testing.T) {
	p := NewProvider(New("https://signal.local", "+111"), []string{"+222"}, "")
	p.approvals[123] = "apr_1"
	var audit []chatout.AuditInteraction
	p.Audit = func(_ context.Context, item chatout.AuditInteraction) error {
		audit = append(audit, item)
		return nil
	}
	inbound := make(chan string, 1)
	if err := p.OnInboundText(func(text string, sender chatout.Sender) {
		inbound <- text + ":" + sender.ID
	}); err != nil {
		t.Fatal(err)
	}
	decisions := make(chan chatout.Decision, 1)
	if err := p.OnDecision("apr_1", func(d chatout.Decision) { decisions <- d }); err != nil {
		t.Fatal(err)
	}
	if err := p.routeEvent(t.Context(), Event{Envelope: Envelope{
		Source: "+222",
		DataMessage: &DataMessage{
			Message: "pwd",
		},
	}}); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-inbound:
		if got != "pwd:+222" {
			t.Fatalf("inbound = %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("missing inbound")
	}
	if err := p.routeEvent(t.Context(), Event{Envelope: Envelope{
		Source: "+222",
		DataMessage: &DataMessage{
			Reaction: json.RawMessage(`{"emoji":"👍","targetSentTimestamp":123}`),
		},
	}}); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-decisions:
		if got.ApprovalID != "apr_1" || got.Verdict != "approve" || got.MessageID != "123" || got.Sender.ID != "+222" {
			t.Fatalf("decision = %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("missing decision")
	}
	if len(audit) != 2 || audit[0].Kind != "provider.signal.text_in" || audit[1].Kind != "provider.signal.reaction" {
		t.Fatalf("audit = %#v", audit)
	}
}

func TestProviderIgnoresUnauthorizedSource(t *testing.T) {
	p := NewProvider(New("https://signal.local", "+111"), []string{"+222"}, "")
	inbound := make(chan string, 1)
	if err := p.OnInboundText(func(text string, _ chatout.Sender) { inbound <- text }); err != nil {
		t.Fatal(err)
	}
	if err := p.routeEvent(t.Context(), Event{Envelope: Envelope{Source: "+333", DataMessage: &DataMessage{Message: "pwd"}}}); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-inbound:
		t.Fatalf("unexpected inbound = %q", got)
	default:
	}
}

func TestProviderTailStreamAuditsChunks(t *testing.T) {
	var messages []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/rpc" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		var req rpcTestRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		messages = append(messages, req.Params["message"].(string))
		writeRPCResult(t, w, req.ID, SendResult{Timestamp: int64(len(messages))})
	}))
	defer srv.Close()
	p := NewProvider(New(srv.URL, "+111"), []string{"+222"}, "")
	var audit []chatout.AuditInteraction
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
	if len(messages) != 2 || len(messages[0]) != ProviderMessageChunkLimit || len(messages[1]) != 2 {
		t.Fatalf("messages = %#v", messages)
	}
	if len(audit) != 2 || audit[0].Kind != "provider.signal.tail_chunk" || audit[0].SessionID != "s1" || audit[0].MessageID != "1" {
		t.Fatalf("audit = %#v", audit)
	}
}

func TestProviderCapabilitiesAndRateLimit(t *testing.T) {
	p := NewProvider(nil, []string{"+222"}, "")
	if p.Name() != "signal" || len(p.Capabilities()) != 6 {
		t.Fatalf("provider = %s %#v", p.Name(), p.Capabilities())
	}
	rl := p.RateLimit()
	if rl.PerSecond.Window != time.Second || rl.PerMinute.Window != time.Minute {
		t.Fatalf("rate limit = %#v", rl)
	}
}
