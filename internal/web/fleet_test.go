package web

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	approvals "github.com/gongahkia/onibi/internal/approval"
	"github.com/gongahkia/onibi/internal/fleet"
	"github.com/gongahkia/onibi/internal/store"
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
	ownerID, err := srv.db.FleetOwnerID(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	host := fleet.Host{
		ID:              "host-heartbeat",
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
	heartbeat := fleet.Heartbeat{Version: fleet.ProtocolVersion, OwnerID: host.OwnerID, HostID: host.ID, SentAt: time.Now().UTC(), BinaryVersion: "v1.1.0", Capabilities: []string{"session.read", "approval.write"}}
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
	foreign := heartbeat
	foreign.OwnerID = "owner-foreign"
	foreign.SentAt = time.Now().UTC().Add(time.Second)
	foreign.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(private, fleet.HeartbeatSigningPayload(foreign)))
	foreignBody, _ := json.Marshal(foreign)
	foreignReq := httptest.NewRequest(http.MethodPost, "/fleet/heartbeat", strings.NewReader(string(foreignBody)))
	foreignW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(foreignW, foreignReq)
	if foreignW.Code != http.StatusNotFound {
		t.Fatalf("cross-owner heartbeat = %d body=%s", foreignW.Code, foreignW.Body.String())
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

func TestFleetHostsMarksStaleAndHeartbeatRecovers(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	host, private := testFleetLinkHost(t, srv)
	now := time.Now().UTC().Truncate(time.Second)
	host.RegisteredAt = now.Add(-2 * fleet.HostStaleAfter)
	host.LastSeenAt = now.Add(-fleet.HostStaleAfter - time.Second)
	if err := srv.db.FleetHostUpsert(context.Background(), host); err != nil {
		t.Fatal(err)
	}
	owner := httptest.NewRecorder()
	_, err := srv.CreateOwnerSession(context.Background(), owner, "phone")
	if err != nil {
		t.Fatal(err)
	}
	listReq := httptest.NewRequest(http.MethodGet, "/fleet/hosts", nil)
	listReq.AddCookie(owner.Result().Cookies()[0])
	listW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(listW, listReq)
	if listW.Code != http.StatusOK || !strings.Contains(listW.Body.String(), `"state":"stale"`) {
		t.Fatalf("fleet hosts = %d body=%s", listW.Code, listW.Body.String())
	}
	heartbeat := fleet.Heartbeat{Version: fleet.ProtocolVersion, OwnerID: host.OwnerID, HostID: host.ID, SentAt: now, BinaryVersion: "v1.2.0", Capabilities: []string{"approval.write", "session.read"}}
	heartbeat.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(private, fleet.HeartbeatSigningPayload(heartbeat)))
	body, err := json.Marshal(heartbeat)
	if err != nil {
		t.Fatal(err)
	}
	heartbeatReq := httptest.NewRequest(http.MethodPost, "/fleet/heartbeat", strings.NewReader(string(body)))
	heartbeatW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(heartbeatW, heartbeatReq)
	if heartbeatW.Code != http.StatusOK {
		t.Fatalf("stale heartbeat = %d body=%s", heartbeatW.Code, heartbeatW.Body.String())
	}
	recovered, ok, err := srv.db.FleetHostGet(context.Background(), host.ID)
	if err != nil || !ok || recovered.State != fleet.HostStateActive || recovered.BinaryVersion != heartbeat.BinaryVersion {
		t.Fatalf("recovered host=%#v ok=%v err=%v", recovered, ok, err)
	}
}

func TestFleetStatusAggregatesOwnerHomeReadModel(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	srv.approvalQueue = approvals.New(srv.db, time.Minute)
	host, _ := testFleetLinkHost(t, srv)
	now := time.Now().UTC()
	srv.sessionList = func(_ context.Context, opts SessionListOptions) ([]SessionSummary, error) {
		if !opts.IncludeRemote {
			t.Fatal("fleet status must request remote sessions")
		}
		return []SessionSummary{{ID: "session-home", HostID: host.ID, Agent: "claude", LastActivity: now.Format(time.RFC3339Nano), PendingApprovalsCount: 1, RecoveryState: fleet.SessionRecoveryOrphaned, RecoveryReason: "tmux reconnect timed out", RecoveryUpdatedAt: now.Format(time.RFC3339Nano), RoleRequired: "owner"}}, nil
	}
	approvalID, _, err := srv.approvalQueue.Request(context.Background(), "session-home", "claude", "Bash", `{"secret":"must-not-leak"}`)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.approvalQueue.DropWaiter(approvalID)
	unauth := httptest.NewRequest(http.MethodGet, "/fleet/status", nil)
	unauthW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(unauthW, unauth)
	if unauthW.Code != http.StatusUnauthorized {
		t.Fatalf("unauth fleet status = %d", unauthW.Code)
	}
	owner := httptest.NewRecorder()
	if _, err := srv.CreateOwnerSession(context.Background(), owner, "phone"); err != nil {
		t.Fatal(err)
	}
	ownerCookie := owner.Result().Cookies()[0]
	req := httptest.NewRequest(http.MethodGet, "/fleet/status", nil)
	req.AddCookie(ownerCookie)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("fleet status = %d body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "must-not-leak") {
		t.Fatal("fleet status leaked approval input")
	}
	var status fleet.HomeStatus
	if err := json.NewDecoder(w.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if err := status.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(status.Hosts) != 1 || status.Hosts[0].ID != host.ID || len(status.Sessions) != 1 || status.Sessions[0].HostID != host.ID || status.Sessions[0].State != string(SessionStateAwaitingApproval) || status.Sessions[0].RecoveryState != fleet.SessionRecoveryOrphaned || status.Sessions[0].RecoveryReason != "tmux reconnect timed out" || len(status.PendingApprovals) != 1 || status.PendingApprovals[0].ID != approvalID || status.PendingApprovals[0].HostID != host.ID {
		t.Fatalf("fleet status = %#v", status)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range cap(errs) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			request := httptest.NewRequest(http.MethodGet, "/fleet/status", nil)
			request.AddCookie(ownerCookie)
			response := httptest.NewRecorder()
			srv.Handler().ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				errs <- fmt.Errorf("concurrent fleet status = %d", response.Code)
				return
			}
			errs <- nil
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestFleetRevokeInvalidatesSessionsAndPendingActions(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	srv.approvalQueue = approvals.New(srv.db, time.Minute)
	host, private := testFleetLinkHost(t, srv)
	approvalID, decisions, err := srv.approvalQueue.Request(context.Background(), "session-revoke", "claude", "Bash", `{"command":"echo keep"}`)
	if err != nil {
		t.Fatal(err)
	}
	owner := httptest.NewRecorder()
	ownerSessionID, err := srv.CreateOwnerSession(context.Background(), owner, "phone")
	if err != nil {
		t.Fatal(err)
	}
	ownerCookie := owner.Result().Cookies()[0]
	second := httptest.NewRecorder()
	secondSessionID, err := srv.CreateOwnerSession(context.Background(), second, "tablet")
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(fleet.RevocationRequest{Version: fleet.ProtocolVersion, HostID: host.ID})
	if err != nil {
		t.Fatal(err)
	}
	missingCSRF := httptest.NewRequest(http.MethodPost, "/fleet/revoke", strings.NewReader(string(body)))
	missingCSRF.AddCookie(ownerCookie)
	missingCSRFW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(missingCSRFW, missingCSRF)
	if missingCSRFW.Code != http.StatusForbidden {
		t.Fatalf("missing csrf revoke = %d", missingCSRFW.Code)
	}
	req := httptest.NewRequest(http.MethodPost, "/fleet/revoke", strings.NewReader(string(body)))
	req.AddCookie(ownerCookie)
	addCSRF(req, ownerSessionID)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("fleet revoke = %d body=%s", w.Code, w.Body.String())
	}
	revoked, ok, err := srv.db.FleetHostGet(context.Background(), host.ID)
	if err != nil || !ok || revoked.State != fleet.HostStateRevoked || revoked.RevokedAt == nil {
		t.Fatalf("revoked host=%#v ok=%v err=%v", revoked, ok, err)
	}
	for _, sessionID := range []string{ownerSessionID, secondSessionID} {
		if status, err := srv.db.WebSessionStatus(context.Background(), sessionID); err != nil || status.Valid || status.Reason != store.WebSessionReasonFleetEmergency {
			t.Fatalf("session %q status=%#v err=%v", sessionID, status, err)
		}
	}
	approval, err := srv.approvalQueue.Get(context.Background(), approvalID)
	if err != nil || approval.State != approvals.StateCancelled {
		t.Fatalf("approval=%#v err=%v", approval, err)
	}
	select {
	case decision := <-decisions:
		if decision.Verdict != approvals.VerdictCancel {
			t.Fatalf("revoke decision = %#v", decision)
		}
	case <-time.After(time.Second):
		t.Fatal("pending action was not cancelled")
	}
	heartbeat := fleet.Heartbeat{Version: fleet.ProtocolVersion, OwnerID: host.OwnerID, HostID: host.ID, SentAt: time.Now().UTC(), BinaryVersion: "v1.1.0", Capabilities: []string{"session.read"}}
	heartbeat.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(private, fleet.HeartbeatSigningPayload(heartbeat)))
	heartbeatBody, err := json.Marshal(heartbeat)
	if err != nil {
		t.Fatal(err)
	}
	heartbeatReq := httptest.NewRequest(http.MethodPost, "/fleet/heartbeat", strings.NewReader(string(heartbeatBody)))
	heartbeatW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(heartbeatW, heartbeatReq)
	if heartbeatW.Code != http.StatusNotFound {
		t.Fatalf("revoked heartbeat = %d", heartbeatW.Code)
	}
}

func TestFleetKeyRotationRequiresDualProofAndInvalidatesOldIdentity(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	host, currentPrivate, cookie, sessionID := testFleetRotationHost(t, srv)
	newPublic, newPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	challenge := requestFleetKeyRotationChallenge(t, srv, cookie, sessionID, fleet.KeyRotationRequest{
		Version:           fleet.ProtocolVersion,
		HostID:            host.ID,
		NewIdentityPublic: base64.RawURLEncoding.EncodeToString(newPublic),
	})
	if err := challenge.Validate(); err != nil {
		t.Fatal(err)
	}
	payload := fleet.KeyRotationSigningPayload(challenge)
	proof := fleet.KeyRotationProof{
		Version:          fleet.ProtocolVersion,
		ChallengeID:      challenge.ID,
		Nonce:            challenge.Nonce,
		CurrentSignature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(currentPrivate, payload)),
		NewSignature:     base64.RawURLEncoding.EncodeToString(ed25519.Sign(newPrivate, payload)),
	}
	badProof := proof
	badProof.NewSignature = "bad"
	badBody, _ := json.Marshal(badProof)
	badReq := httptest.NewRequest(http.MethodPost, "/fleet/rotate/proof", strings.NewReader(string(badBody)))
	badW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(badW, badReq)
	if badW.Code != http.StatusUnauthorized {
		t.Fatalf("single-key rotation proof = %d", badW.Code)
	}
	proofBody, _ := json.Marshal(proof)
	proofReq := httptest.NewRequest(http.MethodPost, "/fleet/rotate/proof", strings.NewReader(string(proofBody)))
	proofW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(proofW, proofReq)
	if proofW.Code != http.StatusOK {
		t.Fatalf("rotation proof = %d body=%s", proofW.Code, proofW.Body.String())
	}
	var rotated fleetKeyRotationProofResponse
	if err := json.NewDecoder(proofW.Body).Decode(&rotated); err != nil {
		t.Fatal(err)
	}
	if rotated.Host.IdentityPublic != challenge.NewIdentityPublic || rotated.HubProof == "" {
		t.Fatalf("rotation response = %#v", rotated)
	}
	hubPublic, err := decodeEd25519Public(challenge.HubPublic)
	if err != nil {
		t.Fatal(err)
	}
	hubProof, err := base64.RawURLEncoding.DecodeString(rotated.HubProof)
	if err != nil || !ed25519.Verify(hubPublic, payload, hubProof) {
		t.Fatalf("hub rotation proof valid=%v err=%v", err == nil && ed25519.Verify(hubPublic, payload, hubProof), err)
	}
	replayReq := httptest.NewRequest(http.MethodPost, "/fleet/rotate/proof", strings.NewReader(string(proofBody)))
	replayW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(replayW, replayReq)
	if replayW.Code != http.StatusNotFound {
		t.Fatalf("replayed rotation proof = %d", replayW.Code)
	}
	oldHeartbeat := fleet.Heartbeat{Version: fleet.ProtocolVersion, OwnerID: host.OwnerID, HostID: host.ID, SentAt: time.Now().UTC(), BinaryVersion: "v1.1.0", Capabilities: []string{"session.read"}}
	oldHeartbeat.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(currentPrivate, fleet.HeartbeatSigningPayload(oldHeartbeat)))
	oldBody, _ := json.Marshal(oldHeartbeat)
	oldReq := httptest.NewRequest(http.MethodPost, "/fleet/heartbeat", strings.NewReader(string(oldBody)))
	oldW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(oldW, oldReq)
	if oldW.Code != http.StatusUnauthorized {
		t.Fatalf("old identity heartbeat = %d", oldW.Code)
	}
	newHeartbeat := oldHeartbeat
	newHeartbeat.SentAt = time.Now().UTC().Add(time.Second)
	newHeartbeat.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(newPrivate, fleet.HeartbeatSigningPayload(newHeartbeat)))
	newBody, _ := json.Marshal(newHeartbeat)
	newReq := httptest.NewRequest(http.MethodPost, "/fleet/heartbeat", strings.NewReader(string(newBody)))
	newW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(newW, newReq)
	if newW.Code != http.StatusOK {
		t.Fatalf("new identity heartbeat = %d body=%s", newW.Code, newW.Body.String())
	}
}

func TestFleetKeyRotationRejectsIncompatibleStaleAndRevokedRequests(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	host, currentPrivate, cookie, sessionID := testFleetRotationHost(t, srv)
	newPublic, newPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	invalidBody, _ := json.Marshal(fleet.KeyRotationRequest{Version: fleet.ProtocolVersion + 1, HostID: host.ID, NewIdentityPublic: base64.RawURLEncoding.EncodeToString(newPublic)})
	invalidReq := httptest.NewRequest(http.MethodPost, "/fleet/rotate/challenge", strings.NewReader(string(invalidBody)))
	invalidReq.AddCookie(cookie)
	addCSRF(invalidReq, sessionID)
	invalidW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(invalidW, invalidReq)
	if invalidW.Code != http.StatusBadRequest {
		t.Fatalf("incompatible rotation request = %d", invalidW.Code)
	}
	challenge := requestFleetKeyRotationChallenge(t, srv, cookie, sessionID, fleet.KeyRotationRequest{Version: fleet.ProtocolVersion, HostID: host.ID, NewIdentityPublic: base64.RawURLEncoding.EncodeToString(newPublic)})
	if _, err := srv.db.SQL().Exec(`UPDATE fleet_key_rotation_challenges SET expires_at = 0 WHERE id = ?`, challenge.ID); err != nil {
		t.Fatal(err)
	}
	payload := fleet.KeyRotationSigningPayload(challenge)
	staleProof, _ := json.Marshal(fleet.KeyRotationProof{
		Version:          fleet.ProtocolVersion,
		ChallengeID:      challenge.ID,
		Nonce:            challenge.Nonce,
		CurrentSignature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(currentPrivate, payload)),
		NewSignature:     base64.RawURLEncoding.EncodeToString(ed25519.Sign(newPrivate, payload)),
	})
	staleReq := httptest.NewRequest(http.MethodPost, "/fleet/rotate/proof", strings.NewReader(string(staleProof)))
	staleW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(staleW, staleReq)
	if staleW.Code != http.StatusNotFound {
		t.Fatalf("stale rotation proof = %d", staleW.Code)
	}
	host.State = fleet.HostStateRevoked
	now := time.Now().UTC()
	host.RevokedAt = &now
	if err := srv.db.FleetHostUpsert(context.Background(), host); err != nil {
		t.Fatal(err)
	}
	revokedBody, _ := json.Marshal(fleet.KeyRotationRequest{Version: fleet.ProtocolVersion, HostID: host.ID, NewIdentityPublic: base64.RawURLEncoding.EncodeToString(newPublic)})
	revokedReq := httptest.NewRequest(http.MethodPost, "/fleet/rotate/challenge", strings.NewReader(string(revokedBody)))
	revokedReq.AddCookie(cookie)
	addCSRF(revokedReq, sessionID)
	revokedW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(revokedW, revokedReq)
	if revokedW.Code != http.StatusConflict {
		t.Fatalf("revoked rotation request = %d", revokedW.Code)
	}
}

func TestFleetEnrollmentRejectsMalformedIncompatibleExpiredAndRevokedInputs(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	ownerID, err := srv.db.FleetOwnerID(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	baseHost := fleet.Host{
		ID:              "host-enroll",
		DisplayName:     "MacBook Pro",
		IdentityPublic:  base64.RawURLEncoding.EncodeToString(public),
		Endpoint:        fleet.Endpoint{Kind: fleet.EndpointMesh, URL: "https://macbook.tailnet.ts.net"},
		ProtocolVersion: fleet.ProtocolVersion,
		BinaryVersion:   "v1.0.0",
	}
	rr := httptest.NewRecorder()
	sessionID, err := srv.CreateOwnerSession(context.Background(), rr, "phone")
	if err != nil {
		t.Fatal(err)
	}
	cookie := rr.Result().Cookies()[0]
	malformedReq := httptest.NewRequest(http.MethodPost, "/fleet/enroll/challenge", strings.NewReader(`{"host":{}}`))
	malformedReq.AddCookie(cookie)
	addCSRF(malformedReq, sessionID)
	malformedW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(malformedW, malformedReq)
	if malformedW.Code != http.StatusBadRequest {
		t.Fatalf("malformed enrollment challenge = %d", malformedW.Code)
	}
	incompatible := baseHost
	incompatible.ProtocolVersion++
	incompatibleBody, _ := json.Marshal(fleetEnrollmentChallengeRequest{Host: incompatible})
	incompatibleReq := httptest.NewRequest(http.MethodPost, "/fleet/enroll/challenge", strings.NewReader(string(incompatibleBody)))
	incompatibleReq.AddCookie(cookie)
	addCSRF(incompatibleReq, sessionID)
	incompatibleW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(incompatibleW, incompatibleReq)
	if incompatibleW.Code != http.StatusBadRequest {
		t.Fatalf("incompatible enrollment challenge = %d", incompatibleW.Code)
	}
	body, _ := json.Marshal(fleetEnrollmentChallengeRequest{Host: baseHost})
	challengeReq := httptest.NewRequest(http.MethodPost, "/fleet/enroll/challenge", strings.NewReader(string(body)))
	challengeReq.AddCookie(cookie)
	addCSRF(challengeReq, sessionID)
	challengeW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(challengeW, challengeReq)
	if challengeW.Code != http.StatusOK {
		t.Fatalf("enrollment challenge = %d body=%s", challengeW.Code, challengeW.Body.String())
	}
	var challenge fleet.EnrollmentChallenge
	if err := json.NewDecoder(challengeW.Body).Decode(&challenge); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.db.SQL().Exec(`UPDATE fleet_enrollment_challenges SET expires_at = 0 WHERE id = ?`, challenge.ID); err != nil {
		t.Fatal(err)
	}
	baseHost.OwnerID = challenge.OwnerID
	baseHost.State = fleet.HostStatePending
	expiredProof, _ := json.Marshal(fleet.EnrollmentProof{Version: fleet.ProtocolVersion, ChallengeID: challenge.ID, Nonce: challenge.Nonce, Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(private, fleet.EnrollmentSigningPayload(challenge, baseHost)))})
	expiredReq := httptest.NewRequest(http.MethodPost, "/fleet/enroll/proof", strings.NewReader(string(expiredProof)))
	expiredW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(expiredW, expiredReq)
	if expiredW.Code != http.StatusNotFound {
		t.Fatalf("expired enrollment proof = %d", expiredW.Code)
	}
	revokedHost := baseHost
	revokedHost.ID = "host-revoked"
	revokedHost.OwnerID = ownerID
	revokedHost.State = fleet.HostStateRevoked
	now := time.Now().UTC()
	revokedHost.RegisteredAt = now.Add(-time.Minute)
	revokedHost.RevokedAt = &now
	if err := srv.db.FleetHostUpsert(context.Background(), revokedHost); err != nil {
		t.Fatal(err)
	}
	revokedBody, _ := json.Marshal(fleetEnrollmentChallengeRequest{Host: revokedHost})
	revokedReq := httptest.NewRequest(http.MethodPost, "/fleet/enroll/challenge", strings.NewReader(string(revokedBody)))
	revokedReq.AddCookie(cookie)
	addCSRF(revokedReq, sessionID)
	revokedW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(revokedW, revokedReq)
	if revokedW.Code != http.StatusConflict {
		t.Fatalf("revoked enrollment challenge = %d", revokedW.Code)
	}
}

func testFleetRotationHost(t *testing.T, srv *Server) (fleet.Host, ed25519.PrivateKey, *http.Cookie, string) {
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
		ID:              "host-rotation",
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
	rr := httptest.NewRecorder()
	sessionID, err := srv.CreateOwnerSession(context.Background(), rr, "phone")
	if err != nil {
		t.Fatal(err)
	}
	return host, private, rr.Result().Cookies()[0], sessionID
}

func requestFleetKeyRotationChallenge(t *testing.T, srv *Server, cookie *http.Cookie, sessionID string, payload fleet.KeyRotationRequest) fleet.KeyRotationChallenge {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/fleet/rotate/challenge", strings.NewReader(string(body)))
	req.AddCookie(cookie)
	addCSRF(req, sessionID)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("key rotation challenge = %d body=%s", w.Code, w.Body.String())
	}
	var challenge fleet.KeyRotationChallenge
	if err := json.NewDecoder(w.Body).Decode(&challenge); err != nil {
		t.Fatal(err)
	}
	return challenge
}
