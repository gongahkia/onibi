package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceAddUseListRemove(t *testing.T) {
	workspaceTestHome(t)
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	out := executeWorkspace(t, "add", "alpha", repo, "--default-transport", "tailscale", "--use")
	if !strings.Contains(out.String(), "Workspace alpha added") {
		t.Fatalf("out = %q", out.String())
	}
	out = executeWorkspace(t, "list")
	if !strings.Contains(out.String(), "* alpha") || !strings.Contains(out.String(), repo) || !strings.Contains(out.String(), "tailscale") {
		t.Fatalf("list = %q", out.String())
	}
	out = executeWorkspace(t, "remove", "alpha")
	if !strings.Contains(out.String(), "Workspace removed: alpha") {
		t.Fatalf("remove = %q", out.String())
	}
	out = executeWorkspace(t, "list")
	if !strings.Contains(out.String(), "No workspaces.") {
		t.Fatalf("list after remove = %q", out.String())
	}
}

func TestWorkspaceExportWritesPortableBundle(t *testing.T) {
	workspaceTestHome(t)
	repo := filepath.Join(t.TempDir(), "repo")
	onibiDir := filepath.Join(repo, ".onibi")
	if err := os.MkdirAll(onibiDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(onibiDir, "trust.toml"), []byte("[[rule]]\neffect = \"auto_approve\"\nexpires = \"never\"\n[rule.match]\ntool = \"Read\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	executeWorkspace(t, "add", "alpha", repo, "--ssh-key", "secret-key-ref", "--default-transport", "tailscale")
	bundle := filepath.Join(t.TempDir(), "bundle")
	out := executeWorkspace(t, "export", "alpha", bundle)
	if !strings.Contains(out.String(), "Workspace alpha exported") {
		t.Fatalf("out = %q", out.String())
	}
	workspaceToml, err := os.ReadFile(filepath.Join(bundle, ".onibi", "workspace.toml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(workspaceToml)
	for _, forbidden := range []string{repo, "secret-key-ref"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("workspace.toml leaked %q:\n%s", forbidden, text)
		}
	}
	for _, want := range []string{`name = 'alpha'`, `default = 'tailscale'`} {
		if !strings.Contains(text, want) {
			t.Fatalf("workspace.toml missing %q:\n%s", want, text)
		}
	}
	if _, err := os.Stat(filepath.Join(bundle, ".onibi", "trust.toml")); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceImportBundleUse(t *testing.T) {
	workspaceTestHome(t)
	repo := filepath.Join(t.TempDir(), "repo")
	onibiDir := filepath.Join(repo, ".onibi")
	if err := os.MkdirAll(onibiDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(onibiDir, "workspace.toml"), []byte("schema_version = 1\nname = \"beta\"\n[transports]\ndefault = \"tailscale\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := executeWorkspace(t, "import", repo, "--use")
	if !strings.Contains(out.String(), "Workspace beta imported") {
		t.Fatalf("out = %q", out.String())
	}
	out = executeWorkspace(t, "list")
	if !strings.Contains(out.String(), "* beta") || !strings.Contains(out.String(), repo) || !strings.Contains(out.String(), "tailscale") {
		t.Fatalf("list = %q", out.String())
	}
}

func TestWorkspaceUsePathImportsAndSetsDefault(t *testing.T) {
	workspaceTestHome(t)
	repo := filepath.Join(t.TempDir(), "repo")
	onibiDir := filepath.Join(repo, ".onibi")
	if err := os.MkdirAll(onibiDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(onibiDir, "workspace.toml"), []byte("schema_version = 1\nname = \"gamma\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := executeWorkspace(t, "use", onibiDir)
	if !strings.Contains(out.String(), "Workspace default: gamma") {
		t.Fatalf("out = %q", out.String())
	}
	out = executeWorkspace(t, "list")
	if !strings.Contains(out.String(), "* gamma") || !strings.Contains(out.String(), repo) {
		t.Fatalf("list = %q", out.String())
	}
}

func workspaceTestHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ONIBI_STORE_KEY_BACKEND", "dotenv")
}

func executeWorkspace(t *testing.T, args ...string) *bytes.Buffer {
	t.Helper()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd := workspaceCmd()
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute workspace %v: %v\nstdout:\n%s\nstderr:\n%s", args, err, out.String(), errOut.String())
	}
	return out
}
