package daemon

import (
	"strings"
	"testing"
)

func TestAgentCommandUsesConfiguredAgentBinary(t *testing.T) {
	t.Setenv("ONIBI_CODEX_BIN", "/opt/bin/codex-dev")
	bin, agent, args, ok := agentCommand("codex", []string{"--model", "gpt"})
	if !ok || bin != "/opt/bin/codex-dev" || agent != "codex" || strings.Join(args, " ") != "--model gpt" {
		t.Fatalf("bin=%q agent=%q args=%#v ok=%v", bin, agent, args, ok)
	}
	if _, ok := agentBinary("unknown"); ok {
		t.Fatal("expected unknown agent to be rejected")
	}
}

func TestAgentCommandShellDefaultsAndOverrides(t *testing.T) {
	t.Setenv("SHELL", "/usr/local/bin/fish")
	bin, agent, args, ok := agentCommand("shell", nil)
	if !ok || bin != "fish" || agent != "shell" || len(args) != 1 || args[0] != "--interactive" {
		t.Fatalf("bin=%q agent=%q args=%#v ok=%v", bin, agent, args, ok)
	}
	bin, agent, args, ok = agentCommand("shell", []string{"bash", "-lc", "echo ok"})
	if !ok || bin != "bash" || agent != "shell" || strings.Join(args, " ") != "-i -lc echo ok" {
		t.Fatalf("bin=%q agent=%q args=%#v ok=%v", bin, agent, args, ok)
	}
	if _, _, ok := shellCommand("not-a-shell", nil); ok {
		t.Fatal("expected shell rejection")
	}
}

func TestPingTextIncludesLiveSessions(t *testing.T) {
	d := New(Options{})
	if err := d.Registry.Add(NewSession("s1", "shell", "shell", nil, 0)); err != nil {
		t.Fatal(err)
	}
	got := d.pingText(t.Context(), 0)
	if !strings.Contains(got, "pong") || !strings.Contains(got, "sessions=1") {
		t.Fatalf("ping = %q", got)
	}
}
