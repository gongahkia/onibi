package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceIsATopLevelCommand(t *testing.T) {
	withDefaultState(t)
	out, _ := executeRoot(t, "workspace", "--help", "--color", "never")
	if !strings.Contains(out.String(), "portable project workspace") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestWorkspaceInitAndValidateRemainPortable(t *testing.T) {
	withDefaultState(t)
	root := t.TempDir()
	out, _ := executeRoot(t, "workspace", "init", root, "--name", "alpha", "--agent", "claude", "--color", "never")
	if !strings.Contains(out.String(), "Created portable workspace") {
		t.Fatalf("out = %q", out.String())
	}
	path := filepath.Join(root, ".onibi", "workspace.toml")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"path", "owner", "token", "secret", "ssh"} {
		if strings.Contains(strings.ToLower(string(body)), private) {
			t.Fatalf("private field %q in %s", private, body)
		}
	}
	out, _ = executeRoot(t, "workspace", "validate", path, "--color", "never")
	if !strings.Contains(out.String(), "Valid portable workspace alpha") {
		t.Fatalf("out = %q", out.String())
	}
}
