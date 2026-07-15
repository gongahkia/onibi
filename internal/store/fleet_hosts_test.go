package store

import (
	"context"
	"strings"
	"sync"
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

func TestFleetHostRejectsUnsafeEnrollmentEndpoint(t *testing.T) {
	db, err := OpenEphemeral(t.TempDir() + "/fleet.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	host := testFleetHost()
	host.Endpoint = fleet.Endpoint{Kind: fleet.EndpointRelay, URL: "https://127.0.0.1"}
	if err := db.FleetHostUpsert(context.Background(), host); err == nil {
		t.Fatal("expected unsafe fleet enrollment endpoint error")
	}
}

func TestFleetHostHeartbeatIsMonotonic(t *testing.T) {
	db, err := OpenEphemeral(t.TempDir() + "/fleet.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	host := testFleetHost()
	host.Budget = fleet.BudgetReport{Date: host.LastSeenAt.Format("2006-01-02"), DailyTokens: 13, GlobalLimit: 10, OnOverrun: "interrupt"}
	if err := db.FleetHostUpsert(context.Background(), host); err != nil {
		t.Fatal(err)
	}
	beat := fleet.Heartbeat{Version: fleet.ProtocolVersion, OwnerID: host.OwnerID, HostID: host.ID, SentAt: host.LastSeenAt.Add(time.Second), BinaryVersion: "v1.2.0", Capabilities: []string{"approval.write", "session.read"}, Budget: fleet.BudgetReport{Date: host.LastSeenAt.Format("2006-01-02"), DailyTokens: 11, GlobalLimit: 10, OnOverrun: "interrupt"}, Signature: "signature"}
	got, applied, err := db.FleetHostRecordHeartbeat(context.Background(), beat)
	if err != nil || !applied || got.BinaryVersion != "v1.2.0" || got.Budget.DailyTokens != 13 {
		t.Fatalf("host=%#v applied=%v err=%v", got, applied, err)
	}
	if _, applied, err := db.FleetHostRecordHeartbeat(context.Background(), beat); err != nil || applied {
		t.Fatalf("replayed heartbeat applied=%v err=%v", applied, err)
	}
	beat.OwnerID = "owner-foreign"
	beat.SentAt = beat.SentAt.Add(time.Second)
	if _, _, err := db.FleetHostRecordHeartbeat(context.Background(), beat); err == nil {
		t.Fatal("expected cross-owner heartbeat error")
	}
}

func TestFleetHostMarksStaleAndRecoversOnNewHeartbeat(t *testing.T) {
	db, err := OpenEphemeral(t.TempDir() + "/fleet.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	host := testFleetHost()
	now := time.Now().UTC().Truncate(time.Second)
	host.RegisteredAt = now.Add(-2 * fleet.HostStaleAfter)
	host.LastSeenAt = now.Add(-fleet.HostStaleAfter - time.Second)
	if err := db.FleetHostUpsert(context.Background(), host); err != nil {
		t.Fatal(err)
	}
	marked, err := db.FleetHostMarkStaleBefore(context.Background(), now.Add(-fleet.HostStaleAfter))
	if err != nil || marked != 1 {
		t.Fatalf("marked=%d err=%v", marked, err)
	}
	stale, ok, err := db.FleetHostGet(context.Background(), host.ID)
	if err != nil || !ok || stale.State != fleet.HostStateStale {
		t.Fatalf("stale host=%#v ok=%v err=%v", stale, ok, err)
	}
	heartbeat := fleet.Heartbeat{Version: fleet.ProtocolVersion, OwnerID: host.OwnerID, HostID: host.ID, SentAt: now, BinaryVersion: "v1.2.0", Capabilities: []string{"session.read"}, Signature: "signature"}
	recovered, applied, err := db.FleetHostRecordHeartbeat(context.Background(), heartbeat)
	if err != nil || !applied || recovered.State != fleet.HostStateActive || recovered.BinaryVersion != heartbeat.BinaryVersion {
		t.Fatalf("recovered=%#v applied=%v err=%v", recovered, applied, err)
	}
}

func TestFleetHostStaleSweepIsConcurrentSafe(t *testing.T) {
	db, err := OpenEphemeral(t.TempDir() + "/fleet.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	host := testFleetHost()
	now := time.Now().UTC().Truncate(time.Second)
	host.RegisteredAt = now.Add(-2 * fleet.HostStaleAfter)
	host.LastSeenAt = now.Add(-fleet.HostStaleAfter - time.Second)
	if err := db.FleetHostUpsert(context.Background(), host); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	marked := make(chan int, 8)
	errs := make(chan error, 8)
	for range cap(marked) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n, err := db.FleetHostMarkStaleBefore(context.Background(), now.Add(-fleet.HostStaleAfter))
			marked <- n
			errs <- err
		}()
	}
	wg.Wait()
	close(marked)
	close(errs)
	total := 0
	for n := range marked {
		total += n
	}
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if total != 1 {
		t.Fatalf("stale hosts marked = %d", total)
	}
}

func TestFleetHostRotateIdentityRequiresCurrentActiveIdentity(t *testing.T) {
	db, err := OpenEphemeral(t.TempDir() + "/fleet.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	host := testFleetHost()
	if err := db.FleetHostUpsert(context.Background(), host); err != nil {
		t.Fatal(err)
	}
	rotated, ok, err := db.FleetHostRotateIdentity(context.Background(), host.ID, host.IdentityPublic, "rotated-public-key")
	if err != nil || !ok || rotated.IdentityPublic != "rotated-public-key" {
		t.Fatalf("rotated=%#v ok=%v err=%v", rotated, ok, err)
	}
	if _, ok, err := db.FleetHostRotateIdentity(context.Background(), host.ID, host.IdentityPublic, "second-public-key"); err != nil || ok {
		t.Fatalf("stale rotation ok=%v err=%v", ok, err)
	}
	rotated.State = fleet.HostStateRevoked
	now := time.Now().UTC()
	rotated.RevokedAt = &now
	if err := db.FleetHostUpsert(context.Background(), rotated); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := db.FleetHostRotateIdentity(context.Background(), host.ID, rotated.IdentityPublic, "third-public-key"); err != nil || ok {
		t.Fatalf("revoked rotation ok=%v err=%v", ok, err)
	}
}
