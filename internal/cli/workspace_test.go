package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gongahkia/onibi/internal/config"
)

func TestWorkspaceCommandsRequireExplicitProfile(t *testing.T) {
	withDefaultState(t)
	_, _, err := executeRootAllowError(t, "experimental", "workspace", "validate", "--color", "never")
	if err == nil || !strings.Contains(err.Error(), "experimental.workspace=true") {
		t.Fatalf("err = %v", err)
	}
}

func TestWorkspaceInitAndValidateRemainPortable(t *testing.T) {
	paths := withDefaultState(t)
	cfg := config.Default()
	cfg.Experimental.Workspace = true
	if err := config.Save(paths.Config, cfg); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	out, _ := executeRoot(t, "experimental", "workspace", "init", root, "--name", "alpha", "--agent", "claude", "--color", "never")
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
	out, _ = executeRoot(t, "experimental", "workspace", "validate", path, "--color", "never")
	if !strings.Contains(out.String(), "Valid portable workspace alpha") {
		t.Fatalf("out = %q", out.String())
	}
}
