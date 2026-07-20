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

func TestProjectConfigAcceptsPrivateTailscaleTransport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.toml")
	if err := os.WriteFile(path, []byte("schema_version = 1\nname = \"alpha\"\n[transports]\ndefault = \"tailscale-private\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadProjectConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Transports.Default != "tailscale-private" {
		t.Fatalf("default transport = %q", cfg.Transports.Default)
	}
}

func TestProjectConfigRejectsRemovedEmailTransport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.toml")
	if err := os.WriteFile(path, []byte("schema_version = 1\nname = \"alpha\"\n[transports]\ndefault = \"email\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadProjectConfig(path)
	if err == nil || !strings.Contains(err.Error(), "no longer supported") {
		t.Fatalf("err = %v", err)
	}
}

func TestProjectConfigRejectsRemovedSMSTransport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.toml")
	if err := os.WriteFile(path, []byte("schema_version = 1\nname = \"alpha\"\n[transports]\ndefault = \"sms\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadProjectConfig(path)
	if err == nil || !strings.Contains(err.Error(), "no longer supported") {
		t.Fatalf("err = %v", err)
	}
}

func TestProjectConfigRejectsRemovedAPNsTransport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.toml")
	if err := os.WriteFile(path, []byte("schema_version = 1\nname = \"alpha\"\n[transports]\ndefault = \"apns\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadProjectConfig(path)
	if err == nil || !strings.Contains(err.Error(), "no longer supported") {
		t.Fatalf("err = %v", err)
	}
}

func TestProjectConfigRejectsRemovedGotifyTransport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.toml")
	if err := os.WriteFile(path, []byte("schema_version = 1\nname = \"alpha\"\n[transports]\ndefault = \"gotify\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadProjectConfig(path)
	if err == nil || !strings.Contains(err.Error(), "no longer supported") {
		t.Fatalf("err = %v", err)
	}
}

func TestProjectConfigIgnoresLegacyPolicyTables(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.toml")
	body := "schema_version = 1\nname = \"alpha\"\n[budget.global]\nmax_tokens_per_day = 1000\n[trust]\npolicy_file = \"trust.toml\"\n[[trust.rule]]\neffect = \"auto_approve\"\nexpires = \"never\"\n[trust.rule.match]\ntool = \"Read\"\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadProjectConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveProjectConfig(path, cfg); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(saved), "budget") || strings.Contains(string(saved), "trust") {
		t.Fatalf("legacy config retained: %s", saved)
	}
}

func TestProjectConfigValidatesAgentsAndHooks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.toml")
	for name, tc := range map[string]struct {
		body string
		want string
	}{
		"unknown default agent": {
			body: "schema_version = 1\nname = \"alpha\"\ndefault_agent = \"unknown-agent\"\n",
			want: "default_agent",
		},
		"unknown hook agent": {
			body: "schema_version = 1\nname = \"alpha\"\n[hooks]\nauto_install = [\"unknown-agent\"]\n",
			want: "hooks.auto_install[0]",
		},
		"unknown hook shell": {
			body: "schema_version = 1\nname = \"alpha\"\n[hooks]\nauto_install = [\"shell:unknown\"]\n",
			want: "hooks.auto_install[0]",
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(tc.body), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := LoadProjectConfig(path)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestProjectConfigAcceptsKnownAgentsAndHooks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.toml")
	body := "schema_version = 1\nname = \"alpha\"\ndefault_agent = \"claude\"\n[hooks]\nauto_install = [\"codex\", \"shell:zsh\"]\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadProjectConfig(path); err != nil {
		t.Fatal(err)
	}
}
