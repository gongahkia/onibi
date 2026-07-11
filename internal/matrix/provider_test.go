package matrix

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

func TestProviderSendsApprovalAndStoresReactionEvent(t *testing.T) {
	var body RoomMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || !strings.Contains(r.URL.Path, "/send/m.room.message/txn-1") {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		writeJSON(t, w, map[string]any{"event_id": "$approval"})
	}))
	defer srv.Close()
	c := New(srv.URL, "tok")
	c.TxnID = func() string { return "txn-1" }
	p := NewProvider(c, "!room:example")
	msgID, err := p.SendApproval(t.Context(), chatout.ApprovalRequest{ID: "apr_1", SessionID: "s1", Agent: "claude", Tool: "Bash", InputJSON: `{"command":"pwd"}`})
	if err != nil {
		t.Fatal(err)
	}
	if msgID != "$approval" || !strings.Contains(body.Body, "React ✅") || p.approvalForEvent("$approval") != "apr_1" {
		t.Fatalf("msgID=%q body=%#v mapped=%q", msgID, body, p.approvalForEvent("$approval"))
	}
}

func TestProviderRoutesTextAndReactionDecision(t *testing.T) {
	p := NewProvider(nil, "!room:example")
	p.OwnerUserID = "@owner:example"
	p.approvalEvents["$approval"] = "apr_1"
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
	if err := p.OnDecision("$approval", func(d chatout.Decision) { decisions <- d }); err != nil {
		t.Fatal(err)
	}
	err := p.routeEvent(t.Context(), Event{
		EventID: "$msg",
		Type:    "m.room.message",
		Sender:  "@owner:example",
		Content: json.RawMessage(`{"msgtype":"m.text","body":"pwd"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-inbound:
		if got != "pwd:@owner:example:!room:example" {
			t.Fatalf("inbound = %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("missing inbound")
	}
	err = p.routeEvent(t.Context(), Event{
		EventID: "$reaction",
		Type:    "m.reaction",
		Sender:  "@owner:example",
		Content: json.RawMessage(`{"m.relates_to":{"rel_type":"m.annotation","event_id":"$approval","key":"✅"}}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-decisions:
		if got.ApprovalID != "apr_1" || got.Verdict != "approve" || got.MessageID != "$approval" || got.Sender.ID != "@owner:example" {
			t.Fatalf("decision = %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("missing decision")
	}
	if len(audit) != 2 || audit[0].Kind != "provider.matrix.text_in" || audit[1].Kind != "provider.matrix.reaction" {
		t.Fatalf("audit = %#v", audit)
	}
}

func TestProviderRoutesEncryptedMegolmText(t *testing.T) {
	pickleKey := []byte("pickle-key")
	outbound, roomKey, err := NewMegolmOutboundState("!room:example", pickleKey)
	if err != nil {
		t.Fatal(err)
	}
	inboundState, err := NewMegolmInboundState(roomKey, "sender-key", pickleKey)
	if err != nil {
		t.Fatal(err)
	}
	_, encrypted, err := EncryptMegolmRoomEvent(outbound, pickleKey, "sender-key", "ONIBI", "!room:example", "m.room.message", RoomMessage{MsgType: "m.text", Body: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(encrypted)
	if err != nil {
		t.Fatal(err)
	}
	p := NewProvider(nil, "!room:example")
	p.OwnerUserID = "@owner:example"
	p.PickleKey = pickleKey
	p.CryptoState = &CryptoState{MegolmInboundSessions: map[string]MegolmInboundState{"inbound": inboundState}}
	inbound := make(chan string, 1)
	if err := p.OnInboundText(func(text string, sender chatout.Sender) {
		inbound <- text + ":" + sender.ID
	}); err != nil {
		t.Fatal(err)
	}
	if err := p.routeEvent(t.Context(), Event{EventID: "$encrypted", Type: EventRoomEncrypted, Sender: "@owner:example", Content: raw}); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-inbound:
		if got != "secret:@owner:example" {
			t.Fatalf("inbound = %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("missing encrypted inbound")
	}
	if p.CryptoState.MegolmInboundSessions["inbound"].SessionID != inboundState.SessionID {
		t.Fatalf("inbound session = %#v", p.CryptoState.MegolmInboundSessions["inbound"])
	}
}

func TestProviderTailStreamAuditsChunks(t *testing.T) {
	var bodies []RoomMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || !strings.Contains(r.URL.Path, "/send/m.room.message/") {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		var body RoomMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		bodies = append(bodies, body)
		writeJSON(t, w, map[string]any{"event_id": "$tail"})
	}))
	defer srv.Close()
	txn := 0
	c := New(srv.URL, "tok")
	c.TxnID = func() string {
		txn++
		return "txn-" + string(rune('0'+txn))
	}
	var audit []chatout.AuditInteraction
	p := NewProvider(c, "!room:example")
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
	if len(bodies) != 2 || len(bodies[0].Body) != MessageChunkLimit || len(bodies[1].Body) != 2 {
		t.Fatalf("bodies = %#v", bodies)
	}
	if len(audit) != 2 || audit[0].Kind != "provider.matrix.tail_chunk" || audit[0].SessionID != "s1" {
		t.Fatalf("audit = %#v", audit)
	}
}

func TestProviderConnectPollsRoom(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var sawSince string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_matrix/client/v3/sync" {
			t.Fatalf("request = %s", r.URL.Path)
		}
		sawSince = r.URL.Query().Get("since")
		writeJSON(t, w, map[string]any{
			"next_batch": "s2",
			"rooms": map[string]any{"join": map[string]any{"!room:example": map[string]any{
				"timeline": map[string]any{"events": []any{map[string]any{
					"event_id": "$msg", "type": "m.room.message", "sender": "@owner:example", "content": map[string]any{"msgtype": "m.text", "body": "hello"},
				}}},
			}}},
		})
	}))
	defer srv.Close()
	c := New(srv.URL, "tok")
	p := NewProvider(c, "!room:example")
	p.Timeout = time.Millisecond
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
		if got != "hello" || p.nextSince() != "s2" || sawSince != "" {
			t.Fatalf("seen=%q since=%q sawSince=%q", got, p.nextSince(), sawSince)
		}
	default:
		t.Fatal("missing inbound")
	}
}

func TestProviderCapabilitiesRateLimitAndReconnectDelay(t *testing.T) {
	p := NewProvider(nil, "!room:example")
	if p.Name() != "matrix" || len(p.Capabilities()) != 6 {
		t.Fatalf("provider = %s %#v", p.Name(), p.Capabilities())
	}
	rl := p.RateLimit()
	if rl.PerSecond.Window != time.Second || rl.PerMinute.Window != time.Minute {
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
