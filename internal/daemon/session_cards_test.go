package daemon

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestSessionCardsShowAliasCountsAndDefault(t *testing.T) {
	d := newApprovalDaemon(t)
	ctx := context.Background()
	dir := t.TempDir()
	if err := d.DB.KVSetString(ctx, projectAliasKey("repo"), dir); err != nil {
		t.Fatal(err)
	}
	s := NewSession("abc123456", "claude-long-session-name-that-wraps", "claude", nil, 1024)
	s.CWD = dir
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	d.setDefaultTarget(ctx, 100, s.ID)
	if _, err := d.DB.PromptEnqueue(ctx, s.ID, 100, "queued prompt"); err != nil {
		t.Fatal(err)
	}
	id, _, err := d.Queue.Request(ctx, s.ID, "claude", "Bash", `{"command":"ls"}`)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Queue.DropWaiter(id)
	got := d.sessionsText(ctx, 100)
	for _, want := range []string{"* claude-long-session-name-that...", "project=repo", "queue=1", "approvals=1"} {
		if !strings.Contains(got, want) {
			t.Fatalf("sessions missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, dir) {
		t.Fatalf("raw path leaked:\n%s", got)
	}
}

func TestTouchSessionPersistsLastActivity(t *testing.T) {
	d := newApprovalDaemon(t)
	ctx := context.Background()
	started := time.Now().Add(-10 * time.Second).Truncate(time.Second)
	s := newSessionAt("abc123", "shell", "shell", nil, 1024, started, started)
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	if err := d.DB.SessionUpsertStart(ctx, s.ID, s.Name, s.Agent, "", s.Cmd, "tmux", "", started); err != nil {
		t.Fatal(err)
	}
	d.touchSession(ctx, s)
	rows, err := d.DB.SessionsActive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("sessions = %#v", rows)
	}
	if !rows[0].LastActivity.After(started) {
		t.Fatalf("last activity = %s, started = %s", rows[0].LastActivity, started)
	}
}
