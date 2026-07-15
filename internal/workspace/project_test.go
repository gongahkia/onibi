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

func TestProjectConfigRejectsTrustPolicyPathOutsideWorkspace(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ProjectRelPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, policyFile := range map[string]string{
		"absolute": `"/tmp/trust.toml"`,
		"escape":   `"../../trust.toml"`,
	} {
		t.Run(name, func(t *testing.T) {
			body := "schema_version = 1\nname = \"alpha\"\n[trust]\npolicy_file = " + policyFile + "\n"
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := LoadProjectConfig(path)
			if err == nil || !strings.Contains(err.Error(), "trust.policy_file") {
				t.Fatalf("err = %v", err)
			}
		})
	}
	if err := SaveProjectConfig(path, ProjectConfig{
		SchemaVersion: 1,
		Name:          "alpha",
		Trust:         ProjectTrust{PolicyFile: "../../trust.toml"},
	}); err == nil || !strings.Contains(err.Error(), "trust.policy_file") {
		t.Fatalf("save err = %v", err)
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

func TestProjectConfigValidatesInlineTrustAndBudget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.toml")
	for name, tc := range map[string]struct {
		body string
		want string
	}{
		"invalid trust effect": {
			body: "schema_version = 1\nname = \"alpha\"\n[[trust.rule]]\neffect = \"allow\"\nexpires = \"never\"\n[trust.rule.match]\ntool = \"Read\"\n",
			want: "invalid effect",
		},
		"missing trust expiry": {
			body: "schema_version = 1\nname = \"alpha\"\n[[trust.rule]]\neffect = \"auto_approve\"\n[trust.rule.match]\ntool = \"Read\"\n",
			want: "expires required",
		},
		"empty trust match": {
			body: "schema_version = 1\nname = \"alpha\"\n[[trust.rule]]\neffect = \"auto_approve\"\nexpires = \"never\"\n",
			want: "match required",
		},
		"unknown trust match": {
			body: "schema_version = 1\nname = \"alpha\"\n[[trust.rule]]\neffect = \"auto_approve\"\nexpires = \"never\"\n[trust.rule.match]\nuser = \"alice\"\n",
			want: "strict mode",
		},
		"negative budget": {
			body: "schema_version = 1\nname = \"alpha\"\n[budget.global]\nmax_tokens_per_day = -1\n",
			want: "must be >= 0",
		},
		"invalid overrun": {
			body: "schema_version = 1\nname = \"alpha\"\n[budget.session]\non_overrun = \"pause\"\n",
			want: "must be one of interrupt, kill, warn",
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

func TestProjectConfigAcceptsInlineTrustAndBudget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.toml")
	body := "schema_version = 1\nname = \"alpha\"\n[[trust.rule]]\neffect = \"auto_approve\"\nexpires = \"5m\"\n[trust.rule.match]\ntool = \"Read\"\n[budget.global]\nmax_tokens_per_day = 1000\n[budget.session]\nmax_tokens = 100\non_overrun = \"kill\"\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadProjectConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Budget.Session.OnOverrun; got != "kill" {
		t.Fatalf("on_overrun = %q", got)
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
