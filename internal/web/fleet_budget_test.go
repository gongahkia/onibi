package web

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/gongahkia/onibi/internal/fleet"
)

func TestFleetBudgetAggregatesHostsAndQueuesEncryptedControls(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	ownerID, err := srv.db.FleetOwnerID(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	day := time.Now().UTC().Format("2006-01-02")
	for _, tc := range []struct {
		id      string
		tokens  int64
		session string
		action  string
	}{
		{id: "host-budget-a", tokens: 6, session: "session-budget-a", action: "interrupt"},
		{id: "host-budget-b", tokens: 5, session: "session-budget-b", action: "kill"},
	} {
		public, _, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		host := fleet.Host{ID: tc.id, OwnerID: ownerID, DisplayName: tc.id, IdentityPublic: base64.RawURLEncoding.EncodeToString(public), Endpoint: fleet.Endpoint{Kind: fleet.EndpointRelay, URL: "https://" + tc.id + ".example.test"}, ProtocolVersion: fleet.ProtocolVersion, BinaryVersion: "v1.0.0", State: fleet.HostStateActive, RegisteredAt: time.Now().UTC(), LastSeenAt: time.Now().UTC(), Budget: fleet.BudgetReport{Date: day, DailyTokens: tc.tokens, GlobalLimit: 10, OnOverrun: tc.action, Sessions: []fleet.BudgetSession{{SessionID: tc.session, Agent: "claude", Tokens: tc.tokens, Measured: true, OnOverrun: tc.action}}}}
		if err := srv.db.FleetHostUpsert(context.Background(), host); err != nil {
			t.Fatal(err)
		}
	}
	if err := srv.evaluateFleetBudget(context.Background(), day); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ host, session string }{{"host-budget-a", "session-budget-a"}, {"host-budget-b", "session-budget-b"}} {
		command, err := srv.db.ControlCommand(context.Background(), fleetBudgetCommandID(day, tc.host, tc.session, "kill", 10))
		if err != nil || command.State != fleet.CommandPending || command.SessionID != tc.session || command.Action != "kill" || string(command.Payload) != `{"session_id":"`+tc.session+`"}` {
			t.Fatalf("command=%#v err=%v", command, err)
		}
	}
	audit, err := srv.db.AuditAll(context.Background())
	if err != nil || len(audit) != 2 {
		t.Fatalf("audit=%#v err=%v", audit, err)
	}
	for _, entry := range audit {
		if entry.Action != "budget.global.overrun" || entry.PayloadHash != "" || strings.Contains(entry.Detail, "session_id") {
			t.Fatalf("audit=%#v", entry)
		}
	}
}

func TestFleetBudgetControlAcknowledgesOverrunOutcome(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	host, private := testFleetLinkHost(t, srv)
	ts := httptest.NewTLSServer(srv.Handler())
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "wss"+strings.TrimPrefix(ts.URL, "https")+"/fleet/link", &websocket.DialOptions{HTTPClient: ts.Client(), Subprotocols: []string{fleetLinkSubprotocol}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	var challenge fleet.LinkChallenge
	if err := wsjson.Read(ctx, conn, &challenge); err != nil {
		t.Fatal(err)
	}
	auth := fleet.LinkAuthenticate{Version: fleet.ProtocolVersion, OwnerID: host.OwnerID, HostID: host.ID, ChallengeID: challenge.ID, Nonce: challenge.Nonce, SentAt: time.Now().UTC()}
	auth.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(private, fleet.LinkAuthenticateSigningPayload(challenge, auth)))
	if err := wsjson.Write(ctx, conn, auth); err != nil {
		t.Fatal(err)
	}
	var accepted fleet.LinkAccepted
	if err := wsjson.Read(ctx, conn, &accepted); err != nil {
		t.Fatal(err)
	}
	day := time.Now().UTC().Format("2006-01-02")
	heartbeat := fleet.Heartbeat{Version: fleet.ProtocolVersion, OwnerID: host.OwnerID, HostID: host.ID, SentAt: time.Now().UTC(), BinaryVersion: "v1.1.0", Budget: fleet.BudgetReport{Date: day, DailyTokens: 11, GlobalLimit: 10, OnOverrun: "interrupt", Sessions: []fleet.BudgetSession{{SessionID: "session-budget", Agent: "claude", Tokens: 11, Measured: true, OnOverrun: "interrupt"}}}}
	heartbeat.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(private, fleet.HeartbeatSigningPayload(heartbeat)))
	body, err := json.Marshal(heartbeat)
	if err != nil {
		t.Fatal(err)
	}
	envelope := fleet.Envelope{Version: fleet.ProtocolVersion, Type: fleet.MessageHeartbeat, RequestID: "budget-heartbeat", SentAt: heartbeat.SentAt, Body: body}
	frame := fleet.LinkFrame{Envelope: envelope, Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(private, fleet.LinkFrameSigningPayload(envelope)))}
	if err := wsjson.Write(ctx, conn, frame); err != nil {
		t.Fatal(err)
	}
	var controlFrame fleet.LinkFrame
	if err := wsjson.Read(ctx, conn, &controlFrame); err != nil {
		t.Fatal(err)
	}
	var control fleet.Control
	if err := json.Unmarshal(controlFrame.Envelope.Body, &control); err != nil || !isFleetBudgetCommand(control.ID) || control.Command != "interrupt" {
		t.Fatalf("control=%#v err=%v", control, err)
	}
	var payload fleet.ControlPayload
	if err := json.Unmarshal(control.Payload, &payload); err != nil || payload.SessionID != "session-budget" || payload.Input != "" || payload.Target != "" {
		t.Fatalf("payload=%#v err=%v", payload, err)
	}
	completed := time.Now().UTC()
	result := fleet.ControlResult{Version: fleet.ProtocolVersion, ID: control.ID, OwnerID: host.OwnerID, HostID: host.ID, State: fleet.CommandSucceeded, CompletedAt: completed}
	resultBody, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	resultEnvelope := fleet.Envelope{Version: fleet.ProtocolVersion, Type: fleet.MessageControlResult, RequestID: control.ID, SentAt: completed, Body: resultBody}
	resultFrame := fleet.LinkFrame{Envelope: resultEnvelope, Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(private, fleet.LinkFrameSigningPayload(resultEnvelope)))}
	if err := wsjson.Write(ctx, conn, resultFrame); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		command, err := srv.db.ControlCommand(context.Background(), control.ID)
		if err == nil && command.State == fleet.CommandSucceeded {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("budget control not acknowledged: command=%#v err=%v", command, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	audit, err := srv.db.AuditAll(context.Background())
	if err != nil || len(audit) != 2 || audit[0].Action != "budget.global.overrun" || audit[1].Action != "budget.global.action" || audit[1].PayloadHash != "" || strings.Contains(audit[1].Detail, "session-budget") {
		t.Fatalf("audit=%#v err=%v", audit, err)
	}
}
