package fleet

import (
	"strings"
	"testing"
	"time"
)

func testHost(t *testing.T) Host {
	t.Helper()
	return Host{
		ID:              "host-macbook",
		OwnerID:         "owner-local",
		DisplayName:     "MacBook Pro",
		IdentityPublic:  "base64-public-key",
		Endpoint:        Endpoint{Kind: EndpointMesh, URL: "https://macbook.tailnet.ts.net"},
		ProtocolVersion: ProtocolVersion,
		BinaryVersion:   "v1.0.0",
		Capabilities:    []string{"session.read", "approval.write", "session.read"},
		State:           HostStateActive,
		RegisteredAt:    time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC),
		LastSeenAt:      time.Date(2026, 7, 13, 0, 1, 0, 0, time.UTC),
	}
}

func TestHostValidateAndNormalize(t *testing.T) {
	host := testHost(t).Normalized()
	if err := host.Validate(); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(host.Capabilities, ","); got != "approval.write,session.read" {
		t.Fatalf("capabilities = %q", got)
	}
}

func TestHostRejectsInvalidEndpointAndRevocationState(t *testing.T) {
	host := testHost(t)
	host.Endpoint.URL = "http://macbook.tailnet.ts.net"
	if err := host.Validate(); err == nil {
		t.Fatal("expected endpoint validation error")
	}
	host = testHost(t)
	host.State = HostStateRevoked
	if err := host.Validate(); err == nil || !strings.Contains(err.Error(), "revoked_at") {
		t.Fatalf("unexpected err: %v", err)
	}
}

func TestEndpointRejectsUnsafeAdapterTargets(t *testing.T) {
	for _, endpoint := range []Endpoint{
		{Kind: EndpointMesh, URL: "https://localhost"},
		{Kind: EndpointRelay, URL: "https://192.168.1.2"},
		{Kind: EndpointRelay, URL: "https://100.64.0.2"},
		{Kind: "ssh", URL: "onibi@host.example.test"},
	} {
		if err := endpoint.Validate(); err == nil {
			t.Fatalf("expected invalid endpoint: %#v", endpoint)
		}
	}
	for _, endpoint := range []Endpoint{
		{Kind: EndpointMesh, URL: "https://100.64.0.2"},
		{Kind: EndpointRelay, URL: "https://relay.example.test"},
	} {
		if err := endpoint.Validate(); err != nil {
			t.Fatalf("endpoint %#v: %v", endpoint, err)
		}
	}
}

func TestEnvelopeRejectsIncompatibleAndMalformedInputs(t *testing.T) {
	env := Envelope{Version: ProtocolVersion, Type: MessageHeartbeat, RequestID: "req-123", SentAt: time.Now(), Body: []byte(`{}`)}
	if err := env.Validate(); err != nil {
		t.Fatal(err)
	}
	env.Version++
	if err := env.Validate(); err == nil || !strings.Contains(err.Error(), "incompatible") {
		t.Fatalf("unexpected err: %v", err)
	}
	env.Version = ProtocolVersion
	env.RequestID = "bad/request"
	if err := env.Validate(); err == nil || !strings.Contains(err.Error(), "request_id") {
		t.Fatalf("unexpected err: %v", err)
	}
}

func TestSnapshotValidatesHostReferences(t *testing.T) {
	host := testHost(t)
	snapshot := Snapshot{
		Version:     ProtocolVersion,
		GeneratedAt: time.Now(),
		Hosts:       []Host{host},
		Sessions:    []Session{{ID: "session-1", HostID: host.ID, Agent: "claude", State: "working", LastActivity: time.Now()}},
		Approvals:   []Approval{{ID: "approval-1", HostID: host.ID, SessionID: "session-1", State: "pending", CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Minute)}},
	}
	if err := snapshot.Validate(); err != nil {
		t.Fatal(err)
	}
	snapshot.Sessions[0].HostID = "unknown-host"
	if err := snapshot.Validate(); err == nil || !strings.Contains(err.Error(), "unknown host") {
		t.Fatalf("unexpected err: %v", err)
	}
}

func TestHomeStatusValidatesVersionAndRedactedApprovalMetadata(t *testing.T) {
	now := time.Date(2026, 7, 14, 1, 0, 0, 0, time.UTC)
	status := HomeStatus{
		Version:     ProtocolVersion,
		GeneratedAt: now,
		Hosts:       []Host{testHost(t)},
		Sessions:    []HomeSessionStatus{{ID: "session-1", HostID: "host-macbook", Agent: "claude", State: "awaiting-approval", LastActivity: now, PendingApprovals: 1, RecoveryState: SessionRecoveryOrphaned, RecoveryReason: "tmux reconnect timed out", RecoveryUpdatedAt: now}},
		PendingApprovals: []HomeApprovalStatus{{
			ID: "approval-1", HostID: "host-macbook", SessionID: "session-1", Agent: "claude", Tool: "Bash", State: "pending", CreatedAt: now, ExpiresAt: now.Add(time.Minute),
		}},
	}
	if err := status.Validate(); err != nil {
		t.Fatal(err)
	}
	status.Sessions[0].HostID = "unknown-host"
	if err := status.Validate(); err == nil || !strings.Contains(err.Error(), "unknown host") {
		t.Fatalf("unexpected err: %v", err)
	}
	status.Sessions[0].HostID = "host-macbook"
	status.PendingApprovals[0].HostID = "unknown-host"
	if err := status.Validate(); err == nil || !strings.Contains(err.Error(), "unknown host") {
		t.Fatalf("unexpected err: %v", err)
	}
	status.PendingApprovals[0].HostID = "host-macbook"
	status.Version++
	if err := status.Validate(); err == nil {
		t.Fatal("expected incompatible home status error")
	}
	status.Version = ProtocolVersion
	status.Sessions[0].PendingApprovals = -1
	if err := status.Validate(); err == nil {
		t.Fatal("expected malformed home status error")
	}
	status.Sessions[0].PendingApprovals = 1
	status.Sessions[0].RecoveryState = "unknown"
	if err := status.Validate(); err == nil {
		t.Fatal("expected invalid recovery state error")
	}
}

func TestEnrollmentSigningPayloadBindsChallengeAndHostIdentity(t *testing.T) {
	host := testHost(t)
	challenge := EnrollmentChallenge{Version: ProtocolVersion, ID: "enroll-123", OwnerID: host.OwnerID, Nonce: "nonce", HubPublic: "hub-public", ExpiresAt: time.Date(2026, 7, 13, 1, 0, 0, 0, time.UTC)}
	if err := challenge.Validate(); err != nil {
		t.Fatal(err)
	}
	payload := string(EnrollmentSigningPayload(challenge, host))
	for _, want := range []string{challenge.ID, challenge.Nonce, host.ID, host.OwnerID, host.IdentityPublic} {
		if !strings.Contains(payload, want) {
			t.Fatalf("payload missing %q: %q", want, payload)
		}
	}
}

func TestHeartbeatSigningPayloadNormalizesCapabilities(t *testing.T) {
	heartbeat := Heartbeat{Version: ProtocolVersion, OwnerID: "owner-local", HostID: "host-macbook", SentAt: time.Date(2026, 7, 13, 1, 0, 0, 0, time.UTC), BinaryVersion: "v1.0.0", Capabilities: []string{"session.read", "APPROVAL.WRITE", "session.read"}, Signature: "signature"}
	if err := heartbeat.Validate(); err != nil {
		t.Fatal(err)
	}
	payload := string(HeartbeatSigningPayload(heartbeat))
	if !strings.Contains(payload, "approval.write,session.read") {
		t.Fatalf("payload = %q", payload)
	}
}

func TestKeyRotationProtocolValidationAndBinding(t *testing.T) {
	challenge := KeyRotationChallenge{
		Version:               ProtocolVersion,
		ID:                    "rotate-123",
		OwnerID:               "owner-local",
		HostID:                "host-macbook",
		CurrentIdentityPublic: "current-public-key",
		NewIdentityPublic:     "new-public-key",
		Nonce:                 "nonce",
		HubPublic:             "hub-public",
		ExpiresAt:             time.Date(2026, 7, 14, 1, 2, 3, 0, time.UTC),
	}
	if err := challenge.Validate(); err != nil {
		t.Fatal(err)
	}
	payload := string(KeyRotationSigningPayload(challenge))
	for _, want := range []string{challenge.ID, challenge.OwnerID, challenge.HostID, challenge.CurrentIdentityPublic, challenge.NewIdentityPublic, challenge.Nonce} {
		if !strings.Contains(payload, want) {
			t.Fatalf("key rotation payload missing %q: %q", want, payload)
		}
	}
	if err := (KeyRotationProof{Version: ProtocolVersion, ChallengeID: challenge.ID, Nonce: challenge.Nonce, CurrentSignature: "current", NewSignature: "new"}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (KeyRotationRequest{Version: ProtocolVersion + 1, HostID: challenge.HostID, NewIdentityPublic: challenge.NewIdentityPublic}).Validate(); err == nil {
		t.Fatal("expected incompatible key rotation request error")
	}
}

func TestLinkProtocolBindsChallengeAndFrames(t *testing.T) {
	challenge := LinkChallenge{Version: ProtocolVersion, ID: "link-123", Nonce: "nonce", ExpiresAt: time.Date(2026, 7, 14, 1, 2, 3, 0, time.UTC)}
	auth := LinkAuthenticate{Version: ProtocolVersion, OwnerID: "owner-local", HostID: "host-macbook", ChallengeID: challenge.ID, Nonce: challenge.Nonce, SentAt: time.Date(2026, 7, 14, 1, 2, 0, 0, time.UTC), Signature: "signature"}
	if err := challenge.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := auth.Validate(); err != nil {
		t.Fatal(err)
	}
	payload := string(LinkAuthenticateSigningPayload(challenge, auth))
	for _, want := range []string{challenge.ID, challenge.Nonce, auth.OwnerID, auth.HostID, auth.SentAt.UTC().Format(time.RFC3339Nano)} {
		if !strings.Contains(payload, want) {
			t.Fatalf("link authentication payload missing %q: %q", want, payload)
		}
	}
	envelope := Envelope{Version: ProtocolVersion, Type: MessageHeartbeat, RequestID: "link-frame", SentAt: auth.SentAt, Body: []byte(`{"host_id":"host-macbook"}`)}
	frame := LinkFrame{Envelope: envelope, Signature: "signature"}
	if err := frame.Validate(); err != nil {
		t.Fatal(err)
	}
	changed := envelope
	changed.Body = []byte(`{"host_id":"host-other"}`)
	if string(LinkFrameSigningPayload(envelope)) == string(LinkFrameSigningPayload(changed)) {
		t.Fatal("link frame payload did not bind body")
	}
}
