package cli

import (
	"strings"
	"testing"
)

func TestUninstallDryRunShowsAllHookInspection(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	out, _ := executeRoot(t, "uninstall", "--dry-run", "--color", "never")
	got := out.String()
	for _, want := range []string{"inspect hooks", "onibi hooks show --all", "remove hooks", "all supported agents and shells"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}

func TestUninstallDryRunShowsTargetedHookInspection(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	out, _ := executeRoot(t, "uninstall", "--dry-run", "--agent", "codex", "--shell", "zsh", "--color", "never")
	got := out.String()
	for _, want := range []string{"onibi hooks show --agent codex", "onibi hooks show --shell zsh", "agent:codex", "shell:zsh"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}
