package slack

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

func TestProviderSendsApprovalWithSlackBlocks(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat.postMessage") {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		writeSlackOK(t, w, map[string]any{"channel": "C1", "ts": "111.222"})
	}))
	defer srv.Close()
	c := New("xapp-token", "xoxb-token")
	c.BaseURL = srv.URL
	p := NewProvider(c, "C1")
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
	if msgID != "C1:111.222" || body["channel"] != "C1" || body["text"] != "Onibi approval apr_1" {
		t.Fatalf("msgID=%q body=%#v", msgID, body)
	}
	blocks := body["blocks"].([]any)
	actions := blocks[1].(map[string]any)["elements"].([]any)
	var ids []string
	for _, raw := range actions {
		button := raw.(map[string]any)
		ids = append(ids, button["action_id"].(string))
		if providerApprovalID(button["value"].(string)) != "apr_1" {
			t.Fatalf("button value = %#v", button)
		}
	}
	if strings.Join(ids, ",") != "approve,deny,edit" {
		t.Fatalf("action ids = %v", ids)
	}
}

func TestProviderRoutesInboundTextAndDecisions(t *testing.T) {
	p := NewProvider(nil, "C1")
	p.Allow = Allowlist{Channels: map[string]bool{"C1": true}}
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
	if err := p.OnDecision("C1:111.222", func(d chatout.Decision) { decisions <- d }); err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.routeEnvelope(t.Context(), Envelope{
		Type:    "events_api",
		Payload: json.RawMessage(`{"event":{"type":"message","channel":"C1","user":"U1","text":"pwd","channel_type":"channel"}}`),
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-inbound:
		if got != "pwd:U1:C1" {
			t.Fatalf("inbound = %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("missing inbound callback")
	}
	value := providerApprovalValue(chatout.ApprovalRequest{ID: "apr_1", SessionID: "s1", Agent: "claude", Tool: "Bash"}, "deny")
	if _, _, err := p.routeEnvelope(t.Context(), Envelope{
		Type:    "interactive",
		Payload: json.RawMessage(`{"type":"block_actions","user":{"id":"U1"},"channel":{"id":"C1"},"message":{"ts":"111.222"},"actions":[{"action_id":"deny","value":` + quote(value) + `}]}`),
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-decisions:
		if got.ApprovalID != "apr_1" || got.Verdict != "deny" || got.MessageID != "C1:111.222" || got.Sender.ID != "U1" {
			t.Fatalf("decision = %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("missing decision callback")
	}
	if len(audit) != 2 || audit[0].Kind != "provider.slack.text_in" || audit[1].Kind != "provider.slack.button" {
		t.Fatalf("audit = %#v", audit)
	}
}

func TestProviderEditModalAndSubmission(t *testing.T) {
	var view map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/chat.postMessage"):
			writeSlackOK(t, w, map[string]any{"channel": "C1", "ts": "111.222"})
			return
		case strings.HasSuffix(r.URL.Path, "/views.open"):
		default:
			t.Fatalf("path = %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&view); err != nil {
			t.Fatal(err)
		}
		writeSlackOK(t, w, map[string]any{"view": map[string]any{"id": "V1"}})
	}))
	defer srv.Close()
	c := New("xapp-token", "xoxb-token")
	c.BaseURL = srv.URL
	p := NewProvider(c, "C1")
	if _, err := p.SendApproval(t.Context(), chatout.ApprovalRequest{ID: "apr_1", InputJSON: `{"command":"ls"}`}); err != nil {
		t.Fatal(err)
	}
	decisions := make(chan chatout.Decision, 1)
	if err := p.OnDecision("apr_1", func(d chatout.Decision) { decisions <- d }); err != nil {
		t.Fatal(err)
	}
	value := providerApprovalValue(chatout.ApprovalRequest{ID: "apr_1"}, "edit")
	if _, _, err := p.routeEnvelope(t.Context(), Envelope{
		Type:    "interactive",
		Payload: json.RawMessage(`{"type":"block_actions","trigger_id":"TR1","user":{"id":"U1"},"channel":{"id":"C1"},"message":{"ts":"111.222"},"actions":[{"action_id":"edit","value":` + quote(value) + `}]}`),
	}); err != nil {
		t.Fatal(err)
	}
	if view["trigger_id"] != "TR1" {
		t.Fatalf("view body = %#v", view)
	}
	modal := view["view"].(map[string]any)
	if modal["callback_id"] != providerEditCallback || modal["private_metadata"] != "apr_1" {
		t.Fatalf("modal = %#v", modal)
	}
	if _, _, err := p.routeEnvelope(t.Context(), Envelope{
		Type:    "interactive",
		Payload: json.RawMessage(`{"type":"view_submission","user":{"id":"U1"},"view":{"id":"V1","callback_id":"onibi_approval_edit","private_metadata":"apr_1","state":{"values":{"edited_input":{"json":{"type":"plain_text_input","value":"{\"command\":\"pwd\"}"}}}}}}`),
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-decisions:
		if got.ApprovalID != "apr_1" || got.Verdict != "edit" || got.UpdatedInput != `{"command":"pwd"}` || got.MessageID != "V1" {
			t.Fatalf("decision = %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("missing edit decision")
	}
}

func TestProviderTailStreamAuditsChunks(t *testing.T) {
	var posts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat.postMessage") {
			t.Fatalf("path = %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		posts = append(posts, body["text"].(string))
		writeSlackOK(t, w, nil)
	}))
	defer srv.Close()
	c := New("xapp-token", "xoxb-token")
	c.BaseURL = srv.URL
	c.PostPace = -1
	var audit []chatout.AuditInteraction
	p := NewProvider(c, "C1")
	p.Audit = func(_ context.Context, item chatout.AuditInteraction) error {
		audit = append(audit, item)
		return nil
	}
	ch := make(chan []byte, 1)
	ch <- []byte(strings.Repeat("x", MessageChunkLimit+2))
	close(ch)
	if err := p.TailStream(t.Context(), "s1", ch); err != nil {
		t.Fatal(err)
	}
	if len(posts) != 2 || len(posts[0]) != MessageChunkLimit || len(posts[1]) != 2 {
		t.Fatalf("posts = %d %v", len(posts), chunkLens(posts))
	}
	if len(audit) != 2 || audit[0].Kind != "provider.slack.tail_chunk" || audit[0].SessionID != "s1" {
		t.Fatalf("audit = %#v", audit)
	}
}

func TestProviderCapabilitiesAndRateLimit(t *testing.T) {
	p := NewProvider(nil, "C1")
	if p.Name() != "slack" {
		t.Fatalf("name = %q", p.Name())
	}
	if len(p.Capabilities()) != 6 {
		t.Fatalf("capabilities = %#v", p.Capabilities())
	}
	rl := p.RateLimit()
	if rl.PerSecond.Limit != 1 || rl.PerSecond.Window != time.Second || rl.PerMinute.Window != time.Minute {
		t.Fatalf("rate limit = %#v", rl)
	}
}

func TestProviderReconnectDelayBounds(t *testing.T) {
	for _, attempt := range []int{-1, 0, 1, 4, 99} {
		got := providerReconnectDelay(attempt)
		normalized := attempt
		if normalized < 0 {
			normalized = 0
		}
		if normalized > 5 {
			normalized = 5
		}
		base := time.Second << normalized
		if base > providerReconnectMax {
			base = providerReconnectMax
		}
		if got < base || got > base+base/2 {
			t.Fatalf("attempt %d delay %s outside [%s,%s]", attempt, got, base, base+base/2)
		}
	}
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
