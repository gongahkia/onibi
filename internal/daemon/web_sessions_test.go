package daemon

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/budget"
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
	d.mu.Lock()
	d.budgetCosts["s1"] = budget.CostEvent{
		SessionID:         "s1",
		Model:             "claude-sonnet-4-6",
		TotalInputTokens:  10,
		TotalOutputTokens: 5,
		TS:                time.Now().UTC(),
	}
	d.mu.Unlock()
	rows, err := d.WebSessions(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %#v", rows)
	}
	first := rows[0]
	if first.ID != "s1" || first.Agent != "claude" || first.CWD != "/tmp/repo" || first.PendingApprovalsCount != 1 || first.TokensUsed != 15 || first.RoleRequired != "owner" {
		t.Fatalf("first = %#v", first)
	}
	if first.StartedAt == "" || first.LastActivity == "" {
		t.Fatalf("missing times: %#v", first)
	}
}
