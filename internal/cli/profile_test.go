package cli

import (
	"strings"
	"testing"
)

func TestProfileCommandsAddListUseRemove(t *testing.T) {
	withDefaultState(t)
	cwd := t.TempDir()
	out, _ := executeRoot(t, "profile", "add", "work", "--transport", "tailscale", "--agent", "sh", "--cwd", cwd, "--color", "never")
	if !strings.Contains(out.String(), "Profile work added") {
		t.Fatalf("add output = %q", out.String())
	}
	out, _ = executeRoot(t, "profile", "list", "--color", "never")
	for _, want := range []string{"work", "transport=tailscale", "agent=sh", "cwd=" + cwd} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("list output missing %q: %q", want, out.String())
		}
	}
	out, _ = executeRoot(t, "profile", "use", "work", "--color", "never")
	if !strings.Contains(out.String(), "Profile default: work") {
		t.Fatalf("use output = %q", out.String())
	}
	out, _ = executeRoot(t, "profile", "list", "--color", "never")
	if !strings.Contains(out.String(), "* work") {
		t.Fatalf("list output missing last-used marker: %q", out.String())
	}
	out, _ = executeRoot(t, "profile", "remove", "work", "--color", "never")
	if !strings.Contains(out.String(), "Profile work removed") {
		t.Fatalf("remove output = %q", out.String())
	}
	out, _ = executeRoot(t, "profile", "list", "--color", "never")
	if !strings.Contains(out.String(), "No profiles.") {
		t.Fatalf("list output = %q", out.String())
	}
}
