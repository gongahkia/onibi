package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestLoadAndSaveShellAndLiveness(t *testing.T) {
	paths := Paths{StateDir: t.TempDir(), Config: filepath.Join(t.TempDir(), "config.yaml")}
	cfg, meta, err := loadBytes(paths.Config, []byte("daemon:\n  liveness_interval: 15s\nshell:\n  default: bash\n  login: false\n"), Default(), LoadMeta{Path: paths.Config, Explicit: map[string]bool{}})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Daemon.LivenessInterval.Std() != 15*time.Second || !meta.Explicit["daemon.liveness_interval"] || cfg.Shell.Default != "bash" || cfg.Shell.Login {
		t.Fatalf("config=%#v meta=%#v", cfg, meta)
	}
	if err := Save(paths.Config, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, _, err := Load(paths)
	if err != nil || loaded.Daemon.LivenessInterval.Std() != 15*time.Second {
		t.Fatalf("loaded=%#v err=%v", loaded, err)
	}
}

func TestDaemonLivenessConfig(t *testing.T) {
	cfg := Default()
	if err := Set(&cfg, "daemon.liveness_interval", "15s"); err != nil {
		t.Fatal(err)
	}
	if cfg.Daemon.LivenessInterval.Std() != 15*time.Second {
		t.Fatalf("config=%#v", cfg.Daemon)
	}
	if err := Set(&cfg, "daemon.liveness_interval", "500ms"); err == nil {
		t.Fatal("accepted short liveness interval")
	}
}

func TestLoadIgnoresRetiredAgentSettings(t *testing.T) {
	old := "daemon:\n  approval_timeout: 5m0s\n  claude_question_timeout: 3m0s\n  approval_sweep_interval: 15s\n  max_subscribers: 32\n  liveness_interval: 15s\n"
	assertRetiredSettingsIgnored(t, old, []string{"daemon.approval_timeout", "daemon.claude_question_timeout", "daemon.approval_sweep_interval", "daemon.max_subscribers"})
}

func TestLoadIgnoresRetiredScreenAndUploadSettings(t *testing.T) {
	// Values outside the old validation ranges must not block loading either.
	old := "daemon:\n  output_buffer_bytes: 1\n  liveness_interval: 15s\n  upload_ttl: 1m\n  upload_max_bytes: 3\nscreen:\n  font: custom\n  font_path: /missing/font.ttf\n"
	assertRetiredSettingsIgnored(t, old, []string{"daemon.output_buffer_bytes", "daemon.upload_ttl", "daemon.upload_max_bytes", "screen.font", "screen.font_path"})
}

func assertRetiredSettingsIgnored(t *testing.T, old string, retired []string) {
	t.Helper()
	paths := Paths{StateDir: t.TempDir()}
	paths.Config = filepath.Join(paths.StateDir, "config.yaml")
	if err := os.WriteFile(paths.Config, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, meta, err := Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Daemon.LivenessInterval.Std() != 15*time.Second || !meta.Explicit["daemon.liveness_interval"] {
		t.Fatalf("config=%#v meta=%#v", cfg.Daemon, meta)
	}
	for _, key := range retired {
		if meta.Explicit[key] {
			t.Fatalf("%s still recorded as set", key)
		}
		if _, err := Get(cfg, key); err == nil {
			t.Fatalf("%s still readable", key)
		}
		if err := Set(&cfg, key, "1m"); err == nil {
			t.Fatalf("%s still settable", key)
		}
	}
	for _, info := range Keys(cfg, meta) {
		if slices.Contains(retired, info.Key) {
			t.Fatalf("%s still listed", info.Key)
		}
	}
	if err := Save(paths.Config, cfg); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(paths.Config)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range retired {
		name := key[strings.LastIndex(key, ".")+1:]
		if strings.Contains(string(raw), name+":") {
			t.Fatalf("saved config kept %s:\n%s", key, raw)
		}
	}
}

func TestSetRejectsUnknownKey(t *testing.T) {
	if err := Set(&Config{}, "daemon.unused", "8192"); err == nil {
		t.Fatal("accepted unknown key")
	}
}

func TestResolveForCWDUsesLocalMultiplexerOverride(t *testing.T) {
	state := t.TempDir()
	project := t.TempDir()
	tmuxConfig := filepath.Join(project, "tmux.local.conf")
	if err := os.WriteFile(tmuxConfig, []byte("set -g status off\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, LocalConfigName), []byte("multiplexer:\n  default: zellij\n  tmux:\n    config: tmux.local.conf\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	paths := Paths{StateDir: state, Config: filepath.Join(state, "config.yaml")}
	resolved, err := ResolveForCWD(paths, project)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := resolved.Config.Multiplexer.Default, "zellij"; got != want {
		t.Fatalf("default=%q want=%q", got, want)
	}
	if got, want := resolved.Config.Multiplexer.Tmux.Config, tmuxConfig; got != want {
		t.Fatalf("tmux config=%q want=%q", got, want)
	}
	if !resolved.Local.Exists || resolved.Local.Path != filepath.Join(project, LocalConfigName) {
		t.Fatalf("local meta=%#v", resolved.Local)
	}
}

func TestResolveTmuxConfigPrefersProject(t *testing.T) {
	project := t.TempDir()
	path := filepath.Join(project, ".tmux.conf")
	if err := os.WriteFile(path, []byte("set -g mouse on\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveTmuxConfig(project, "auto")
	if err != nil || got != path {
		t.Fatalf("config=%q err=%v", got, err)
	}
}
