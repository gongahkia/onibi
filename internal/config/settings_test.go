package config

import (
	"os"
	"path/filepath"
	"testing"
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
