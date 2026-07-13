package daemon

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/budget"
	"github.com/gongahkia/onibi/internal/store"
	"github.com/gongahkia/onibi/internal/web"
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
	rows, err := d.WebSessions(t.Context(), web.SessionListOptions{})
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

func TestWebSessionsIncludesTailnetPeers(t *testing.T) {
	status := `{"BackendState":"Running","Self":{"DNSName":"self.tail.ts.net."},"Peer":{"n1":{"DNSName":"peer.tail.ts.net.","HostName":"work-mac"},"n2":{"DNSName":"plain.tail.ts.net.","HostName":"no-daemon"}}}`
	var probed []string
	d := New(Options{
		TailnetStatus: func(context.Context) ([]byte, error) {
			return []byte(status), nil
		},
		TailnetHealth: func(_ context.Context, url string) (bool, error) {
			probed = append(probed, url)
			return url == "https://peer.tail.ts.net/", nil
		},
	})
	rows, err := d.WebSessions(t.Context(), web.SessionListOptions{IncludeRemote: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %#v", rows)
	}
	got := rows[0]
	if !got.Remote || got.PeerName != "work-mac" || got.RemoteURL != "https://peer.tail.ts.net/" || got.RoleRequired != "remote" {
		t.Fatalf("remote row = %#v", got)
	}
	if !slices.Contains(probed, "https://peer.tail.ts.net/") || !slices.Contains(probed, "https://plain.tail.ts.net/") {
		t.Fatalf("probed = %#v", probed)
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
	rows, err := d.WebSessions(ctx, web.SessionListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].ID != "s1" || rows[1].ID != "s2" {
		t.Fatalf("rows = %#v", rows)
	}
}
