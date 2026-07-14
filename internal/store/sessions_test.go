package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/fleet"
)

func TestSessionStartRecentAndEnd(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "d.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	started := time.Now().Add(-time.Minute).Truncate(time.Second)
	if err := db.SessionUpsertStart(ctx, "s1", "claude", "claude", "/tmp", "claude --resume", "pty", "", started); err != nil {
		t.Fatal(err)
	}
	active, err := db.SessionsRecent(ctx, 10, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].ID != "s1" || active[0].Command != "claude --resume" || active[0].Ended {
		t.Fatalf("active = %#v", active)
	}
	if err := db.SessionMarkEnded(ctx, "s1", time.Now()); err != nil {
		t.Fatal(err)
	}
	active, err = db.SessionsRecent(ctx, 10, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 0 {
		t.Fatalf("active after end = %#v", active)
	}
	all, err := db.SessionsRecent(ctx, 10, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || !all[0].Ended {
		t.Fatalf("all = %#v", all)
	}
}

func TestSessionRecoveryStateMachineIsDurable(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "d.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	if err := db.SessionUpsertStart(ctx, "s1", "claude", "claude", "/tmp", "claude", "tmux", "onibi-s1", now); err != nil {
		t.Fatal(err)
	}
	entry, ok, err := db.Session(ctx, "s1")
	if err != nil || !ok || entry.RecoveryState != fleet.SessionRecoveryHealthy || entry.RecoveryReason != "" {
		t.Fatalf("initial session=%#v ok=%v err=%v", entry, ok, err)
	}
	if changed, err := db.SessionTransitionRecovery(ctx, "s1", fleet.SessionRecoveryReconnecting, "tmux capture disconnected", now.Add(time.Second)); err != nil || !changed {
		t.Fatalf("reconnecting changed=%v err=%v", changed, err)
	}
	if changed, err := db.SessionTransitionRecovery(ctx, "s1", fleet.SessionRecoveryReconnecting, "tmux capture disconnected", now.Add(2*time.Second)); err != nil || changed {
		t.Fatalf("duplicate reconnect changed=%v err=%v", changed, err)
	}
	if changed, err := db.SessionTransitionRecovery(ctx, "s1", fleet.SessionRecoveryHealthy, "", now.Add(3*time.Second)); err != nil || !changed {
		t.Fatalf("healthy changed=%v err=%v", changed, err)
	}
	if changed, err := db.SessionTransitionRecovery(ctx, "s1", fleet.SessionRecoveryFailed, "unsupported transport", now.Add(4*time.Second)); err != nil || !changed {
		t.Fatalf("failed changed=%v err=%v", changed, err)
	}
	if changed, err := db.SessionTransitionRecovery(ctx, "s1", fleet.SessionRecoveryHealthy, "", now.Add(5*time.Second)); err == nil || changed {
		t.Fatalf("failed-to-healthy changed=%v err=%v", changed, err)
	}
	if changed, err := db.SessionTransitionRecovery(ctx, "s1", fleet.SessionRecoveryRecovering, "retrying recovery", now.Add(6*time.Second)); err != nil || !changed {
		t.Fatalf("recovering changed=%v err=%v", changed, err)
	}
	if changed, err := db.SessionTransitionRecovery(ctx, "s1", fleet.SessionRecoveryOrphaned, "tmux reconnect timed out", now.Add(7*time.Second)); err != nil || !changed {
		t.Fatalf("orphaned changed=%v err=%v", changed, err)
	}
	if err := db.SessionMarkEnded(ctx, "s1", now.Add(8*time.Second)); err != nil {
		t.Fatal(err)
	}
	entry, ok, err = db.Session(ctx, "s1")
	if err != nil || !ok || !entry.Ended || entry.RecoveryState != fleet.SessionRecoveryTerminated || entry.RecoveryReason != "session terminated" {
		t.Fatalf("terminated session=%#v ok=%v err=%v", entry, ok, err)
	}
}

func TestSessionRecoveryMigrationPreservesLegacySession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.sqlite")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`CREATE TABLE sessions (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  agent TEXT NOT NULL,
  cwd TEXT,
  cmd TEXT,
  transport TEXT NOT NULL DEFAULT 'pty',
  tmux_target TEXT,
  started_at INTEGER NOT NULL,
  last_activity INTEGER,
  ended_at INTEGER
);
INSERT INTO sessions(id, name, agent, transport, started_at, last_activity) VALUES ('s1', 'legacy', 'claude', 'tmux', 10, 11);`); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	entry, ok, err := db.Session(context.Background(), "s1")
	if err != nil || !ok || entry.RecoveryState != fleet.SessionRecoveryHealthy || entry.RecoveryReason != "" || !entry.RecoveryUpdatedAt.IsZero() {
		t.Fatalf("migrated session=%#v ok=%v err=%v", entry, ok, err)
	}
}
