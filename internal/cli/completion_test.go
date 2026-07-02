package cli

import (
	"strings"
	"testing"
)

func TestCompletionCommandEmitsScripts(t *testing.T) {
	for _, tc := range []struct {
		shell string
		want  string
	}{
		{"bash", "complete -o default"},
		{"zsh", "#compdef onibi"},
		{"fish", "complete -c onibi"},
	} {
		t.Run(tc.shell, func(t *testing.T) {
			out, _ := executeRoot(t, "completion", tc.shell, "--color", "never")
			if !strings.Contains(out.String(), tc.want) {
				t.Fatalf("%s completion missing %q", tc.shell, tc.want)
			}
		})
	}
}

func TestCompletionCommandRejectsUnsupportedShell(t *testing.T) {
	cmd := Root()
	var out strings.Builder
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"completion", "powershell", "--color", "never"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "unsupported shell") {
		t.Fatalf("unexpected err: %v", err)
	}
}
