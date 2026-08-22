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
	if config.Sources["onepa"].Enabled {
		t.Fatal("partner availability sources must require explicit enablement")
	}
	for _, sourceID := range []string{"sba-stadium", "singapore-badminton-hall", "smash-arena", "wyse-active"} {
		if !config.Sources[sourceID].Enabled {
			t.Fatalf("%s must be enabled by default", sourceID)
		}
	}
	if config.Sources["smash-arena"].AvailabilityMaxDays != 1 {
		t.Fatalf("Smash Arena default horizon = %d, want 1", config.Sources["smash-arena"].AvailabilityMaxDays)
	}
	if config.Daemon.RefreshMinutes != 30 {
		t.Fatalf("default refresh = %d, want 30 minutes", config.Daemon.RefreshMinutes)
	}
}

func TestLoadExampleStyleConfigKeepsBuiltInReaders(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	contents := []byte("version = 1\n[sources.onepa]\nenabled = false\n")
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, sourceID := range []string{"sba-stadium", "singapore-badminton-hall", "smash-arena", "wyse-active"} {
		if !config.Sources[sourceID].Enabled {
			t.Fatalf("%s was lost while loading partial config", sourceID)
		}
	}
}

func TestResolveSecret(t *testing.T) {
	t.Setenv("KAYPOH_TEST_SECRET", "present")
	config := Config{}
	value, err := config.ResolveSecret("env:KAYPOH_TEST_SECRET")
	if err != nil || value != "present" {
		t.Fatalf("ResolveSecret() = %q, %v", value, err)
	}
	if _, err := config.ResolveSecret("env:KAYPOH_MISSING_SECRET"); err == nil {
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

func TestValidateRejectsEnabledPartnerWithoutReadAccess(t *testing.T) {
	config, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	config.Sources["onepa"] = Source{Enabled: true}
	if err := config.Validate(); err == nil {
		t.Fatal("enabled partner without reader configuration was accepted")
	}
}
