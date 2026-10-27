package daemon

import (
	"testing"
	"time"
)

func TestCodexStartsAsATerminalProgram(t *testing.T) {
	t.Setenv("ONIBI_CODEX_BIN", "")
	d, _ := testDaemon(t)
	bin, name, args, ok := d.agentCommand("Codex", []string{"--model", "x"})
	if !ok || bin != "codex" || name != "codex" || len(args) != 2 {
		t.Fatalf("bin=%q name=%q args=%v ok=%t", bin, name, args, ok)
	}
}

func TestRestoreEndsAppServerSessionsInsteadOfResuming(t *testing.T) {
	d, _ := testDaemon(t)
	if err := d.DB.SessionUpsertStart(t.Context(), "aaaa1111", "old", "codex", t.TempDir(), "codex app-server", "codex", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	d.restoreSessions(t.Context())
	if _, err := d.Registry.Get("aaaa1111"); err == nil {
		t.Fatal("App Server session was restored")
	}
	active, err := d.DB.SessionsActive(t.Context())
	if err != nil || len(active) != 0 {
		t.Fatalf("active=%#v err=%v", active, err)
	}
}
