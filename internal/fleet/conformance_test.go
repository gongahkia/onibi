package fleet

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestProtocolVersionConformance(t *testing.T) {
	now := time.Date(2026, 7, 14, 1, 2, 3, 0, time.UTC)
	host := testHost(t)
	challenge := EnrollmentChallenge{Version: ProtocolVersion, ID: "enroll-123", OwnerID: host.OwnerID, Nonce: "nonce", HubPublic: "hub-public", ExpiresAt: now.Add(time.Minute)}
	rotation := KeyRotationChallenge{Version: ProtocolVersion, ID: "rotate-123", OwnerID: host.OwnerID, HostID: host.ID, CurrentIdentityPublic: "current-public", NewIdentityPublic: "new-public", Nonce: "nonce", HubPublic: "hub-public", ExpiresAt: now.Add(time.Minute)}
	linkChallenge := LinkChallenge{Version: ProtocolVersion, ID: "link-123", Nonce: "nonce", ExpiresAt: now.Add(time.Minute)}
	cases := []struct {
		name     string
		validate func(uint16) error
	}{
		{"envelope", func(version uint16) error {
			return (Envelope{Version: version, Type: MessageHeartbeat, RequestID: "request-123", SentAt: now, Body: []byte(`{}`)}).Validate()
		}},
		{"host", func(version uint16) error {
			candidate := host
			candidate.ProtocolVersion = version
			return candidate.Validate()
		}},
		{"snapshot", func(version uint16) error {
			return (Snapshot{Version: version, GeneratedAt: now, Hosts: []Host{host}}).Validate()
		}},
		{"home-status", func(version uint16) error {
			return (HomeStatus{Version: version, GeneratedAt: now, Hosts: []Host{host}}).Validate()
		}},
		{"enrollment-challenge", func(version uint16) error {
			candidate := challenge
			candidate.Version = version
			return candidate.Validate()
		}},
		{"enrollment-proof", func(version uint16) error {
			return (EnrollmentProof{Version: version, ChallengeID: challenge.ID, Nonce: challenge.Nonce, Signature: "signature"}).Validate()
		}},
		{"rotation-request", func(version uint16) error {
			return (KeyRotationRequest{Version: version, HostID: host.ID, NewIdentityPublic: "new-public"}).Validate()
		}},
		{"rotation-challenge", func(version uint16) error {
			candidate := rotation
			candidate.Version = version
			return candidate.Validate()
		}},
		{"rotation-proof", func(version uint16) error {
			return (KeyRotationProof{Version: version, ChallengeID: rotation.ID, Nonce: rotation.Nonce, CurrentSignature: "current", NewSignature: "next"}).Validate()
		}},
		{"heartbeat", func(version uint16) error {
			return (Heartbeat{Version: version, OwnerID: host.OwnerID, HostID: host.ID, SentAt: now, BinaryVersion: "v1.0.0", Signature: "signature"}).Validate()
		}},
		{"revocation", func(version uint16) error { return (RevocationRequest{Version: version, HostID: host.ID}).Validate() }},
		{"link-challenge", func(version uint16) error {
			candidate := linkChallenge
			candidate.Version = version
			return candidate.Validate()
		}},
		{"link-authentication", func(version uint16) error {
			return (LinkAuthenticate{Version: version, OwnerID: host.OwnerID, HostID: host.ID, ChallengeID: linkChallenge.ID, Nonce: linkChallenge.Nonce, SentAt: now, Signature: "signature"}).Validate()
		}},
		{"link-acceptance", func(version uint16) error {
			return (LinkAccepted{Version: version, OwnerID: host.OwnerID, HostID: host.ID, ChallengeID: linkChallenge.ID, Nonce: linkChallenge.Nonce, SentAt: now, Signature: "signature"}).Validate()
		}},
		{"link-frame", func(version uint16) error {
			return (LinkFrame{Envelope: Envelope{Version: version, Type: MessageHeartbeat, RequestID: "frame-123", SentAt: now, Body: []byte(`{}`)}, Signature: "signature"}).Validate()
		}},
		{"control", func(version uint16) error {
			return (Control{Version: version, ID: "control-123", OwnerID: host.OwnerID, HostID: host.ID, Command: "interrupt", ExpiresAt: now.Add(time.Minute)}).Validate()
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.validate(ProtocolVersion); err != nil {
				t.Fatalf("current protocol rejected: %v", err)
			}
			for _, version := range []uint16{0, ProtocolVersion + 1} {
				err := tc.validate(version)
				if err == nil || !strings.Contains(err.Error(), "incompatible") {
					t.Fatalf("version %d error = %v", version, err)
				}
			}
		})
	}
}

func TestProtocolMalformedAndOwnerBindingConformance(t *testing.T) {
	now := time.Date(2026, 7, 14, 1, 2, 3, 0, time.UTC)
	cases := []struct {
		name     string
		validate func() error
	}{
		{"envelope", func() error {
			return (Envelope{Version: ProtocolVersion, Type: MessageHeartbeat, RequestID: "bad/request", SentAt: now, Body: []byte(`{}`)}).Validate()
		}},
		{"host", func() error { host := testHost(t); host.OwnerID = ""; return host.Validate() }},
		{"enrollment-challenge", func() error {
			return (EnrollmentChallenge{Version: ProtocolVersion, ID: "enroll-123", Nonce: "nonce", HubPublic: "hub-public", ExpiresAt: now}).Validate()
		}},
		{"heartbeat", func() error {
			return (Heartbeat{Version: ProtocolVersion, HostID: "host-macbook", SentAt: now, BinaryVersion: "v1.0.0", Signature: "signature"}).Validate()
		}},
		{"link-authentication", func() error {
			return (LinkAuthenticate{Version: ProtocolVersion, HostID: "host-macbook", ChallengeID: "link-123", Nonce: "nonce", SentAt: now, Signature: "signature"}).Validate()
		}},
		{"link-acceptance", func() error {
			return (LinkAccepted{Version: ProtocolVersion, HostID: "host-macbook", ChallengeID: "link-123", Nonce: "nonce", SentAt: now, Signature: "signature"}).Validate()
		}},
		{"control", func() error {
			return (Control{Version: ProtocolVersion, ID: "control-123", HostID: "host-macbook", Command: "interrupt", ExpiresAt: now.Add(time.Minute)}).Validate()
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.validate(); err == nil {
				t.Fatal("expected malformed input error")
			}
		})
	}
	heartbeat := Heartbeat{Version: ProtocolVersion, OwnerID: "owner-local", HostID: "host-macbook", SentAt: now, BinaryVersion: "v1.0.0", Signature: "signature"}
	changedHeartbeat := heartbeat
	changedHeartbeat.OwnerID = "owner-foreign"
	if string(HeartbeatSigningPayload(heartbeat)) == string(HeartbeatSigningPayload(changedHeartbeat)) {
		t.Fatal("heartbeat payload did not bind owner")
	}
	challenge := LinkChallenge{Version: ProtocolVersion, ID: "link-123", Nonce: "nonce", ExpiresAt: now.Add(time.Minute)}
	auth := LinkAuthenticate{Version: ProtocolVersion, OwnerID: heartbeat.OwnerID, HostID: heartbeat.HostID, ChallengeID: challenge.ID, Nonce: challenge.Nonce, SentAt: now, Signature: "signature"}
	changedAuth := auth
	changedAuth.OwnerID = "owner-foreign"
	if string(LinkAuthenticateSigningPayload(challenge, auth)) == string(LinkAuthenticateSigningPayload(challenge, changedAuth)) {
		t.Fatal("link authentication payload did not bind owner")
	}
	accepted := LinkAccepted{Version: ProtocolVersion, OwnerID: heartbeat.OwnerID, HostID: heartbeat.HostID, ChallengeID: challenge.ID, Nonce: challenge.Nonce, SentAt: now, Signature: "signature"}
	changedAccepted := accepted
	changedAccepted.OwnerID = "owner-foreign"
	if string(LinkAcceptedSigningPayload(challenge, accepted)) == string(LinkAcceptedSigningPayload(challenge, changedAccepted)) {
		t.Fatal("link acceptance payload did not bind owner")
	}
	control := Control{Version: ProtocolVersion, ID: "control-123", OwnerID: heartbeat.OwnerID, HostID: heartbeat.HostID, Command: "interrupt", ExpiresAt: now.Add(time.Minute)}
	changedControl := control
	changedControl.OwnerID = "owner-foreign"
	body, err := json.Marshal(control)
	if err != nil {
		t.Fatal(err)
	}
	changedBody, err := json.Marshal(changedControl)
	if err != nil {
		t.Fatal(err)
	}
	frame := Envelope{Version: ProtocolVersion, Type: MessageControl, RequestID: "frame-123", SentAt: now, Body: body}
	changedFrame := frame
	changedFrame.Body = changedBody
	if string(LinkFrameSigningPayload(frame)) == string(LinkFrameSigningPayload(changedFrame)) {
		t.Fatal("control frame payload did not bind owner")
	}
}
