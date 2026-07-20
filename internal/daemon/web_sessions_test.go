package daemon

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/store"
)

func TestWebSessionsAggregatesActiveRows(t *testing.T) {
	db, err := store.OpenEphemeral(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	d := New(Options{DB: db})
	started := time.Now().Add(-time.Minute).UTC().Truncate(time.Second)
	if err := db.SessionUpsertStart(t.Context(), "s1", "main", "claude", "/tmp/repo", "claude", "pty", "", started); err != nil {
		t.Fatal(err)
	}
	if err := db.SessionUpsertStart(t.Context(), "s2", "shell", "shell", "/tmp/other", "zsh", "pty", "", started.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := d.Queue.Request(t.Context(), "s1", "claude", "Bash", `{"command":"ls"}`); err != nil {
		t.Fatal(err)
	}
	rows, err := d.WebSessions(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %#v", rows)
	}
	first := rows[0]
	if first.ID != "s1" || first.Agent != "claude" || first.CWD != "/tmp/repo" || first.PendingApprovalsCount != 1 || first.RecoveryState != store.SessionRecoveryHealthy || first.RoleRequired != "owner" {
		t.Fatalf("first = %#v", first)
	}
	if first.StartedAt == "" || first.LastActivity == "" || first.RecoveryUpdatedAt == "" {
		t.Fatalf("missing times: %#v", first)
	}
}

func TestWebSessionsIncludeAllActiveSessions(t *testing.T) {
	db, err := store.OpenEphemeral(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	alphaRoot := filepath.Join(t.TempDir(), "alpha")
	betaRoot := filepath.Join(t.TempDir(), "beta")
	if err := db.SessionUpsertStart(ctx, "s1", "main", "claude", filepath.Join(alphaRoot, "pkg"), "claude", "pty", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := db.SessionUpsertStart(ctx, "s2", "other", "codex", betaRoot, "codex", "pty", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	d := New(Options{DB: db})
	rows, err := d.WebSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].ID != "s1" || rows[1].ID != "s2" {
		t.Fatalf("rows = %#v", rows)
	}
}
