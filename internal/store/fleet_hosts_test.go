package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/fleet"
)

func testFleetHost() fleet.Host {
	registered := time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC)
	return fleet.Host{
		ID:              "host-macbook",
		OwnerID:         "owner-local",
		DisplayName:     "MacBook Pro",
		IdentityPublic:  "public-key",
		Endpoint:        fleet.Endpoint{Kind: fleet.EndpointMesh, URL: "https://macbook.tailnet.ts.net"},
		ProtocolVersion: fleet.ProtocolVersion,
		BinaryVersion:   "v1.0.0",
		Capabilities:    []string{"session.read", "approval.write"},
		State:           fleet.HostStateActive,
		RegisteredAt:    registered,
		LastSeenAt:      registered.Add(time.Minute),
	}
}

func TestFleetHostRoundTripIsEncrypted(t *testing.T) {
	db, err := OpenEphemeral(t.TempDir() + "/fleet.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	host := testFleetHost()
	if err := db.FleetHostUpsert(context.Background(), host); err != nil {
		t.Fatal(err)
	}
	got, ok, err := db.FleetHostGet(context.Background(), host.ID)
	if err != nil || !ok {
		t.Fatalf("get ok=%v err=%v", ok, err)
	}
	if got.DisplayName != host.DisplayName || got.Endpoint.URL != host.Endpoint.URL || got.OwnerID != host.OwnerID {
		t.Fatalf("host = %#v", got)
	}
	var sealed []byte
	if err := db.SQL().QueryRow(`SELECT data_enc FROM fleet_hosts WHERE id = ?`, host.ID).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(sealed), host.DisplayName) || strings.Contains(string(sealed), host.Endpoint.URL) || strings.Contains(string(sealed), host.OwnerID) {
		t.Fatal("fleet host metadata stored plaintext")
	}
}

func TestFleetHostRejectsTamperedRecord(t *testing.T) {
	db, err := OpenEphemeral(t.TempDir() + "/fleet.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	host := testFleetHost()
	if err := db.FleetHostUpsert(context.Background(), host); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().Exec(`UPDATE fleet_hosts SET data_enc = ? WHERE id = ?`, []byte("tampered"), host.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.FleetHostGet(context.Background(), host.ID); err == nil {
		t.Fatal("expected tampered fleet host error")
	}
}

func TestFleetHostHeartbeatIsMonotonic(t *testing.T) {
	db, err := OpenEphemeral(t.TempDir() + "/fleet.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	host := testFleetHost()
	if err := db.FleetHostUpsert(context.Background(), host); err != nil {
		t.Fatal(err)
	}
	beat := fleet.Heartbeat{Version: fleet.ProtocolVersion, HostID: host.ID, SentAt: host.LastSeenAt.Add(time.Second), BinaryVersion: "v1.2.0", Capabilities: []string{"approval.write", "session.read"}, Signature: "signature"}
	got, applied, err := db.FleetHostRecordHeartbeat(context.Background(), beat)
	if err != nil || !applied || got.BinaryVersion != "v1.2.0" {
		t.Fatalf("host=%#v applied=%v err=%v", got, applied, err)
	}
	if _, applied, err := db.FleetHostRecordHeartbeat(context.Background(), beat); err != nil || applied {
		t.Fatalf("replayed heartbeat applied=%v err=%v", applied, err)
	}
}
