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
	if cfg.Daemon.MaxSubscribers != 32 {
		t.Fatalf("max subscribers = %d", cfg.Daemon.MaxSubscribers)
	}
}

func TestLoadPartialTracksExplicitKeys(t *testing.T) {
	paths := testPaths(t)
	body := []byte("daemon:\n  turn_idle_threshold: 7s\n  max_subscribers: 7\nshell:\n  min_duration: 12s\n  default: zsh\n  login: false\nweb:\n  listen_addr: ':9443'\ntransport:\n  mode: auto\nterminal:\n  default: ghostty\nupdate:\n  auto: true\n  channel: stable\n")
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
	if !meta.Explicit["daemon.turn_idle_threshold"] || !meta.Explicit["daemon.max_subscribers"] || !meta.Explicit["shell.min_duration"] || !meta.Explicit["shell.default"] || !meta.Explicit["shell.login"] || !meta.Explicit["web.listen_addr"] || !meta.Explicit["transport.mode"] || !meta.Explicit["terminal.default"] {
		t.Fatalf("explicit map missing keys: %#v", meta.Explicit)
	}
	if meta.Explicit["daemon.approval_timeout"] {
		t.Fatal("defaulted approval timeout marked explicit")
	}
	if cfg.Daemon.TurnIdleThreshold.Std() != 7*time.Second {
		t.Fatalf("turn idle threshold = %s", cfg.Daemon.TurnIdleThreshold)
	}
	if cfg.Daemon.MaxSubscribers != 7 {
		t.Fatalf("max subscribers = %d", cfg.Daemon.MaxSubscribers)
	}
	if cfg.Shell.MinDuration.Std() != 12*time.Second {
		t.Fatalf("shell min duration = %s", cfg.Shell.MinDuration)
	}
	if cfg.Shell.Default != "zsh" || cfg.Shell.Login {
		t.Fatalf("shell config = %#v", cfg.Shell)
	}
	if cfg.Terminal.Default != "ghostty" {
		t.Fatalf("terminal.default = %s", cfg.Terminal.Default)
	}
	if meta.Explicit["update.auto"] || meta.Explicit["update.channel"] {
		t.Fatalf("legacy update config retained: %#v", meta.Explicit)
	}
	if cfg.Web.ListenAddr != ":9443" || cfg.Transport.Mode != "auto" {
		t.Fatalf("web/transport config = %#v %#v", cfg.Web, cfg.Transport)
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
	if err := Set(&cfg, "daemon.max_subscribers", "0"); err == nil {
		t.Fatal("expected max subscribers min error")
	}
	if err := Set(&cfg, "daemon.max_subscribers", "64"); err != nil {
		t.Fatal(err)
	}
	if got, _ := Get(cfg, "daemon.max_subscribers"); got != "64" {
		t.Fatalf("daemon.max_subscribers = %s", got)
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
	if err := Set(&cfg, "terminal.default", "ghostty"); err != nil {
		t.Fatal(err)
	}
	if got, _ := Get(cfg, "terminal.default"); got != "ghostty" {
		t.Fatalf("terminal.default = %s", got)
	}
	if err := Set(&cfg, "provider.output.max_chunks", "3"); err != nil {
		t.Fatal(err)
	}
	if err := Set(&cfg, "provider.output.max_bytes", "4096"); err != nil {
		t.Fatal(err)
	}
	if err := Set(&cfg, "provider.output.redaction", "strict"); err != nil {
		t.Fatal(err)
	}
	if got, _ := Get(cfg, "provider.output.redaction"); got != "strict" {
		t.Fatalf("provider.output.redaction = %s", got)
	}
	if err := Set(&cfg, "provider.output.slack.max_bytes", "2048"); err != nil {
		t.Fatal(err)
	}
	if err := Set(&cfg, "provider.output.discord.redaction", "off"); err != nil {
		t.Fatal(err)
	}
	if err := Set(&cfg, "provider.output.zulip.max_chunks", "4"); err != nil {
		t.Fatal(err)
	}
	if err := Set(&cfg, "provider.output.irc.max_bytes", "2048"); err != nil {
		t.Fatal(err)
	}
	if got, _ := Get(cfg, "provider.output.slack.max_bytes"); got != "2048" {
		t.Fatalf("provider.output.slack.max_bytes = %s", got)
	}
	if got, _ := Get(cfg, "provider.output.discord.redaction"); got != "off" {
		t.Fatalf("provider.output.discord.redaction = %s", got)
	}
	if got, _ := Get(cfg, "provider.output.zulip.max_chunks"); got != "4" {
		t.Fatalf("provider.output.zulip.max_chunks = %s", got)
	}
	if got, _ := Get(cfg, "provider.output.irc.max_bytes"); got != "2048" {
		t.Fatalf("provider.output.irc.max_bytes = %s", got)
	}
	if err := Set(&cfg, "provider.output.discord.redaction", "inherit"); err != nil {
		t.Fatal(err)
	}
	if got, _ := Get(cfg, "provider.output.discord.redaction"); got != "inherit" {
		t.Fatalf("provider.output.discord.redaction = %s", got)
	}
}

func TestProviderOutputValidation(t *testing.T) {
	cfg := Default()
	if err := Set(&cfg, "provider.output.max_chunks", "0"); err == nil {
		t.Fatal("expected max_chunks validation error")
	}
	cfg = Default()
	if err := Set(&cfg, "provider.output.max_bytes", "128"); err == nil {
		t.Fatal("expected max_bytes validation error")
	}
	cfg = Default()
	if err := Set(&cfg, "provider.output.redaction", "none"); err == nil {
		t.Fatal("expected redaction validation error")
	}
	cfg = Default()
	if err := Set(&cfg, "provider.output.slack.max_bytes", "128"); err == nil {
		t.Fatal("expected slack max_bytes validation error")
	}
	cfg = Default()
	if err := Set(&cfg, "provider.output.slack.redaction", "none"); err == nil {
		t.Fatal("expected slack redaction validation error")
	}
}

func TestTerminalDefaultValues(t *testing.T) {
	for _, value := range []string{"auto", "ghostty", "none"} {
		t.Run(value, func(t *testing.T) {
			cfg := Default()
			if err := Set(&cfg, "terminal.default", value); err != nil {
				t.Fatal(err)
			}
			got, err := Get(cfg, "terminal.default")
			if err != nil {
				t.Fatal(err)
			}
			if got != value {
				t.Fatalf("got %s", got)
			}
		})
	}
}

func TestTerminalDefaultRejectsUnsupportedValue(t *testing.T) {
	cfg := Default()
	err := Set(&cfg, "terminal.default", "wezterm")
	if err == nil || !strings.Contains(err.Error(), "terminal.default must be one of auto, ghostty, or none") {
		t.Fatalf("unexpected err: %v", err)
	}
}

func TestLoadRejectsDeprecatedTerminalDefault(t *testing.T) {
	paths := testPaths(t)
	if err := os.WriteFile(paths.Config, []byte("terminal:\n  default: iterm2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := Load(paths)
	if err == nil || !strings.Contains(err.Error(), "no longer supported") {
		t.Fatalf("err = %v", err)
	}
}

func TestUpdateConfigKeysAreRemoved(t *testing.T) {
	cfg := Default()
	err := Set(&cfg, "update.channel", "beta")
	if err == nil || !strings.Contains(err.Error(), "unknown config key") {
		t.Fatalf("unexpected err: %v", err)
	}
}

func TestTransportModeValues(t *testing.T) {
	for _, value := range []string{"lan", "lan-loopback", "tailscale", "tailscale-private", "wireguard", "zerotier", "cloudflare-quick", "cloudflare-named", "ngrok", "auto"} {
		t.Run(value, func(t *testing.T) {
			cfg := Default()
			if err := Set(&cfg, "transport.mode", value); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDeferredProviderTransportRequiresExplicitOptIn(t *testing.T) {
	cfg := Default()
	if err := Set(&cfg, "transport.mode", "telegram"); err == nil || !strings.Contains(err.Error(), "experimental.providers=true") {
		t.Fatalf("unexpected err: %v", err)
	}
	if err := Set(&cfg, "experimental.providers", "true"); err != nil {
		t.Fatal(err)
	}
	if err := Set(&cfg, "transport.mode", "telegram"); err != nil {
		t.Fatal(err)
	}
	if got, err := Get(cfg, "experimental.providers"); err != nil || got != "true" {
		t.Fatalf("experimental.providers = %q, %v", got, err)
	}
}

func TestTransportModeRejectsUnsupportedValue(t *testing.T) {
	cfg := Default()
	err := Set(&cfg, "transport.mode", "satellite")
	if err == nil || !strings.Contains(err.Error(), "transport.mode must be a v1 web transport") {
		t.Fatalf("unexpected err: %v", err)
	}
}

func TestSaveLoadTerminalDefault(t *testing.T) {
	paths := testPaths(t)
	cfg := Default()
	if err := Set(&cfg, "terminal.default", "ghostty"); err != nil {
		t.Fatal(err)
	}
	if err := Save(paths.Config, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, meta, err := Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if !meta.Explicit["terminal.default"] {
		t.Fatalf("explicit map missing terminal.default: %#v", meta.Explicit)
	}
	if loaded.Terminal.Default != "ghostty" {
		t.Fatalf("terminal.default = %s", loaded.Terminal.Default)
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
	if err := Set(&cfg, "shell.default", "/usr/bin/elvish"); err == nil {
		t.Fatal("expected unsupported shell path error")
	}
}
