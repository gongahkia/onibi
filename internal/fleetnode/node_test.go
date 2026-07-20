package fleetnode

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/fleet"
	"github.com/gongahkia/onibi/internal/store"
)

func TestEnrollmentPersistsVerifiedFleetLink(t *testing.T) {
	ctx := context.Background()
	db, err := store.OpenEphemeral(t.TempDir() + "/fleet-node.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	identity, err := LoadOrCreateIdentity(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	host, err := identity.Host("Mesh host", fleet.Endpoint{Kind: fleet.EndpointMesh, URL: "https://100.64.0.2"}, "v1.0.0", []string{"session.read"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	host.OwnerID = "owner-local"
	host.RegisteredAt = now
	challengePublic, challengePrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	challenge := fleet.EnrollmentChallenge{Version: fleet.ProtocolVersion, ID: "enroll-host", OwnerID: host.OwnerID, Nonce: "nonce", HubPublic: base64.RawURLEncoding.EncodeToString(challengePublic), ExpiresAt: now.Add(time.Minute)}
	proof, err := identity.Sign(challenge, host)
	if err != nil {
		t.Fatal(err)
	}
	if err := proof.Validate(); err != nil {
		t.Fatal(err)
	}
	host.State = fleet.HostStateActive
	host.LastSeenAt = now
	hubProof := ed25519.Sign(challengePrivate, fleet.EnrollmentSigningPayload(challenge, host))
	config, err := Configure(ctx, db, identity, Enrollment{HubURL: "https://hub.example.test", Challenge: challenge, Host: host, HubProof: base64.RawURLEncoding.EncodeToString(hubProof)})
	if err != nil {
		t.Fatal(err)
	}
	if config.OwnerID != host.OwnerID || config.HostID != host.ID {
		t.Fatalf("config = %#v", config)
	}
	loaded, found, err := LoadConfig(ctx, db)
	if err != nil || !found || !reflect.DeepEqual(loaded, config) {
		t.Fatalf("loaded=%#v found=%v err=%v", loaded, found, err)
	}
	var raw []byte
	if err := db.SQL().QueryRow(`SELECT value FROM kv WHERE key = ?`, identityKey).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), identity.PrivateKey) {
		t.Fatal("fleet private key stored plaintext")
	}
}

func TestConfigureRejectsInvalidHubProof(t *testing.T) {
	ctx := context.Background()
	db, err := store.OpenEphemeral(t.TempDir() + "/fleet-node.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	identity, err := LoadOrCreateIdentity(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	host, err := identity.Host("Mesh host", fleet.Endpoint{Kind: fleet.EndpointMesh, URL: "https://100.64.0.2"}, "v1.0.0", nil)
	if err != nil {
		t.Fatal(err)
	}
	host.OwnerID, host.RegisteredAt, host.State = "owner-local", now, fleet.HostStateActive
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	challenge := fleet.EnrollmentChallenge{Version: fleet.ProtocolVersion, ID: "enroll-host", OwnerID: host.OwnerID, Nonce: "nonce", HubPublic: base64.RawURLEncoding.EncodeToString(public), ExpiresAt: now.Add(time.Minute)}
	if _, err := Configure(ctx, db, identity, Enrollment{HubURL: "https://hub.example.test", Challenge: challenge, Host: host, HubProof: "bad"}); err == nil {
		t.Fatal("expected invalid hub proof")
	}
}
