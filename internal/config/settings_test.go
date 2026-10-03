package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadAndSaveOutputBuffer(t *testing.T) {
	paths := Paths{StateDir: t.TempDir(), Config: filepath.Join(t.TempDir(), "config.yaml")}
	cfg, meta, err := loadBytes(paths.Config, []byte("daemon:\n  output_buffer_bytes: 8192\nshell:\n  default: bash\n  login: false\n"), Default(), LoadMeta{Path: paths.Config, Explicit: map[string]bool{}})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Daemon.OutputBufferBytes != 8192 || !meta.Explicit["daemon.output_buffer_bytes"] || cfg.Shell.Default != "bash" || cfg.Shell.Login {
		t.Fatalf("config=%#v meta=%#v", cfg, meta)
	}
	if err := Save(paths.Config, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, _, err := Load(paths)
	if err != nil || loaded.Daemon.OutputBufferBytes != 8192 {
		t.Fatalf("loaded=%#v err=%v", loaded, err)
	}
}

func TestDaemonLivenessAndUploadConfig(t *testing.T) {
	cfg := Default()
	for key, value := range map[string]string{"daemon.liveness_interval": "15s", "daemon.claude_question_timeout": "4m", "daemon.upload_ttl": "48h", "daemon.upload_max_bytes": "3145728"} {
		if err := Set(&cfg, key, value); err != nil {
			t.Fatalf("%s: %v", key, err)
		}
	}
	if cfg.Daemon.LivenessInterval.Std() != 15*time.Second || cfg.Daemon.ClaudeQuestionTimeout.Std() != 4*time.Minute || cfg.Daemon.UploadTTL.Std() != 48*time.Hour || cfg.Daemon.UploadMaxBytes != 3<<20 {
		t.Fatalf("config=%#v", cfg.Daemon)
	}
	if err := Set(&cfg, "daemon.upload_ttl", "30m"); err == nil {
		t.Fatal("accepted short upload ttl")
	}
	if err := Set(&cfg, "daemon.claude_question_timeout", "10s"); err == nil {
		t.Fatal("accepted short question timeout")
	}
}

func TestSetRejectsUnknownKey(t *testing.T) {
	if err := Set(&Config{}, "daemon.unused", "8192"); err == nil {
		t.Fatal("accepted unknown key")
	}
}

func TestScreenFontConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "font.ttf")
	if err := os.WriteFile(path, []byte("font"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := Default()
	if err := Set(&cfg, "screen.font", "caskaydia-cove-nerd"); err != nil {
		t.Fatal(err)
	}
	if cfg.Screen.Font != "caskaydia-cove-nerd" {
		t.Fatalf("font=%q", cfg.Screen.Font)
	}
	if err := Set(&cfg, "screen.font_path", path); err != nil {
		t.Fatal(err)
	}
	if err := Set(&cfg, "screen.font", "custom"); err != nil {
		t.Fatal(err)
	}
	if err := Set(&cfg, "screen.font", "unknown"); err == nil {
		t.Fatal("accepted unknown font")
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
