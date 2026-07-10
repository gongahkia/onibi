package discord

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

func TestProviderSendsApprovalWithComponents(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/channels/C1/messages") {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		_ = json.NewEncoder(w).Encode(Message{ID: "m1", ChannelID: "C1"})
	}))
	defer srv.Close()
	c := New("bot-token")
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
	if msgID != "m1" || int(body["flags"].(float64)) != ComponentsV2Flag {
		t.Fatalf("msgID=%q body=%#v", msgID, body)
	}
	components := body["components"].([]any)
	buttons := components[1].(map[string]any)["components"].([]any)
	var ids []string
	for _, raw := range buttons {
		ids = append(ids, raw.(map[string]any)["custom_id"].(string))
	}
	if strings.Join(ids, ",") != "onibi:approval:approve:apr_1,onibi:approval:deny:apr_1,onibi:approval:edit:apr_1" {
		t.Fatalf("custom ids = %v", ids)
	}
}

func TestProviderRoutesMessageAndDecision(t *testing.T) {
	var callbacks []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/interactions/i1/tok/callback") {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		callbacks = append(callbacks, body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	c := New("bot-token")
	c.BaseURL = srv.URL
	p := NewProvider(c, "C1")
	p.Allow = map[string]bool{"C1": true}
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
	if err := p.OnDecision("m1", func(d chatout.Decision) { decisions <- d }); err != nil {
		t.Fatal(err)
	}
	_, err := p.routeFrame(t.Context(), GatewayFrame{Op: OpDispatch, T: "MESSAGE_CREATE", D: mustJSON(MessageCreate{
		ChannelID: "C1",
		Content:   "pwd",
		Author: struct {
			ID string `json:"id"`
		}{ID: "U1"},
	})})
	if err != nil {
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
	_, err = p.routeFrame(t.Context(), GatewayFrame{Op: OpDispatch, T: "INTERACTION_CREATE", D: mustJSON(map[string]any{
		"id": "i1", "token": "tok", "type": 3, "channel_id": "C1",
		"user":    map[string]any{"id": "U1"},
		"message": map[string]any{"id": "m1", "channel_id": "C1"},
		"data":    map[string]any{"custom_id": "onibi:approval:deny:apr_1", "component_type": 2},
	})})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-decisions:
		if got.ApprovalID != "apr_1" || got.Verdict != "deny" || got.MessageID != "m1" || got.Sender.ID != "U1" {
			t.Fatalf("decision = %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("missing decision")
	}
	if len(callbacks) != 1 || int(callbacks[0]["type"].(float64)) != 4 {
		t.Fatalf("callbacks = %#v", callbacks)
	}
	if len(audit) != 2 || audit[0].Kind != "provider.discord.text_in" || audit[1].Kind != "provider.discord.button" {
		t.Fatalf("audit = %#v", audit)
	}
}

func TestProviderEditModalAndSubmit(t *testing.T) {
	var callbacks []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/interactions/") {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		callbacks = append(callbacks, body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	c := New("bot-token")
	c.BaseURL = srv.URL
	p := NewProvider(c, "C1")
	p.approvals["apr_1"] = chatout.ApprovalRequest{ID: "apr_1", InputJSON: `{"command":"ls"}`}
	decisions := make(chan chatout.Decision, 1)
	if err := p.OnDecision("apr_1", func(d chatout.Decision) { decisions <- d }); err != nil {
		t.Fatal(err)
	}
	_, err := p.routeFrame(t.Context(), GatewayFrame{Op: OpDispatch, T: "INTERACTION_CREATE", D: mustJSON(map[string]any{
		"id": "i1", "token": "tok", "type": 3, "channel_id": "C1",
		"user":    map[string]any{"id": "U1"},
		"message": map[string]any{"id": "m1", "channel_id": "C1"},
		"data":    map[string]any{"custom_id": "onibi:approval:edit:apr_1", "component_type": 2},
	})})
	if err != nil {
		t.Fatal(err)
	}
	if len(callbacks) != 1 || int(callbacks[0]["type"].(float64)) != 9 {
		t.Fatalf("callbacks = %#v", callbacks)
	}
	modal := callbacks[0]["data"].(map[string]any)
	if modal["custom_id"] != "onibi:approval_edit:apr_1" {
		t.Fatalf("modal = %#v", modal)
	}
	_, err = p.routeFrame(t.Context(), GatewayFrame{Op: OpDispatch, T: "INTERACTION_CREATE", D: mustJSON(map[string]any{
		"id": "i2", "token": "tok2", "type": 5, "channel_id": "C1",
		"user": map[string]any{"id": "U1"},
		"data": map[string]any{
			"custom_id": "onibi:approval_edit:apr_1",
			"components": []any{map[string]any{"type": 1, "components": []any{
				map[string]any{"type": 4, "custom_id": "json", "value": `{"command":"pwd"}`},
			}}},
		},
	})})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-decisions:
		if got.ApprovalID != "apr_1" || got.Verdict != "edit" || got.UpdatedInput != `{"command":"pwd"}` {
			t.Fatalf("decision = %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("missing edit decision")
	}
}

func TestProviderTailStreamCreatesThreadAndAudits(t *testing.T) {
	var posts []string
	var threadBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/channels/C1/messages"):
			_ = json.NewEncoder(w).Encode(Message{ID: "seed", ChannelID: "C1"})
		case strings.HasSuffix(r.URL.Path, "/channels/C1/messages/seed/threads"):
			if err := json.NewDecoder(r.Body).Decode(&threadBody); err != nil {
				t.Fatal(err)
			}
			_ = json.NewEncoder(w).Encode(Channel{ID: "T1", GuildID: "G1", Name: "onibi-s1"})
		case strings.HasSuffix(r.URL.Path, "/channels/T1/messages"):
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			posts = append(posts, body["content"].(string))
			_ = json.NewEncoder(w).Encode(Message{ID: "tail", ChannelID: "T1"})
		default:
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	c := New("bot-token")
	c.BaseURL = srv.URL
	c.Sleep = func(context.Context, time.Duration) error { return nil }
	var audit []chatout.AuditInteraction
	p := NewProvider(c, "C1")
	p.Audit = func(_ context.Context, item chatout.AuditInteraction) error {
		audit = append(audit, item)
		return nil
	}
	ch := make(chan []byte, 1)
	ch <- []byte(strings.Repeat("x", MessageChunkLimit+3))
	close(ch)
	if err := p.TailStream(t.Context(), "s1", ch); err != nil {
		t.Fatal(err)
	}
	if threadBody["name"] != "onibi-s1" || len(posts) != 2 || len(posts[0]) != MessageChunkLimit || len(posts[1]) != 3 {
		t.Fatalf("thread=%#v posts=%d", threadBody, len(posts))
	}
	if len(audit) != 3 || audit[0].Kind != "provider.discord.thread" || audit[1].Kind != "provider.discord.tail_chunk" {
		t.Fatalf("audit = %#v", audit)
	}
}

func TestProviderCapabilitiesRateLimitAndReconnectDelay(t *testing.T) {
	p := NewProvider(nil, "C1")
	if p.Name() != "discord" || len(p.Capabilities()) != 6 {
		t.Fatalf("provider = %s %#v", p.Name(), p.Capabilities())
	}
	rl := p.RateLimit()
	if rl.PerSecond.Limit != 50 || rl.PerSecond.Window != time.Second || rl.PerMinute.Window != time.Minute {
		t.Fatalf("rate limit = %#v", rl)
	}
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
		if base > providerReconnectMaxWait {
			base = providerReconnectMaxWait
		}
		if got < base || got > base+base/2 {
			t.Fatalf("attempt %d delay %s outside [%s,%s]", attempt, got, base, base+base/2)
		}
	}
}
