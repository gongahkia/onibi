package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testPaths(t *testing.T) Paths {
	t.Helper()
	dir := t.TempDir()
	return Paths{StateDir: dir, Config: filepath.Join(dir, "config.yaml")}
}

func TestLoadMissingUsesDefaults(t *testing.T) {
	paths := testPaths(t)
	cfg, meta, err := Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Exists {
		t.Fatal("missing config reported as loaded")
	}
	if cfg.Daemon.ApprovalTimeout.Std() != 5*time.Minute {
		t.Fatalf("approval timeout = %s", cfg.Daemon.ApprovalTimeout)
	}
}

func TestLoadPartialTracksExplicitKeys(t *testing.T) {
	paths := testPaths(t)
	body := []byte("daemon:\n  turn_idle_threshold: 7s\nshell:\n  min_duration: 12s\n  default: zsh\n  login: false\n")
	if err := os.WriteFile(paths.Config, body, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, meta, err := Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if !meta.Exists {
		t.Fatal("config file not loaded")
	}
	if !meta.Explicit["daemon.turn_idle_threshold"] || !meta.Explicit["shell.min_duration"] || !meta.Explicit["shell.default"] || !meta.Explicit["shell.login"] {
		t.Fatalf("explicit map missing keys: %#v", meta.Explicit)
	}
	if meta.Explicit["daemon.approval_timeout"] {
		t.Fatal("defaulted approval timeout marked explicit")
	}
	if cfg.Daemon.TurnIdleThreshold.Std() != 7*time.Second {
		t.Fatalf("turn idle threshold = %s", cfg.Daemon.TurnIdleThreshold)
	}
	if cfg.Shell.MinDuration.Std() != 12*time.Second {
		t.Fatalf("shell min duration = %s", cfg.Shell.MinDuration)
	}
	if cfg.Shell.Default != "zsh" || cfg.Shell.Login {
		t.Fatalf("shell config = %#v", cfg.Shell)
	}
}

func TestLoadRejectsUnknownKeys(t *testing.T) {
	paths := testPaths(t)
	if err := os.WriteFile(paths.Config, []byte("daemon:\n  nope: 1s\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := Load(paths)
	if err == nil || !strings.Contains(err.Error(), "field nope not found") {
		t.Fatalf("unexpected err: %v", err)
	}
}

func TestSetValidates(t *testing.T) {
	cfg := Default()
	if err := Set(&cfg, "daemon.pty_buffer_bytes", "2048"); err == nil {
		t.Fatal("expected small buffer error")
	}
	if err := Set(&cfg, "daemon.pty_buffer_bytes", "131072"); err != nil {
		t.Fatal(err)
	}
	got, err := Get(cfg, "daemon.pty_buffer_bytes")
	if err != nil {
		t.Fatal(err)
	}
	if got != "131072" {
		t.Fatalf("got %s", got)
	}
	if err := Set(&cfg, "shell.default", "fish"); err != nil {
		t.Fatal(err)
	}
	if err := Set(&cfg, "shell.login", "false"); err != nil {
		t.Fatal(err)
	}
	if got, _ := Get(cfg, "shell.default"); got != "fish" {
		t.Fatalf("shell.default = %s", got)
	}
	if got, _ := Get(cfg, "shell.login"); got != "false" {
		t.Fatalf("shell.login = %s", got)
	}
}

func TestApprovalTimeoutHardMax(t *testing.T) {
	cfg := Default()
	if err := Set(&cfg, "daemon.approval_timeout", "6m"); err == nil {
		t.Fatal("expected approval timeout max error")
	}
	if err := Set(&cfg, "daemon.approval_timeout", "5m"); err != nil {
		t.Fatal(err)
	}
}

func TestShellDefaultRejectsUnsupportedPath(t *testing.T) {
	cfg := Default()
	if err := Set(&cfg, "shell.default", "/usr/bin/pwsh"); err == nil {
		t.Fatal("expected unsupported shell path error")
	}
}
