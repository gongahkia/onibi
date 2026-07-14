package daemon

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/gongahkia/onibi/internal/fleet"
)

func TestFleetLinkReconnectsAndVerifiesHubControls(t *testing.T) {
	const ownerID = "owner-local"
	hostPublic, hostPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	hubPublic, hubPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var connections atomic.Int32
	var heartbeats atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/fleet/link" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{fleetLinkSubprotocol}})
		if err != nil {
			return
		}
		defer conn.CloseNow()
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		connections.Add(1)
		challenge := fleet.LinkChallenge{Version: fleet.ProtocolVersion, ID: "link-test", Nonce: "nonce", ExpiresAt: time.Now().UTC().Add(time.Minute)}
		if err := wsjson.Write(ctx, conn, challenge); err != nil {
			return
		}
		var auth fleet.LinkAuthenticate
		if err := wsjson.Read(ctx, conn, &auth); err != nil {
			return
		}
		if auth.OwnerID != ownerID {
			_ = conn.Close(websocket.StatusPolicyViolation, "invalid owner")
			return
		}
		signature, err := base64.RawURLEncoding.DecodeString(auth.Signature)
		if err != nil || !ed25519.Verify(hostPublic, fleet.LinkAuthenticateSigningPayload(challenge, auth), signature) {
			_ = conn.Close(websocket.StatusPolicyViolation, "invalid authentication")
			return
		}
		accepted := fleet.LinkAccepted{Version: fleet.ProtocolVersion, OwnerID: auth.OwnerID, HostID: auth.HostID, ChallengeID: challenge.ID, Nonce: challenge.Nonce, SentAt: time.Now().UTC()}
		accepted.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(hubPrivate, fleet.LinkAcceptedSigningPayload(challenge, accepted)))
		if err := wsjson.Write(ctx, conn, accepted); err != nil {
			return
		}
		var heartbeat fleet.LinkFrame
		if err := wsjson.Read(ctx, conn, &heartbeat); err != nil {
			return
		}
		if heartbeat.Envelope.Type != fleet.MessageHeartbeat {
			_ = conn.Close(websocket.StatusPolicyViolation, "heartbeat required")
			return
		}
		heartbeats.Add(1)
		control := fleet.Control{Version: fleet.ProtocolVersion, ID: "control-test", OwnerID: auth.OwnerID, HostID: auth.HostID, Command: "interrupt", ExpiresAt: time.Now().UTC().Add(time.Minute)}
		body, err := json.Marshal(control)
		if err != nil {
			return
		}
		envelope := fleet.Envelope{Version: fleet.ProtocolVersion, Type: fleet.MessageControl, RequestID: "control-frame", SentAt: time.Now().UTC(), Body: body}
		frame := fleet.LinkFrame{Envelope: envelope, Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(hubPrivate, fleet.LinkFrameSigningPayload(envelope)))}
		if err := wsjson.Write(ctx, conn, frame); err != nil {
			return
		}
		_ = conn.Close(websocket.StatusNormalClosure, "reconnect")
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	controls := make(chan fleet.Control, 2)
	link, err := NewFleetLink(FleetLinkOptions{
		HubURL:        "https://hub.example.test",
		OwnerID:       ownerID,
		HostID:        "host-daemon",
		PrivateKey:    hostPrivate,
		HubPublic:     hubPublic,
		BinaryVersion: "v1.0.0",
		ReconnectMin:  time.Millisecond,
		ReconnectMax:  5 * time.Millisecond,
		Dial: func(ctx context.Context, _ string) (*websocket.Conn, error) {
			conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/fleet/link", &websocket.DialOptions{Subprotocols: []string{fleetLinkSubprotocol}})
			return conn, err
		},
		OnControl: func(_ context.Context, control fleet.Control) error {
			controls <- control
			if len(controls) == 2 {
				cancel()
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	err = link.Run(ctx)
	if err != context.Canceled {
		t.Fatalf("fleet link run error = %v", err)
	}
	if connections.Load() < 2 || heartbeats.Load() < 2 || len(controls) != 2 {
		t.Fatalf("connections=%d heartbeats=%d controls=%d", connections.Load(), heartbeats.Load(), len(controls))
	}
}

func TestFleetLinkRequiresHTTPSHubURL(t *testing.T) {
	_, err := NewFleetLink(FleetLinkOptions{HubURL: "http://hub.example.test", OwnerID: "owner-local", HostID: "host-daemon", PrivateKey: make(ed25519.PrivateKey, ed25519.PrivateKeySize), HubPublic: make(ed25519.PublicKey, ed25519.PublicKeySize), BinaryVersion: "v1.0.0"})
	if err == nil {
		t.Fatal("expected HTTPS fleet hub URL error")
	}
}

func TestFleetLinkRejectsMalformedLocalStatus(t *testing.T) {
	_, err := NewFleetLink(FleetLinkOptions{HubURL: "https://hub.example.test", OwnerID: "owner-local", HostID: "host-daemon", PrivateKey: make(ed25519.PrivateKey, ed25519.PrivateKeySize), HubPublic: make(ed25519.PublicKey, ed25519.PublicKeySize), BinaryVersion: "v1.0.0", Capabilities: []string{"bad/capability"}})
	if err == nil {
		t.Fatal("expected malformed fleet link status error")
	}
}
