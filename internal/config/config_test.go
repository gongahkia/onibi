package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingUsesDefaults(t *testing.T) {
	config, err := Load(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !config.Sources["sportsg-facilities"].Enabled {
		t.Fatal("expected SportSG source enabled by default")
	}
}

func TestResolveSecret(t *testing.T) {
	t.Setenv("COURTSG_TEST_SECRET", "present")
	config := Config{}
	value, err := config.ResolveSecret("env:COURTSG_TEST_SECRET")
	if err != nil || value != "present" {
		t.Fatalf("ResolveSecret() = %q, %v", value, err)
	}
	if _, err := config.ResolveSecret("env:COURTSG_MISSING_SECRET"); err == nil {
		t.Fatal("expected missing variable error")
	}
}

func TestWriteExampleRefusesOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := WriteExample(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	if err := WriteExample(path); err == nil {
		t.Fatal("expected overwrite refusal")
	}
}
