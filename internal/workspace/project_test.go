package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindProjectFileWalksParentsAndLoadsConfig(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".onibi"), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ProjectRelPath)
	if err := os.WriteFile(path, []byte(`
schema_version = 1
name = "alpha"
default_agent = "claude"

[transports]
default = "tailscale"
web = ["lan", "tailscale"]
`), 0o600); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "packages", "app")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	found, ok, err := FindProjectFile(nested)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if found.Root != root || found.Path != path {
		t.Fatalf("found = %#v", found)
	}
	cfg, err := LoadProjectConfig(found.Path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Name != "alpha" || cfg.Transports.Default != "tailscale" {
		t.Fatalf("cfg = %#v", cfg)
	}
}

func TestLoadProjectConfigRejectsUnknownTopLevel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.toml")
	if err := os.WriteFile(path, []byte(`
schema_version = 1
name = "alpha"
unknown = true
`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadProjectConfig(path)
	if err == nil || !strings.Contains(err.Error(), "strict mode") {
		t.Fatalf("err = %v", err)
	}
}
