package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestUninstallDryRunShowsAllHookInspection(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	out, _ := executeRoot(t, "uninstall", "--dry-run", "--color", "never")
	got := out.String()
	for _, want := range []string{"inspect hooks", "onibi hooks --show --all", "remove hooks", "all supported agents and shells"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}

func TestUninstallDryRunShowsTargetedHookInspection(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	out, _ := executeRoot(t, "uninstall", "--dry-run", "--agent", "codex", "--shell", "zsh", "--color", "never")
	got := out.String()
	for _, want := range []string{"onibi hooks --show --agent codex", "onibi hooks --show --shell zsh", "agent:codex", "shell:zsh"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}

func TestUninstallDryRunJSON(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	out, _ := executeRoot(t, "uninstall", "--dry-run", "--json", "--state", "--color", "never")
	var plan []uninstallPlanItem
	if err := json.Unmarshal(out.Bytes(), &plan); err != nil {
		t.Fatalf("json: %v\n%s", err, out.String())
	}
	if len(plan) == 0 {
		t.Fatal("empty uninstall plan")
	}
	var foundState, foundSecrets bool
	for _, item := range plan {
		if item.Action == "remove state" && item.Risk == "high" {
			foundState = true
		}
		if item.Action == "remove secrets" && item.Risk == "high" {
			foundSecrets = true
		}
	}
	if !foundState || !foundSecrets {
		t.Fatalf("plan missing state/secrets: %+v", plan)
	}
}

func TestUninstallJSONRequiresDryRun(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	_, _, err := executeRootAllowError(t, "uninstall", "--json", "--yes", "--color", "never")
	if err == nil || !strings.Contains(err.Error(), "--json requires --dry-run") {
		t.Fatalf("err = %v", err)
	}
}

func TestUninstallNonInteractiveRequiresYes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	_, _, err := executeRootAllowError(t, "uninstall", "--color", "never")
	if err == nil || !strings.Contains(err.Error(), "uninstall requires --yes") {
		t.Fatalf("err = %v", err)
	}
}

func TestUninstallAllHooksIsNoopOnFreshHome(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if _, _, err := executeRootAllowError(t, "uninstall", "--yes", "--hooks", "--all-hooks", "--color", "never"); err != nil {
		t.Fatalf("uninstall fresh hooks: %v", err)
	}
}

func TestUninstallInteractiveCancel(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	oldInput := inputIsTerminal
	oldOutput := outputIsTerminal
	inputIsTerminal = func(any) bool { return true }
	outputIsTerminal = func(any) bool { return true }
	t.Cleanup(func() {
		inputIsTerminal = oldInput
		outputIsTerminal = oldOutput
	})
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd := Root()
	cmd.SetIn(strings.NewReader("n\n"))
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs([]string{"uninstall", "--color", "never"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute uninstall cancel: %v\nstdout:\n%s\nstderr:\n%s", err, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "Continue? [y/N]") || !strings.Contains(out.String(), "Cancelled.") {
		t.Fatalf("cancel output:\n%s", out.String())
	}
}

func TestUninstallStateRequiresTypedConfirmation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	oldInput := inputIsTerminal
	oldOutput := outputIsTerminal
	inputIsTerminal = func(any) bool { return true }
	outputIsTerminal = func(any) bool { return true }
	t.Cleanup(func() {
		inputIsTerminal = oldInput
		outputIsTerminal = oldOutput
	})
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd := Root()
	cmd.SetIn(strings.NewReader("y\n"))
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs([]string{"uninstall", "--state", "--color", "never"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute uninstall state cancel: %v\nstdout:\n%s\nstderr:\n%s", err, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), `Type "delete onibi state"`) || !strings.Contains(out.String(), "Cancelled.") {
		t.Fatalf("typed cancel output:\n%s", out.String())
	}
}
