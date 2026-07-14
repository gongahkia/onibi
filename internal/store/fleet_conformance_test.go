package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/fleet"
)

func TestFleetSchemaMigrationAndPersistenceConformance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fleet-legacy.sqlite")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`CREATE TABLE schema_version (version INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	db, err := Open(path, WithStoreKey(key))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, table := range []string{"fleet_hosts", "fleet_enrollment_challenges", "fleet_key_rotation_challenges"} {
		if len(tableColumns(t, db, table)) == 0 {
			t.Fatalf("migration did not create %s", table)
		}
	}
	var version int
	if err := db.SQL().QueryRowContext(ctx, `SELECT version FROM schema_version WHERE version = 13`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	ownerID, err := db.FleetOwnerID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	host := testFleetHost()
	host.OwnerID = ownerID
	if err := db.FleetHostUpsert(ctx, host); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(path, WithStoreKey(key))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	persisted, ok, err := db.FleetHostGet(ctx, host.ID)
	if err != nil || !ok || persisted.OwnerID != ownerID || persisted.IdentityPublic != host.IdentityPublic {
		t.Fatalf("persisted host=%#v ok=%v err=%v", persisted, ok, err)
	}
}

func TestFleetLifecyclePersistenceConformance(t *testing.T) {
	db := openTemp(t)
	ctx := context.Background()
	ownerID, err := db.FleetOwnerID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	host := testFleetHost()
	host.OwnerID = ownerID
	host.RegisteredAt = now.Add(-2 * fleet.HostStaleAfter)
	host.LastSeenAt = now.Add(-fleet.HostStaleAfter - time.Second)
	if err := db.FleetHostUpsert(ctx, host); err != nil {
		t.Fatal(err)
	}
	marked, err := db.FleetHostMarkStaleBefore(ctx, now.Add(-fleet.HostStaleAfter))
	if err != nil || marked != 1 {
		t.Fatalf("stale sweep marked=%d err=%v", marked, err)
	}
	heartbeat := fleet.Heartbeat{Version: fleet.ProtocolVersion, OwnerID: ownerID, HostID: host.ID, SentAt: now, BinaryVersion: "v1.2.0", Capabilities: []string{"session.read"}, Signature: "signature"}
	recovered, applied, err := db.FleetHostRecordHeartbeat(ctx, heartbeat)
	if err != nil || !applied || recovered.State != fleet.HostStateActive {
		t.Fatalf("recovered host=%#v applied=%v err=%v", recovered, applied, err)
	}
	if err := db.PutWebSession(ctx, "session-123", "iPhone", now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO approvals(id, session_id, agent, tool, input_json, state, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, "approval-123", "session-123", "claude", "Bash", `{}`, "pending", now.Unix(), now.Add(time.Minute).Unix()); err != nil {
		t.Fatal(err)
	}
	const revokers = 12
	results := make(chan bool, revokers)
	errs := make(chan error, revokers)
	var wg sync.WaitGroup
	for range revokers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, revoked, err := db.FleetHostEmergencyRevoke(ctx, ownerID, host.ID, now.Add(time.Second))
			results <- revoked
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	successes := 0
	for revoked := range results {
		if revoked {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("successful revocations = %d", successes)
	}
	revoked, ok, err := db.FleetHostGet(ctx, host.ID)
	if err != nil || !ok || revoked.State != fleet.HostStateRevoked || revoked.RevokedAt == nil {
		t.Fatalf("revoked host=%#v ok=%v err=%v", revoked, ok, err)
	}
	if _, applied, err := db.FleetHostRecordHeartbeat(ctx, fleet.Heartbeat{Version: fleet.ProtocolVersion, OwnerID: ownerID, HostID: host.ID, SentAt: now.Add(2 * time.Second), BinaryVersion: "v1.3.0", Signature: "signature"}); err != nil || applied {
		t.Fatalf("revoked heartbeat applied=%v err=%v", applied, err)
	}
	status, err := db.WebSessionStatus(ctx, "session-123")
	if err != nil || status.Valid || status.Reason != WebSessionReasonFleetEmergency {
		t.Fatalf("web session status=%#v err=%v", status, err)
	}
	var state, reason string
	if err := db.SQL().QueryRowContext(ctx, `SELECT state, reason FROM approvals WHERE id = ?`, "approval-123").Scan(&state, &reason); err != nil {
		t.Fatal(err)
	}
	if state != "cancelled" || reason != "fleet emergency host revocation" {
		t.Fatalf("approval state=%q reason=%q", state, reason)
	}
}
