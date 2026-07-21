package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectConfigRoundTripContainsOnlyPortableSettings(t *testing.T) {
	root := t.TempDir()
	path, err := ProjectPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveProjectConfig(path, ProjectConfig{Name: "alpha", DefaultAgent: "claude"}); err != nil {
		t.Fatal(err)
	}
	got, err := LoadProjectConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.SchemaVersion != 1 || got.Name != "alpha" || got.DefaultAgent != "claude" {
		t.Fatalf("config = %#v", got)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"path", "owner", "token", "secret", "ssh"} {
		if strings.Contains(strings.ToLower(string(body)), private) {
			t.Fatalf("workspace contains private field %q: %s", private, body)
		}
	}
}

func TestProjectConfigRejectsPrivateAndUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.toml")
	for name, body := range map[string]string{
		"path":  "schema_version = 1\nname = \"alpha\"\npath = \"/private\"\n",
		"owner": "schema_version = 1\nname = \"alpha\"\nowner = \"user\"\n",
		"token": "schema_version = 1\nname = \"alpha\"\ntoken = \"secret\"\n",
		"table": "schema_version = 1\nname = \"alpha\"\n[host]\nname = \"local\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadProjectConfig(path); err == nil {
				t.Fatal("private field accepted")
			}
		})
	}
}

func TestProjectConfigRejectsInvalidVersionNameAndAgent(t *testing.T) {
	for name, cfg := range map[string]ProjectConfig{
		"version": {SchemaVersion: 2, Name: "alpha"},
		"name":    {SchemaVersion: 1, Name: "Alpha"},
		"agent":   {SchemaVersion: 1, Name: "alpha", DefaultAgent: "unknown-agent"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateProjectConfig(cfg); err == nil {
				t.Fatal("invalid project config accepted")
			}
		})
	}
}

func TestProjectPathRequiresDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ProjectPath(path); err == nil {
		t.Fatal("file accepted as workspace root")
	}
}
