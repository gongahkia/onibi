package web

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/gongahkia/onibi/internal/fleet"
)

func TestFleetLinkAuthenticatesHeartbeatAndDeliversControl(t *testing.T) {
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
	if accepted.OwnerID != host.OwnerID || accepted.HostID != host.ID || accepted.ChallengeID != challenge.ID {
		t.Fatalf("acceptance = %#v", accepted)
	}
	heartbeat := fleet.Heartbeat{Version: fleet.ProtocolVersion, OwnerID: host.OwnerID, HostID: host.ID, SentAt: time.Now().UTC(), BinaryVersion: "v1.1.0", Capabilities: []string{"approval.write", "session.read"}}
	heartbeat.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(private, fleet.HeartbeatSigningPayload(heartbeat)))
	body, err := json.Marshal(heartbeat)
	if err != nil {
		t.Fatal(err)
	}
	envelope := fleet.Envelope{Version: fleet.ProtocolVersion, Type: fleet.MessageHeartbeat, RequestID: "link-heartbeat", SentAt: heartbeat.SentAt, Body: body}
	frame := fleet.LinkFrame{Envelope: envelope, Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(private, fleet.LinkFrameSigningPayload(envelope)))}
	if err := wsjson.Write(ctx, conn, frame); err != nil {
		t.Fatal(err)
	}
	control := fleet.Control{Version: fleet.ProtocolVersion, ID: "control-123", OwnerID: host.OwnerID, HostID: host.ID, Command: "interrupt", ExpiresAt: time.Now().UTC().Add(time.Minute)}
	deadline := time.Now().Add(time.Second)
	for {
		err = srv.SendFleetControl(ctx, control)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("send fleet control: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	var controlFrame fleet.LinkFrame
	if err := wsjson.Read(ctx, conn, &controlFrame); err != nil {
		t.Fatal(err)
	}
	if controlFrame.Envelope.Type != fleet.MessageControl {
		t.Fatalf("control frame = %#v", controlFrame)
	}
	var delivered fleet.Control
	if err := json.Unmarshal(controlFrame.Envelope.Body, &delivered); err != nil {
		t.Fatal(err)
	}
	if delivered.ID != control.ID || delivered.OwnerID != host.OwnerID || delivered.HostID != host.ID {
		t.Fatalf("delivered control = %#v", delivered)
	}
	controls := []fleet.Control{
		{Version: fleet.ProtocolVersion, ID: "control-124", OwnerID: host.OwnerID, HostID: host.ID, Command: "interrupt", ExpiresAt: time.Now().UTC().Add(time.Minute)},
		{Version: fleet.ProtocolVersion, ID: "control-125", OwnerID: host.OwnerID, HostID: host.ID, Command: "interrupt", ExpiresAt: time.Now().UTC().Add(time.Minute)},
	}
	errCh := make(chan error, len(controls))
	var wg sync.WaitGroup
	for _, concurrentControl := range controls {
		wg.Add(1)
		go func(control fleet.Control) {
			defer wg.Done()
			errCh <- srv.SendFleetControl(ctx, control)
		}(concurrentControl)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("concurrent fleet control: %v", err)
		}
	}
	deliveredIDs := make(map[string]bool, len(controls))
	for range controls {
		if err := wsjson.Read(ctx, conn, &controlFrame); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(controlFrame.Envelope.Body, &delivered); err != nil {
			t.Fatal(err)
		}
		deliveredIDs[delivered.ID] = true
	}
	for _, control := range controls {
		if !deliveredIDs[control.ID] {
			t.Fatalf("missing concurrent control %q", control.ID)
		}
	}
	deadline = time.Now().Add(time.Second)
	for {
		updated, ok, err := srv.db.FleetHostGet(ctx, host.ID)
		if err != nil {
			t.Fatal(err)
		}
		if ok && updated.BinaryVersion == "v1.1.0" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("heartbeat was not persisted: host=%#v ok=%v", updated, ok)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestFleetLinkRejectsVersionStaleAndRevokedFrames(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	host, private := testFleetLinkHost(t, srv)
	valid := testFleetLinkHeartbeatFrame(t, host, private, time.Now().UTC())
	if err := srv.applyFleetLinkFrame(context.Background(), host.ID, valid); err != nil {
		t.Fatal(err)
	}
	crossOwner := valid
	var heartbeat fleet.Heartbeat
	if err := json.Unmarshal(crossOwner.Envelope.Body, &heartbeat); err != nil {
		t.Fatal(err)
	}
	heartbeat.OwnerID = "owner-foreign"
	heartbeat.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(private, fleet.HeartbeatSigningPayload(heartbeat)))
	body, err := json.Marshal(heartbeat)
	if err != nil {
		t.Fatal(err)
	}
	crossOwner.Envelope.Body = body
	crossOwner.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(private, fleet.LinkFrameSigningPayload(crossOwner.Envelope)))
	if err := srv.applyFleetLinkFrame(context.Background(), host.ID, crossOwner); err == nil {
		t.Fatal("expected cross-owner fleet link frame error")
	}
	incompatible := valid
	incompatible.Envelope.Version++
	if err := srv.applyFleetLinkFrame(context.Background(), host.ID, incompatible); err == nil {
		t.Fatal("expected incompatible fleet link frame error")
	}
	malformed := valid
	malformed.Signature = ""
	if err := srv.applyFleetLinkFrame(context.Background(), host.ID, malformed); err == nil {
		t.Fatal("expected malformed fleet link frame error")
	}
	stale := testFleetLinkHeartbeatFrame(t, host, private, valid.Envelope.SentAt)
	if err := srv.applyFleetLinkFrame(context.Background(), host.ID, stale); err == nil {
		t.Fatal("expected stale fleet link frame error")
	}
	host.State = fleet.HostStateRevoked
	now := time.Now().UTC()
	host.RevokedAt = &now
	if err := srv.db.FleetHostUpsert(context.Background(), host); err != nil {
		t.Fatal(err)
	}
	revoked := testFleetLinkHeartbeatFrame(t, host, private, time.Now().UTC().Add(time.Second))
	if err := srv.applyFleetLinkFrame(context.Background(), host.ID, revoked); err == nil {
		t.Fatal("expected revoked fleet link frame error")
	}
}

func TestFleetLinkRejectsCrossOwnerAuthentication(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	host, private := testFleetLinkHost(t, srv)
	ts := httptest.NewTLSServer(srv.Handler())
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
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
	auth := fleet.LinkAuthenticate{Version: fleet.ProtocolVersion, OwnerID: "owner-foreign", HostID: host.ID, ChallengeID: challenge.ID, Nonce: challenge.Nonce, SentAt: time.Now().UTC()}
	auth.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(private, fleet.LinkAuthenticateSigningPayload(challenge, auth)))
	if err := wsjson.Write(ctx, conn, auth); err != nil {
		t.Fatal(err)
	}
	var accepted fleet.LinkAccepted
	if err := wsjson.Read(ctx, conn, &accepted); err == nil {
		t.Fatal("expected cross-owner fleet link authentication error")
	}
}

func TestSendFleetControlRejectsCrossOwner(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	host, _ := testFleetLinkHost(t, srv)
	control := fleet.Control{Version: fleet.ProtocolVersion, ID: "control-foreign", OwnerID: "owner-foreign", HostID: host.ID, Command: "interrupt", ExpiresAt: time.Now().UTC().Add(time.Minute)}
	if err := srv.SendFleetControl(context.Background(), control); err == nil {
		t.Fatal("expected cross-owner fleet control error")
	}
}

func TestFleetLinkRequiresTLS(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	req := httptest.NewRequest(http.MethodGet, "/fleet/link", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUpgradeRequired {
		t.Fatalf("plain fleet link status = %d", w.Code)
	}
}

func testFleetLinkHost(t *testing.T, srv *Server) (fleet.Host, ed25519.PrivateKey) {
	t.Helper()
	ownerID, err := srv.db.FleetOwnerID(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	host := fleet.Host{
		ID:              "host-fleet-link",
		OwnerID:         ownerID,
		DisplayName:     "Mac Studio",
		IdentityPublic:  base64.RawURLEncoding.EncodeToString(public),
		Endpoint:        fleet.Endpoint{Kind: fleet.EndpointMesh, URL: "https://studio.tailnet.ts.net"},
		ProtocolVersion: fleet.ProtocolVersion,
		BinaryVersion:   "v1.0.0",
		State:           fleet.HostStateActive,
		RegisteredAt:    time.Now().UTC().Add(-time.Minute),
		LastSeenAt:      time.Now().UTC().Add(-time.Minute),
	}
	if err := srv.db.FleetHostUpsert(context.Background(), host); err != nil {
		t.Fatal(err)
	}
	return host, private
}

func testFleetLinkHeartbeatFrame(t *testing.T, host fleet.Host, private ed25519.PrivateKey, sentAt time.Time) fleet.LinkFrame {
	t.Helper()
	heartbeat := fleet.Heartbeat{Version: fleet.ProtocolVersion, OwnerID: host.OwnerID, HostID: host.ID, SentAt: sentAt, BinaryVersion: "v1.1.0", Capabilities: []string{"session.read"}}
	heartbeat.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(private, fleet.HeartbeatSigningPayload(heartbeat)))
	body, err := json.Marshal(heartbeat)
	if err != nil {
		t.Fatal(err)
	}
	envelope := fleet.Envelope{Version: fleet.ProtocolVersion, Type: fleet.MessageHeartbeat, RequestID: "link-heartbeat", SentAt: sentAt, Body: body}
	return fleet.LinkFrame{Envelope: envelope, Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(private, fleet.LinkFrameSigningPayload(envelope)))}
}
