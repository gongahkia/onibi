package web

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/fleet"
	"github.com/gongahkia/onibi/internal/fleetnode"
)

func TestFleetMeshHealthSignsConfiguredIdentity(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	identity, err := fleetnode.LoadOrCreateIdentity(t.Context(), srv.db)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	host, err := identity.Host("Work Mac", fleet.Endpoint{Kind: fleet.EndpointMesh, URL: "https://peer.tail.ts.net"}, "v1.0.0", nil)
	if err != nil {
		t.Fatal(err)
	}
	host.OwnerID = "owner-local"
	host.State = fleet.HostStateActive
	host.RegisteredAt = now
	hubPublic, hubPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	challenge := fleet.EnrollmentChallenge{Version: fleet.ProtocolVersion, ID: "mesh-health-enroll", OwnerID: host.OwnerID, Nonce: "mesh-health-enrollment-nonce", HubPublic: base64.RawURLEncoding.EncodeToString(hubPublic), ExpiresAt: now.Add(time.Minute)}
	hubProof := ed25519.Sign(hubPrivate, fleet.EnrollmentSigningPayload(challenge, host))
	if _, err := fleetnode.Configure(t.Context(), srv.db, identity, fleetnode.Enrollment{HubURL: "https://hub.example.test", Challenge: challenge, Host: host, HubProof: base64.RawURLEncoding.EncodeToString(hubProof)}); err != nil {
		t.Fatal(err)
	}
	request := fleet.MeshHealthRequest{Version: fleet.ProtocolVersion, Nonce: "mesh-health-nonce-0001", SentAt: now}
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/fleet/mesh-health", strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("mesh health = %d body=%s", w.Code, w.Body.String())
	}
	var response fleet.MeshHealthResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	public, err := identity.PublicKey()
	if err != nil {
		t.Fatal(err)
	}
	signature, err := base64.RawURLEncoding.DecodeString(response.Signature)
	if err != nil || !ed25519.Verify(public, fleet.MeshHealthSigningPayload(request, response), signature) {
		t.Fatalf("response=%#v signature_valid=%v err=%v", response, err == nil && ed25519.Verify(public, fleet.MeshHealthSigningPayload(request, response), signature), err)
	}
}

func TestFleetMeshHealthRejectsUnconfiguredNode(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	request := fleet.MeshHealthRequest{Version: fleet.ProtocolVersion, Nonce: "mesh-health-nonce-0001", SentAt: time.Now().UTC()}
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/fleet/mesh-health", strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("mesh health = %d body=%s", w.Code, w.Body.String())
	}
}
