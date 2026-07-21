package config

import (
	"os"
	"strings"
	"testing"
)

func TestMigrateArchivesRemovedFeaturesAndGatesProvider(t *testing.T) {
	paths := testPaths(t)
	original := "voice:\n  enabled: true\nworkspace:\n  name: legacy\nteam:\n  owner: legacy\ntransport:\n  mode: telegram\n"
	if err := os.WriteFile(paths.Config, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := Migrate(paths)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed() || len(result.Changes) != 4 {
		t.Fatalf("migration result = %#v", result)
	}
	backup, err := os.ReadFile(result.BackupPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != original {
		t.Fatalf("backup = %q", backup)
	}
	cfg, _, err := Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Transport.Mode != "telegram" || !cfg.Experimental.Providers {
		t.Fatalf("config = %#v", cfg)
	}
	migrated, err := os.ReadFile(paths.Config)
	if err != nil {
		t.Fatal(err)
	}
	for _, removed := range []string{"voice:", "workspace:", "team:"} {
		if strings.Contains(string(migrated), removed) {
			t.Fatalf("migrated config retained %s: %s", removed, migrated)
		}
	}
}

func TestMigrateLeavesV1ConfigUntouched(t *testing.T) {
	paths := testPaths(t)
	if err := Save(paths.Config, Default()); err != nil {
		t.Fatal(err)
	}
	result, err := Migrate(paths)
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed() || result.BackupPath != "" {
		t.Fatalf("migration result = %#v", result)
	}
}

func TestMigratePreservesExperimentalWorkspaceProfile(t *testing.T) {
	paths := testPaths(t)
	body := "workspace:\n  name: legacy\nexperimental:\n  workspace: true\n"
	if err := os.WriteFile(paths.Config, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Migrate(paths); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Experimental.Workspace {
		t.Fatal("experimental workspace profile was removed")
	}
}
