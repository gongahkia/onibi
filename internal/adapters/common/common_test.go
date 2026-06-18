package common

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gongahkia/onibi/internal/store"
)

func TestCommandRequiresManagedSession(t *testing.T) {
	cmd := Command("/tmp/onibi-notify", "codex", "codex", "approval_request", true, "provider")
	for _, want := range []string{"ONIBI_SESSION_ID", "exec", "--agent codex", "--wait"} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("command missing %q: %s", want, cmd)
		}
	}
}

func TestUnguardedCommandHasNoSessionGuard(t *testing.T) {
	cmd := UnguardedCommand("/tmp/onibi-notify", "shell", "shell", "cmd_done", false, "")
	if strings.Contains(cmd, "ONIBI_SESSION_ID") {
		t.Fatalf("unguarded command = %s", cmd)
	}
	if !strings.Contains(cmd, "--agent shell") {
		t.Fatalf("command = %s", cmd)
	}
}

func TestVersionedCommandRoundTrip(t *testing.T) {
	cmd := VersionedCommand("/tmp/onibi-notify", "gemini", "gemini", "agent_message", false, "")
	if got := CommandVersion(cmd); got != IntegrationVersion {
		t.Fatalf("CommandVersion = %q", got)
	}
	if !strings.Contains(cmd, VersionEnv+"=\""+IntegrationVersion+"\"") {
		t.Fatalf("command missing version env: %s", cmd)
	}
}

func TestBackupOriginalDoesNotOverwriteSameSourceHash(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "state", "onibi.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	src := filepath.Join(dir, "hooks.json")
	if err := os.WriteFile(src, []byte(`{"hooks":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := BackupOriginal(context.Background(), db, "codex", src)
	if err != nil {
		t.Fatal(err)
	}
	if first == "" {
		t.Fatal("backup path missing")
	}
	if err := os.WriteFile(first, []byte("sentinel"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := BackupOriginal(context.Background(), db, "codex", src)
	if err != nil {
		t.Fatal(err)
	}
	if second != first {
		t.Fatalf("backup path changed: %q != %q", second, first)
	}
	body, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "sentinel" {
		t.Fatalf("backup overwritten: %q", body)
	}
}
