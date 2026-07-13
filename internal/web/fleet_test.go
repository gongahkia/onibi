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
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/fleet"
)

func TestFleetEnrollmentRequiresOwnerCSRFAndVerifiesHostProof(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	host := fleet.Host{
		ID:              "host-macbook",
		DisplayName:     "MacBook Pro",
		IdentityPublic:  base64.RawURLEncoding.EncodeToString(public),
		Endpoint:        fleet.Endpoint{Kind: fleet.EndpointMesh, URL: "https://macbook.tailnet.ts.net"},
		ProtocolVersion: fleet.ProtocolVersion,
		BinaryVersion:   "v1.0.0",
		Capabilities:    []string{"session.read", "approval.write"},
	}
	body, err := json.Marshal(fleetEnrollmentChallengeRequest{Host: host})
	if err != nil {
		t.Fatal(err)
	}
	unauth := httptest.NewRequest(http.MethodPost, "/fleet/enroll/challenge", strings.NewReader(string(body)))
	unauthW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(unauthW, unauth)
	if unauthW.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated challenge = %d", unauthW.Code)
	}
	rr := httptest.NewRecorder()
	sessionID, err := srv.CreateOwnerSession(context.Background(), rr, "phone")
	if err != nil {
		t.Fatal(err)
	}
	missingCSRF := httptest.NewRequest(http.MethodPost, "/fleet/enroll/challenge", strings.NewReader(string(body)))
	missingCSRF.AddCookie(rr.Result().Cookies()[0])
	missingCSRFW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(missingCSRFW, missingCSRF)
	if missingCSRFW.Code != http.StatusForbidden {
		t.Fatalf("challenge without csrf = %d", missingCSRFW.Code)
	}
	challengeReq := httptest.NewRequest(http.MethodPost, "/fleet/enroll/challenge", strings.NewReader(string(body)))
	challengeReq.AddCookie(rr.Result().Cookies()[0])
	addCSRF(challengeReq, sessionID)
	challengeW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(challengeW, challengeReq)
	if challengeW.Code != http.StatusOK {
		t.Fatalf("challenge = %d body=%s", challengeW.Code, challengeW.Body.String())
	}
	var challenge fleet.EnrollmentChallenge
	if err := json.NewDecoder(challengeW.Body).Decode(&challenge); err != nil {
		t.Fatal(err)
	}
	if err := challenge.Validate(); err != nil || challenge.OwnerID == "" {
		t.Fatalf("challenge=%#v err=%v", challenge, err)
	}
	host.OwnerID = challenge.OwnerID
	host.State = fleet.HostStatePending
	host.RegisteredAt = host.RegisteredAt.UTC()
	proof := fleet.EnrollmentProof{
		Version:     fleet.ProtocolVersion,
		ChallengeID: challenge.ID,
		Nonce:       challenge.Nonce,
		Signature:   base64.RawURLEncoding.EncodeToString(ed25519.Sign(private, fleet.EnrollmentSigningPayload(challenge, host))),
	}
	proofBody, err := json.Marshal(proof)
	if err != nil {
		t.Fatal(err)
	}
	proofReq := httptest.NewRequest(http.MethodPost, "/fleet/enroll/proof", strings.NewReader(string(proofBody)))
	proofW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(proofW, proofReq)
	if proofW.Code != http.StatusOK {
		t.Fatalf("proof = %d body=%s", proofW.Code, proofW.Body.String())
	}
	var enrolled fleetEnrollmentProofResponse
	if err := json.NewDecoder(proofW.Body).Decode(&enrolled); err != nil {
		t.Fatal(err)
	}
	if enrolled.Host.State != fleet.HostStateActive || enrolled.HubProof == "" {
		t.Fatalf("enrollment=%#v", enrolled)
	}
	hubPublic, err := decodeEd25519Public(challenge.HubPublic)
	if err != nil {
		t.Fatal(err)
	}
	hubProof, err := base64.RawURLEncoding.DecodeString(enrolled.HubProof)
	if err != nil || !ed25519.Verify(hubPublic, fleet.EnrollmentSigningPayload(challenge, enrolled.Host), hubProof) {
		t.Fatalf("hub proof valid=%v err=%v", err == nil && ed25519.Verify(hubPublic, fleet.EnrollmentSigningPayload(challenge, enrolled.Host), hubProof), err)
	}
	getReq := httptest.NewRequest(http.MethodGet, "/fleet/hosts", nil)
	getReq.AddCookie(rr.Result().Cookies()[0])
	getW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(getW, getReq)
	if getW.Code != http.StatusOK || !strings.Contains(getW.Body.String(), host.ID) {
		t.Fatalf("hosts = %d body=%s", getW.Code, getW.Body.String())
	}
	replayReq := httptest.NewRequest(http.MethodPost, "/fleet/enroll/proof", strings.NewReader(string(proofBody)))
	replayW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(replayW, replayReq)
	if replayW.Code != http.StatusNotFound {
		t.Fatalf("replay = %d body=%s", replayW.Code, replayW.Body.String())
	}
}

func TestFleetEnrollmentRejectsInvalidProofWithoutConsumingChallenge(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	host := fleet.Host{ID: "host-mini", DisplayName: "Mac mini", IdentityPublic: base64.RawURLEncoding.EncodeToString(public), Endpoint: fleet.Endpoint{Kind: fleet.EndpointRelay, URL: "https://fleet.example.test"}, ProtocolVersion: fleet.ProtocolVersion, BinaryVersion: "v1.0.0"}
	body, _ := json.Marshal(fleetEnrollmentChallengeRequest{Host: host})
	rr := httptest.NewRecorder()
	sessionID, err := srv.CreateOwnerSession(context.Background(), rr, "phone")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/fleet/enroll/challenge", strings.NewReader(string(body)))
	req.AddCookie(rr.Result().Cookies()[0])
	addCSRF(req, sessionID)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("challenge = %d body=%s", w.Code, w.Body.String())
	}
	var challenge fleet.EnrollmentChallenge
	if err := json.NewDecoder(w.Body).Decode(&challenge); err != nil {
		t.Fatal(err)
	}
	badProof, _ := json.Marshal(fleet.EnrollmentProof{Version: fleet.ProtocolVersion, ChallengeID: challenge.ID, Nonce: challenge.Nonce, Signature: "bad"})
	badReq := httptest.NewRequest(http.MethodPost, "/fleet/enroll/proof", strings.NewReader(string(badProof)))
	badW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(badW, badReq)
	if badW.Code != http.StatusUnauthorized {
		t.Fatalf("bad proof = %d", badW.Code)
	}
	host.OwnerID, host.State, host.RegisteredAt = challenge.OwnerID, fleet.HostStatePending, host.RegisteredAt.UTC()
	goodProof, _ := json.Marshal(fleet.EnrollmentProof{Version: fleet.ProtocolVersion, ChallengeID: challenge.ID, Nonce: challenge.Nonce, Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(private, fleet.EnrollmentSigningPayload(challenge, host)))})
	goodReq := httptest.NewRequest(http.MethodPost, "/fleet/enroll/proof", strings.NewReader(string(goodProof)))
	goodW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(goodW, goodReq)
	if goodW.Code != http.StatusOK {
		t.Fatalf("valid proof after invalid proof = %d body=%s", goodW.Code, goodW.Body.String())
	}
}

func TestFleetHeartbeatRequiresValidMonotonicHostProof(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	host := fleet.Host{
		ID:              "host-heartbeat",
		OwnerID:         "owner-local",
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
	heartbeat := fleet.Heartbeat{Version: fleet.ProtocolVersion, HostID: host.ID, SentAt: time.Now().UTC(), BinaryVersion: "v1.1.0", Capabilities: []string{"session.read", "approval.write"}}
	heartbeat.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(private, fleet.HeartbeatSigningPayload(heartbeat)))
	body, _ := json.Marshal(heartbeat)
	req := httptest.NewRequest(http.MethodPost, "/fleet/heartbeat", strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("heartbeat = %d body=%s", w.Code, w.Body.String())
	}
	replay := httptest.NewRequest(http.MethodPost, "/fleet/heartbeat", strings.NewReader(string(body)))
	replayW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(replayW, replay)
	if replayW.Code != http.StatusConflict {
		t.Fatalf("replayed heartbeat = %d body=%s", replayW.Code, replayW.Body.String())
	}
	heartbeat.Signature = "invalid"
	bad, _ := json.Marshal(heartbeat)
	badReq := httptest.NewRequest(http.MethodPost, "/fleet/heartbeat", strings.NewReader(string(bad)))
	badW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(badW, badReq)
	if badW.Code != http.StatusUnauthorized {
		t.Fatalf("bad heartbeat = %d body=%s", badW.Code, badW.Body.String())
	}
}
